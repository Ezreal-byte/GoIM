package core

import (
	"errors"
	"goim/internal/filemove"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestReceivePublicationRecoversBothSidesOfTheMove(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		directory bool
		moved     bool
	}{
		{name: "staged file"},
		{name: "published file", moved: true},
		{name: "staged directory", directory: true},
		{name: "published directory", directory: true, moved: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			n := testNodeAt(t, "receiver", dir)
			id, publication := fixtureReceivePublication(t, n, scenario.directory)
			if scenario.moved {
				if err := filemove.Exclusive(publication.Stage, publication.Destination); err != nil {
					t.Fatal(err)
				}
				if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: id}); err != nil {
					t.Fatal(err)
				}
				if n.Snapshot().Transfers[0].Status == "cancelled" {
					t.Fatal("cancel changed the outcome of an already published receive")
				}
			} else if _, err := os.Stat(publication.Destination); !os.IsNotExist(err) {
				t.Fatalf("staging exposed the destination before publication: %v", err)
			}
			if err := n.Close(); err != nil {
				t.Fatal(err)
			}
			n = testNodeAt(t, "receiver", dir)
			eventually(t, "receive publication recovered with the sender offline", func() bool {
				return n.Snapshot().Transfers[0].Status == "completed"
			})
			path := publication.Destination
			if scenario.directory {
				path = filepath.Join(path, "nested", "file.txt")
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "complete contents" {
				t.Fatalf("published payload = %q, %v", data, err)
			}
			if _, exists := n.receivePublication(id); exists {
				t.Fatal("completion retained a pending publication")
			}
		})
	}
}

func TestCancelBeforeReceivePublicationKeepsDestinationAbsent(t *testing.T) {
	n := testNode(t, "receiver")
	id, publication := fixtureReceivePublication(t, n, true)
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: id}); err != nil {
		t.Fatal(err)
	}
	_ = n.recoverReceivePublication(t.Context(), id, publication)
	for _, path := range []string{publication.Stage, publication.Destination} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancelled receive left %s: %v", path, err)
		}
	}
	if task := n.Snapshot().Transfers[0]; task.Status != "cancelled" {
		t.Fatalf("cancelled receive became %s", task.Status)
	}
}

func TestDirectoryPublicationPreservesExistingFolder(t *testing.T) {
	n := testNode(t, "receiver")
	id, publication := fixtureReceivePublication(t, n, true)
	if err := os.Mkdir(publication.Destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := n.recoverReceivePublication(t.Context(), id, publication); err == nil {
		t.Fatal("publishing replaced an existing directory")
	}
	if entries, err := os.ReadDir(publication.Destination); err != nil || len(entries) != 0 {
		t.Fatalf("the existing directory changed: %v, %v", entries, err)
	}
}

func fixtureReceivePublication(t *testing.T, n *Node, directory bool) (string, receivePublication) {
	t.Helper()
	id, parent, contents := newID(), t.TempDir(), "complete contents"
	destination := filepath.Join(parent, "received")
	publication := receivePublication{Destination: destination, Directory: directory}
	var err error
	if err := n.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{ID: id, FileID: id, Direction: "receive", PeerID: "offline", Path: destination, Size: int64(len(contents)), Status: "verifying"})
	}); err != nil {
		t.Fatal(err)
	}
	ctx, finish, ok := n.transferContext(id)
	if !ok {
		t.Fatal("could not start receive")
	}
	defer finish()
	if directory {
		publication.Stage, err = os.MkdirTemp(parent, directoryStagePrefix+id+"-")
		if err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(publication.Stage, "nested")
		if err := os.Mkdir(nested, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nested, "file.txt"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		publication.Digest, publication.Size, err = receiveDirectoryDigest(t.Context(), publication.Stage)
	} else {
		publication, err = n.stageReceiveFile(ctx, id, destination, strings.NewReader(contents), int64(len(contents)), "")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := n.durable(func(p *persisted) []pending {
		p.ReceivePublications = map[string]receivePublication{id: publication}
		p.State.Files = append(p.State.Files, domain.File{ID: id, Name: "received", Folder: directory, Size: publication.Size})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id, publication
}

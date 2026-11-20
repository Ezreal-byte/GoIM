package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestClearInterruptedReceivePartials(t *testing.T) {
	n := testNode(t, "receiver")
	destination := filepath.Join(t.TempDir(), "keep")
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(destination)
	for _, transport := range []string{"", legacyIPMSG, transportNWT} {
		id := newID()
		task := domain.Transfer{ID: id, Direction: "receive", Status: "failed", Done: 4, Path: destination, LegacyTransport: transport}
		part := filepath.Join(n.dir, "partials", id+".part")
		if transport == transportNWT {
			part = n.nativePartialPath(id)
			task.RemoteShareID = "group"
			task.FolderStage = filepath.Join(filepath.Dir(destination), directoryStagePrefix+id+"-part")
			write(filepath.Join(task.FolderStage, "child.txt"))
		}
		write(part)
		if err := n.change(func(s *domain.State) { s.Transfers = append(s.Transfers, task) }); err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		n.transfers[id] = context.CancelFunc(func() {})
		n.mu.Unlock()
		cmd := domain.Command{Kind: domain.ClearTransferPartial, TransferID: id}
		if err := n.execute(cmd); err == nil {
			t.Fatal("cleared a running transfer")
		}
		if _, err := os.Stat(part); err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		delete(n.transfers, id)
		n.mu.Unlock()
		for range 2 {
			if err := n.execute(cmd); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(part); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("partial remained", err)
		}
		if task.FolderStage != "" {
			if _, err := os.Stat(task.FolderStage); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stage remained", err)
			}
		}
		if b, err := os.ReadFile(destination); err != nil || string(b) != "keep" {
			t.Fatal("cleanup affected final destination", err)
		}
		s := n.Snapshot()
		got := s.Transfers[transferIndex(&s, id)]
		if got.Done != 0 || got.Status != "failed" || got.FolderStage != "" {
			t.Fatalf("invalid post-cleanup state: %+v", got)
		}
	}
}
func TestClearPartialPreservesAliasedDownloadDestination(t *testing.T) {
	n := testNode(t, "receiver")
	id := newID()
	part := filepath.Join(n.dir, "partials", id+".part")
	if err := os.MkdirAll(filepath.Dir(part), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, []byte("download"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(n.dir, "destination")
	if err := os.Link(part, target); err != nil {
		t.Fatal(err)
	}
	if err := n.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{ID: id, Status: "failed", Direction: "receive", Path: target, Done: 8})
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.clearTransferPartial(id); err == nil {
		t.Fatal("cleared an aliased destination")
	}
	if content, err := os.ReadFile(part); err != nil || string(content) != "download" {
		t.Fatal("lost transfer data", err)
	}
}

func TestCompatibilityTransferRate(t *testing.T) {
	n := testNode(t, "sender")
	start := time.Now().Add(-time.Second)
	if err := n.change(func(s *domain.State) {
		s.Transfers = []domain.Transfer{
			{ID: "ipmsg", Direction: "send", Status: "transferring", LegacyTransport: legacyIPMSG, RemotePacket: "a", RemoteFile: "1", Size: 4096, Done: 1024, UpdatedAt: start},
			{ID: "nwt", Direction: "send", Status: "transferring", LegacyTransport: transportNWT, RemotePacket: "11", RemotePeerID: "remote", Size: 4096, Done: 1024, UpdatedAt: start},
		}
	}); err != nil {
		t.Fatal(err)
	}
	n.receiveLegacyProgress(10, 1, 2048, 4096, false, nil)
	n.receiveNeiwangtongProgress("remote", 11, 2048, 4096, false, nil)
	for _, task := range n.Snapshot().Transfers {
		if task.Speed <= 0 || task.Speed > 1100 {
			t.Fatalf("%s rate = %f", task.ID, task.Speed)
		}
	}
	before := n.Snapshot().Transfers[1]
	for _, offset := range []int64{1024, 2048} {
		n.receiveNeiwangtongProgress("remote", 11, offset, 4096, false, nil)
		after := n.Snapshot().Transfers[1]
		if after.Done != before.Done || after.Speed != before.Speed || !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("duplicate acknowledgement changed the last speed sample: before=%+v after=%+v", before, after)
		}
	}
	n.receiveLegacyProgress(10, 1, 4096, 4096, true, nil)
	n.receiveNeiwangtongProgress("remote", 11, 4096, 4096, true, nil)
	for _, task := range n.Snapshot().Transfers {
		if task.Speed != 0 || task.Status != "completed" {
			t.Fatalf("stale speed: %+v", task)
		}
	}
}

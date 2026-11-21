package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestCancelDiscardsPartialAfterReceiveWorkerExits(t *testing.T) {
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
		_, finish, ok := n.transferContext(id)
		if !ok {
			t.Fatal("could not start receive")
		}
		cmd := domain.Command{Kind: domain.CancelTransfer, TransferID: id}
		if err := n.execute(cmd); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(part); err != nil {
			t.Fatal(err)
		}
		finish()
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
		if got.Done != 0 || got.Status != "cancelled" || got.FolderStage != "" {
			t.Fatalf("invalid post-cleanup state: %+v", got)
		}
	}
}

func TestCancelPartialPreservesAliasedDownloadDestination(t *testing.T) {
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
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: id}); err != nil {
		t.Fatal(err)
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

func TestPartialRecoveryKeepsResumablePrefixes(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "receiver", dir)
	partialDir := filepath.Join(dir, "partials")
	if err := os.MkdirAll(partialDir, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		status, problem string
		remove          bool
	}{
		{"failed", "network interrupted", false}, {"waiting", "", false}, {"failed", errInvalidPrefix.Error(), true},
		{"cancelled", "", true}, {"rejected", "", true}, {"completed", "", true},
	}
	ids := make([]string, len(cases))
	for i, c := range cases {
		id := newID()
		ids[i] = id
		if err := os.WriteFile(filepath.Join(partialDir, id+".part"), []byte("prefix"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := n.change(func(s *domain.State) {
			s.Transfers = append(s.Transfers, domain.Transfer{ID: id, Direction: "receive", Status: c.status, Done: 6, Error: c.problem})
		}); err != nil {
			t.Fatal(err)
		}
	}
	// This snapshot uses the pre-cleanup store format.
	n.mu.Lock()
	n.data.PartialCleanup = nil
	n.mu.Unlock()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "receiver", dir)
	state := n.Snapshot()
	for i, c := range cases {
		data, err := os.ReadFile(filepath.Join(partialDir, ids[i]+".part"))
		row := state.Transfers[transferIndex(&state, ids[i])]
		if c.remove {
			wantDone := int64(0)
			if c.status == "completed" {
				wantDone = 6
			}
			if !errors.Is(err, os.ErrNotExist) || row.Done != wantDone {
				t.Fatalf("%s %s: stale prefix or progress", c.status, c.problem)
			}
		} else if err != nil || string(data) != "prefix" || row.Done != 6 {
			t.Fatalf("%s should resume: %v %+v", c.status, err, row)
		}
	}
}

func TestManualContactRefreshCompletesOnce(t *testing.T) {
	n := testNode(t, "refresh")
	for range 2 {
		if err := n.execute(domain.Command{Kind: domain.RefreshContacts}); err != nil {
			t.Fatal(err)
		}
	}
	if s := n.Snapshot(); !s.ContactRefresh.Pending || s.ContactRefresh.Sequence != 1 {
		t.Fatalf("manual refresh was not coalesced: %d", s.ContactRefresh.Sequence)
	}
	eventually(t, "manual discovery round completed", func() bool { s := n.Snapshot(); return !s.ContactRefresh.Pending && s.ContactRefresh.Sequence == 2 })
}

func TestInterruptedReceiveScratchRecovery(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "receiver", dir)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	db, stored, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	var stages []string
	for _, directory := range []bool{false, true} {
		id := newID()
		prefix := ".goim-receive-"
		if directory {
			prefix = directoryStagePrefix
		}
		stage := filepath.Join(parent, prefix+id+"-part")
		if directory {
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(stage, []byte("incomplete copy"), 0600); err != nil {
			t.Fatal(err)
		}
		stages = append(stages, stage)
		// Model a process exit during the destination copy or a direct native folder receive.
		stored.rememberReceiveScratch(id, receiveScratch{Stage: stage, Destination: filepath.Join(parent, "final"), Directory: directory})
	}
	if err := os.WriteFile(filepath.Join(parent, "final"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(saveStore(db, &stored), db.Close()); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "receiver", dir)
	for _, stage := range stages {
		if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale stage %q: %v", stage, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(parent, "final")); err != nil || string(b) != "keep" {
		t.Fatal("cleanup affected the saved file", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.data.ReceiveScratch) != 0 {
		t.Fatal("orphaned scratch records")
	}
}

func TestTitleGitHubSettingSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	n := testNodeAt(t, "receiver", dir)
	if n.Snapshot().Settings.HideTitleGitHub {
		t.Fatal("hidden by default")
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "show_title_github", Bool: false}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "receiver", dir)
	if !n.Snapshot().Settings.HideTitleGitHub {
		t.Fatal("choice lost on restart")
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "show_title_github", Bool: true}); err != nil {
		t.Fatal(err)
	}
	if n.Snapshot().Settings.HideTitleGitHub {
		t.Fatal("icon could not be restored")
	}
}

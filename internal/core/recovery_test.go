package core

import (
	"context"
	"fmt"
	"goim/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineOutboxReplaysInCommittedMessageOrder(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	peer := b.Snapshot().Self
	_ = a.change(func(s *domain.State) { s.Peers = append(s.Peers, peer); ensureDirect(s, peer) })
	conv := directID(a.Snapshot().Self.ID, peer.ID)
	for i := 0; i < 12; i++ {
		if err := a.sendText(conv, fmt.Sprintf("ordered-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	connectNodes(t, a, b)
	eventually(t, "ordered replay", func() bool { return len(b.Snapshot().Messages) == 12 })
	for i, m := range b.Snapshot().Messages {
		if m.Text != fmt.Sprintf("ordered-%02d", i) {
			t.Fatalf("message %d arrived as %q", i, m.Text)
		}
	}
}
func TestRestartFinalizesInstalledFileWithSenderOffline(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "installed.txt")
	content := []byte("verified local copy")
	_ = os.WriteFile(path, content, 0600)
	if err := a.sendFile(direct(t, a, b), path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	dest := filepath.Join(b.dir, "installed.txt")
	_ = os.WriteFile(dest, content, 0600)
	_ = b.change(func(s *domain.State) { s.Transfers[0].Path = dest; s.Transfers[0].Status = "verifying" })
	dir := b.dir
	_ = b.Close()
	_ = a.Close()
	restarted := testNodeAt(t, "乙", dir)
	eventually(t, "offline installed recovery", func() bool { return restarted.Snapshot().Transfers[0].Status == "completed" })
	if restarted.Snapshot().Peer(a.Snapshot().Self.ID).Online {
		t.Fatal("test unexpectedly relied on sender online")
	}
}

func TestReceivePersistenceFailureDoesNotAdvanceSeen(t *testing.T) {
	n := testNode(t, "我")
	_ = n.change(func(s *domain.State) { s.Peers = append(s.Peers, domain.Peer{ID: "peer", Name: "同事"}) })
	_ = n.db.Close()
	e := envelope{ID: newID(), Type: "message", Message: domain.Message{ID: newID(), SenderID: "peer", Kind: "text", Text: "必须落盘后确认"}}
	if err := n.receive("peer", e); err == nil {
		t.Fatal("receive hid persistence failure")
	}
	if len(n.Snapshot().Messages) != 0 {
		t.Fatal("uncommitted message leaked")
	}
	n.mu.Lock()
	seen := n.data.Seen["peer:"+e.ID]
	n.mu.Unlock()
	if seen {
		t.Fatal("failed message was marked acknowledged")
	}
}
func TestAckPersistenceFailureRetainsPendingDelivery(t *testing.T) {
	n := testNode(t, "我")
	_ = n.change(func(s *domain.State) {
		s.Peers = append(s.Peers, domain.Peer{ID: "peer", Name: "同事"})
		ensureDirect(s, s.Peer("peer"))
	})
	conv := directID(n.Snapshot().Self.ID, "peer")
	if err := n.sendText(conv, "离线排队"); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	id := ""
	for k := range n.data.Outbox {
		id = k
	}
	n.mu.Unlock()
	if id == "" {
		t.Fatal("missing durable outbox")
	}
	_ = n.db.Close()
	if err := n.receive("peer", envelope{Type: "ack", ID: id}); err == nil {
		t.Fatal("ack hid disk failure")
	}
	n.mu.Lock()
	_, retained := n.data.Outbox[id]
	n.mu.Unlock()
	if !retained || n.Snapshot().Messages[0].Status == "delivered" {
		t.Fatal("failed ack lost retry checkpoint")
	}
}
func TestAtomicFileInstallRefusesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src, dest := filepath.Join(dir, "part"), filepath.Join(dir, "destination")
	_ = os.WriteFile(src, []byte("incoming"), 0600)
	_ = os.WriteFile(dest, []byte("keep existing"), 0600)
	if err := installFile(src, dest); err == nil {
		t.Fatal("overwrote existing file")
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "keep existing" {
		t.Fatal("existing data changed")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("failed install removed resumable partial")
	}
}
func TestReceiveRecoversAfterInstalledFileBeforeCompletionCommit(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "恢复.txt")
	content := []byte("already installed before process exit")
	_ = os.WriteFile(path, content, 0600)
	if err := a.sendFile(direct(t, a, b), path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file offered", func() bool { return len(b.Snapshot().Transfers) == 1 })
	transfer := b.Snapshot().Transfers[0]
	dest := filepath.Join(t.TempDir(), "恢复.txt")
	_ = os.WriteFile(dest, content, 0600)
	_ = b.change(func(s *domain.State) {
		i := transferIndex(s, transfer.ID)
		s.Transfers[i].Path = dest
		s.Transfers[i].Status = "waiting"
	})
	if err := b.download(context.Background(), transfer.ID, ""); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	if s.Transfers[0].Status != "completed" || s.Transfers[0].Path != dest {
		t.Fatal("installed file recovery failed")
	}
	eventually(t, "sender completion ack", func() bool { return a.Snapshot().Transfers[0].Status == "completed" })
}
func TestChangedSourceFailsWithoutInstallingDestination(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "变化.txt")
	_ = os.WriteFile(path, []byte("original"), 0600)
	if err := a.sendFile(direct(t, a, b), path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	_ = os.WriteFile(path, []byte("modified"), 0600)
	b.startReceive(b.Snapshot().Transfers[0].ID, "")
	eventually(t, "changed source failure", func() bool { return b.Snapshot().Transfers[0].Status == "failed" })
	if _, err := os.Stat(filepath.Join(b.Snapshot().Settings.DownloadDir, "变化.txt")); !os.IsNotExist(err) {
		t.Fatal("changed source installed as success")
	}
}

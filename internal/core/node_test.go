package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goim/internal/domain"
)

func testNode(t *testing.T, name string) *Node { t.Helper(); return testNodeAt(t, name, t.TempDir()) }
func testNodeAt(t *testing.T, name, dir string) *Node {
	t.Helper()
	n, err := New(Options{DataDir: dir, Listen: "127.0.0.1:0", Name: name, DisableDiscovery: true, DisableIPMSG: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	n.change(func(s *domain.State) { s.Settings.DownloadDir = filepath.Join(dir, "downloads") })
	return n
}
func eventually(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out: " + what)
}
func connectNodes(t *testing.T, a, b *Node) {
	t.Helper()
	a.Connect(b.Address())
	eventually(t, "TLS peers connected", func() bool {
		return a.Snapshot().Peer(b.Snapshot().Self.ID).Online && b.Snapshot().Peer(a.Snapshot().Self.ID).Online
	})
}
func direct(t *testing.T, a, b *Node) string {
	t.Helper()
	id := b.Snapshot().Self.ID
	if err := a.execute(domain.Command{Kind: domain.StartChat, PeerID: id}); err != nil {
		t.Fatal(err)
	}
	return directID(a.Snapshot().Self.ID, id)
}
func TestRealTLSMessagesDeduplicateAndSurviveRestart(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	if err := a.sendText(conv, "你好，局域网 👋"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "message persisted at receiver", func() bool { return len(b.Snapshot().Messages) == 1 })
	eventually(t, "durable acknowledgement", func() bool { return a.Snapshot().Messages[0].Status == "delivered" })
	m := a.Snapshot().Messages[0]
	a.reliable(b.Snapshot().Self.ID, envelope{Type: "message", ID: newID(), Message: m})
	time.Sleep(100 * time.Millisecond)
	if len(b.Snapshot().Messages) != 1 {
		t.Fatal("duplicate message inserted")
	}
	dir := b.dir
	oldID := b.Snapshot().Self.ID
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offline detected", func() bool { return !a.Snapshot().Peer(oldID).Online })
	if err := a.sendText(conv, "重连后补发"); err != nil {
		t.Fatal(err)
	}
	b2 := testNodeAt(t, "乙", dir)
	if b2.Snapshot().Self.ID != oldID {
		t.Fatal("identity changed across restart")
	}
	connectNodes(t, a, b2)
	eventually(t, "outbox replay", func() bool { return len(b2.Snapshot().Messages) == 2 })
}
func TestRealFileTransferResumesAndVerifies(t *testing.T) {
	a, b := testNode(t, "发送者"), testNode(t, "接收者")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	content := bytes.Repeat([]byte("GoIM真实分块文件\n"), 20000)
	source := filepath.Join(t.TempDir(), "资料.bin")
	if err := os.WriteFile(source, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.sendFile(conv, source, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "file offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	task := b.Snapshot().Transfers[0]
	partialDir := filepath.Join(b.dir, "partials")
	_ = os.MkdirAll(partialDir, 0700)
	offset := len(content) / 3
	if err := os.WriteFile(filepath.Join(partialDir, task.ID+".part"), content[:offset], 0600); err != nil {
		t.Fatal(err)
	}
	b.startReceive(task.ID, "")
	eventually(t, "download verified", func() bool { return b.Snapshot().Transfers[0].Status == "completed" })
	actual, err := os.ReadFile(b.Snapshot().Transfers[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, content) {
		t.Fatal("transferred bytes differ")
	}
	eventually(t, "sender receives completion", func() bool { return a.Snapshot().Transfers[0].Status == "completed" })
}
func createTestGroup(t *testing.T, a, b, c *Node) string {
	t.Helper()
	connectNodes(t, a, b)
	connectNodes(t, a, c)
	if err := a.createGroup("产品讨论组", []string{b.Snapshot().Self.ID, c.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []*Node{b, c} {
		eventually(t, "group invitation", func() bool { return len(n.Snapshot().Invitations) > 0 })
		if err := n.answerInvite(n.Snapshot().Invitations[0].ID, true); err != nil {
			t.Fatal(err)
		}
	}
	id := a.Snapshot().Groups[0].ID
	for _, n := range []*Node{a, b, c} {
		eventually(t, "three accepted members", func() bool { return len(n.Snapshot().Group(id).Members) == 3 })
	}
	eventually(t, "member mesh", func() bool {
		return b.Snapshot().Peer(c.Snapshot().Self.ID).Online && c.Snapshot().Peer(b.Snapshot().Self.ID).Online
	})
	return id
}
func TestGroupWorksWithoutOwnerAndEnforcesPermissions(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "林晓"), testNode(t, "许知")
	id := createTestGroup(t, a, b, c)
	if err := b.manageGroup(domain.Command{Kind: domain.RenameGroup, GroupID: id, Name: "越权"}); err == nil {
		t.Fatal("non-owner renamed group")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.sendText("group:"+id, "群主离线后继续聊天"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "group message without owner", func() bool {
		for _, m := range c.Snapshot().Messages {
			if m.Text == "群主离线后继续聊天" {
				return true
			}
		}
		return false
	})
}
func TestOwnershipTransferCertificateAndRemoval(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "林晓"), testNode(t, "许知")
	id := createTestGroup(t, a, b, c)
	if err := a.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: b.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "ownership invitation", func() bool {
		for _, v := range b.Snapshot().Invitations {
			if v.Kind == "ownership" {
				return true
			}
		}
		return false
	})
	for _, v := range b.Snapshot().Invitations {
		if v.Kind == "ownership" {
			if err := b.answerInvite(v.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, n := range []*Node{a, b, c} {
		eventually(t, "new owner installed", func() bool { return n.Snapshot().Group(id).OwnerID == b.Snapshot().Self.ID })
	}
	if !verifyGroup(c.Snapshot().Group(id)) {
		t.Fatal("transferred group cannot be independently verified")
	}
	if err := b.manageGroup(domain.Command{Kind: domain.RemoveMember, GroupID: id, PeerID: c.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "removed member notified", func() bool { return !contains(c.Snapshot().Group(id).Members, c.Snapshot().Self.ID) })
	if err := c.sendText("group:"+id, "越权发送"); err == nil {
		t.Fatal("removed member may send")
	}
}
func TestRejectAndReadOnlyShare(t *testing.T) {
	a, b := testNode(t, "分享者"), testNode(t, "访客")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "分享.txt")
	_ = os.WriteFile(path, []byte("仅此文件"), 0600)
	if err := a.sendFile("", path, true, ""); err != nil {
		t.Fatal(err)
	}
	a.broadcastCatalog()
	eventually(t, "share catalog", func() bool { return len(b.Snapshot().Files) > 0 })
	f := b.Snapshot().Files[0]
	if f.SourcePath != "" {
		t.Fatal("sender local path leaked")
	}
	if err := b.execute(domain.Command{Kind: domain.GetShared, FileID: f.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "share offer", func() bool { return len(b.Snapshot().Transfers) > 0 })
	if err := b.execute(domain.Command{Kind: domain.RejectTransfer, TransferID: b.Snapshot().Transfers[0].ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "rejection reaches sender", func() bool { return len(a.Snapshot().Transfers) > 0 && a.Snapshot().Transfers[0].Status == "rejected" })
}

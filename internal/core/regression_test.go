package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"goim/internal/domain"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsharingPreservesDownloadedMetadata(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "文件.txt")
	_ = os.WriteFile(path, []byte("保留文件"), 0600)
	if err := a.sendFile("", path, true, ""); err != nil {
		t.Fatal(err)
	}
	a.broadcastCatalog()
	eventually(t, "catalog", func() bool { return len(b.Snapshot().Files) > 0 })
	f := b.Snapshot().Files[0]
	_ = b.execute(domain.Command{Kind: domain.GetShared, FileID: f.ID})
	eventually(t, "offer", func() bool { return len(b.Snapshot().Transfers) > 0 })
	b.startReceive(b.Snapshot().Transfers[0].ID, "")
	eventually(t, "download", func() bool { return b.Snapshot().Transfers[0].Status == "completed" })
	_ = a.execute(domain.Command{Kind: domain.RemoveShare, FileID: f.ID})
	eventually(t, "unshare", func() bool { return !b.Snapshot().File(f.ID).Shared })
	if b.Snapshot().File(f.ID).ID == "" {
		t.Fatal("downloaded file metadata removed")
	}
	if _, err := os.Stat(b.Snapshot().Transfers[0].Path); err != nil {
		t.Fatal(err)
	}
}
func TestRejectedTransferCanBeRequestedAgain(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	path := filepath.Join(t.TempDir(), "重试.txt")
	_ = os.WriteFile(path, []byte("重新接收"), 0600)
	if err := a.sendFile(conv, path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	old := b.Snapshot().Transfers[0]
	_ = b.execute(domain.Command{Kind: domain.RejectTransfer, TransferID: old.ID})
	eventually(t, "rejected", func() bool { return a.Snapshot().Transfers[0].Status == "rejected" })
	if err := b.execute(domain.Command{Kind: domain.RetryTransfer, TransferID: old.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "new authorized transfer completed", func() bool { s := b.Snapshot(); return len(s.Transfers) == 2 && s.Transfers[1].Status == "completed" })
	if b.Snapshot().Transfers[1].ID == old.ID {
		t.Fatal("retry reused terminal transfer identity")
	}
}
func TestImageOfferRequiresDefaultConsentAndIgnoresRemotePath(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	path := filepath.Join(t.TempDir(), "图片.png")
	var imageBytes bytes.Buffer
	_ = png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_ = os.WriteFile(path, imageBytes.Bytes(), 0600)
	if err := a.sendFile(conv, path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "image offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	if b.Snapshot().Transfers[0].Status != "offered" {
		t.Fatal("image bypassed default confirmation")
	}
	f := a.Snapshot().Files[0]
	f.ID = newID()
	f.SourcePath = "C:/private/secret.png"
	if err := b.receiveOffer(a.Snapshot().Self.ID, envelope{File: f, TransferID: newID()}); err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().File(f.ID).SourcePath != "" {
		t.Fatal("untrusted remote local path retained")
	}
}
func TestRemoteCompletionCannotCompleteReceiveTask(t *testing.T) {
	n := testNode(t, "我")
	n.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{ID: "t", PeerID: "peer", Direction: "receive", Status: "offered", Size: 100})
	})
	if err := n.receiveFileStatus("peer", envelope{TransferID: "t", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if n.Snapshot().Transfers[0].Status != "offered" {
		t.Fatal("peer forged local download completion")
	}
}
func TestAtomicFailureRollsBackStateAndOutbox(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	conv := direct(t, a, b)
	_ = a.db.Close()
	if err := a.sendText(conv, "不能丢失发送队列"); err == nil {
		t.Fatal("expected disk failure")
	}
	if len(a.Snapshot().Messages) != 0 {
		t.Fatal("uncommitted message leaked into visible state")
	}
	a.mu.Lock()
	count := len(a.data.Outbox)
	a.mu.Unlock()
	if count != 0 {
		t.Fatal("rollback retained outbox map additions")
	}
}
func TestOwnershipOfferCannotRedirectOrRaceAdministrativeChanges(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	if err := a.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: b.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: c.Snapshot().Self.ID}); err == nil {
		t.Fatal("concurrent ownership transfer accepted")
	}
	if err := a.manageGroup(domain.Command{Kind: domain.RenameGroup, GroupID: id, Name: "竞态"}); err == nil {
		t.Fatal("group mutated during ownership handoff")
	}
	var offer domain.Invitation
	for _, v := range a.Snapshot().Invitations {
		if v.Kind == "ownership" {
			offer = v
		}
	}
	offer.GroupID = "another-group"
	_ = a.receiveGroup(b.Snapshot().Self.ID, envelope{Type: "ownership_accept", Invitation: offer, Status: "accepted"})
	if a.Snapshot().Group(id).OwnerID != a.Snapshot().Self.ID {
		t.Fatal("tampered ownership response changed owner")
	}
}
func TestRevocationCancelsGroupTransferContexts(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	ctx, finish, ok := a.transferContext("active-group-task")
	if !ok {
		t.Fatal("context unavailable")
	}
	defer finish()
	a.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{ID: "active-group-task", ConversationID: "group:" + id, PeerID: c.Snapshot().Self.ID, Direction: "send", Status: "transferring"})
	})
	if err := a.manageGroup(domain.Command{Kind: domain.RemoveMember, GroupID: id, PeerID: c.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("removed member's file I/O was not cancelled")
	}
}
func TestExpectedPeerIdentityIsPinnedWhenDialing(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	_, wrong, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.dialTo(b.Address(), keyID(wrong.Public().(ed25519.PublicKey)), true)
	a.mu.Lock()
	a.mu.Unlock()
	eventually(t, "mismatched dial finished", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.dialing[b.Address()] })
	if len(a.Snapshot().Peers) > 0 {
		t.Fatal("unexpected endpoint added as signed-discovery contact")
	}
}
func TestAvatarAndNicknamePropagation(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	picture := image.NewRGBA(image.Rect(0, 0, 10, 10))
	picture.Set(5, 5, color.RGBA{255, 0, 0, 255})
	var out bytes.Buffer
	_ = png.Encode(&out, picture)
	path := filepath.Join(t.TempDir(), "头像.png")
	_ = os.WriteFile(path, out.Bytes(), 0600)
	if err := a.setProfile("新昵称", path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "profile update", func() bool {
		p := b.Snapshot().Peer(a.Snapshot().Self.ID)
		return p.Name == "新昵称" && p.Avatar != ""
	})
	if b.Snapshot().Peer(a.Snapshot().Self.ID).Avatar == path {
		t.Fatal("remote avatar referenced sender's filesystem")
	}
}
func TestCancelledHashAndWindowsNames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(path, []byte("test"), 0600)
	if _, err := fileHashContext(ctx, path); err == nil {
		t.Fatal("hash ignored cancellation")
	}
	for _, name := range []string{"CON", "NUL.txt", "COM1.log", "LPT9.txt", "name.", "name ", "a\nb"} {
		if validFile(domain.File{ID: strings.Repeat("a", 32), Name: name, Hash: strings.Repeat("b", 64)}) {
			t.Fatalf("reserved name accepted: %s", name)
		}
	}
}

package core

import (
	"goim/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormerOwnerCannotTruncateAdoptedOwnership(t *testing.T) {
	a, b, c := testNode(t, "甲"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	original := a.Snapshot().Group(id)
	if err := a.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: b.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "ownership invite", func() bool {
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
	eventually(t, "adopted ownership", func() bool { return c.Snapshot().Group(id).OwnerID == b.Snapshot().Self.ID })
	original.Epoch = c.Snapshot().Group(id).Epoch + 100
	signGroup(&original, a.key)
	if err := c.receiveGroup(a.Snapshot().Self.ID, envelope{Type: "group_state", Group: original}); err == nil {
		t.Fatal("former owner rolled back proof chain")
	}
	if c.Snapshot().Group(id).OwnerID != b.Snapshot().Self.ID {
		t.Fatal("ownership changed after invalid rollback")
	}
	inv := domain.Invitation{ID: newID(), GroupID: id, FromID: a.Snapshot().Self.ID, ToID: c.Snapshot().Self.ID, Kind: "ownership", Status: "pending"}
	if err := c.receiveGroup(a.Snapshot().Self.ID, envelope{Type: "ownership_offer", Group: original, Invitation: inv}); err == nil {
		t.Fatal("offer path bypassed ownership continuity")
	}
}
func TestAcceptedOwnershipPreventsConcurrentLeave(t *testing.T) {
	a, b, c := testNode(t, "甲"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	_ = b.change(func(s *domain.State) {
		s.Invitations = append(s.Invitations, domain.Invitation{ID: newID(), GroupID: id, FromID: a.Snapshot().Self.ID, Kind: "ownership", Status: "accepted"})
	})
	if err := b.manageGroup(domain.Command{Kind: domain.LeaveGroup, GroupID: id}); err == nil {
		t.Fatal("left during committed ownership acceptance")
	}
	if b.Snapshot().Group(id).Left {
		t.Fatal("group became left during handoff")
	}
}
func TestTerminalStatusBeforeOfferCannotResurrectTransfer(t *testing.T) {
	n := testNode(t, "我")
	peer := "peer"
	tid := newID()
	if err := n.receiveFileStatus(peer, envelope{TransferID: tid, Status: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	f := domain.File{ID: newID(), Name: "cancelled.txt", Size: 1, Hash: strings.Repeat("a", 64), SenderID: peer}
	if err := n.receiveOffer(peer, envelope{TransferID: tid, File: f}); err != nil {
		t.Fatal(err)
	}
	if len(n.Snapshot().Transfers) != 0 || len(n.Snapshot().Messages) != 0 {
		t.Fatal("late offer resurrected a terminal task")
	}
}
func TestSenderCancellationRevokesRecipientRetry(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "撤回.txt")
	_ = os.WriteFile(path, []byte("private"), 0600)
	if err := a.sendFile(direct(t, a, b), path, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	task := a.Snapshot().Transfers[0]
	if err := a.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.receiveFileRetry(b.Snapshot().Self.ID, envelope{TransferID: task.ID, File: domain.File{ID: task.FileID}}); err == nil {
		t.Fatal("recipient bypassed sender revocation")
	}
	if len(a.Snapshot().Transfers) != 1 {
		t.Fatal("revoked retry created a new task")
	}
}
func TestShareRevocationRejectsHistoricalRetry(t *testing.T) {
	a, b := testNode(t, "甲"), testNode(t, "乙")
	connectNodes(t, a, b)
	path := filepath.Join(t.TempDir(), "分享.txt")
	_ = os.WriteFile(path, []byte("shared"), 0600)
	if err := a.sendFile("", path, true, ""); err != nil {
		t.Fatal(err)
	}
	f := a.Snapshot().Files[0]
	if err := a.offerShared(b.Snapshot().Self.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	task := a.Snapshot().Transfers[0]
	_ = a.execute(domain.Command{Kind: domain.RemoveShare, FileID: f.ID})
	if err := a.receiveFileRetry(b.Snapshot().Self.ID, envelope{TransferID: task.ID, File: domain.File{ID: f.ID}}); err == nil {
		t.Fatal("unshared file granted a historical retry")
	}
}

package core

import (
	"goim/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	eventually(t, "sender cancellation received", func() bool { return b.Snapshot().Transfers[0].Status == "cancelled" })
	if err := b.retryTransfer(b.Snapshot().Transfers[0]); err != nil {
		t.Fatal(err)
	}
	eventually(t, "revoked retry terminates", func() bool {
		return b.Snapshot().Transfers[0].Status == "cancelled" && b.Snapshot().Transfers[0].Error != ""
	})
	eventually(t, "retry acknowledged", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, d := range b.data.Outbox {
			if d.Envelope.Type == "file_retry" {
				return false
			}
		}
		return true
	})
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
	if err := a.receiveFileRetry(b.Snapshot().Self.ID, envelope{TransferID: task.ID, File: domain.File{ID: f.ID}}); err != nil {
		t.Fatal(err)
	}
	if len(a.Snapshot().Transfers) != 1 {
		t.Fatal("unshared file granted a new transfer")
	}
}

func TestReplacedOwnershipOfferReconcilesOfflineAcceptedTarget(t *testing.T) {
	a, b, c := testNode(t, "甲"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	if err := a.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: b.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	invID := ""
	eventually(t, "old handoff offer", func() bool {
		for _, v := range b.Snapshot().Invitations {
			if v.Kind == "ownership" {
				invID = v.ID
				return true
			}
		}
		return false
	})
	aDir, bDir := a.dir, b.dir
	_ = a.Close()
	if err := b.answerInvite(invID, true); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	a2 := testNodeAt(t, "甲", aDir)
	connectNodes(t, a2, c)
	_ = a2.durable(func(p *persisted) []pending {
		v := p.OwnerOffers[invID]
		v.CreatedAt = time.Now().Add(-6 * time.Minute)
		p.OwnerOffers[invID] = v
		return nil
	})
	if err := a2.manageGroup(domain.Command{Kind: domain.TransferOwner, GroupID: id, PeerID: c.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	cInv := ""
	eventually(t, "new handoff offer", func() bool {
		for _, v := range c.Snapshot().Invitations {
			if v.Kind == "ownership" {
				cInv = v.ID
				return true
			}
		}
		return false
	})
	if err := c.answerInvite(cInv, false); err != nil {
		t.Fatal(err)
	}
	b2 := testNodeAt(t, "乙", bDir)
	connectNodes(t, a2, b2)
	eventually(t, "old accepted invitation expires", func() bool {
		for _, v := range b2.Snapshot().Invitations {
			if v.ID == invID {
				return v.Status == "expired"
			}
		}
		return false
	})
	if err := b2.manageGroup(domain.Command{Kind: domain.LeaveGroup, GroupID: id}); err != nil {
		t.Fatal(err)
	}
}

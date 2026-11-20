package desktop

import (
	"goim/internal/domain"
	"strings"
	"testing"
)

func TestContactAndChatMenusEditTheSameRemark(t *testing.T) {
	for _, contact := range []bool{true, false} {
		h := newShellTestHarnessAtSize(t, shellTestState(t), 1000, 700)
		peer := h.ui.state.Peers[0]
		if contact {
			h.ui.section = sectionContacts
			h.wake()
			h.rightClickID(h.ui.peerRowIDs[peer.ID])
		} else {
			h.rightClickID(h.ui.convRowIDs["conv-alice"])
		}
		h.settle()
		h.clickID(h.ui.ids.shellMenuRemark)
		if h.ui.organize.mode != domain.SetContactRemark || h.ui.organize.peerID != peer.ID {
			t.Fatal("remark menu did not select its peer")
		}
		h.typeInto(h.ui.ids.organizeField, "同事小王")
		h.clickID(h.ui.ids.organizeSave)
		command := h.lastCommand()
		if command.Kind != domain.SetContactRemark || command.PeerID != peer.ID || command.Name != "同事小王" {
			t.Fatalf("command=%+v", command)
		}
	}
}

func TestLongRemarkAndUsernameFitAtFontExtremes(t *testing.T) {
	for _, font := range []int{9, 14, 28} {
		state := shellTestState(t)
		state.Settings.InterfaceFontSize = font
		state.Peers[0].Remark = strings.Repeat("同事", 20)
		state.Peers[0].Name = strings.Repeat("OriginalUsername", 3)
		h := newShellTestHarnessAtSize(t, state, 1000, 700)
		h.ui.selectConversation("conv-alice")
		h.wake()
		requireShellLayout(t, h.snapshot())
		h.ui.section = sectionContacts
		h.ui.showContact(state.Peers[0].ID)
		h.wake()
		requireShellLayout(t, h.snapshot())
	}
}

func TestRemarkIsPrimaryAndUsernameRemainsSearchable(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].Remark = "同事小王"
	h := newShellTestHarnessAtSize(t, state, 1000, 700)
	peer := h.ui.state.Peers[0]
	conversation := domain.Conversation{Kind: "direct", PeerID: peer.ID, Title: "old name"}
	card := h.ui.conversationCard(conversation)
	if card.title != "同事小王" || card.username != peer.Name {
		t.Fatalf("card=%+v", card)
	}
	for _, query := range []string{peer.Name, peer.Remark} {
		if len(h.ui.filteredPeers([]domain.Peer{peer}, query)) != 1 {
			t.Fatalf("cannot find %q", query)
		}
	}
	h.ui.section = sectionContacts
	h.ui.showContact(peer.ID)
	h.ui.invalidator.Invalidate()
	h.settle()
	if !h.hasText("同事小王") || !h.hasText(peer.Name) {
		t.Fatal("remark or username is missing")
	}
	requireShellLayout(t, h.snapshot())
}

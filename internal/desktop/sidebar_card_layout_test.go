package desktop

import "testing"

func TestSidebarConversationAndContactCardsUseCompactHeights(t *testing.T) {
	state := shellTestState(t)
	state.Invitations = nil
	state.Peers = state.Peers[:1]
	state.Groups[0].Members = []string{"self", "peer-a"}
	state.Groups[0].Pending = nil

	h := newShellTestHarnessAtSize(t, state, 1000, 700)
	conversation, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("conversation card is not on screen")
	}
	if conversation.Bounds.Height != sidebarConversationHeight {
		t.Fatalf("conversation card height = %.1f, want %.1f",
			conversation.Bounds.Height, sidebarConversationHeight)
	}

	h.clickID(h.ui.ids.navContacts)
	h.settle()
	requireShellLayout(t, h.snapshot())

	self, ok := h.findHit(h.ui.ids.selfRow)
	if !ok {
		t.Fatal("self contact card is not on screen")
	}
	peer, ok := h.findHit(h.ui.peerRowIDs["peer-a"])
	if !ok {
		t.Fatal("contact card is not on screen")
	}
	group, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("group contact card is not on screen")
	}
	if self.Bounds.Height != sidebarSelfHeight ||
		peer.Bounds.Height != sidebarContactHeight ||
		group.Bounds.Height != sidebarGroupHeight {
		t.Fatalf("card heights self=%.1f peer=%.1f group=%.1f, want %.1f/%.1f/%.1f",
			self.Bounds.Height, peer.Bounds.Height, group.Bounds.Height,
			sidebarSelfHeight, sidebarContactHeight, sidebarGroupHeight)
	}
}

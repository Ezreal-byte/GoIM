package desktop

import (
	"testing"

	"goim/internal/domain"
)

func TestContactDetailsAndAvatarMenuKeepCurrentChat(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].ClientType = domain.ClientNeiwangtong
	state.Peers[0].Transport = domain.TransportIPMSG
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navContacts)
	before := len(h.backend.commandLog())
	h.clickID(h.ui.peerRowIDs[state.Peers[0].ID])
	if h.ui.section != sectionContacts || !h.hasText("用户类型") ||
		!h.hasText("内网通") || !h.hasText("飞鸽／飞秋兼容") || h.hasHit(h.ui.ids.chatEditor) {
		t.Fatal("contact selection did not display a separate profile and its connection type")
	}
	for _, c := range h.backend.commandLog()[before:] {
		if c.Kind == domain.StartChat || c.Kind == domain.MarkRead {
			t.Fatalf("viewing a profile changed the conversation: %+v", c)
		}
	}
	requireShellLayout(t, h.snapshot())
	h.clickID(h.ui.ids.contactTalk)
	if h.ui.section != sectionMessages || h.ui.selected != "conv-alice" {
		t.Fatalf("send-message action opened %s / %s", h.ui.section, h.ui.selected)
	}
	h.rightClickID(h.ui.ids.chatHeaderAvatar)
	h.clickID(h.ui.ids.shellMenuProfile)
	if h.ui.memberProfile != state.Peers[0].ID || h.ui.selected != "conv-alice" || !h.hasText("用户类型") {
		t.Fatal("avatar menu did not open the shared profile without leaving chat")
	}
	requireShellLayout(t, h.snapshot())
}

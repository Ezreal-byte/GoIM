package desktop

import (
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
)

func TestUnreadTrayPanelSelectsAConversationAndDismissesWithoutReading(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[1].Unread = 3
	state.Peers[0].Remark = "研发同事"
	state.Peers[0].Avatar = writeShellTestPNG(t, "tray-contact.png", 80, 80)
	h := newShellTestHarness(t, state)
	h.waitFor(func() bool { return bitmapReady(h.ui.avatarBitmap(state.Peers[0].Avatar)) })
	r := desktopRuntime{ui: h.ui}
	h.ui.section = sectionSettings
	dismissed := 0
	view := newUnreadTrayPanel(h.ui, h.ui.unreadTrayChats(), r.openUnreadChat, func() { dismissed++ })
	t.Cleanup(view.page.Close)
	session, err := vivaheadless.Open(view.config(app.StatusAnchor{}), nil, vivaheadless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	for _, label := range []string{"产品讨论组", "研发同事", "3", "暂不处理"} {
		if err := session.Expect("text", label); err != nil {
			t.Fatal(err)
		}
	}
	if directory := os.Getenv("GOIM_TRAY_PANEL_QA"); directory != "" {
		shellQAShot(t, session, directory, "unread-tray-panel.png")
	}
	found := false
	for _, hit := range session.Snapshot().Hits {
		if hit.Label == "暂不处理" {
			if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			found = true
			break
		}
	}
	if !found || dismissed != 1 || h.ui.state.Conversations[1].Unread != 3 || h.ui.section != sectionSettings {
		t.Fatal("dismissing the panel must keep the conversation unread")
	}
	shellQAClick(t, session, view.tree.ID(view.rows, "conv-alice"))
	if h.ui.section != sectionMessages || h.ui.selected != "conv-alice" {
		t.Fatalf("tray selected %q in %q", h.ui.selected, h.ui.section)
	}
}

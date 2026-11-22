package desktop

import (
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"

	"goim/internal/domain"
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
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
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

func TestUnreadTrayPanelSingleConversationFitsAt200Percent(t *testing.T) {
	state := shellTestState(t)
	state.Conversations[0].Unread = 0
	state.Conversations[1].Unread = 1
	h := newShellTestHarness(t, state)
	view := newUnreadTrayPanel(h.ui, h.ui.unreadTrayChats(), func(string) {}, func() {})
	t.Cleanup(view.page.Close)
	config := view.config(app.StatusAnchor{})
	if config.Width > 184 || config.Height > 68 {
		t.Fatalf("single-conversation popup is too large: %d × %d", config.Width, config.Height)
	}
	session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("GOIM_TRAY_PANEL_QA"); directory != "" {
		shellQAShot(t, session, directory, "unread-tray-single.png")
	}
}

func TestUnreadTrayPanelFitsLargestInterfaceText(t *testing.T) {
	state := shellTestState(t)
	state.Settings.InterfaceFontSize = domain.InterfaceFontSizeMaximum
	state.Conversations[1].Unread = 3
	h := newShellTestHarness(t, state)
	view := newUnreadTrayPanel(h.ui, h.ui.unreadTrayChats(), func(string) {}, func() {})
	t.Cleanup(view.page.Close)
	session, err := vivaheadless.Open(view.config(app.StatusAnchor{}), nil, vivaheadless.Options{Scale: 1.75})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
}

func TestUnreadTrayReminderDismissesUntilAnotherMessage(t *testing.T) {
	state := shellTestState(t)
	state.Settings.Shortcuts = nil
	state.Settings.Sound, state.Settings.DesktopNotifications = false, false
	state.Conversations[1].Unread = 1
	h := newShellTestHarness(t, state)
	host := &trayReminderTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		window, err := application.OpenWindow(app.Config{}, nil)
		if err != nil {
			return err
		}
		defer window.Close()
		window.Hide()
		normal := app.StatusIcon{Size: 1, RGBA: []byte{0, 0, 0, 255}}
		tray, err := application.AddStatusItem(app.StatusItemConfig{Icon: normal})
		if err != nil {
			return err
		}
		r := desktopRuntime{
			ui: h.ui, backend: h.ui.backend, application: application, window: window, tray: tray,
			post: func(func()) {}, seen: make(map[string]bool), heardTransfers: make(map[string]bool),
			trayNormal: normal, trayFlash: attentionIcon(normal),
		}
		defer r.stopAttention()
		for _, message := range h.ui.state.Messages {
			r.seen[message.ID] = true
		}
		arrive := func(id string) {
			c := &h.ui.state.Conversations[1]
			c.Unread++
			h.ui.state.Messages = append(h.ui.state.Messages, domain.Message{
				ID: id, ConversationID: c.ID, SenderID: c.PeerID, Kind: "text",
			})
			h.ui.state.Revision++
			r.observe()
			r.tickAttention(time.Now())
		}

		arrive("first-new")
		if host.tray.open == nil || !r.flashOn {
			t.Fatal("incoming message did not enable hover and flashing")
		}
		if host.tray.open(app.StatusAnchor{}) == nil || r.trayPanel == nil {
			t.Fatal("enabled hover did not open the unread panel")
		}
		r.trayPanel.dismiss()
		if r.trayPanel != nil || host.tray.open != nil || r.flashOn || !r.flashDue.IsZero() || host.tray.icon.RGBA[3] != 255 {
			t.Fatal("dismissing did not disable hover and restore the tray icon")
		}
		if r.showUnreadTrayPanel(app.StatusAnchor{}) != nil || len(r.unreadTrayItems()) == 0 {
			t.Fatal("dismissing must suppress stale hover without clearing unread menu entries")
		}
		h.ui.state.Revision++
		r.observe()
		if host.tray.open != nil || r.flashOn {
			t.Fatal("an unrelated state update reactivated the reminder")
		}
		arrive("second-new")
		if host.tray.open == nil || !r.flashOn {
			t.Fatal("a new incoming message did not reactivate hover and flashing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type trayReminderTestHost struct {
	app.Host
	tray trayReminderTestControl
}

func (*trayReminderTestHost) SetDockBadge(string) {}
func (*trayReminderTestHost) SetProgress(float32) {}
func (*trayReminderTestHost) OpenWindow(config app.Config, _ *gfx.Canvas, closed func()) (app.WindowControl, error) {
	return trayReminderTestWindow{closed: func() {
		if config.OnClosed != nil {
			config.OnClosed()
		}
		closed()
	}}, nil
}
func (h *trayReminderTestHost) AddStatusItem(config app.StatusItemConfig) (app.StatusItemControl, error) {
	h.tray.icon = config.Icon
	return &h.tray, nil
}

type trayReminderTestWindow struct {
	app.WindowControl
	closed func()
}

func (w trayReminderTestWindow) Close()                { w.closed() }
func (trayReminderTestWindow) Hide()                   {}
func (trayReminderTestWindow) Frame() (gfx.Rect, bool) { return gfx.Rect{}, false }

type trayReminderTestControl struct {
	app.StatusItemControl
	icon app.StatusIcon
	open func(app.StatusAnchor) *app.Window
}

func (c *trayReminderTestControl) SetIcon(icon app.StatusIcon) error { c.icon = icon; return nil }
func (*trayReminderTestControl) SetTooltip(string)                   {}
func (c *trayReminderTestControl) SetHoverPanel(open func(app.StatusAnchor) *app.Window) bool {
	c.open = open
	return true
}

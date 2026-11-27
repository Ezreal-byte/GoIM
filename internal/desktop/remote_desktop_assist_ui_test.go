package desktop

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopMorePopoverRequestsAssistance(t *testing.T) {
	state := remoteDesktopTestState(t)
	h := newShellTestHarness(t, state)
	h.ui.selected = "conv-alice"
	h.ui.invalidator.Invalidate()
	if err := h.driver.Pump(); err != nil {
		t.Fatal(err)
	}

	remote, ok := h.findHit(h.ui.ids.chatRemoteDesktop)
	if !ok {
		t.Fatal("remote desktop button missing")
	}
	more, ok := h.findHit(h.ui.ids.chatRemoteDesktopMore)
	if !ok || more.Disabled || more.Bounds.X < remote.Bounds.Right()-1 {
		t.Fatalf("remote desktop more arrow missing or misplaced: remote=%+v more=%+v", remote, more)
	}

	h.clickHit(more)
	if !h.ui.remoteDesktopMenuOpen {
		t.Fatal("clicking more arrow did not open remote desktop popover")
	}
	snap := h.driver.Snapshot()
	var clicked bool
	for _, hit := range snap.Hits {
		if hit.Label == i18n.T("请求对方协助") {
			h.clickHit(hit)
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatalf("request assistance menu item missing in popover: %+v", printTexts(snap))
	}
	if cmd := h.lastCommand(); cmd.Kind != domain.RequestRemoteAssistance || cmd.PeerID != state.Peers[0].ID {
		t.Fatalf("expected RequestRemoteAssistance command, got %+v", cmd)
	}
	if h.ui.remoteDesktopMenuOpen {
		t.Fatal("popover remained open after selecting an action")
	}
}

func TestRemoteDesktopReverseAssistConsentNotification(t *testing.T) {
	state := remoteDesktopTestState(t)
	state.RemoteDesktop = domain.RemoteDesktopSession{
		ID: "assist-in", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending,
		Incoming: true, Host: false,
	}
	view := newRemoteDesktopView(state)
	view.avatarShell = &UI{theme: view.theme, palette: paletteFor(view.theme, state.Settings.ColorScheme)}
	accepted := false
	view.accept = func() { accepted = true }
	view.stop = func() {}
	t.Cleanup(view.dispose)

	driver := newRemoteDesktopTestDriver(t, view.config())
	requireShellLayout(t, driver.Snapshot())
	text := strings.Join(printTexts(driver.Snapshot()), " ")
	if !strings.Contains(text, i18n.T("请求你协助控制 TA 的电脑")) {
		t.Fatalf("reverse assist invite text missing: %q", text)
	}
	clickRemoteDesktopControl(t, driver, "同意协助")
	if !accepted {
		t.Fatal("accepting reverse assist did not trigger callback")
	}
}

func TestRemoteDesktopControlledOverlayCollapsesAndExpandsAndStops(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLanguage(language)
			state := remoteDesktopTestState(t)
			// Requester who asked peer for help: Incoming=false, Host=true, Control=true.
			state.RemoteDesktop = domain.RemoteDesktopSession{
				ID: "controlled-1", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopActive,
				Incoming: false, Host: true, Control: true,
			}
			view := newRemoteDesktopView(state)
			view.avatarShell = &UI{theme: view.theme, palette: paletteFor(view.theme, state.Settings.ColorScheme)}
			stopped := false
			var resizes [][2]int
			view.stop = func() { stopped = true }
			view.resize = func(w, h int) { resizes = append(resizes, [2]int{w, h}) }
			t.Cleanup(view.dispose)

			cfg := view.config()
			if cfg.WindowMode != app.WindowNotification {
				t.Fatalf("expected WindowNotification mode, got %v", cfg.WindowMode)
			}
			driver := newRemoteDesktopTestDriver(t, cfg)
			requireShellLayout(t, driver.Snapshot())

			// Collapse into compact pill
			clickRemoteDesktopControl(t, driver, "折叠浮窗")
			if !view.collapsed {
				t.Fatal("overlay did not enter collapsed state")
			}
			if len(resizes) != 1 {
				t.Fatalf("expected 1 resize on collapse, got %+v", resizes)
			}
			collapsedW, collapsedH := sharingCollapsedSize(view.theme)
			if resizes[0] != [2]int{collapsedW, collapsedH} {
				t.Fatalf("collapse resize = %v, want [%d %d]", resizes[0], collapsedW, collapsedH)
			}
			if err := driver.Resize(collapsedW, collapsedH); err != nil {
				t.Fatal(err)
			}
			requireShellLayout(t, driver.Snapshot())

			// Expand back to full overlay
			clickRemoteDesktopControl(t, driver, "展开远程协助状态")
			if view.collapsed {
				t.Fatal("overlay did not expand back")
			}
			expandedW, expandedH := invitationSize(view.theme)
			if len(resizes) != 2 || resizes[1] != [2]int{expandedW, expandedH} {
				t.Fatalf("expand resize = %+v, want [%d %d]", resizes, expandedW, expandedH)
			}
			if err := driver.Resize(expandedW, expandedH); err != nil {
				t.Fatal(err)
			}
			requireShellLayout(t, driver.Snapshot())

			// Stop assistance at any time
			clickRemoteDesktopControl(t, driver, "停止共享")
			if !stopped {
				t.Fatal("stop action did not trigger")
			}
		})
	}
}

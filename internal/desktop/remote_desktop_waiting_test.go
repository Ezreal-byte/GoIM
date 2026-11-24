package desktop

import (
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopWaitingKeepsCancelDuringConnection(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLanguage(language)
			state := remoteDesktopTestState(t)
			state.Settings.InterfaceFontSize = 28
			state.RemoteDesktop = domain.RemoteDesktopSession{
				ID: "outgoing-wait", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending,
			}
			view := newRemoteDesktopView(state)
			t.Cleanup(view.dispose)
			cancelled := 0
			view.stop = func() { cancelled++ }
			config := view.config()
			config.Width, config.Height = config.MinWidth, config.MinHeight
			driver := newRemoteDesktopTestDriver(t, config)
			requireShellLayout(t, driver.Snapshot())
			if !strings.Contains(strings.Join(printTexts(driver.Snapshot()), " "), i18n.T("等待对方确认远程协助…")) {
				t.Fatal("waiting status is missing")
			}
			if err := driver.Advance(240 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if cancelled != 0 || remoteDesktopTestHit(t, driver, "取消").Disabled {
				t.Fatal("waiting did not leave the cancel action available")
			}

			state.RemoteDesktop.Status = domain.RemoteDesktopConnecting
			view.update(state)
			view.wake.Invalidate()
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
			requireShellLayout(t, driver.Snapshot())
			if !strings.Contains(strings.Join(printTexts(driver.Snapshot()), " "), i18n.T("正在建立远程协助…")) {
				t.Fatal("connection status is missing")
			}
			clickRemoteDesktopControl(t, driver, "取消")
			if cancelled != 1 {
				t.Fatalf("Cancel dispatched %d times, want 1", cancelled)
			}
		})
	}
}

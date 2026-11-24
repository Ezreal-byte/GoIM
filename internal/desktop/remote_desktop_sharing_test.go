package desktop

import (
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopSharingNotification(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLanguage(language)
			state := remoteDesktopTestState(t)
			state.Settings.InterfaceFontSize = 28
			state.RemoteDesktop = domain.RemoteDesktopSession{
				ID: "sharing", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopActive, Incoming: true,
			}
			view := newRemoteDesktopView(state)
			view.avatarShell = &UI{theme: view.theme, palette: paletteFor(view.theme, state.Settings.ColorScheme)}
			t.Cleanup(view.dispose)
			backend := newShellTestBackend(state)
			runtime := &desktopRuntime{backend: backend}
			view.stop = func() { runtime.endRemoteDesktopView(view) }
			config := view.config()
			if config.WindowMode != app.WindowNotification || config.Origin != nil {
				t.Fatal("active sharing must use the native bottom-right notification")
			}
			driver := newRemoteDesktopTestDriver(t, config)
			requireShellLayout(t, driver.Snapshot())
			text := strings.Join(printTexts(driver.Snapshot()), " ")
			if !strings.Contains(text, i18n.T("正在查看你的屏幕")) ||
				strings.Contains(text, i18n.T("正在共享系统声音")) {
				t.Fatal("view-only status did not report the granted permissions")
			}

			state.RemoteDesktop.Control, state.RemoteDesktop.Audio = true, true
			view.update(state)
			backend.setState(state)
			view.wake.Invalidate()
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
			requireShellLayout(t, driver.Snapshot())
			text = strings.Join(printTexts(driver.Snapshot()), " ")
			if !strings.Contains(text, i18n.T("正在控制你的电脑")) ||
				!strings.Contains(text, i18n.T("正在共享系统声音")) {
				t.Fatal("active control or system audio status is missing")
			}
			clickRemoteDesktopControl(t, driver, "停止共享")
			clickRemoteDesktopControl(t, driver, "停止共享")
			if commands := backend.commandLog(); len(commands) != 1 || commands[0].Kind != domain.EndRemoteDesktop ||
				commands[0].RemoteDesktopID != view.session.ID {
				t.Fatalf("unexpected stop-sharing commands: %+v", commands)
			}
		})
	}
}

func TestRemoteDesktopSharingScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	state := remoteDesktopTestState(t)
	state.Settings.Language, state.Settings.Theme, state.Settings.InterfaceFontSize = i18n.Chinese, "light", 15
	state.RemoteDesktop = domain.RemoteDesktopSession{
		ID: "sharing", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopActive, Incoming: true, Control: true,
	}
	captureRemoteDesktopNotification(t, state, directory, "remote-sharing-light-zh.png")
	state.Settings.Language, state.Settings.Theme, state.Settings.InterfaceFontSize = i18n.English, "dark", 28
	state.RemoteDesktop.Audio = true
	captureRemoteDesktopNotification(t, state, directory, "remote-sharing-dark-large-en.png")
}

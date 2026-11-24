package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopViewerTitlebarKeepsActionsAndFrameSeparate(t *testing.T) {
	language := i18n.Language()
	i18n.SetLanguage(i18n.English)
	t.Cleanup(func() { i18n.SetLanguage(language) })

	view, driver, _ := remoteDesktopTestViewer(t, true, true)
	config := view.config()
	if config.WindowMode != app.WindowCustomTitlebar || config.WindowControls == nil {
		t.Fatal("viewer did not preserve native window controls in its titlebar")
	}
	view.peerName = "Isolated development desktop"
	view.theme = themeForSettings(domain.Settings{Theme: "dark", InterfaceFontSize: 28})
	if err := driver.Resize(config.MinWidth, config.MinHeight); err != nil {
		t.Fatal(err)
	}
	top := float32(config.TitlebarHeight)
	requireShellLayout(t, driver.Snapshot())
	screen := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds
	if screen.Y < top {
		t.Fatal("remote screen overlaps the titlebar")
	}
	if driver.PressAt(156, top/2) != app.PressMove {
		t.Fatal("the title area cannot be dragged")
	}
	for _, label := range []string{"快捷键", "声音", "全屏", "结束协助"} {
		action := remoteDesktopTestHit(t, driver, label)
		if action.Disabled || action.Bounds.Bottom() > top {
			t.Fatalf("action %q falls outside the titlebar: %+v", label, action.Bounds)
		}
		if driver.PressAt(action.Bounds.X+action.Bounds.Width/2, action.Bounds.Y+action.Bounds.Height/2) == app.PressMove {
			t.Fatalf("action %q moves the window on press", label)
		}
	}

	clickRemoteDesktopControl(t, driver, "快捷键")
	remoteDesktopTestHit(t, driver, "释放键鼠")
	if screen := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds; screen.Y < view.viewer.toolbarHeight() {
		t.Fatal("expanded shortcuts cover the interactive remote screen")
	}
	requireShellLayout(t, driver.Snapshot())
	clickRemoteDesktopControl(t, driver, "快捷键")
	if got := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds; got != screen {
		t.Fatalf("collapsing shortcuts did not restore the screen bounds: got %+v, want %+v", got, screen)
	}
	if err := driver.SetFullscreen(true); err != nil {
		t.Fatal(err)
	}
	remoteDesktopTestHit(t, driver, "退出全屏")
	if driver.PressAt(156, top/2) == app.PressMove {
		t.Fatal("the fullscreen viewer still has a window drag region")
	}
	ended := false
	view.stop = func() { ended = true }
	clickRemoteDesktopControl(t, driver, "结束协助")
	if !ended {
		t.Fatal("the fullscreen viewer could not end assistance")
	}
}

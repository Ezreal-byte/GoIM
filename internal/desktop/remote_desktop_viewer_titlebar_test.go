package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopViewerTitlebarKeepsActionsAndFrameSeparate(t *testing.T) {
	language := i18n.Language()
	i18n.SetLanguage(i18n.English)
	t.Cleanup(func() { i18n.SetLanguage(language) })

	view, driver, backend := remoteDesktopTestViewer(t, true, true)
	config := view.config()
	if config.WindowMode != app.WindowCustomTitlebar || config.WindowControls == nil {
		t.Fatal("viewer did not preserve native window controls in its titlebar")
	}
	if config.TitlebarHeight != 42 {
		t.Fatalf("titlebar height = %g, want 42 at the default font size", config.TitlebarHeight)
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
	shortcut := remoteDesktopTestHit(t, driver, "释放键鼠")
	if shortcut.Bounds.Y < top || !view.viewer.shortcutsOpen {
		t.Fatal("shortcuts did not open below the titlebar")
	}
	requireShellLayout(t, driver.Snapshot())
	if got := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds; got != screen {
		t.Fatalf("opening shortcuts resized the remote screen: got %+v, want %+v", got, screen)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if view.viewer.shortcutsOpen {
		t.Fatal("Escape did not close shortcuts")
	}
	view.viewer.setDevices([]string{"USB Headset"})
	clickRemoteDesktopControl(t, driver, "声音")
	remoteDesktopTestHit(t, driver, "静音")
	requireShellLayout(t, driver.Snapshot())
	clickRemoteDesktopControl(t, driver, "扬声器")
	clickRemoteDesktopControl(t, driver, "USB Headset")
	if len(backend.inputs) != 1 || backend.inputs[0].Action != "audio" || backend.inputs[0].Device != "USB Headset" {
		t.Fatalf("audio popover did not select the output device: %+v", backend.inputs)
	}
	backend.inputs = nil
	if got := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds; got != screen {
		t.Fatalf("opening audio resized the remote screen: got %+v, want %+v", got, screen)
	}
	x, y := screen.X+12, screen.Bottom()-12
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if view.viewer.audioOpen || view.viewer.captured || len(backend.inputs) != 0 {
		t.Fatalf("clicking the image to dismiss audio reached the remote desktop: %+v", backend.inputs)
	}
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !view.viewer.captured || len(backend.inputs) != 2 {
		t.Fatalf("clicking the image after dismissal did not capture the remote desktop: %+v", backend.inputs)
	}
	view.viewer.releaseInput()
	if got := remoteDesktopTestHit(t, driver, "远程屏幕").Bounds; got != screen {
		t.Fatalf("closing a popover resized the remote screen: got %+v, want %+v", got, screen)
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

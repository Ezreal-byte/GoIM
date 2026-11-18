package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"

	"goim/internal/selfupdate"
)

func TestDesktopFeatureRepairScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	state := shellTestState(t)
	shell, session := openShellQA(t, state, 1372, 1080)
	shell.panels.setAutostartProvider(
		func() (bool, error) { return true, nil },
		func(bool) error { return nil },
	)
	shell.panels.loadAutostart()

	shellQAClick(t, session, shell.ids.navSettings)
	shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsGeneral)))
	if err := session.Expect("text", "开机自启"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "settings-general-autostart.png")

	shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsProfile)))
	shellQAClick(t, session, shell.panels.settingsExtra.profile)
	if err := session.Expect("text", "个人资料"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "profile-dialog-comfortable-actions.png")
	shellQAEscape(t, session)

	shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsAbout)))
	if err := session.Expect("text", "导出脱敏报告"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "settings-about-diagnostics.png")

	shell.updater = &updateController{view: updateView{
		phase: updateOffered,
		offer: selfupdate.Offer{
			Version: "0.1.8", Automatic: true,
			NotesURL: selfupdate.ReleaseURL("0.1.8"),
		},
		notes: "## GoIM 0.1.8\n\n" +
			"本次更新修复了 **Windows 更新安装器**，并改善了 Linux 兼容性。\n\n" +
			"- 修复独立聊天窗口置顶与缩放\n" +
			"- Linux 包支持 `GLIBC 2.31` 基线\n" +
			"- [查看完整更新说明](https://github.com/Ezreal-byte/GoIM/releases)\n\n" +
			"| 平台 | 安装包 |\n| --- | --- |\n| Windows | Setup.exe |\n| Linux | DEB / tar.gz |",
	}}
	shell.observeUpdate()
	shell.updateOpen = true
	shell.invalidator.Invalidate()
	if err := session.Wait(150 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("text", "GoIM 0.1.8"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "update-dialog-markdown.png")

	detachedState := shellTestState(t)
	detachedBackend := newShellTestBackend(detachedState)
	detached := newDetachedConversationView(detachedBackend, "conv-alice")
	t.Cleanup(detached.Close)
	detachedSession, err := vivaheadless.Open(detached.DetachedConfig(), nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(detachedSession.Close)
	if err := detachedSession.Wait(150 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := detachedSession.Expect("text", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := detachedSession.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if err := detachedSession.Screenshot(filepath.Join(directory, "detached-conversation-window.png")); err != nil {
		t.Fatal(err)
	}

	minimumState := shellTestState(t)
	minimumShell, minimumSession := openShellQA(t, minimumState, 1000, 700)
	minimumShell.panels.setAutostartProvider(
		func() (bool, error) { return true, nil },
		func(bool) error { return nil },
	)
	minimumShell.panels.loadAutostart()
	shellQAClick(t, minimumSession, minimumShell.ids.navSettings)
	shellQAClick(t, minimumSession, minimumShell.panels.ids.settingsCategories.At(int(panelSettingsGeneral)))
	if err := minimumSession.Expect("text", "开机自启"); err != nil {
		t.Fatal(err)
	}
	if err := minimumSession.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if err := minimumSession.Scroll(gfx.ScrollEvent{X: 800, Y: 600, DeltaY: 620}); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, minimumSession, directory, "settings-general-autostart-minimum.png")
}

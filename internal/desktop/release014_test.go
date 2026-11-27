package desktop

import (
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestManualConnectDialogWaitsForResultAndAllowsRetry(t *testing.T) {
	state := shellMinimalState(t)
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.sidebarConnect)
	h.typeInto(h.ui.ids.connectField, "192.168.1.44:7331")
	h.clickID(h.ui.ids.connectSubmit)
	state.ConnectionFeedback = domain.ConnectionFeedback{Sequence: 1, Address: "192.168.1.44:7331", Pending: true}
	state.Revision++
	h.publish(state)
	if !h.ui.connectOpen || !h.hasText("正在连接，请稍候") {
		t.Fatal("connecting state was not visible")
	}
	count := len(h.backend.commandLog())
	h.clickID(h.ui.ids.connectSubmit)
	if len(h.backend.commandLog()) != count {
		t.Fatal("connection could be submitted twice")
	}
	state.ConnectionFeedback.Sequence++
	state.ConnectionFeedback.Pending = false
	state.ConnectionFeedback.Problem = "目标端口拒绝连接，请确认对方 GoIM 已启动且端口正确。"
	state.Revision++
	h.publish(state)
	if !h.ui.connectOpen || h.ui.connectPending || !h.hasText("目标端口拒绝连接") {
		t.Fatal("failed connection closed the dialog or omitted its reason")
	}
	h.clickID(h.ui.ids.connectSubmit)
	state.ConnectionFeedback.Sequence += 2
	state.ConnectionFeedback.Problem = ""
	state.Revision++
	h.publish(state)
	if h.ui.connectOpen || !h.hasText("连接成功") {
		t.Fatal("successful connection was not acknowledged")
	}
}

func TestSecuritySettingsRejectsUnsupportedSystemUnlockUpfront(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.HasLockPassword, state.Settings.SystemUnlock = true, false
	h := newShellTestHarness(t, state)
	h.ui.panels.settingsExtra.systemUnlockAvailable = false
	h.clickID(h.ui.ids.navSettings)
	h.clickID(h.ui.panels.ids.settingsCategories.At(int(panelSettingsSecurity)))
	if !h.hasText("系统验证当前不可用") {
		t.Fatal("unsupported system verification was not explained")
	}
	h.clickID(h.ui.panels.settingsExtra.lockSystem)
	if h.ui.panels.settingsExtra.passwordOpen || hasCommandKind(h.backend.commandLog(), domain.SetSystemUnlock) {
		t.Fatal("unsupported switch could enable system verification")
	}
}

func TestClearUserDataRequiresSecondConfirmation(t *testing.T) {
	state := panelTestDefaultState(t)
	h := aboutTestOpen(t, state, ui.LightTheme(), i18n.Chinese, ui.Rect{X: 68, Width: 1372, Height: 1800})
	h.click(h.panels.ids.aboutResetData)
	if !h.panels.resetDataOpen || len(h.commands) != 0 {
		t.Fatal("clear-data action skipped confirmation")
	}
	h.click(h.panels.ids.aboutResetCancel)
	if h.panels.resetDataOpen || hasCommandKind(h.commands, domain.ResetUserData) {
		t.Fatal("canceling confirmation cleared user data")
	}
	h.click(h.panels.ids.aboutResetData)
	h.click(h.panels.ids.aboutResetConfirm)
	if h.panels.resetDataOpen || h.requireLastCommand().Kind != domain.ResetUserData {
		t.Fatal("confirming did not request clear-and-exit")
	}
}

func TestRelease014ScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	previousLanguage := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previousLanguage) })
	for _, theme := range []string{"light", "dark"} {
		for _, language := range []string{i18n.Chinese, i18n.English} {
			t.Run(theme+"-"+language, func(t *testing.T) {
				state := shellMinimalState(t)
				state.Settings.Theme, state.Settings.Language = theme, language
				shell, session := openShellQA(t, state, 1440, 1080)
				shellQAClick(t, session, shell.ids.navSettings)
				shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsAbout)))
				shellQAClick(t, session, shell.panels.ids.aboutResetData)
				if err := session.Wait(700 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("text", i18n.T("清除并退出")); err != nil {
					t.Fatal(err)
				}
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				shellQAShot(t, session, directory, "reset-confirm-"+theme+"-"+language+".png")
			})
		}
	}
}

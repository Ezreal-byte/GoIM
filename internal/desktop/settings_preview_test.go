package desktop

import (
	"testing"

	"goim/internal/domain"
)

func TestMessagePreviewDefaultsOnAndPreservesPrivacyChoice(t *testing.T) {
	for _, private := range []bool{false, true} {
		state := panelTestDefaultState(t)
		if state.Settings.HideNotificationText {
			t.Fatal("new profiles must show message previews by default")
		}
		state.Settings.HideNotificationText = private
		h := panelTestNewHarness(t, panelModuleSettings, state)
		h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNotifications)))
		hit, ok := h.findHit(h.panels.settingsExtra.notificationPrivacy)
		if !ok || hit.Label != "打开消息预览" || hit.Checked == private {
			t.Fatalf("private=%v: preview switch %+v", private, hit)
		}
		h.click(h.panels.settingsExtra.notificationPrivacy)
		command := h.requireLastCommand()
		if command.Kind != domain.SetSetting || command.Setting != "hide_notification_text" || command.Bool == private {
			t.Fatalf("private=%v: command %+v", private, command)
		}
	}
}

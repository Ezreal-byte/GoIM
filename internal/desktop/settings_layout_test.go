package desktop

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (h *panelTestHarness) findLabel(value string) (app.Label, bool) {
	for _, label := range h.driver.Snapshot().Labels {
		if label.Value == value {
			return label, true
		}
	}
	return app.Label{}, false
}

func TestSettingsLayoutCapsWidthAndUses38DPControls(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1600, Height: 1800,
	})
	if h.panels.settingsWidth != panelSettingsContentMax {
		t.Fatalf("settings content width = %.1f, want %.1f", h.panels.settingsWidth, panelSettingsContentMax)
	}
	themeHit, ok := h.findHitLabelRole(i18n.T("明亮"), gfx.RoleRadio)
	if !ok {
		t.Fatal("theme choice is not visible")
	}
	if themeHit.Bounds.Height != panelSettingsControlH {
		t.Fatalf("theme choice height = %.1f, want %.1f", themeHit.Bounds.Height, panelSettingsControlH)
	}

	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsTransfers)))
	duplicate, ok := h.findHitLabelRole(i18n.T("同名文件"), gfx.RoleCombobox)
	if !ok {
		t.Fatal("duplicate-policy select is not visible")
	}
	if duplicate.Bounds.Height != panelSettingsControlH {
		t.Fatalf("select height = %.1f, want %.1f", duplicate.Bounds.Height, panelSettingsControlH)
	}

	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	if !h.hasText(i18n.T("自动")) {
		t.Fatal("manual connection does not default to Automatic")
	}
	protocol, ok := h.findHitLabelRole(i18n.T("连接方式"), gfx.RoleCombobox)
	if !ok {
		t.Fatal("connection select is not visible")
	}
	address, ok := h.findHit(h.panels.settingsExtra.connectAddress)
	if !ok {
		t.Fatal("connection address is not visible")
	}
	if protocol.Bounds.Height != panelSettingsControlH || address.Bounds.Height != panelSettingsControlH {
		t.Fatalf("connection controls are %.1f/%.1f high, want %.1f",
			protocol.Bounds.Height, address.Bounds.Height, panelSettingsControlH)
	}

	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsSecurity)))
	if _, visible := h.findHit(h.panels.settingsExtra.passwordNew); visible {
		t.Fatal("password fields appeared before the dialog opened")
	}
	h.click(h.panels.settingsExtra.passwordTrigger)
	password, ok := h.findHit(h.panels.settingsExtra.passwordNew)
	if !ok {
		t.Fatal("new-password field is not visible")
	}
	if password.Bounds.Height != panelSettingsControlH {
		t.Fatalf("password field height = %.1f, want %.1f", password.Bounds.Height, panelSettingsControlH)
	}
	h.click(h.panels.settingsExtra.passwordNew)
	if err := h.driver.Type("secret1"); err != nil {
		t.Fatal(err)
	}
	if got := h.panels.settingsExtra.password.Text(); got != "secret1" {
		t.Fatalf("password keyboard input = %q", got)
	}
}

func TestSettingsLayoutStacksRowsWhenNarrow(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 932, Height: 1800,
	})
	if !h.panels.settingsNarrow || h.panels.settingsWidth >= panelSettingsNarrowAt {
		t.Fatalf("narrow layout state = narrow:%v width:%.1f", h.panels.settingsNarrow, h.panels.settingsWidth)
	}
	label, ok := h.findLabel("主题")
	if !ok {
		t.Fatal("theme label is not visible")
	}
	control, ok := h.findHitLabelRole(i18n.T("明亮"), gfx.RoleRadio)
	if !ok {
		t.Fatal("theme control is not visible")
	}
	if control.Bounds.Y <= label.Bounds.Y {
		t.Fatalf("narrow row did not stack: label %+v, control %+v", label.Bounds, control.Bounds)
	}
	h.requireLayout()
}

func TestSettingsConnectionFormSubmitsAutomaticTransport(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	h.click(h.panels.settingsExtra.connectAddress)
	if err := h.driver.Type("192.168.1.44"); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	command := h.requireLastCommand()
	if command.Kind != domain.Connect || command.Address != "192.168.1.44" || command.Transport != domain.TransportAuto {
		t.Fatalf("automatic connection command = %+v", command)
	}
}

func TestManualConnectionSelectorKeepsTheDialogOpenOnEscape(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	h.clickID(h.ui.ids.sidebarConnect)
	h.clickLabel("连接方式")
	if _, ok := h.findHitLabel("飞鸽／飞秋兼容"); !ok {
		t.Fatal("the available transports did not open")
	}
	if err := h.driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.findHitLabel("飞鸽／飞秋兼容"); ok || !h.hasHit(h.ui.ids.connectField) {
		t.Fatal("Escape should dismiss the list and leave the address field in the dialog")
	}
	h.clickLabel("连接方式")
	h.clickLabel("GoIM")
	h.clickLabel("连接方式")
	h.clickLabel("自动")
	h.typeInto(h.ui.ids.connectField, "192.168.1.44")
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if command := h.lastCommand(); command.Kind != domain.Connect ||
		command.Address != "192.168.1.44" || command.Transport != domain.TransportAuto {
		t.Fatalf("manual connection command = %+v", command)
	}
}

func TestCompatibilitySwitchSubmitsSavedPort(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.Neiwangtong = domain.CompatibilityConfig{Enabled: true, Port: 9011}
	state.NeiwangtongStatus.Active = true
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	editor := &h.panels.settingsExtra.compat[0]

	h.click(editor.portID)
	if err := h.driver.Type("0"); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	h.click(editor.toggleID)
	command := h.requireLastCommand()
	if command.Kind != domain.SetCompatibility || command.Transport != domain.TransportNeiwangtong ||
		command.Compatibility.Enabled || command.Compatibility.Port != 9011 {
		t.Fatalf("compatibility toggle command = %+v", command)
	}
}

func TestCompatibilityPortSavesOnEnter(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.IPMSG = domain.CompatibilityConfig{Enabled: true, Port: domain.IPMSGPort}
	state.IPMSGStatus.Active = true
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	editor := &h.panels.settingsExtra.compat[1]
	editor.field.SetText("")
	h.click(editor.portID)
	if err := h.driver.Type("2426"); err != nil {
		t.Fatal(err)
	}
	h.clearCommands()
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	command := h.requireLastCommand()
	if command.Kind != domain.SetCompatibility || command.Transport != domain.TransportIPMSG ||
		!command.Compatibility.Enabled || command.Compatibility.Port != 2426 {
		t.Fatalf("compatibility port command = %+v", command)
	}
}

func TestCompatibilityFailureKeepsSavedStateAndAllowsRetry(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.Neiwangtong = domain.CompatibilityConfig{Enabled: true, Port: 9011}
	state.NeiwangtongStatus = domain.CompatibilityStatus{Problem: "启动失败"}
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1800,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	editor := &h.panels.settingsExtra.compat[0]

	before, ok := h.findHit(editor.toggleID)
	if !ok || !before.Checked {
		t.Fatalf("saved enabled state was not shown: %+v", before)
	}
	h.click(editor.toggleID)
	if command := h.requireLastCommand(); command.Kind != domain.SetCompatibility || command.Compatibility.Enabled {
		t.Fatalf("compatibility toggle command = %+v", command)
	}

	h.publish(state)
	after, ok := h.findHit(editor.toggleID)
	if !ok || !after.Checked {
		t.Fatalf("failed save left the switch unsaved: %+v", after)
	}
	save, ok := h.findHit(editor.saveID)
	if !ok || save.Disabled {
		t.Fatalf("failed compatibility service cannot be retried: %+v", save)
	}
}

func TestShortcutKeycapDialogFocusCancelClearAndDefault(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1080,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsShortcuts)))
	if !h.hasText("应用内") || !h.hasText("全局") {
		t.Fatal("shortcut scopes are not grouped")
	}

	h.click(h.panels.ids.shortcutRecord.At(2))
	if h.panels.shortcutEdit != 2 {
		t.Fatalf("keycap did not open recording: index %d", h.panels.shortcutEdit)
	}
	if focused := h.driver.Snapshot().Focused; focused != h.panels.ids.shortcutSave.At(2) {
		t.Fatalf("dialog focus = %v, want save %v", focused, h.panels.ids.shortcutSave.At(2))
	}
	h.click(h.panels.ids.shortcutCancel.At(2))
	if h.panels.shortcutEdit != -1 || len(h.commands) != 0 {
		t.Fatalf("cancel left recording state or submitted: index=%d commands=%+v", h.panels.shortcutEdit, h.commands)
	}

	h.click(h.panels.ids.shortcutRecord.At(2))
	h.click(h.panels.ids.shortcutDialogClear.At(2))
	h.clearCommands()
	h.click(h.panels.ids.shortcutSave.At(2))
	command := h.requireLastCommand()
	var shortcuts []domain.Shortcut
	if err := json.Unmarshal([]byte(command.Value), &shortcuts); err != nil {
		t.Fatal(err)
	}
	if shortcuts[2].Keys != "" || shortcuts[1].Keys != state.Settings.Shortcuts[1].Keys {
		t.Fatalf("clear changed other shortcuts: %+v", shortcuts)
	}

	h.click(h.panels.ids.shortcutRecord.At(2))
	h.click(h.panels.ids.shortcutDialogReset.At(2))
	h.clearCommands()
	h.click(h.panels.ids.shortcutSave.At(2))
	command = h.requireLastCommand()
	if err := json.Unmarshal([]byte(command.Value), &shortcuts); err != nil {
		t.Fatal(err)
	}
	if shortcuts[2].Keys != "Ctrl+Shift+N" {
		t.Fatalf("default shortcut = %q", shortcuts[2].Keys)
	}
}

func TestShortcutRecordingEscapeRestoresKeycapFocus(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1080,
	})
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsShortcuts)))
	h.click(h.panels.ids.shortcutRecord.At(2))
	if err := h.driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if h.panels.shortcutEdit != -1 || len(h.commands) != 0 {
		t.Fatalf("Escape left recording or submitted: index=%d commands=%+v", h.panels.shortcutEdit, h.commands)
	}
	if focused := h.driver.Snapshot().Focused; focused != h.panels.ids.shortcutRecord.At(2) {
		t.Fatalf("focus after Escape = %v, want keycap %v", focused, h.panels.ids.shortcutRecord.At(2))
	}
}

func TestSettingsLayoutLocaleEntriesLoadAfterMainCatalog(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	i18n.SetLanguage(i18n.English)
	if got := i18n.T("自动"); got != "Automatic" {
		t.Fatalf("automatic = %q", got)
	}
	if got := i18n.T("录制快捷键"); got != "Record shortcut" {
		t.Fatalf("record shortcut = %q", got)
	}
}

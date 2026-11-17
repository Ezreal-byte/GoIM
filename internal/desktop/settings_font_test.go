package desktop

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func waitForInterfaceFonts(t *testing.T, h *panelTestHarness) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !h.panels.settingsExtra.interfaceFont.known {
		select {
		case <-h.driver.Wakes():
		case <-ticker.C:
		}
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out loading interface fonts")
		}
	}
	h.requireLayout()
}

func TestInterfaceFontPickerPreviewsSearchesAppliesAndResets(t *testing.T) {
	i18n.SetLanguage(i18n.Chinese)
	t.Cleanup(func() { i18n.SetLanguage(i18n.Chinese) })
	state := panelTestDefaultState(t)
	state.Settings.InterfaceFont = "Segoe UI"
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	font := &h.panels.settingsExtra.interfaceFont
	h.panels.fontFamilies = func() []string {
		return []string{"Microsoft YaHei UI", "Noto Sans", "Segoe UI"}
	}
	var applied []string
	h.panels.setInterfaceFont = func(family string) bool {
		applied = append(applied, family)
		return true
	}

	h.click(font.trigger)
	waitForInterfaceFonts(t, h)
	if !h.hasText(interfaceFontSample) || !h.hasText(interfaceFontSampleSymbols) {
		t.Fatal("font preview samples are missing")
	}
	if !h.hasText("当前：Segoe UI") {
		t.Fatal("the saved interface font is not named on the settings row")
	}

	h.click(font.search)
	if err := h.driver.Type("YaHei"); err != nil {
		t.Fatal(err)
	}
	if !h.hasText("Microsoft YaHei UI") || h.hasText("Noto Sans") {
		t.Fatal("font search did not narrow the installed families")
	}
	h.clearCommands()
	h.click(font.confirm)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "interface_font" || command.Value != "Microsoft YaHei UI" {
		t.Fatalf("interface font command = %+v", command)
	}
	if len(applied) != 1 || applied[0] != "Microsoft YaHei UI" {
		t.Fatalf("applied interface fonts = %#v", applied)
	}
	if font.open {
		t.Fatal("confirming the font left its picker open")
	}

	h.click(font.trigger)
	h.clickLabelRole(i18n.T("系统默认"), gfx.RoleButton)
	h.clearCommands()
	h.click(font.confirm)
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "interface_font" || command.Value != "" {
		t.Fatalf("reset interface font command = %+v", command)
	}
	if len(applied) != 2 || applied[1] != "" {
		t.Fatalf("reset did not tell Viva to use its system font: %#v", applied)
	}
}

func TestInterfaceFontPickerExplainsAMissingSavedFamily(t *testing.T) {
	i18n.SetLanguage(i18n.Chinese)
	t.Cleanup(func() { i18n.SetLanguage(i18n.Chinese) })
	state := panelTestDefaultState(t)
	state.Settings.InterfaceFont = "Missing Family"
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	font := &h.panels.settingsExtra.interfaceFont
	h.panels.fontFamilies = func() []string { return []string{"Segoe UI", "Noto Sans"} }

	h.click(font.trigger)
	waitForInterfaceFonts(t, h)
	if !font.missing {
		t.Fatal("a saved family absent from the system was not marked missing")
	}
	h.click(font.cancel)
	if !h.hasText(i18n.T("已保存的字体未安装，暂时使用系统字体")) {
		t.Fatal("the missing-font explanation is not visible")
	}
}

func TestInterfaceFontPickerUsesNativeWheelAndDraggableScrollbar(t *testing.T) {
	i18n.SetLanguage(i18n.Chinese)
	t.Cleanup(func() { i18n.SetLanguage(i18n.Chinese) })
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	font := &h.panels.settingsExtra.interfaceFont
	families := make([]string, 80)
	for index := range families {
		families[index] = "Test Font " + string(rune('A'+index/26)) + "-" + string(rune('A'+index%26))
	}
	h.panels.fontFamilies = func() []string { return families }
	h.click(font.trigger)
	waitForInterfaceFonts(t, h)

	row, ok := h.findHitLabelRole("Test Font A-A", gfx.RoleButton)
	if !ok {
		t.Fatal("first font row is not on screen")
	}
	center := ui.Point{
		X: row.Bounds.X + row.Bounds.Width/2,
		Y: row.Bounds.Y + row.Bounds.Height/2,
	}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(center.X, center.Y); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	var bar gfx.Hit
	barFound := false
	for _, hit := range h.driver.Snapshot().Hits {
		if hit.Label == "Vertical scrollbar" {
			bar, barFound = hit, true
			break
		}
	}
	if !barFound {
		var labels []string
		for _, hit := range h.driver.Snapshot().Hits {
			if hit.Label != "" {
				labels = append(labels, hit.Label)
			}
		}
		t.Fatalf("native font-list scrollbar was not exposed at %+v; labels=%v", center, labels)
	}
	if err := h.driver.Scroll(gfx.ScrollEvent{
		X: center.X, Y: center.Y, DeltaY: interfaceFontRowStep * 4, Precise: true,
	}); err != nil {
		t.Fatal(err)
	}
	h.requireLayout()
	if font.scroll <= 0 {
		t.Fatal("wheel input did not move the native font-list scroll state")
	}

	before := font.scroll
	startX, startY := bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2
	if err := h.driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(startX, startY+40); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Release(startX, startY+40, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	h.requireLayout()
	if font.scroll == before {
		t.Fatal("dragging the native scrollbar did not move the font list")
	}
}

package desktop

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestLaunchAtLoginReadsRunsAndCachesTheSystemSetting(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	readCalls, setCalls := 0, 0
	installed := true
	h.panels.setAutostartProvider(
		func() (bool, error) {
			readCalls++
			return installed, nil
		},
		func(enabled bool) error {
			setCalls++
			installed = enabled
			return nil
		},
	)
	h.panels.loadAutostart()
	pumpAutostartPanel(t, h)
	if readCalls != 1 || !h.panels.settingsExtra.autostart.known || !h.panels.settingsExtra.autostart.on {
		t.Fatalf("initial state: reads=%d autostart=%+v", readCalls, h.panels.settingsExtra.autostart)
	}
	hit, ok := h.findHit(h.panels.ids.settingsLaunchAtLogin)
	if !ok || !hit.Checked || hit.Disabled {
		t.Fatalf("enabled switch = %+v, visible=%v", hit, ok)
	}

	h.click(h.panels.ids.settingsLaunchAtLogin)
	pumpAutostartPanel(t, h)
	if readCalls != 1 || setCalls != 1 || installed || h.panels.settingsExtra.autostart.on ||
		h.panels.settingsExtra.autostart.pending {
		t.Fatalf("successful change: reads=%d sets=%d installed=%v autostart=%+v",
			readCalls, setCalls, installed, h.panels.settingsExtra.autostart)
	}
	pumpAutostartPanel(t, h)
	if readCalls != 1 {
		t.Fatalf("repaint queried the operating system again: reads=%d", readCalls)
	}
}

func TestLaunchAtLoginFailureRevertsAndExplainsTheSystemError(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	installed := false
	h.panels.setAutostartProvider(
		func() (bool, error) { return installed, nil },
		func(bool) error { return errors.New("login item is read-only") },
	)
	h.panels.loadAutostart()
	pumpAutostartPanel(t, h)

	h.click(h.panels.ids.settingsLaunchAtLogin)
	pumpAutostartPanel(t, h)
	hit, ok := h.findHit(h.panels.ids.settingsLaunchAtLogin)
	if !ok || hit.Checked || h.panels.settingsExtra.autostart.on ||
		h.panels.settingsExtra.autostart.pending {
		t.Fatalf("failed change did not revert: hit=%+v autostart=%+v",
			hit, h.panels.settingsExtra.autostart)
	}
	if h.panels.settingsExtra.autostart.err == "" || !h.hasText("login item is read-only") {
		t.Fatal("system failure was not shown in the settings row")
	}
}

func TestLaunchAtLoginReadFailureDoesNotWrite(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarnessBounds(t, panelModuleSettings, state, ui.Rect{
		X: 68, Y: 0, Width: 1372, Height: 1500,
	})
	writes := 0
	h.panels.setAutostartProvider(
		func() (bool, error) { return false, errors.New("registry unavailable") },
		func(bool) error {
			writes++
			return nil
		},
	)
	h.panels.loadAutostart()
	pumpAutostartPanel(t, h)
	hit, ok := h.findHit(h.panels.ids.settingsLaunchAtLogin)
	if !ok || !hit.Disabled || writes != 0 {
		t.Fatalf("read failure switch=%+v visible=%v writes=%d", hit, ok, writes)
	}
	if !h.hasText("registry unavailable") {
		t.Fatal("read failure was not shown")
	}
}

func pumpAutostartPanel(t *testing.T, h *panelTestHarness) {
	t.Helper()
	h.wake.Invalidate()
	for range 2 {
		if err := h.driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
}

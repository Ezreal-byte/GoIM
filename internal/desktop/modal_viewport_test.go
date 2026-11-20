package desktop

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestPanelDialogCoversWholeWindowAndBlocksNavigation(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		state := printerExperienceState(t)
		state.Settings.Theme = theme
		h := newShellTestHarnessAtSize(t, state, 1000, 700)
		h.ui.panels.print.shareOpen = true
		h.wake()
		dismiss, ok := h.findHit(h.ui.panels.print.ids.shareDismiss)
		if !ok || dismiss.Bounds != (gfx.Rect{Width: 1000, Height: 700}) {
			t.Fatalf("scrim=%+v present=%v", dismiss.Bounds, ok)
		}
		requireFullWindowScrim(t, h)
		requireShellLayout(t, h.snapshot())
		before := h.ui.section
		if err := h.driver.Click(30, 200); err != nil {
			t.Fatal(err)
		}
		if h.ui.section != before {
			t.Fatal("click passed through the modal into navigation")
		}
		if h.ui.panels.print.shareOpen {
			t.Fatal("click on rail scrim did not dismiss dialog")
		}
	}
}

func requireFullWindowScrim(t *testing.T, h *shellTestHarness) {
	t.Helper()
	viewport := gfx.Rect{Width: 1000, Height: 700}
	for _, primitive := range h.snapshot().Primitives {
		if primitive.Layer == 70 && primitive.Bounds == viewport {
			if primitive.Clipped && primitive.Clip != viewport {
				t.Fatalf("full-window scrim is clipped to %+v", primitive.Clip)
			}
			return
		}
	}
	t.Fatal("full-window scrim was not painted")
}

func TestSettingsAndShareDialogsCoverTitleBar(t *testing.T) {
	cases := []struct {
		name, section string
		open          func(*Panels) gfx.ID
	}{
		{"share", sectionShares, func(p *Panels) gfx.ID {
			p.shareDialog.open = true
			return p.ids.shareDialogDismiss
		}},
		{"reset data", sectionSettings, func(p *Panels) gfx.ID {
			p.resetDataOpen = true
			return p.ids.aboutResetDismiss
		}},
		{"password", sectionSettings, func(p *Panels) gfx.ID {
			p.settingsTab = panelSettingsSecurity
			p.settingsExtra.passwordOpen = true
			return p.settingsExtra.passwordDismiss
		}},
		{"font", sectionSettings, func(p *Panels) gfx.ID {
			p.settingsExtra.interfaceFont.open = true
			return p.settingsExtra.interfaceFont.dismiss
		}},
		{"shortcut", sectionSettings, func(p *Panels) gfx.ID {
			p.settingsTab = panelSettingsShortcuts
			p.beginShortcutEdit(0)
			return p.ids.shortcutDialogDismiss
		}},
	}
	for _, theme := range []string{"light", "dark"} {
		for _, tc := range cases {
			t.Run(theme+"/"+tc.name, func(t *testing.T) {
				state := shellTestState(t)
				state.Settings.Theme = theme
				state.Settings.LastSection = tc.section
				h := newShellTestHarnessAtSize(t, state, 1000, 700)
				dismissID := tc.open(&h.ui.panels)
				h.wake()
				if hit, ok := h.findHit(dismissID); !ok || hit.Bounds != (gfx.Rect{Width: 1000, Height: 700}) {
					t.Fatalf("scrim=%+v present=%v", hit.Bounds, ok)
				}
				requireFullWindowScrim(t, h)
				if err := h.driver.Click(500, 15); err != nil {
					t.Fatal(err)
				}
				if _, ok := h.findHit(dismissID); ok {
					t.Fatal("click on title-bar scrim did not dismiss dialog")
				}
			})
		}
	}
}

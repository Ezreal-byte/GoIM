package desktop

import (
	"github.com/vivaui/viva/gfx"
	"testing"
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

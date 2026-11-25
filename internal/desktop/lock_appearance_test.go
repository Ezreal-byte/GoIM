package desktop

import (
	"testing"

	"goim/internal/domain"
)

func TestLockScreenTracksThemeAndSharesTitlebar(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.HasLockPassword = true
	h := newShellTestHarness(t, state)
	u := h.ui
	pinBefore, ok := h.findHit(u.ids.mainPin)
	if !ok {
		t.Fatal("the main titlebar has no pin action")
	}

	state.Locked = true
	for _, mode := range []string{"light", "dark"} {
		for _, scheme := range domain.ColorSchemes() {
			state.Settings.Theme, state.Settings.ColorScheme = mode, scheme
			state.Revision++
			h.publish(state)
			h.settle()

			want := themeForSettings(state.Settings)
			lock := &u.lockUI
			if lock.theme.Background != paletteFor(want, scheme).chat ||
				lock.theme.Foreground != want.Foreground ||
				lock.theme.Primary != want.Primary || lock.theme.PrimaryForeground != want.PrimaryForeground {
				t.Fatalf("lock colors did not follow %s/%s", mode, scheme)
			}
			pin, ok := h.findHit(u.ids.mainPin)
			if !ok || pin.Bounds != pinBefore.Bounds || !h.hasHit(u.ids.titleLAN) {
				t.Fatal("the shared titlebar did not stay in place")
			}
			if h.hasHit(u.ids.navMessages) || h.hasHit(u.ids.navSettings) {
				t.Fatal("the locked workspace exposed navigation")
			}
			field, ok := h.findHit(lock.focus)
			if !ok || field.Bounds.Y < titlebarHeight || !h.hasText("GoIM 已锁定") {
				t.Fatal("the lock form was not below the titlebar")
			}
			snapshot := h.snapshot()
			for _, label := range snapshot.Texts {
				if label.Value == "GoIM" {
					t.Fatal("the locked titlebar repeated the GoIM wordmark")
				}
			}
			requireShellLayout(t, snapshot)
		}
	}
}

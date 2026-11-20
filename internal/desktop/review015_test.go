package desktop

import (
	"fmt"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestNavigationFitsAllSupportedFontSizesAndLanguages(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		for _, font := range []int{9, 14, 18, 28} {
			for _, theme := range []string{"light", "dark"} {
				t.Run(fmt.Sprintf("%s/%d/%s", language, font, theme), func(t *testing.T) {
					state := shellMinimalState(t)
					state.Settings.Language, state.Settings.Theme, state.Settings.InterfaceFontSize = language, theme, font
					h := newShellTestHarnessAtSize(t, state, 1000, 700)
					for _, problem := range vivaheadless.CheckLayout(h.snapshot()) {
						t.Errorf("layout %s for %q: %s", problem.Kind, problem.Text, problem.Detail)
					}
					hit, ok := h.findHit(h.ui.ids.navLock)
					if !ok || hit.Role != gfx.RoleButton || hit.Label != i18n.T("锁定应用") {
						t.Fatalf("lock action lost its accessible name: %+v", hit)
					}
				})
			}
		}
	}
}

func TestEmptyConversationHintUsesSelectedLanguage(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	state := shellMinimalState(t)
	state.Settings.Language = i18n.English
	h := newShellTestHarness(t, state)
	if !h.hasText("Chat over your LAN.") {
		t.Fatal("empty chat hint was not translated into English")
	}
}

func BenchmarkEmptyConversationSidebarPaint(b *testing.B) {
	previous := i18n.Language()
	b.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, font := range []int{14, 28} {
		b.Run(fmt.Sprint(font), func(b *testing.B) {
			settings := domain.DefaultSettings(b.TempDir())
			settings.LastSection, settings.Language, settings.InterfaceFontSize = "messages", i18n.English, font
			shell := New(newShellTestBackend(domain.State{
				Self: domain.Peer{ID: "self", Name: "Me", Online: true}, Settings: settings,
			}))
			defer shell.Close()
			config := shell.Config()
			config.Width, config.Height = 1000, 700
			driver, err := app.NewDriver(config, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer driver.Close()
			shell.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				shell.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestNavigationTypographyScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, theme := range []string{"light", "dark"} {
		for _, font := range []int{14, 18, 28} {
			t.Run(fmt.Sprintf("%s/%d", theme, font), func(t *testing.T) {
				state := shellMinimalState(t)
				state.Settings.Language, state.Settings.Theme, state.Settings.InterfaceFontSize = i18n.English, theme, font
				_, session := openShellQA(t, state, 1000, 700)
				if err := session.Expect("layout", "clean"); err != nil {
					t.Fatal(err)
				}
				shellQAShot(t, session, directory, fmt.Sprintf("navigation-en-%s-%d.png", theme, font))
			})
		}
	}
}

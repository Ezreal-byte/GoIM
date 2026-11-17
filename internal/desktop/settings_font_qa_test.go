package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestSettingsInterfaceFontScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.Theme = theme
			shell, session := openShellQA(t, state, 1200, 900)
			shellQAClick(t, session, shell.ids.navSettings)
			shellQAClick(t, session, shell.panels.settingsExtra.interfaceFont.trigger)
			var family string
			if err := session.WaitFor(func(snapshot app.Snapshot) bool {
				font := &shell.panels.settingsExtra.interfaceFont
				if font.known && len(font.families) > 0 {
					family = font.families[0]
					return true
				}
				return false
			}, 10*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("settings-interface-font-"+theme+".png"))

			var familyHit gfx.Hit
			for _, hit := range session.Snapshot().Hits {
				if hit.Label == family && hit.Role == gfx.RoleButton {
					familyHit = hit
					break
				}
			}
			if familyHit.ID.IsZero() {
				t.Fatalf("enumerable font %q is missing from the picker", family)
			}
			if err := session.Click(familyHit.Bounds.X+familyHit.Bounds.Width/2, familyHit.Bounds.Y+familyHit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			shellQAClick(t, session, shell.panels.settingsExtra.interfaceFont.confirm)
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("settings-interface-font-applied-"+theme+".png"))
		})
	}
}

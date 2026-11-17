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
			if err := session.WaitFor(func(snapshot app.Snapshot) bool {
				for _, text := range snapshot.Texts {
					if text.Value == "系统默认" {
						return true
					}
				}
				return false
			}, 5*time.Second); err != nil {
				t.Fatal(err)
			}
			if err := session.Expect("layout", "clean"); err != nil {
				t.Fatal(err)
			}
			shellQAShot(t, session, directory, filepath.Join("settings-interface-font-"+theme+".png"))

			var arial gfx.Hit
			for _, hit := range session.Snapshot().Hits {
				if hit.Label == "Arial" && hit.Role == gfx.RoleButton {
					arial = hit
					break
				}
			}
			if arial.ID.IsZero() {
				t.Fatal("Arial is missing from the installed font list")
			}
			if err := session.Click(arial.Bounds.X+arial.Bounds.Width/2, arial.Bounds.Y+arial.Bounds.Height/2); err != nil {
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

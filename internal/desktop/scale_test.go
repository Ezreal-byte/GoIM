package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// Exercises the real painter and layout at desktop DPI scales. The fixture is
// deliberately deterministic; independent-process communication is tested by
// smoke-headless.ps1 rather than inferred from these visual states.
func TestDesktopScaleMatrix(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		for _, scale := range []float64{1, 1.5, 2} {
			for _, size := range [][2]int{{1440, 960}, {1000, 700}} {
				name := fmt.Sprintf("%s-%g-%dx%d", theme, scale, size[0], size[1])
				t.Run(name, func(t *testing.T) {
					state := shellTestState(t)
					state.Settings.Theme = theme
					shell := New(newShellTestBackend(state))
					defer shell.Close()
					config := shell.Config()
					config.Width = size[0]
					config.Height = size[1]
					session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: scale})
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					click := func(id ui.ID) {
						t.Helper()
						for _, h := range session.Snapshot().Hits {
							if h.ID == id {
								if err := session.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
									t.Fatal(err)
								}
								return
							}
						}
						t.Fatalf("missing control %v", id)
					}
					check := func(view string) {
						t.Helper()
						if err := session.Wait(700 * time.Millisecond); err != nil {
							t.Fatal(err)
						}
						requireShellLayout(t, session.Snapshot())
						if dir := os.Getenv("GOIM_QA_SCREENSHOTS"); dir != "" {
							if err := os.MkdirAll(dir, 0755); err != nil {
								t.Fatal(err)
							}
							if err := session.Screenshot(filepath.Join(dir, name+"-"+view+".png")); err != nil {
								t.Fatal(err)
							}
						}
					}
					check("chat")
					click(shell.ids.chatHeaderMembers)
					check("members")
					if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
						t.Fatal(err)
					}
					click(shell.ids.navSettings)
					check("settings")
				})
			}
		}
	}
}

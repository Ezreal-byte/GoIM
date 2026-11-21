package desktop

import (
	"math"
	"os"
	"testing"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestLargeTypographyInTransferAndShareTables(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			state := transferDesignState(t.TempDir())
			state.Files = filePageSampleState(t).Files
			state.Settings.Language = language
			state.Settings.InterfaceFontSize = domain.InterfaceFontSizeMaximum
			shell, session := openShellQA(t, state, 1000, 700)
			for _, section := range []string{sectionTransfers, sectionShares} {
				id, heading := shell.ids.navTransfers, "文件传输"
				if section == sectionShares {
					id, heading = shell.ids.navShares, "文件共享"
				}
				shellQAClick(t, session, id)
				snapshot := session.Snapshot()
				requireShellLayout(t, snapshot)
				if section == sectionShares {
					refresh := shellQAHit(t, session, shell.panels.ids.sharesRefresh)
					if refresh.Bounds.Right() > 1000 {
						t.Errorf("refresh button extends past the right edge: %+v", refresh.Bounds)
					}
				}
				found := false
				for _, label := range snapshot.Texts {
					if label.Value == i18n.T(heading) && label.Style.Size == shell.theme.Metrics.ScaledText(29) {
						found = true
					}
				}
				if !found {
					t.Errorf("%s heading did not fit at the selected font size", section)
				}
				if dir := os.Getenv("GOIM_QA_SCREENSHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					shellQAShot(t, session, dir, section+"-"+language+"-28.png")
				}
			}
		})
	}
}

func TestLargeTypographyMessageLineHeight(t *testing.T) {
	state := shellTestState(t)
	state.Settings.InterfaceFontSize = domain.InterfaceFontSizeMaximum
	for i := range state.Messages {
		if state.Messages[i].ID == "message-1" {
			state.Messages[i].Text = "First line\nSecond line"
		}
	}
	h := newShellTestHarness(t, state)
	lines := map[string]float32{}
	for _, label := range h.snapshot().Texts {
		if label.Value == "First line" || label.Value == "Second line" {
			lines[label.Value] = label.Bounds.Y
			if label.Style.Size != float32(domain.InterfaceFontSizeMaximum) {
				t.Errorf("message body size = %v", label.Style.Size)
			}
		}
	}
	if len(lines) != 2 || math.Abs(float64(lines["Second line"]-lines["First line"]-42)) > 1 {
		t.Errorf("expected two message lines 42 logical pixels apart, got %v", lines)
	}
}

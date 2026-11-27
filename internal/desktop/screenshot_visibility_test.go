package desktop

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
)

type screenshotTestWindow struct {
	hidden, minimized, closed bool
	shows                     int
	name                      string
	order                     *[]string
}

func (w *screenshotTestWindow) Hide() { w.hidden = true }
func (w *screenshotTestWindow) Show() {
	w.hidden = false
	w.shows++
	*w.order = append(*w.order, w.name)
}
func (w *screenshotTestWindow) Hidden() bool    { return w.hidden }
func (w *screenshotTestWindow) Minimized() bool { return w.minimized }
func (w *screenshotTestWindow) Closed() bool    { return w.closed }

func TestScreenshotRestoresOnlyPreviouslyVisibleAndStillAllowedWindows(t *testing.T) {
	for _, outcome := range []string{"completed", "cancelled", "failed", "locked", "exiting"} {
		t.Run(outcome, func(t *testing.T) {
			var order []string
			main := &screenshotTestWindow{name: "main", order: &order}
			chat := &screenshotTestWindow{name: "chat", order: &order}
			closed := &screenshotTestWindow{name: "closed", order: &order}
			hidden := &screenshotTestWindow{hidden: true, name: "hidden", order: &order}
			minimized := &screenshotTestWindow{minimized: true, name: "minimized", order: &order}
			windows := hideScreenshotWindows([]screenshotWindow{main, chat, closed, hidden, minimized})
			if len(windows) != 3 || !main.hidden || !chat.hidden {
				t.Fatal("did not hide visible windows")
			}
			closed.closed = true
			restoreScreenshotWindows(windows, chat, func(w screenshotWindow) bool { return outcome != "exiting" && (outcome != "locked" || w == main) })
			if hidden.shows != 0 || minimized.shows != 0 || closed.shows != 0 {
				t.Fatal("restored an initially hidden, minimized, or closed window")
			}
			if outcome == "exiting" {
				if len(order) != 0 {
					t.Fatal("restored windows after exit")
				}
			} else if outcome == "locked" {
				if chat.shows != 0 || main.shows != 1 {
					t.Fatal("exposed chat while locked")
				}
			} else if len(order) != 2 || order[1] != "chat" {
				t.Fatal("originating chat did not regain focus last")
			}
		})
	}
}

func TestScreenshotButtonAndMenuFollowOppositeWindowPolicies(t *testing.T) {
	for _, keep := range []bool{false, true} {
		state := shellTestState(t)
		state.Settings.KeepWindowOnScreenshot = keep
		h := newShellTestHarness(t, state)
		h.ui.selectConversation("conv-alice")
		var requests []bool
		h.ui.requestScreenshot = func(keep bool) { requests = append(requests, keep) }
		h.settle()
		h.clickID(h.ui.ids.chatScreenshot)
		h.clickID(h.ui.screenshotMenuID)
		label := "保留窗口截图"
		if keep {
			label = "隐藏窗口截图"
		}
		h.clickLabel(label)
		if len(requests) != 2 || requests[0] != keep || requests[1] == keep {
			t.Fatalf("incorrect screenshot policies: %v", requests)
		}
		h.clickID(h.ui.screenshotMenuID)
		for _, hit := range h.snapshot().Hits {
			if hit.Label == "设置" && hit.Role == gfx.RoleMenuItem {
				h.clickHit(hit)
				break
			}
		}
		if h.ui.section != sectionSettings || h.ui.panels.settingsTab != panelSettingsGeneral {
			t.Fatal("menu did not open general settings")
		}
	}
}

func TestScreenshotWindowSettingIsSearchableAndSaves(t *testing.T) {
	h := panelTestNewHarnessBounds(t, panelModuleSettings, panelTestDefaultState(t), ui.Rect{Width: 1040, Height: 600})
	h.typeInto(h.panels.ids.settingsSearch, "截图")
	h.clickLabelRole("点击截图按钮时保留当前窗口", gfx.RoleButton)
	if err := h.driver.Advance(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := h.driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	// Let newly visible text finish measuring before the pointer press.
	hit, ok := h.findHit(h.panels.ids.settingsScreenshot)
	if !ok {
		t.Fatal("search did not reveal the setting")
	}
	if err := h.driver.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	h.click(h.panels.ids.settingsScreenshot)
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "keep_window_on_screenshot" || !command.Bool {
		t.Fatalf("incorrect setting: %+v", command)
	}
}

//go:build vivahead

package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestChatPolishQA(t *testing.T) {
	dir := os.Getenv("GOIM_CHAT_POLISH_QA")
	if dir == "" {
		t.Skip("set GOIM_CHAT_POLISH_QA for screenshots")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	state := promoState(t)
	state.Settings.Theme = "dark"
	state.Settings.RecentEmoji = []string{"🌹", "😂", "🤭", "🥺", "😺", "🤔", "😎", "🥰", "😁", "🙏"}
	u := New(newShellTestBackend(state))
	defer u.Close()
	cfg := u.Config()
	cfg.Width, cfg.Height = 1440, 850
	session, err := headless.Open(cfg, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	u.invalidator.Invalidate()
	path := state.File("poster").SourcePath
	if err := session.WaitFor(func(_ app.Snapshot) bool { return bitmapReady(u.fileBitmap(path)) }, 4*time.Second); err != nil {
		t.Fatal(err)
	}
	point := func(id gfx.ID) (float32, float32) {
		t.Helper()
		hit, ok := findSessionHit(session.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing hit %v", id)
		}
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	click := func(id gfx.ID) {
		t.Helper()
		x, y := point(id)
		if err := session.Click(x, y); err != nil {
			t.Fatal(err)
		}
	}
	shot := func(name string) {
		t.Helper()
		if err := session.Move(720, 22); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(500 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	click(u.ids.navMessages)
	click(u.convRowIDs["north"])
	shot("composer-empty")
	click(u.ids.chatEditorField)
	if err := session.Type("哈"); err != nil {
		t.Fatal(err)
	}
	click(u.ids.chatEmoji)
	click(u.emojiIDs["star-eyes"])
	click(u.ids.chatEmoji)
	click(u.emojiIDs["kiss"])
	if err := session.Type("哈哈"); err != nil {
		t.Fatal(err)
	}
	shot("composer-focused")
	click(u.ids.sidebarSearch)
	shot("composer-blurred")

	screenshotHit, ok := findSessionHit(session.Snapshot().Hits, u.ids.chatScreenshot)
	if !ok {
		t.Fatal("missing screenshot composer action")
	}
	if err := session.Move(
		screenshotHit.Bounds.X+screenshotHit.Bounds.Width/2,
		screenshotHit.Bounds.Y+screenshotHit.Bounds.Height/2,
	); err != nil {
		t.Fatal(err)
	}
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(dir, "composer-screenshot-hover.png")); err != nil {
		t.Fatal(err)
	}
	if err := session.Move(720, 22); err != nil {
		t.Fatal(err)
	}
	click(u.ids.chatEmoji)
	if err := session.Expect("text", "所有表情"); err != nil {
		t.Fatal(err)
	}
	shot("emoji-picker")
	firstVisibleEmoji := func() int {
		hits := session.Snapshot().Hits
		for index, emoji := range emojiChoices {
			if hit, ok := findSessionHit(hits, u.emojiIDs[emoji.Name]); ok &&
				hit.Bounds.Height > 20 && hit.Bounds.Y >= u.emojiPanel.Y+12 {
				return index
			}
		}
		t.Fatal("no emoji is visible")
		return 0
	}
	before := firstVisibleEmoji()
	if err := session.Move(u.emojiPanel.X+24, u.emojiPanel.Y+76); err != nil {
		t.Fatal(err)
	}
	if err := session.Scroll(gfx.ScrollEvent{X: u.emojiPanel.X + 24, Y: u.emojiPanel.Y + 76, DeltaY: 230}); err != nil {
		t.Fatal(err)
	}
	scrolled := firstVisibleEmoji()
	if scrolled <= before {
		t.Fatal("scroll did not move the emoji grid")
	}
	var scrollbar gfx.ID
	for _, hit := range session.Snapshot().Hits {
		if hit.Role == gfx.RoleSeparator && hit.Label == "Vertical scrollbar" {
			scrollbar = hit.ID
			break
		}
	}
	scrollX, scrollY := point(scrollbar)
	if err := session.Press(scrollX, scrollY); err != nil {
		t.Fatal(err)
	}
	if err := session.Move(scrollX, scrollY-120); err != nil {
		t.Fatal(err)
	}
	if err := session.Release(scrollX, scrollY-120); err != nil {
		t.Fatal(err)
	}
	if got := firstVisibleEmoji(); got >= scrolled {
		t.Fatalf("drag did not move the emoji grid up: %v -> %v", scrolled, got)
	}
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}

	click(u.ids.profileAvatar)
	click(u.ids.profilePresence.At(2))
	shot("profile-status")
	if err := session.Keys("esc"); err != nil {
		t.Fatal(err)
	}

	x, y := point(u.fileButtonIDs["checklist"])
	if err := session.RightClick(x, y); err != nil {
		t.Fatal(err)
	}
	shot("message-actions")
}

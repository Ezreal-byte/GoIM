//go:build vivahead

package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
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
	state.Settings.RecentEmoji = []string{"🌹", "😂", "🤭", "🥺", "😺", "🤔", "😎", "🥰", "😁", "🙏"}
	state.Notice = "已保存"
	u := New(newShellTestBackend(state))
	defer u.Close()
	cfg := u.Config()
	cfg.Width, cfg.Height = 1440, 980
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
	point := func(id ui.ID) (float32, float32) {
		t.Helper()
		hit, ok := findSessionHit(session.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing hit %v", id)
		}
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	click := func(id ui.ID) {
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
	click(u.ids.chatEmoji)
	if err := session.Expect("text", "所有表情"); err != nil {
		t.Fatal(err)
	}
	shot("emoji-picker")
	before := u.emojiView.ScrollState(u.ids.emojiScroll).Offset
	if err := session.Scroll(ui.ScrollEvent{X: u.emojiPanel.X + 24, Y: u.emojiPanel.Y + 76, DeltaY: 230}); err != nil {
		t.Fatal(err)
	}
	if after := u.emojiView.ScrollState(u.ids.emojiScroll).Offset; after == before {
		t.Fatal("scroll did not move the emoji grid")
	}
	scrolled := u.emojiView.ScrollState(u.ids.emojiScroll).Offset
	scrollX, scrollY := point(u.ids.emojiScrollbar)
	if err := session.Press(scrollX, scrollY); err != nil {
		t.Fatal(err)
	}
	if err := session.Move(scrollX, scrollY-120); err != nil {
		t.Fatal(err)
	}
	if err := session.Release(scrollX, scrollY-120); err != nil {
		t.Fatal(err)
	}
	if got := u.emojiView.ScrollState(u.ids.emojiScroll).Offset; got >= scrolled {
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

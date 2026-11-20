package desktop

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestCopyPartOfSoftWrappedMessage(t *testing.T) {
	state := shellTestState(t)
	value := "开头🙂" + strings.Repeat("产品设计 alpha beta   工程团队 ", 8) + "结束"
	state.Messages = state.Messages[:1]
	state.Messages[0].Text = value
	h := newShellTestHarness(t, state)
	hit, ok := h.findHit(h.ui.messageTextIDs["message-1"])
	if !ok {
		t.Fatal("missing message paragraph")
	}
	var lines []gfx.Text
	for _, label := range h.snapshot().Texts {
		if pointInside(label.Bounds.X+1, label.Bounds.Y+1, hit.Bounds) {
			lines = append(lines, label)
		}
	}
	if len(lines) < 2 {
		t.Fatal("message did not soft-wrap")
	}
	c := h.ui.chatPage.View.Frame.Canvas()
	a, b := lines[0], lines[1]
	start := len("开头")
	_, firstRune := utf8.DecodeRuneInString(b.Value)
	firstEnd := len(a.Value)
	secondStart := firstEnd + strings.Index(value[firstEnd:], b.Value)
	want := value[start : secondStart+firstRune]
	fromX := a.Bounds.X + c.TextCaretStyle(a.Value, a.Style, start)
	toX := b.Bounds.X + c.TextCaretStyle(b.Value, b.Style, firstRune)
	if err := h.driver.Press(fromX, a.Bounds.Y+2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(toX, b.Bounds.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Release(toX, b.Bounds.Y+2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if got := h.ui.textSelection.selected(); got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
	// A right click elsewhere in the same message switches Copy back to
	// the whole message.
	if err := h.driver.ClickButton(a.Bounds.X+1, a.Bounds.Y+2, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.ids.messageMenuCopy)
	if got := h.driver.Clipboard(); got != value {
		t.Fatalf("copied stale selection %q", got)
	}
}

func TestMessageTextSelectionAcrossLines(t *testing.T) {
	for _, window := range []string{"main", "detached"} {
		t.Run(window, func(t *testing.T) {
			state := shellTestState(t)
			state.Messages[0].Text = "第一行  \r\n第二行🙂\n第三行"
			var h *shellTestHarness
			if window == "main" {
				h = newShellTestHarness(t, state)
			} else {
				backend := newShellTestBackend(state)
				u := newDetachedConversationView(backend, "conv-group")
				t.Cleanup(u.Close)
				d, err := app.NewDriver(u.DetachedConfig(), nil)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(d.Close)
				h = &shellTestHarness{t: t, ui: u, backend: backend, driver: d}
			}
			hit, ok := h.findHit(h.ui.messageTextIDs["message-1"])
			if !ok {
				t.Fatal("missing text hit")
			}
			// Select in reverse, from the end of the second line to the start of
			// the first. The original CRLF, trailing spaces, and emoji survive.
			r := hit.Bounds
			x, y := r.X+r.Width-0.1, r.Y+r.Height/2
			if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if err := h.driver.Move(r.X+0.1, r.Y+2); err != nil {
				t.Fatal(err)
			}
			if err := h.driver.Release(r.X+0.1, r.Y+2, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			want := "第一行  \r\n第二行🙂"
			if got := h.ui.textSelection.selected(); got != want {
				t.Fatalf("selected %q, want %q", got, want)
			}
			for _, modifier := range []gfx.Modifiers{gfx.ModifierControl, gfx.ModifierMeta} {
				handled, err := h.driver.ShortcutChord(modifier, 'c')
				if err != nil || !handled || h.driver.Clipboard() != want {
					t.Fatalf("keyboard copy: handled=%v err=%v text=%q", handled, err, h.driver.Clipboard())
				}
			}
			if err := h.driver.ClickButton(r.X+2, r.Y+2, gfx.PointerButtonSecondary); err != nil {
				t.Fatal(err)
			}
			h.clickID(h.ui.ids.messageMenuCopy)
			if got := h.driver.Clipboard(); got != want {
				t.Fatalf("context menu copied %q", got)
			}
		})
	}
}

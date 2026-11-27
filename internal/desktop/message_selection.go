package desktop

import (
	"strings"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type messageTextSelection struct {
	id         gfx.ID
	text       string
	rangeState base.TextSelection
	spans      []gfx.TextSpan
}

func (s *messageTextSelection) clear() {
	s.id, s.text = gfx.ID{}, ""
	s.rangeState.Clear()
}

func (s *messageTextSelection) selected() string {
	a, b := s.rangeState.Anchor.Offset, s.rangeState.Focus.Offset
	if min(a, b) < 0 || max(a, b) > len(s.text) {
		return ""
	}
	return s.text[min(a, b):max(a, b)]
}

func (u *UI) messageParagraph(f ui.C, m domain.Message, limit int, size, height float32) {
	id := u.messageTextIDs[m.ID]
	ui.Paragraph(f, m.Text, limit).FontSize(textSize(f, size)).LineHeight(textSize(f, height)).
		HitWith(id, m.Text, gfx.HitOptions{Role: gfx.RoleGroup, Cursor: gfx.PointerCursorText, ReadOnly: true, SkipTab: true}).
		Draw(u.drawTextSelection)
	u.messageContextPointer(f.Canvas(), id, m)
}

// A wrapped line omits whitespace at a soft break, the newline at a hard
// break, and text replaced by a final ellipsis. Keep selections as offsets in
// the original message so copying across lines preserves that whitespace.
func messageLineStart(text string, previousEnd int) int {
	i := previousEnd
	for i < len(text) && text[i] == ' ' {
		i++
	}
	if strings.HasPrefix(text[i:], "\r\n") {
		return i + 2
	}
	if strings.HasPrefix(text[i:], "\n") {
		return i + 1
	}
	return i
}

func (u *UI) paintMessageSelection(d *ui.DrawContext) {
	n, c := d.Node, d.Canvas
	s := &u.textSelection
	event, input := c.PointerOn(n.ID)
	if !input && (s.id != n.ID || s.rangeState.Empty()) {
		return
	}
	if n.LineCount == 0 || n.LineH <= 0 {
		return
	}
	if input && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
		s.id, s.text = n.ID, n.Text
		c.RequestFocus(n.ID)
	}
	if s.id != n.ID || s.text != n.Text {
		return
	}
	lines := d.Frame.Lines[n.LineFirst : n.LineFirst+n.LineCount]
	// The paragraph has just painted these labels. Their bounds and strings
	// include alignment and any directional marks inserted by the text engine.
	labels := c.Texts()
	if len(labels) < len(lines) {
		return
	}
	labels = labels[len(labels)-len(lines):]
	pointerLine := min(max(int((event.Y-d.Rect.Y)/n.LineH), 0), len(lines)-1)
	var offset int
	for i, line := range lines {
		if i != 0 {
			offset = messageLineStart(n.Text, offset)
		}
		visible := line
		if !strings.HasPrefix(n.Text[offset:], line) {
			visible = strings.TrimSuffix(line, "…")
		}
		end := min(offset+len(visible), len(n.Text))
		label := labels[i]
		markBytes := len(label.Value) - len(line)
		if markBytes < 0 {
			return
		}
		if input && i == pointerLine {
			byteAt := c.TextOffsetAtStyle(label.Value, label.Style, event.X-label.Bounds.X)
			point := base.TextPoint{Offset: offset + min(max(byteAt-markBytes, 0), end-offset)}
			if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
				a, b := s.rangeState.Anchor.Offset, s.rangeState.Focus.Offset
				if point.Offset < min(a, b) || point.Offset > max(a, b) {
					s.rangeState.Clear()
				}
			}
			if s.rangeState.Pointer(event, func(gfx.Point) (base.TextPoint, bool) { return point, true }).Handled {
				c.Consume()
			}
		}
		a, b := s.rangeState.Anchor.Offset, s.rangeState.Focus.Offset
		start, stop := max(min(a, b), offset), min(max(a, b), end)
		if start < stop {
			s.spans = c.TextSelectionStyle(s.spans[:0], label.Value, label.Style, start-offset+markBytes, stop-offset+markBytes)
			tint := d.Theme.Selection
			tint.Alpha *= 0.38
			for _, span := range s.spans {
				c.Fill(gfx.Rect{X: label.Bounds.X + span.X, Y: label.Bounds.Y, Width: span.Width, Height: label.Bounds.Height}, tint)
			}
		}
		offset = end
	}
}

package desktop

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	radiusControl          = float32(8)
	radiusCard             = float32(8)
	dialogActionButtonSize = components.ButtonSizeLarge
	dialogActionMinWidth   = float32(88)
	dialogActionHeight     = float32(40)
)

// namedButton keeps the ID shared with a focus request or a shell action.
func namedButton(c ui.C, id gfx.ID, label string) ui.Box {
	return ui.Button(c, label).Hit(id, label)
}

func fontToken(theme gfx.Theme, size float32) float32 {
	return size / theme.Metrics.EffectiveTextScale()
}

func (u *UI) railExtent() float32 { return max(railWidth, 56*u.theme.Metrics.EffectiveTextScale()) }

func (u *UI) textExtent(base, extra float32) float32 {
	return base + extra*max(u.theme.Metrics.EffectiveTextScale()-1, 0)
}

func dialogActionButton(c ui.C, id gfx.ID, label string) ui.Box {
	return namedButton(c, id, label).Height(dialogActionHeight).MinWidth(dialogActionMinWidth)
}

// paintDialogActions gives every modal the same comfortable target size. Viva's
// shared button row is intentionally compact; dialog decisions need a wider,
// easier-to-hit footer than card actions.
func paintDialogActions(canvas *gfx.Canvas, theme gfx.Theme, footer gfx.Rect, actions ...components.ButtonAction) {
	if canvas == nil || len(actions) == 0 || footer.Width <= 0 || footer.Height <= 0 {
		return
	}
	widths := make([]float32, len(actions))
	var total float32
	gap := theme.Metrics.GapItem
	for i, action := range actions {
		button := components.BeginButton(canvas, theme, gfx.ID{}, gfx.Rect{}).
			Size(dialogActionButtonSize).Height(dialogActionHeight).SizeToFit(action.Label)
		widths[i] = max(dialogActionMinWidth, button.BoundsWidth())
		total += widths[i]
	}
	total += gap * float32(len(actions)-1)
	scale := min(float32(1), footer.Width/total)
	x := footer.X + footer.Width - total*scale
	for i, action := range actions {
		width := widths[i] * scale
		button := components.BeginButton(canvas, theme, action.ID, gfx.Rect{
			X: x, Y: footer.Y + (footer.Height-dialogActionHeight)/2,
			Width: width, Height: dialogActionHeight,
		}).Size(dialogActionButtonSize).Height(dialogActionHeight)
		if action.Disabled {
			button = button.Disabled()
		}
		switch action.Variant {
		case components.ButtonDestructive:
			button.Destructive(action.Label)
		case components.ButtonOutline:
			button.Outline(action.Label)
		case components.ButtonSecondary:
			button.Secondary(action.Label)
		case components.ButtonGhost:
			button.Ghost(action.Label)
		case components.ButtonLink:
			button.Link(action.Label)
		case components.ButtonDestructiveOutline:
			button.DestructiveOutline(action.Label)
		default:
			button.Default(action.Label)
		}
		x += width + gap
	}
}

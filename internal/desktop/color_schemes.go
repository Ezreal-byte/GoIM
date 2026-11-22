package desktop

import (
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

type schemeTones struct {
	background, surface, ink, mutedInk, primary, selection ui.Color
}

// Light and dark have independent surface and accent values so that text,
// primary controls and selected conversations retain their contrast in both.
var schemeColors = map[string]struct{ light, dark schemeTones }{
	"pine": {
		light: schemeTones{ui.RGB(238, 246, 241), ui.RGB(251, 254, 251), ui.RGB(26, 52, 44), ui.RGB(75, 101, 90), ui.RGB(18, 104, 79), ui.RGB(199, 232, 214)},
		dark:  schemeTones{ui.RGB(22, 38, 34), ui.RGB(33, 52, 45), ui.RGB(225, 240, 230), ui.RGB(165, 191, 175), ui.RGB(120, 212, 169), ui.RGB(43, 79, 62)},
	},
	"clay": {
		light: schemeTones{ui.RGB(250, 242, 232), ui.RGB(255, 251, 245), ui.RGB(64, 44, 38), ui.RGB(116, 89, 74), ui.RGB(148, 65, 42), ui.RGB(246, 218, 196)},
		dark:  schemeTones{ui.RGB(43, 32, 29), ui.RGB(58, 44, 38), ui.RGB(248, 231, 214), ui.RGB(202, 176, 156), ui.RGB(241, 164, 126), ui.RGB(92, 59, 46)},
	},
	"iris": {
		light: schemeTones{ui.RGB(244, 241, 250), ui.RGB(253, 251, 255), ui.RGB(48, 40, 68), ui.RGB(100, 89, 118), ui.RGB(105, 74, 153), ui.RGB(228, 216, 248)},
		dark:  schemeTones{ui.RGB(32, 29, 43), ui.RGB(46, 41, 60), ui.RGB(238, 232, 249), ui.RGB(186, 174, 205), ui.RGB(202, 176, 246), ui.RGB(72, 57, 94)},
	},
}

func schemeLabel(scheme string) string {
	switch scheme {
	case "pine":
		return i18n.T("松青")
	case "clay":
		return i18n.T("陶土")
	case "iris":
		return i18n.T("鸢尾")
	default:
		return i18n.T("经典")
	}
}

func (c schemeTones) apply(t *ui.Theme) {
	t.Background, t.Card, t.Popover = c.background, c.surface, c.surface
	t.Foreground, t.CardForeground, t.PopoverForeground = c.ink, c.ink, c.ink
	t.MutedForeground = c.mutedInk
	t.Primary, t.Ring = c.primary, c.primary
	t.PrimaryForeground = c.surface
	if c.background.Red < 0.5 {
		t.PrimaryForeground = c.background
	}
	t.Selection, t.SelectionForeground = c.selection, c.ink
	t.CardElevated = c.surface.Mix(c.primary, .06)
	t.Hover = c.surface.Mix(c.primary, .11)
	t.Secondary, t.Muted, t.Accent = t.CardElevated, t.CardElevated, t.Hover
	t.SecondaryForeground, t.AccentForeground = c.ink, c.ink
	t.Border = c.surface.Mix(c.ink, .16)
	t.Input = t.Border
}

func colorSchemePalette(t ui.Theme) palette {
	rail := t.Background.Mix(t.Foreground, .09)
	incoming := t.Card
	if t.Background.Red < .5 {
		rail = t.Background.Mix(ui.RGB(0, 0, 0), .2)
		incoming = t.CardElevated
	}
	return palette{
		rail: rail, sidebar: t.Background, chat: t.Background.Mix(t.Card, .72), header: t.Card,
		border: t.Border, hover: t.Hover, selected: t.Selection, selectedText: t.Primary,
		icon: t.MutedForeground, secondaryText: t.MutedForeground,
		mutedAvatar: t.CardElevated, avatarText: t.Foreground,
		incoming: incoming, own: t.Selection, bubbleBorder: t.Foreground.WithAlpha(.09),
		fileCard: t.CardElevated, fileBorder: t.Border, invite: t.Popover,
		success: t.Success, danger: t.Danger,
	}
}

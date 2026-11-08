package desktop

import "github.com/vivaui/viva/ui"

// These are GoIM's own classic yellow faces. The picker does not ask the OS
// emoji font to draw them. Unicode remains on the wire for older GoIM clients.
type emojiChoice struct {
	Name, Label, Value string
	Art                ui.Icon
}

var emojiChoices = makeChatEmoji()

func emojiArt(value string) (ui.Icon, bool) {
	for _, choice := range emojiChoices {
		if value == choice.Value {
			return choice.Art, true
		}
	}
	return ui.Icon{}, false
}

func makeChatEmoji() []emojiChoice {
	specs := [...]struct{ name, label, value, eyes, mouth, accent string }{
		{"grin", "龇牙", "😀", "dot", "teeth", ""},
		{"smile", "开心", "😃", "wide", "laugh", ""},
		{"laugh", "大笑", "😄", "closed", "teeth", ""},
		{"beaming", "得意", "😁", "closed", "teeth", "cheeks"},
		{"sweat-smile", "尴尬", "😅", "closed", "laugh", "sweat"},
		{"joy", "笑哭", "😂", "closed", "laugh", "tears"},
		{"slightly-smile", "微笑", "🙂", "dot", "smile", ""},
		{"wink", "眨眼", "😉", "wink", "smile", "cheeks"},
		{"blush", "害羞", "😊", "closed", "smile", "cheeks"},
		{"heart-eyes", "喜欢", "😍", "heart", "laugh", ""},
		{"star-eyes", "崇拜", "🤩", "star", "teeth", ""},
		{"kiss", "飞吻", "😘", "wink", "kiss", "heart"},
		{"cool", "酷", "😎", "glasses", "smile", ""},
		{"thinking", "思考", "🤔", "thinking", "aside", "hand"},
		{"neutral", "无语", "😐", "dot", "flat", ""},
		{"sleep", "困了", "😴", "sleep", "small", "sleep"},
		{"cry", "委屈", "😢", "sad", "sad", "tear"},
		{"sob", "大哭", "😭", "sleep", "laugh", "tears"},
		{"angry", "生气", "😠", "angry", "sad", "warm"},
		{"mind-blown", "震惊", "🤯", "wide", "open", "spark"},
		{"party", "庆祝", "🥳", "wink", "kiss", "party"},
		{"surprise", "惊讶", "😮", "wide", "open", ""},
		{"tongue", "调皮", "😛", "dot", "tongue", ""},
		{"squint", "偷笑", "😆", "closed", "laugh", "cheeks"},
	}
	out := make([]emojiChoice, 0, len(specs))
	for _, s := range specs {
		out = append(out, emojiChoice{s.name, s.label, s.value, classicFace(s.eyes, s.mouth, s.accent)})
	}
	return out
}

func classicFace(eyes, mouth, accent string) ui.Icon {
	ink, blush, tear := ui.RGB(88, 43, 32), ui.RGB(251, 119, 112), ui.RGB(63, 159, 243)
	light, shade := ui.RGB(255, 238, 151), ui.RGB(251, 170, 49)
	if accent == "warm" {
		light, shade = ui.RGB(255, 180, 118), ui.RGB(247, 109, 66)
	}
	icon := ui.Icon{ViewBox: 32}
	layer := func(fill ui.Color, path ...ui.PathCommand) {
		icon.Layers = append(icon.Layers, ui.IconLayer{Fill: fill, Commands: path})
	}
	oval := func(fill ui.Color, x, y, rx, ry float32) {
		const k = 0.55228475
		layer(fill, ui.MoveTo(x+rx, y), ui.CubicTo(x+rx, y+k*ry, x+k*rx, y+ry, x, y+ry),
			ui.CubicTo(x-k*rx, y+ry, x-rx, y+k*ry, x-rx, y),
			ui.CubicTo(x-rx, y-k*ry, x-k*rx, y-ry, x, y-ry),
			ui.CubicTo(x+k*rx, y-ry, x+rx, y-k*ry, x+rx, y), ui.ClosePath())
	}
	wedge := func(fill ui.Color, ax, ay, bx, by, cx, cy float32) {
		layer(fill, ui.MoveTo(ax, ay), ui.LineTo(bx, by), ui.LineTo(cx, cy), ui.ClosePath())
	}
	heart := func(x, y float32) {
		layer(ui.RGB(239, 61, 103), ui.MoveTo(x, y+3), ui.CubicTo(x-10, y-2, x-4, y-7, x, y-3),
			ui.CubicTo(x+4, y-7, x+10, y-2, x, y+3), ui.ClosePath())
	}
	oval(ui.RGB(186, 98, 28), 16, 16, 14.1, 14.1)
	oval(shade, 16, 15.7, 13.3, 13.3)
	icon.Layers[len(icon.Layers)-1].Gradient = &ui.LinearGradient{Start: ui.Point{Y: 3}, End: ui.Point{Y: 29}, From: light, To: shade}
	if accent == "cheeks" {
		oval(blush, 8.1, 18.2, 2.3, 1.3)
		oval(blush, 23.9, 18.2, 2.3, 1.3)
	}
	for i, x := range [...]float32{11.2, 20.8} {
		switch {
		case eyes == "heart":
			heart(x, 13)
		case eyes == "star":
			wedge(ink, x, 8.6, x+3.5, 15.5, x-3.5, 15.5)
			wedge(ink, x, 16.5, x-3.8, 10.9, x+3.8, 10.9)
		case eyes == "glasses":
			oval(ui.RGB(29, 45, 65), x, 13, 4.2, 3.3)
			wedge(ui.RGB(120, 159, 209), x-2.4, 11.5, x+1, 11, x-1, 14)
		case eyes == "closed" || eyes == "wink" && i == 1 || eyes == "sleep":
			direction := float32(1)
			if eyes == "sleep" {
				direction = -1
			}
			layer(ink, ui.MoveTo(x-3.1, 14), ui.QuadTo(x, 14-7*direction, x+3.1, 14),
				ui.QuadTo(x, 14-3.6*direction, x-3.1, 14), ui.ClosePath())
		default:
			rx, ry := float32(1.45), float32(2.05)
			if eyes == "wide" {
				oval(ui.RGB(255, 252, 243), x, 13.1, 3, 3.6)
				rx, ry = 1.5, 2.15
			}
			oval(ink, x, 13.1, rx, ry)
			if eyes == "sad" || eyes == "thinking" || eyes == "angry" {
				y := float32(8.8)
				if i == 1 {
					y = 10.3
				}
				wedge(ink, x-2.8, y, x+2.7, 10.8, x-2.5, y+1.7)
			}
		}
	}
	switch mouth {
	case "teeth", "laugh", "tongue":
		layer(ink, ui.MoveTo(8.2, 17.2), ui.QuadTo(16, 19.1, 23.8, 17.2),
			ui.CubicTo(23, 27, 9, 27, 8.2, 17.2), ui.ClosePath())
		if mouth == "teeth" {
			layer(ui.RGB(255, 252, 242), ui.MoveTo(9.2, 18), ui.QuadTo(16, 19.5, 22.8, 18),
				ui.LineTo(21.6, 20.6), ui.QuadTo(16, 22, 10.4, 20.6), ui.ClosePath())
		}
		if mouth == "tongue" {
			oval(blush, 16, 23.6, 3.6, 2.6)
		} else if mouth == "laugh" {
			oval(blush, 16, 23.3, 2.9, 1.15)
		}
	case "small", "open", "kiss":
		rx, ry := float32(2.1), float32(2.25)
		if mouth == "open" {
			rx, ry = 3.1, 4
		}
		oval(ink, 16, 21, rx, ry)
	case "flat", "aside":
		oval(ink, 16, 20.8, 3.2, 0.85)
	default:
		direction := float32(1)
		if mouth == "sad" {
			direction = -1
		}
		layer(ink, ui.MoveTo(11, 19), ui.QuadTo(16, 19+8*direction, 21, 19),
			ui.QuadTo(16, 19+12*direction, 11, 19), ui.ClosePath())
	}
	switch accent {
	case "sweat", "tear", "tears":
		x, y := float32(25), float32(18)
		if accent == "sweat" {
			y = 6
		}
		for _, offset := range [...]float32{0, -18} {
			if offset != 0 && accent != "tears" {
				break
			}
			layer(tear, ui.MoveTo(x+offset, y-3.5), ui.CubicTo(x-5+offset, y+1.2, x-3+offset, y+4, x+offset, y+4),
				ui.CubicTo(x+3+offset, y+4, x+4+offset, y+1.2, x+offset, y-3.5), ui.ClosePath())
		}
	case "heart":
		heart(26, 23)
	case "sleep":
		wedge(ui.RGB(86, 130, 213), 21, 4, 29, 4, 23, 10)
		wedge(ui.RGB(86, 130, 213), 24, 7, 28, 10, 21, 10)
	case "hand":
		oval(ui.RGB(255, 224, 130), 20, 24, 6.3, 3.7)
	case "spark", "party":
		wedge(ui.RGB(82, 114, 230), 20, 6, 26, 1, 29, 9)
		oval(ui.RGB(238, 77, 132), 26, 3, 1.8, 1.8)
		wedge(ui.RGB(86, 190, 160), 7, 2, 9, 7, 4, 5)
	}
	return icon
}

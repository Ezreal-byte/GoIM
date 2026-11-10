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
		{"plead", "可怜", "🥺", "plead", "sad", ""},
		{"woozy", "晕乎乎", "🥴", "dizzy", "aside", "cheeks"},
		{"confused", "疑惑", "😕", "thinking", "sad", "question"},
		{"frown", "不开心", "🙁", "sad", "sad", ""},
		{"worried", "担心", "😟", "wide", "sad", ""},
		{"disappointed", "失望", "😞", "sleep", "sad", ""},
		{"pensive", "低落", "😔", "sleep", "flat", "cheeks"},
		{"relieved", "松口气", "😌", "sleep", "smile", "cheeks"},
		{"scream", "吓到了", "😱", "wide", "open", "cold"},
		{"flushed", "脸红", "😳", "wide", "small", "cheeks"},
		{"grimace", "咬牙", "😬", "wide", "teeth", ""},
		{"cold", "冷汗", "😰", "sad", "open", "cold"},
		{"sweat", "擦汗", "😓", "sleep", "sad", "sweat"},
		{"weary", "好累", "😩", "sleep", "open", ""},
		{"tired", "困倦", "😫", "closed", "sad", ""},
		{"yawn", "打哈欠", "🥱", "sleep", "open", "hand"},
		{"zipper", "闭嘴", "🤐", "dot", "zip", ""},
		{"shush", "嘘", "🤫", "wide", "small", "shush"},
		{"smirk", "坏笑", "😏", "side", "smile", ""},
		{"unamused", "无奈", "😒", "side", "flat", ""},
		{"roll-eyes", "翻白眼", "🙄", "up", "flat", ""},
		{"suspicious", "怀疑", "🤨", "thinking", "aside", ""},
		{"nerd", "学霸", "🤓", "specs", "teeth", ""},
		{"monocle", "仔细看看", "🧐", "specs", "flat", ""},
		{"pout", "气鼓鼓", "😡", "angry", "sad", "warm"},
		{"swear", "气坏了", "🤬", "angry", "zip", "warm"},
		{"huff", "哼", "😤", "angry", "flat", "steam"},
		{"hot", "好热", "🥵", "sleep", "tongue", "warm"},
		{"freeze", "好冷", "🥶", "wide", "teeth", "cold"},
		{"sick", "戴口罩", "😷", "dot", "mask", ""},
		{"bandage", "受伤", "🤕", "sad", "sad", "bandage"},
		{"sneeze", "打喷嚏", "🤧", "sleep", "open", "bandage"},
		{"yum", "馋了", "😋", "closed", "tongue", "cheeks"},
		{"tongue-wink", "鬼脸", "😜", "wink", "tongue", ""},
		{"zany", "搞怪", "🤪", "dizzy", "tongue", ""},
		{"halo", "乖巧", "😇", "closed", "smile", "halo"},
		{"hugs", "抱抱", "🤗", "closed", "smile", "hug"},
		{"love", "爱心脸", "🥰", "closed", "smile", "love"},
		{"smile-tear", "含泪微笑", "🥲", "dot", "smile", "tear"},
		{"rofl", "笑倒", "🤣", "closed", "laugh", "tears"},
		{"salute", "敬礼", "🫡", "dot", "flat", "salute"},
		{"hand-mouth", "捂嘴", "🤭", "closed", "smile", "hand"},
		{"cat", "猫猫", "😺", "wide", "smile", "cat"},
		{"heart", "爱心", "❤️", "", "", "symbol"},
		{"rose", "玫瑰", "🌹", "", "", "symbol"},
		{"thumb", "点赞", "👍", "", "", "symbol"},
		{"clap", "鼓掌", "👏", "", "", "symbol"},
		{"wave", "挥手", "👋", "", "", "symbol"},
		{"pray", "合十", "🙏", "", "", "symbol"},
		{"sparkle", "闪闪发光", "✨", "", "", "symbol"},
		{"gift", "礼物", "🎁", "", "", "symbol"},
	}
	out := make([]emojiChoice, 0, len(specs))
	for _, s := range specs {
		var icon ui.Icon
		if s.accent == "symbol" {
			icon = emojiSymbol(s.name)
		} else {
			icon = classicFace(s.eyes, s.mouth, s.accent)
		}
		out = append(out, emojiChoice{s.name, s.label, s.value, icon})
	}
	return out
}

func classicFace(eyes, mouth, accent string) ui.Icon {
	ink, blush, tear := ui.RGB(88, 43, 32), ui.RGB(251, 119, 112), ui.RGB(63, 159, 243)
	light, shade := ui.RGB(255, 248, 175), ui.RGB(255, 156, 20)
	if accent == "warm" {
		light, shade = ui.RGB(255, 180, 118), ui.RGB(247, 109, 66)
	}
	if accent == "cold" {
		light, shade = ui.RGB(212, 242, 255), ui.RGB(83, 167, 230)
	}
	if accent == "cat" {
		light, shade = ui.RGB(255, 245, 222), ui.RGB(245, 167, 72)
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
	if accent == "cat" {
		wedge(ui.RGB(203, 126, 49), 4, 12, 5, 2, 13, 7)
		wedge(ui.RGB(203, 126, 49), 19, 7, 27, 2, 28, 12)
	}
	oval(ui.RGB(181, 96, 19), 16, 16, 14.1, 14.1)
	oval(shade, 16, 15.7, 13.3, 13.3)
	icon.Layers[len(icon.Layers)-1].Gradient = &ui.LinearGradient{Start: ui.Point{Y: 3}, End: ui.Point{Y: 29}, From: light, To: shade}
	oval(ui.RGBA(255, 255, 255, .28), 11.6, 6.5, 5.8, 2.1)
	if accent == "cheeks" || accent == "love" {
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
		case eyes == "specs":
			oval(ink, x, 13, 4.3, 3.8)
			oval(ui.RGB(255, 252, 238), x, 13, 3.55, 3.1)
			oval(ink, x, 13.25, 1.5, 2)
			if i == 0 {
				oval(ink, 16, 12.8, 2.2, .55)
			}
		case eyes == "plead":
			oval(ui.RGB(255, 252, 238), x, 13.5, 4, 4.8)
			oval(ink, x, 14, 3.15, 3.95)
			oval(ui.RGB(255, 255, 255), x-1, 12.4, 1.25, 1.35)
			oval(ui.RGB(255, 255, 255), x+1.1, 15.7, .65, .65)
		case eyes == "side" || eyes == "up" || eyes == "dizzy":
			oval(ui.RGB(255, 252, 238), x, 13.3, 3.25, 3.6)
			px, py := x-1, float32(13.6)
			if eyes == "up" {
				px, py = x, 11.4
			}
			if eyes == "dizzy" && i == 1 {
				px, py = x+1, 14.5
			}
			oval(ink, px, py, 1.7, 2)
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
	case "zip":
		oval(ink, 16, 21, 5.6, 1.1)
		for _, x := range [...]float32{12, 14, 16, 18, 20} {
			oval(ui.RGB(255, 249, 231), x, 21, .55, .68)
		}
	case "mask":
		layer(ui.RGB(248, 253, 255), ui.MoveTo(7, 17), ui.QuadTo(16, 19, 25, 17), ui.LineTo(23, 24),
			ui.QuadTo(16, 28, 9, 24), ui.ClosePath())
		oval(ui.RGB(174, 211, 230), 16, 22, 5, .6)
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
	case "question":
		layer(ui.RGB(69, 100, 139), ui.MoveTo(23, 6), ui.CubicTo(26, 1, 32, 5, 28, 8), ui.QuadTo(26, 9, 26, 11),
			ui.LineTo(24, 11), ui.QuadTo(23, 9, 26, 7), ui.QuadTo(28, 5, 25, 6), ui.ClosePath())
		oval(ui.RGB(69, 100, 139), 25.1, 13.2, 1.05, 1.05)
	case "halo":
		oval(ui.RGB(57, 156, 242), 16, 5.4, 9, 2.8)
		oval(ui.RGB(224, 249, 255), 16, 5.4, 6.8, 1.2)
	case "steam":
		oval(ui.RGB(255, 253, 247), 7, 19, 4.3, 2.1)
		oval(ui.RGB(255, 253, 247), 25, 19, 4.3, 2.1)
	case "bandage":
		layer(ui.RGB(255, 250, 239), ui.MoveTo(4, 8), ui.QuadTo(17, 3, 27, 10), ui.LineTo(28, 14),
			ui.QuadTo(17, 8, 3, 13), ui.ClosePath())
	case "shush":
		oval(ui.RGB(189, 106, 23), 18, 23, 2.8, 6.2)
		oval(ui.RGB(255, 209, 100), 18, 22.9, 2.15, 5.9)
	case "hug":
		oval(ui.RGB(237, 132, 30), 5.7, 23.7, 4.3, 3.9)
		oval(ui.RGB(255, 217, 104), 5.6, 23.4, 3.6, 3.1)
		oval(ui.RGB(237, 132, 30), 26.4, 23.7, 4.3, 3.9)
		oval(ui.RGB(255, 217, 104), 26.4, 23.4, 3.6, 3.1)
	case "love":
		heart(6, 9)
		heart(24, 19)
	case "salute":
		layer(ui.RGB(255, 208, 100), ui.MoveTo(17, 8), ui.LineTo(28, 3), ui.LineTo(30, 6), ui.LineTo(20, 12), ui.ClosePath())
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

// The symbol tiles are drawn with the same baked vector palette as the faces.
func emojiSymbol(name string) ui.Icon {
	icon := ui.Icon{ViewBox: 32}
	layer := func(color ui.Color, commands ...ui.PathCommand) {
		icon.Layers = append(icon.Layers, ui.IconLayer{Fill: color, Commands: commands})
	}
	poly := func(color ui.Color, xy ...float32) {
		commands := []ui.PathCommand{ui.MoveTo(xy[0], xy[1])}
		for i := 2; i < len(xy); i += 2 {
			commands = append(commands, ui.LineTo(xy[i], xy[i+1]))
		}
		layer(color, append(commands, ui.ClosePath())...)
	}
	gold, shade := ui.RGB(255, 209, 87), ui.RGB(203, 112, 30)
	red, green := ui.RGB(232, 65, 80), ui.RGB(54, 148, 74)
	switch name {
	case "rose":
		poly(green, 15, 29, 15, 15, 18, 15, 18, 29)
		poly(green, 16, 24, 7, 20, 8, 16, 16, 20, 24, 17, 26, 19, 17, 24)
		layer(red, ui.MoveTo(9, 4), ui.QuadTo(16, 1, 23, 5), ui.CubicTo(25, 18, 10, 21, 9, 4), ui.ClosePath())
		layer(ui.RGB(255, 137, 131), ui.MoveTo(12, 5), ui.QuadTo(20, 3, 22, 7), ui.QuadTo(15, 9, 12, 5), ui.ClosePath())
	case "heart":
		layer(red, ui.MoveTo(16, 28), ui.CubicTo(7, 22, 2, 17, 2, 11), ui.CubicTo(2, 3, 11, 1, 16, 8),
			ui.CubicTo(21, 1, 30, 3, 30, 11), ui.CubicTo(30, 17, 25, 22, 16, 28), ui.ClosePath())
		layer(ui.RGBA(255, 255, 255, .35), ui.MoveTo(6, 10), ui.QuadTo(8, 5, 13, 7), ui.QuadTo(9, 8, 8, 12), ui.ClosePath())
	case "sparkle":
		poly(shade, 15, 2, 19, 11, 28, 15, 19, 19, 15, 28, 11, 19, 2, 15, 11, 11)
		poly(gold, 15, 4, 18, 12, 26, 15, 18, 18, 15, 26, 12, 18, 4, 15, 12, 12)
		poly(ui.RGB(255, 242, 156), 26, 2, 27, 5, 30, 6, 27, 7, 26, 10, 25, 7, 22, 6, 25, 5)
	case "gift":
		poly(red, 5, 12, 27, 12, 27, 27, 5, 27)
		poly(ui.RGB(255, 108, 106), 4, 9, 28, 9, 28, 14, 4, 14)
		poly(gold, 14, 9, 18, 9, 18, 27, 14, 27)
		layer(gold, ui.MoveTo(16, 10), ui.CubicTo(1, 11, 6, -1, 16, 7), ui.CubicTo(26, -1, 31, 11, 16, 10), ui.ClosePath())
	default:
		// The four hand gestures use their own silhouettes, cuffs and highlights.
		switch name {
		case "thumb":
			poly(shade, 5, 14, 10, 14, 14, 11, 17, 3, 20, 3, 22, 6, 21, 12, 27, 12, 29, 15, 26, 26, 23, 29, 9, 28, 5, 26)
			poly(gold, 8, 15, 14, 13, 18, 5, 20, 5, 19, 14, 26, 14, 27, 16, 24, 26, 22, 27, 9, 26)
		case "wave":
			poly(shade, 8, 27, 5, 18, 6, 15, 10, 17, 7, 7, 9, 5, 13, 12, 11, 4, 14, 3, 17, 11, 17, 3, 20, 3, 21, 12, 23, 6, 26, 7, 26, 19, 22, 27, 17, 29)
			poly(gold, 10, 26, 7, 18, 12, 20, 9, 8, 15, 14, 13, 5, 19, 14, 19, 5, 21, 15, 24, 9, 24, 19, 21, 26, 17, 27)
		case "clap":
			poly(shade, 6, 26, 4, 19, 11, 15, 10, 6, 13, 4, 18, 12, 20, 5, 23, 6, 25, 13, 29, 15, 27, 23, 21, 28, 12, 29)
			poly(gold, 8, 24, 6, 20, 14, 15, 12, 7, 14, 6, 19, 15, 22, 8, 23, 15, 27, 16, 25, 22, 20, 26, 12, 27)
			poly(ui.RGB(252, 156, 49), 6, 7, 2, 5, 2, 3, 7, 5)
		case "pray":
			poly(shade, 5, 26, 9, 17, 15, 4, 17, 3, 19, 5, 23, 17, 27, 26, 22, 29, 16, 25, 10, 29)
			poly(gold, 8, 25, 13, 12, 16, 6, 16, 23, 11, 27)
			poly(ui.RGB(255, 228, 125), 17, 23, 17, 6, 20, 13, 24, 25, 21, 27)
		}
		poly(ui.RGBA(255, 251, 214, .62), 12, 19, 19, 17, 22, 18, 17, 20)
	}
	return icon
}

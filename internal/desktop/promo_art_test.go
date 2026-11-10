//go:build vivahead

package desktop

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// This is the group's fictional design attachment. Viva renders the artwork
// and then renders it again as an ordinary image in the actual chat.
func writePromoPoster(t *testing.T, path string) {
	t.Helper()
	const width, height = 720, 384
	session, err := vivaheadless.Open(app.Config{
		Width: width, Height: height, WindowMode: app.WindowFrameless,
		Paint: func(c *ui.Canvas, size ui.Size) {
			canvas := ui.Rect{Width: size.Width, Height: size.Height}
			c.FillGradient(canvas, 0, 180, []ui.GradientStop{
				ui.Stop(0, ui.RGB(39, 44, 66)), ui.Stop(0.34, ui.RGB(116, 82, 109)),
				ui.Stop(0.69, ui.RGB(232, 144, 108)), ui.Stop(1, ui.RGB(249, 195, 135)),
			})
			c.Rounded(ui.Rect{X: 493, Y: 59, Width: 114, Height: 114}, 57, ui.RGB(255, 220, 158))
			for _, star := range []ui.Point{{X: 366, Y: 68}, {X: 422, Y: 44}, {X: 457, Y: 93}, {X: 636, Y: 86}, {X: 663, Y: 61}, {X: 675, Y: 108}} {
				c.Rounded(ui.Rect{X: star.X, Y: star.Y, Width: 2, Height: 2}, 1, ui.RGBA(255, 231, 201, .8))
			}
			fill := func(ink ui.Color, xy ...float32) {
				commands := make([]ui.PathCommand, 0, len(xy)/2+1)
				commands = append(commands, ui.MoveTo(xy[0], xy[1]))
				for i := 2; i < len(xy); i += 2 {
					commands = append(commands, ui.LineTo(xy[i], xy[i+1]))
				}
				commands = append(commands, ui.ClosePath())
				c.DrawPath(ui.NewPath(width, height, commands...), canvas, ui.PathStyle{Fill: ink})
			}
			fill(ui.RGB(140, 103, 121), 0, 259, 67, 224, 133, 235, 220, 197, 266, 212, 315, 186, 357, 209, 398, 177, 432, 207, 465, 185, 500, 198, 545, 160, 574, 173, 630, 150, 675, 183, 720, 166, 720, 315, 0, 315)
			fill(ui.RGB(77, 73, 94), 0, 284, 65, 258, 118, 264, 197, 226, 248, 257, 310, 215, 346, 231, 394, 186, 428, 214, 470, 222, 502, 200, 543, 227, 585, 209, 627, 230, 669, 200, 720, 211, 720, 324, 0, 324)
			fill(ui.RGB(53, 65, 83), 0, 298, 78, 280, 134, 288, 209, 258, 253, 270, 331, 242, 379, 262, 430, 243, 482, 260, 528, 229, 585, 260, 640, 246, 681, 266, 720, 246, 720, 384, 0, 384)
			c.FillGradient(ui.Rect{Y: 294, Width: width, Height: 90}, 0, 180, []ui.GradientStop{
				ui.Stop(0, ui.RGB(77, 78, 100)), ui.Stop(0.4, ui.RGB(52, 63, 82)), ui.Stop(1, ui.RGB(25, 42, 57)),
			})
			for i := 0; i < 13; i++ {
				y := float32(299 + i*5)
				x := float32(351 + (i*23)%71 - i*4)
				w := float32(82 + (i*37)%142)
				c.Rounded(ui.Rect{X: x, Y: y, Width: w, Height: 1.5}, .75, ui.RGBA(248, 172, 132, .22-float32(i)*.011))
			}
			fill(ui.RGB(21, 38, 51), 0, 343, 50, 329, 88, 334, 138, 320, 177, 332, 229, 330, 270, 345, 297, 353, 324, 384, 0, 384)
			fill(ui.RGB(20, 34, 49), 720, 317, 663, 329, 625, 328, 589, 347, 549, 350, 513, 373, 495, 384, 720, 384)
			for _, pine := range []struct{ x, y, h float32 }{{29, 346, 44}, {66, 337, 55}, {91, 339, 35}, {124, 334, 42}, {167, 337, 26}, {678, 335, 49}, {650, 344, 33}, {708, 325, 54}, {611, 348, 26}} {
				x, y, h := pine.x, pine.y, pine.h
				fill(ui.RGB(17, 32, 46), x-2, y, x-2, y-h*.13, x-h*.18, y-h*.13, x-h*.08, y-h*.42,
					x-h*.14, y-h*.39, x-h*.05, y-h*.65, x-h*.09, y-h*.62, x, y-h, x+h*.09, y-h*.62,
					x+h*.05, y-h*.65, x+h*.14, y-h*.39, x+h*.08, y-h*.42, x+h*.18, y-h*.13, x+2, y-h*.13, x+2, y)
			}
			c.PushLayer(2)
			c.Label("NORTH RIDGE  /  2026", ui.Rect{X: 48, Y: 43, Width: 390, Height: 22}, ui.TextStyle{Size: 16, Weight: ui.WeightSemibold, Color: ui.RGB(255, 224, 173)})
			c.Label("北岭计划", ui.Rect{X: 46, Y: 72, Width: 425, Height: 74}, ui.TextStyle{Size: 58, Weight: ui.WeightBold, Color: ui.RGB(255, 248, 223)})
			c.Label("山野设计季 · 秋日企划", ui.Rect{X: 50, Y: 157, Width: 352, Height: 30}, ui.TextStyle{Size: 20, Color: ui.RGB(253, 223, 193)})
			c.PopLayer()
		},
	}, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Screenshot(path); err != nil {
		t.Fatal(err)
	}
}

// Illustrative portraits, not photographs of real people. Use distinct colors
// and silhouettes so the chat is legible even at avatar size.
func writePromoAvatar(t *testing.T, path string, index int) {
	t.Helper()
	rgb := func(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }
	backgrounds := [...]color.NRGBA{
		rgb(156, 192, 225), rgb(255, 201, 173), rgb(163, 208, 193), rgb(232, 193, 219), rgb(235, 211, 153),
		rgb(185, 196, 236), rgb(168, 213, 221), rgb(244, 182, 150), rgb(188, 212, 158),
	}
	shirts := [...]color.NRGBA{
		rgb(36, 88, 145), rgb(162, 63, 79), rgb(31, 110, 105), rgb(126, 70, 148), rgb(153, 91, 39),
		rgb(62, 76, 132), rgb(42, 107, 125), rgb(178, 73, 49), rgb(62, 106, 67),
	}
	skins := [...]color.NRGBA{rgb(247, 191, 146), rgb(248, 212, 174), rgb(213, 149, 111), rgb(241, 179, 132)}
	hair := [...]color.NRGBA{rgb(36, 40, 56), rgb(70, 43, 47), rgb(44, 50, 59), rgb(100, 61, 45)}
	picture := image.NewNRGBA(image.Rect(0, 0, 96, 96))
	base, shirt, skin, ink := backgrounds[index], shirts[index], skins[index%len(skins)], hair[index%len(hair)]
	ellipse := func(x, y, cx, cy, rx, ry float64) bool {
		return (x-cx)*(x-cx)/(rx*rx)+(y-cy)*(y-cy)/(ry*ry) <= 1
	}
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			xf, yf := float64(x), float64(y)
			d := max(0, 1-math.Hypot(xf-26, yf-13)/110) * .21
			pixel := rgb(uint8(float64(base.R)+(255-float64(base.R))*d), uint8(float64(base.G)+(255-float64(base.G))*d), uint8(float64(base.B)+(255-float64(base.B))*d))
			long := index%3 == 1 || index == 3
			if long && ellipse(xf, yf, 48, 46, 24, 35) {
				pixel = ink
			}
			if ellipse(xf, yf, 48, 102, 36, 32) {
				pixel = shirt
			}
			if ellipse(xf, yf, 48, 69, 10, 14) {
				pixel = skin
			}
			if ellipse(xf, yf, 48, 36, 23, 24) {
				pixel = ink
			}
			if index%4 == 1 && ellipse(xf, yf, 65, 21, 11, 11) {
				pixel = ink
			}
			if ellipse(xf, yf, 48, 45, 18, 24) || ellipse(xf, yf, 29, 48, 4, 6) || ellipse(xf, yf, 67, 48, 4, 6) {
				pixel = skin
			}
			if ellipse(xf, yf, 45+float64(index%3)*4, 30, 18, 10) && yf < 39+.12*(xf-48) {
				pixel = ink
			}
			if ellipse(xf, yf, 41, 46, 1.5, 2) || ellipse(xf, yf, 56, 46, 1.5, 2) {
				pixel = ink
			}
			if index%3 == 2 {
				for _, cx := range []float64{41, 56} {
					outer := ellipse(xf, yf, cx, 46, 6, 4.8)
					inner := ellipse(xf, yf, cx, 46, 4.9, 3.7)
					if outer && !inner {
						pixel = ink
					}
				}
				if x >= 47 && x <= 50 && y == 45 {
					pixel = ink
				}
			}
			if y >= 56 && y <= 58 && math.Abs(xf-49) <= 3-float64(y-56)*.6 {
				pixel = rgb(168, 77, 77)
			}
			if ellipse(xf, yf, 36, 53, 3.2, 1.7) || ellipse(xf, yf, 61, 53, 3.2, 1.7) {
				pixel = rgb(224, 137, 116)
			}
			picture.SetNRGBA(x, y, pixel)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, picture); err != nil {
		t.Fatal(err)
	}
}

func writePromoGrid(t *testing.T, path string) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 480, 680))
	for y := 0; y < 680; y++ {
		for x := 0; x < 480; x++ {
			pixel := color.NRGBA{R: 254, G: 246, B: 230, A: 255}
			if x > 32 && x < 448 && y > 48 && y < 628 {
				pixel = color.NRGBA{R: 63, G: 58, B: 85, A: 255}
				if y > 340+max(x-236, 236-x)/2 {
					pixel = color.NRGBA{R: 29, G: 54, B: 70, A: 255}
				}
				if (x-330)*(x-330)+(y-220)*(y-220) < 45*45 {
					pixel = color.NRGBA{R: 250, G: 191, B: 129, A: 255}
				}
			}
			picture.SetNRGBA(x, y, pixel)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, picture); err != nil {
		t.Fatal(err)
	}
}

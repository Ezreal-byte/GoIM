package desktop

import (
	"math"
	"sync"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	localSendLogoQuarterFrames = 128
	localSendLogoColumns       = 16
	localSendLogoTurn          = 15 * time.Second
)

func localSendReceiveGreen(theme ui.Theme) gfx.Color {
	if panelDark(theme) {
		return gfx.RGBA(0x82, 0xd5, 0xc8, 1)
	}
	return gfx.RGBA(0x00, 0x6a, 0x60, 1)
}

// The official mark repeats every quarter turn. Sharing one quarter's
// samples keeps the animation smooth without keeping four copies in memory.
var localSendReceiveAtlases = sync.OnceValue(func() [2]gfx.Bitmap {
	mark := localSendMarks[1]
	return [2]gfx.Bitmap{
		localSendLogoAtlas(mark, localSendReceiveGreen(ui.LightTheme()), mark.ID^(1<<63)),
		localSendLogoAtlas(mark, localSendReceiveGreen(ui.DarkTheme()), mark.ID^(1<<62)),
	}
})

func (p *Panels) drawLocalSendReceiveLogo(f ui.C, theme ui.Theme) {
	v := &p.localSend
	const size = 38
	index := 0
	if panelDark(theme) {
		index = 1
	}
	atlas := v.logoAtlases[index]
	if atlas.Width == 0 {
		drawLocalSendMark(f, theme, size)
		return
	}
	id := p.tree.ID(v.scope, "receive-logo-motion")
	animating := v.animateLogo && v.modal == localSendNoModal && !f.Canvas().ReduceMotion()
	cell := atlas.Width / localSendLogoColumns
	ui.Row(f.Part("receive-logo")).Size(size, size).Draw(func(d *ui.DrawContext) {
		frame := 0
		if animating {
			if progress, playing := d.Canvas.PlayOnce(id, 0, true, localSendLogoTurn); playing {
				frame = int(progress*float32(4*localSendLogoQuarterFrames)) % localSendLogoQuarterFrames
			}
		}
		d.Canvas.DrawBitmapRegion(atlas, gfx.Rect{
			X:     float32(frame%localSendLogoColumns*cell + 1),
			Y:     float32(frame/localSendLogoColumns*cell + 1),
			Width: float32(cell - 2), Height: float32(cell - 2),
		}, d.Rect)
	})
}

func localSendLogoAtlas(mark gfx.Bitmap, color gfx.Color, textureID uint64) gfx.Bitmap {
	size := mark.Width
	cell := size + 2
	width := cell * localSendLogoColumns
	height := cell * (localSendLogoQuarterFrames / localSendLogoColumns)
	stride := width * 4
	pixels := make([]byte, stride*height)
	center := float64(size-1) / 2
	red, green, blue := float64(color.Red), float64(color.Green), float64(color.Blue)
	for frame := range localSendLogoQuarterFrames {
		sine, cosine := math.Sincos(math.Pi / 2 * float64(frame) / localSendLogoQuarterFrames)
		xBase := frame%localSendLogoColumns*cell + 1
		yBase := frame/localSendLogoColumns*cell + 1
		for y := range size {
			for x := range size {
				dx, dy := float64(x)-center, float64(y)-center
				sx, sy := center+dx*cosine+dy*sine, center-dx*sine+dy*cosine
				alpha := sampleLocalSendLogoAlpha(mark, sx, sy)
				at := (yBase+y)*stride + (xBase+x)*4
				pixels[at] = byte(blue*float64(alpha) + .5)
				pixels[at+1] = byte(green*float64(alpha) + .5)
				pixels[at+2] = byte(red*float64(alpha) + .5)
				pixels[at+3] = alpha
			}
		}
	}
	return gfx.Bitmap{Pix: pixels, Width: width, Height: height, Stride: stride, ID: textureID, Version: 1}
}

func sampleLocalSendLogoAlpha(mark gfx.Bitmap, x, y float64) byte {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	get := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= mark.Width || y >= mark.Height {
			return 0
		}
		return float64(mark.Pix[y*mark.Stride+x*4+3])
	}
	top := get(x0, y0)*(1-fx) + get(x0+1, y0)*fx
	bottom := get(x0, y0+1)*(1-fx) + get(x0+1, y0+1)*fx
	return byte(top*(1-fy) + bottom*fy + .5)
}

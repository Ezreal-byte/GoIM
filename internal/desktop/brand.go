package desktop

import (
	"runtime"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/brand"
)

var (
	titleMark = logoBitmap("goim-48")
	lockMark  = logoBitmap("goim-96")
)

func logoBitmap(name string) ui.Bitmap {
	picture := brand.Image(name)
	size := picture.Bounds().Dx()
	pixels := make([]byte, size*size*4)
	for i := 0; i < len(pixels); i += 4 {
		a := uint16(picture.Pix[i+3])
		pixels[i] = byte((uint16(picture.Pix[i+2])*a + 127) / 255)
		pixels[i+1] = byte((uint16(picture.Pix[i+1])*a + 127) / 255)
		pixels[i+2] = byte((uint16(picture.Pix[i])*a + 127) / 255)
		pixels[i+3] = byte(a)
	}
	return ui.Bitmap{Pix: pixels, Width: size, Height: size, Stride: size * 4, ID: 0x474F494D00000000 | uint64(size), Version: 1}
}

func paintGoIMMark(canvas *ui.Canvas, bounds ui.Rect, mark ui.Bitmap) {
	edge := min(bounds.Width, bounds.Height)
	if edge > 0 {
		canvas.DrawBitmap(mark, ui.Rect{
			X: bounds.X + (bounds.Width-edge)/2, Y: bounds.Y + (bounds.Height-edge)/2,
			Width: edge, Height: edge,
		})
	}
}

func windowIcon() app.WindowIcon {
	picture := brand.Image("goim-256")
	return app.WindowIcon{Size: picture.Bounds().Dx(), RGBA: picture.Pix}
}

func trayIcon() app.StatusIcon {
	name, template := "goim-64", false
	if runtime.GOOS == "darwin" {
		name, template = "goim-tray-mac-44", true
	}
	picture := brand.Image(name)
	return app.StatusIcon{Size: picture.Bounds().Dx(), RGBA: picture.Pix, Template: template}
}

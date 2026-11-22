package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestLinuxEmojiFallbackRendersColor(t *testing.T) {
	engine, ok := app.PlatformTextEngine().(gfx.TextRasterizer)
	if !ok {
		t.Skip("native text rasterizer unavailable")
	}
	_, unregister, err := app.RegisterFont(emojiFallbackFont)
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	for _, sample := range []string{"测试 🥳 emoji", "A 🥶 B", "🌹 ❤️"} {
		bitmap, err := engine.RasterizeText(sample, gfx.TextStyle{Size: 26, Color: gfx.RGB(0, 0, 0)}, 1, 280, 40)
		if err != nil {
			t.Fatal(err)
		}
		colored := false
		for i := 0; i+3 < len(bitmap.Pix); i += 4 {
			if bitmap.Pix[i+3] > 64 && bitmap.Pix[i] != bitmap.Pix[i+2] {
				colored = true
				break
			}
		}
		if !colored {
			t.Errorf("%q rendered without color", sample)
		}
	}
}

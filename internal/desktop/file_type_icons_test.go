package desktop

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestFileTypeGlyphsCoverBundledSources(t *testing.T) {
	entries, err := os.ReadDir("../fileicon/svg")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		kind, svg := strings.CutSuffix(entry.Name(), ".svg")
		if !svg {
			continue
		}
		icon := fileTypeGlyphs[kind]
		if icon == nil {
			t.Errorf("%s has no desktop glyph", kind)
		} else if kind != "file" && len(icon.Layers) == 0 {
			t.Errorf("%s lost its SVG colors", kind)
		}
	}
}

func TestFileTypeIconsRenderWithOriginalColorsAndThemeFallback(t *testing.T) {
	for _, theme := range []ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		theme.Muted = ui.RGB(17, 34, 51)
		theme.MutedForeground = ui.RGB(40, 144, 200)
		page := ui.NewPage(ui.PageConfig{Theme: &theme})
		cfg := app.Config{Width: 144, Height: 48, WindowMode: app.WindowFrameless, Paint: func(c *gfx.Canvas, size gfx.Size) {
			page.Draw(c, gfx.Rect{Width: size.Width, Height: size.Height}, func(f ui.C) {
				ui.Row(f).Fill().Children(func() {
					drawFileIcon(f, theme, fileTypeGlyphs["folder"], 48, 24)
					drawFileIcon(f, theme, fileTypeGlyphs["file"], 48, 24)
					drawFileIcon(f, theme, fileTypeGlyphs["go"], 48, 24)
				})
			})
		}}
		session, err := headless.Open(cfg, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		img, err := session.Image()
		session.Close()
		page.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := img.RGBAAt(4, 24); got != (color.RGBA{17, 34, 51, 255}) {
			t.Fatalf("file glyph tile lost its muted background: %v", got)
		}
		var originalFolder, mutedFile, goBlue bool
		for y := 0; y < 48; y++ {
			for x := 0; x < 48; x++ {
				originalFolder = originalFolder || img.RGBAAt(x, y) == (color.RGBA{141, 110, 99, 255})
				mutedFile = mutedFile || img.RGBAAt(x+48, y) == (color.RGBA{40, 144, 200, 255})
				goBlue = goBlue || img.RGBAAt(x+96, y) == (color.RGBA{0, 172, 193, 255})
			}
		}
		if !originalFolder || !mutedFile || !goBlue {
			t.Fatalf("folder color: %t, file theme tint: %t, Go color: %t", originalFolder, mutedFile, goBlue)
		}
	}
}

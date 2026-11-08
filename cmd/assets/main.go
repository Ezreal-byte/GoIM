// Command assets rasterizes the shared GoIM vector mark for the platform icon.
package main

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"goim/internal/desktop"
	"log"
	"os"
)

func main() {
	if err := os.MkdirAll("internal/desktop/assets", 0755); err != nil {
		log.Fatal(err)
	}
	s, err := headless.Open(app.Config{Width: 512, Height: 512, Paint: func(c *ui.Canvas, _ ui.Size) {
		c.Fill(ui.Rect{Width: 512, Height: 512}, ui.RGBA(0, 0, 0, 0))
		desktop.PaintGoIMMark(c, ui.Rect{X: 40, Y: 40, Width: 432, Height: 432})
	}}, nil, headless.Options{Scale: 1})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if err = s.Screenshot("internal/desktop/assets/icon.png"); err != nil {
		log.Fatal(err)
	}
}

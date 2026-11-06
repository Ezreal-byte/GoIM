// Command assets rasterizes the application's actual compiled Lucide mark.
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
		c.Fill(ui.Rect{Width: 512, Height: 512}, ui.RGB(20, 71, 230))
		c.DrawIcon(desktop.MessageCircle, ui.Rect{X: 96, Y: 96, Width: 320, Height: 320}, ui.RGB(255, 255, 255))
	}}, nil, headless.Options{Scale: 1})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if err = s.Screenshot("internal/desktop/assets/icon.png"); err != nil {
		log.Fatal(err)
	}
}

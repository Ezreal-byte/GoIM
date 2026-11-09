// Package brand provides GoIM's generated runtime logo assets.
package brand

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

//go:embed assets/goim-48.png assets/goim-64.png assets/goim-96.png assets/goim-256.png assets/goim-tray-mac-44.png
var assets embed.FS

// PNG returns an embedded runtime representation of the logo.
func PNG(name string) []byte {
	data, err := assets.ReadFile("assets/" + name + ".png")
	if err != nil {
		panic(fmt.Errorf("GoIM brand: %w", err))
	}
	return data
}

// Image returns straight RGBA pixels for a generated runtime logo.
func Image(name string) *image.NRGBA {
	picture, err := png.Decode(bytes.NewReader(PNG(name)))
	if err != nil {
		panic(fmt.Errorf("GoIM brand: decode %s: %w", name, err))
	}
	bounds := picture.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(out, out.Bounds(), picture, bounds.Min, draw.Src)
	return out
}

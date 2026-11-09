// Command assets exports GoIM's approved logo for UI and operating system use.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/tools/appbundle"
	"github.com/vivaui/viva/tools/winpkg"
)

const assetDir = "internal/brand/assets"

func main() {
	if err := export(); err != nil {
		log.Fatal(err)
	}
}

func export() error {
	file, err := os.Open(filepath.Join(assetDir, "goim-master.png"))
	if err != nil {
		return err
	}
	master, err := png.Decode(file)
	file.Close()
	if err != nil {
		return err
	}
	if bounds := master.Bounds(); bounds.Dx() != bounds.Dy() || bounds.Dx() < appbundle.RecommendedIconPixels {
		return fmt.Errorf("the GoIM master must be square and at least 1024 px")
	}
	for _, size := range []int{16, 20, 24, 32, 48, 64, 96, 128, 256, 512, 1024} {
		if err := writePNG(fmt.Sprintf("goim-%d.png", size), appbundle.Downscale(master, size)); err != nil {
			return err
		}
	}
	// The menu bar tints the two bubbles itself. Keep the opaque black tile
	// out of the template so it does not turn into a solid square.
	if err := writePNG("goim-tray-mac-44.png", trayMask(master, 44)); err != nil {
		return err
	}
	ico, _, err := winpkg.EncodeICO(master)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(assetDir, "goim.ico"), ico, 0644); err != nil {
		return err
	}
	icns, _, err := appbundle.EncodeICNS(master)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(assetDir, "goim.icns"), icns, 0644)
}

func writePNG(name string, picture image.Image) error {
	file, err := os.Create(filepath.Join(assetDir, name))
	if err != nil {
		return err
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(file, picture); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func trayMask(master image.Image, size int) image.Image {
	bounds := master.Bounds()
	mask := image.NewNRGBA(bounds)
	var mark image.Rectangle
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := master.At(x, y).RGBA()
			value := (r + g + b) / (3 * 257)
			if value < 16 {
				continue
			}
			mask.SetNRGBA(x, y, color.NRGBA{A: uint8(value)})
			if value >= 96 {
				mark = mark.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if mark.Empty() {
		return appbundle.Downscale(mask, size)
	}
	side := max(mark.Dx(), mark.Dy())
	side += side / 4
	x, y := (mark.Min.X+mark.Max.X-side)/2, (mark.Min.Y+mark.Max.Y-side)/2
	crop := image.Rect(x, y, x+side, y+side)
	if !crop.In(bounds) {
		crop = bounds
	}
	return appbundle.Downscale(mask.SubImage(crop), size)
}

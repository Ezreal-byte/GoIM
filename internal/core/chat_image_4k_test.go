package core

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFull4KPNGQualifiesAsInlineImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screenshot.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(f, image.NewRGBA(image.Rect(0, 0, 3840, 2160))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := inlineImageType(path, info.Size()); got != "image/png" {
		t.Fatalf("full 4K screenshot treated as %q", got)
	}
}

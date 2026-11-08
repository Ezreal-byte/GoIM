package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestScreenshotToolPathHonorsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goim-snip-test")
	if err := os.WriteFile(path, []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOIM_SNIP_PATH", path)
	got, err := screenshotToolPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("screenshot tool path = %q, want %q", got, path)
	}
}

func TestReadScreenshotValidatesStrictPNG(t *testing.T) {
	var encoded bytes.Buffer
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.NRGBA{R: 20, G: 40, B: 60, A: 255})
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readScreenshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, encoded.Bytes()) {
		t.Fatal("screenshot bytes changed while reading")
	}
	if err := os.WriteFile(path, []byte("not png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readScreenshot(path); err == nil {
		t.Fatal("invalid screenshot content was accepted")
	}
}

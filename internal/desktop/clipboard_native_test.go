//go:build windows || linux || darwin

package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestNativeClipboardPNGRoundTrip(t *testing.T) {
	if os.Getenv("GOIM_CLIPBOARD_TEST") == "" {
		t.Skip("set GOIM_CLIPBOARD_TEST=1 to overwrite the real clipboard")
	}
	var encoded bytes.Buffer
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 128})
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	if err := setClipboardPNG("", encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	got, err := app.ClipboardImagePNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != src.Bounds() || color.NRGBAModel.Convert(img.At(1, 1)) != src.NRGBAAt(1, 1) {
		t.Fatal("clipboard image differs from the supplied image")
	}
}

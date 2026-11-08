//go:build windows

package desktop

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestWindowsClipboardPNGRoundTrip(t *testing.T) {
	if os.Getenv("GOIM_CLIPBOARD_TEST") == "" {
		t.Skip("set GOIM_CLIPBOARD_TEST=1 to overwrite the real clipboard")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	if err := setClipboardPNG("", encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	got, err := app.ClipboardImagePNG()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("clipboard image is empty after write")
	}
}

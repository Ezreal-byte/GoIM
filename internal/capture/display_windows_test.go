//go:build windows

package capture

import (
	"os"
	"testing"
	"unsafe"
)

func TestWindowsABIStructLayouts(t *testing.T) {
	if got := unsafe.Sizeof(monitorInfoEx{}); got != 104 {
		t.Fatalf("MONITORINFOEXW size = %d, want 104", got)
	}
	if got := unsafe.Sizeof(devModeW{}); got != 220 {
		t.Fatalf("DEVMODEW size = %d, want 220", got)
	}
	if got := unsafe.Offsetof(devModeW{}.PelsWidth); got != 172 {
		t.Fatalf("DEVMODEW.dmPelsWidth offset = %d, want 172", got)
	}
	if got := unsafe.Sizeof(bitmapInfoHeader{}); got != 40 {
		t.Fatalf("BITMAPINFOHEADER size = %d, want 40", got)
	}
}

func TestNativeWindowsCaptureProbe(t *testing.T) {
	if os.Getenv("GOIM_SNIP_NATIVE_TEST") == "" {
		t.Skip("set GOIM_SNIP_NATIVE_TEST=1 to capture the real desktop")
	}
	displays, err := Displays()
	if err != nil {
		t.Fatal(err)
	}
	first := displays[0]
	image, err := CaptureRect(first.Bounds)
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != first.Bounds.Dx() || image.Bounds().Dy() != first.Bounds.Dy() {
		t.Fatalf("capture size = %v, monitor bounds = %v", image.Bounds(), first.Bounds)
	}
	t.Logf("captured display %d %s bounds=%v work=%v dpi=%d pixel=%+v",
		first.Index, first.Name, first.Bounds, first.Work, first.DPI, image.RGBAAt(0, 0))
}

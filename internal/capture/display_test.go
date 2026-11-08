package capture

import (
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestDisplayGeometryHandlesNegativeOriginsAndGaps(t *testing.T) {
	displays := []Display{
		{Index: 0, Bounds: image.Rect(-2560, -200, -640, 880), Primary: false},
		{Index: 1, Bounds: image.Rect(0, 0, 1920, 1080), Primary: true},
	}
	if got, want := DisplayUnion(displays), image.Rect(-2560, -200, 1920, 1080); got != want {
		t.Fatalf("DisplayUnion = %v, want %v", got, want)
	}
	if got, ok := DisplayAt(displays, image.Pt(-2000, -100)); !ok || got.Index != 0 {
		t.Fatalf("DisplayAt(left monitor) = %+v, %v", got, ok)
	}
	if got, ok := DisplayAt(displays, image.Pt(10, 10)); !ok || got.Index != 1 {
		t.Fatalf("DisplayAt(primary) = %+v, %v", got, ok)
	}
	if _, ok := DisplayAt(displays, image.Pt(-100, 100)); ok {
		t.Fatal("DisplayAt treated the gap between monitors as a display")
	}
}

func TestCaptureAllRequestsThePhysicalVirtualDesktop(t *testing.T) {
	displays := []Display{
		{Index: 0, Bounds: image.Rect(-1920, -300, 0, 780)},
		{Index: 1, Bounds: image.Rect(0, 0, 1280, 720), Primary: true},
	}
	requested := image.Rectangle{}
	desktop, err := captureAllWith(displays, func(bounds image.Rectangle) (*image.RGBA, error) {
		requested = bounds
		return image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy())), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := image.Rect(-1920, -300, 1280, 780)
	if requested != want || desktop.Bounds != want {
		t.Fatalf("capture request = %v, desktop bounds = %v, want %v", requested, desktop.Bounds, want)
	}
	if got := desktop.Image.Bounds().Size(); got != want.Size() {
		t.Fatalf("image size = %v, want %v", got, want.Size())
	}
	display, local, ok := desktop.At(image.Pt(-1920, -300))
	if !ok || display.Index != 0 || local != image.Pt(0, 0) {
		t.Fatalf("Desktop.At = display %+v, local %v, %v", display, local, ok)
	}
}

func TestCaptureAllRejectsBackendGeometryMismatch(t *testing.T) {
	displays := []Display{{Bounds: image.Rect(-10, -20, 30, 40)}}
	_, err := captureAllWith(displays, func(image.Rectangle) (*image.RGBA, error) {
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
	})
	if err == nil {
		t.Fatal("captureAllWith accepted an image with the wrong geometry")
	}
}

func TestCaptureRectRejectsInvalidAndOversizedRegionsBeforePlatform(t *testing.T) {
	if _, err := CaptureRect(image.Rect(10, 10, 10, 20)); !errors.Is(err, ErrInvalidBounds) {
		t.Fatalf("empty region error = %v, want ErrInvalidBounds", err)
	}
	large := image.Rect(0, 0, maxCapturePixels+1, 1)
	if _, err := CaptureRect(large); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized region error = %v, want ErrTooLarge", err)
	}
}

func TestDisplayScale(t *testing.T) {
	if got := (Display{DPI: 144}).Scale(); got != 1.5 {
		t.Fatalf("scale at 144 DPI = %v, want 1.5", got)
	}
	if got := (Display{}).Scale(); got != 1 {
		t.Fatalf("scale at unknown DPI = %v, want 1", got)
	}
}

func TestDisplayUnionIgnoresEmptyDisplays(t *testing.T) {
	if got := DisplayUnion(nil); !got.Empty() {
		t.Fatalf("DisplayUnion(nil) = %v, want empty", got)
	}
	union := DisplayUnion([]Display{{Bounds: image.Rectangle{}}, {Bounds: image.Rect(-1, -1, 1, 1)}})
	if union != image.Rect(-1, -1, 1, 1) {
		t.Fatalf("DisplayUnion = %v", union)
	}
}

func TestDesktopAtPreservesVirtualOrigin(t *testing.T) {
	desktop := &Desktop{
		Bounds:   image.Rect(-300, -100, 900, 700),
		Displays: []Display{{Bounds: image.Rect(-300, -100, 500, 700)}},
	}
	display, local, ok := desktop.At(image.Pt(-50, 25))
	if !ok || display.Bounds.Min != image.Pt(-300, -100) || local != image.Pt(250, 125) {
		t.Fatalf("At = %+v, %v, %v", display, local, ok)
	}
}

func TestCaptureAllWithPreservesDisplayMetadata(t *testing.T) {
	displays := []Display{{
		Index: 3, Name: `\\.\DISPLAY4`, Bounds: image.Rect(-100, 0, 100, 50),
		Work: image.Rect(-100, 0, 100, 30), Primary: true, DPI: 120,
	}}
	desktop, err := captureAllWith(displays, func(bounds image.Rectangle) (*image.RGBA, error) {
		image := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		for index := 0; index < len(image.Pix); index += 4 {
			image.Pix[index+3] = 255
		}
		return image, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(desktop.Displays) != 1 || desktop.Displays[0].Name != `\\.\DISPLAY4` ||
		desktop.Displays[0].Scale() != 1.25 || desktop.Displays[0].Work.Max.Y != 30 {
		t.Fatalf("display metadata = %+v", desktop.Displays)
	}
	if desktop.Image.RGBAAt(0, 0) != (color.RGBA{A: 255}) {
		t.Fatalf("background pixel = %+v", desktop.Image.RGBAAt(0, 0))
	}
}

package capture

import (
	"errors"
	"fmt"
	"image"
)

// ErrUnsupported reports that the current platform has no native capture
// backend. The document and export APIs remain usable.
var ErrUnsupported = errors.New("capture: native screen capture is unsupported on this platform")

// ErrNoDisplays reports that the system returned no active displays.
var ErrNoDisplays = errors.New("capture: no active displays")

// ErrInvalidBounds reports an empty or otherwise unusable capture rectangle.
var ErrInvalidBounds = errors.New("capture: invalid bounds")

// ErrTooLarge reports a capture region above the package's memory safety limit.
var ErrTooLarge = errors.New("capture: region is too large")

const maxCapturePixels = 100_000_000

// Display is one physical display in the virtual desktop coordinate space.
// Bounds can have negative coordinates. DPI is the monitor's effective DPI;
// zero means the platform did not report it.
type Display struct {
	Index   int
	Name    string
	Bounds  image.Rectangle
	Work    image.Rectangle
	Primary bool
	DPI     uint32
}

// Scale returns the effective device-pixel scale, with 1 for an unreported DPI.
func (display Display) Scale() float64 {
	if display.DPI == 0 {
		return 1
	}
	return float64(display.DPI) / 96
}

// Desktop is a one-shot capture of the complete virtual desktop. Image begins at
// local pixel (0,0); Bounds carries the physical virtual-desktop origin, which
// can be negative. Displays retain their physical screen coordinates.
type Desktop struct {
	Image    *image.RGBA
	Bounds   image.Rectangle
	Displays []Display
}

// Displays returns active displays in the platform's enumeration order.
func Displays() ([]Display, error) {
	displays, err := platformDisplays()
	if err != nil {
		return nil, err
	}
	if len(displays) == 0 {
		return nil, ErrNoDisplays
	}
	return displays, nil
}

// CaptureAll captures the union of every active display, including negative
// origins and the gaps between displays.
func CaptureAll() (*Desktop, error) {
	displays, err := Displays()
	if err != nil {
		return nil, err
	}
	return captureAllWith(displays, CaptureRect)
}

// CaptureDisplay captures one display by index.
func CaptureDisplay(index int) (*Desktop, error) {
	displays, err := Displays()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(displays) {
		return nil, fmt.Errorf("capture: display index %d is outside 0..%d", index, len(displays)-1)
	}
	display := displays[index]
	source, err := CaptureRect(display.Bounds)
	if err != nil {
		return nil, err
	}
	if err := checkCapturedImage(source, display.Bounds); err != nil {
		return nil, err
	}
	return &Desktop{
		Image:    source,
		Bounds:   display.Bounds,
		Displays: []Display{display},
	}, nil
}

// CaptureRect captures an arbitrary physical desktop rectangle. The result has
// local bounds starting at (0,0).
func CaptureRect(bounds image.Rectangle) (*image.RGBA, error) {
	bounds = canonicalRect(bounds)
	if bounds.Empty() {
		return nil, ErrInvalidBounds
	}
	if int64(bounds.Dx())*int64(bounds.Dy()) > maxCapturePixels {
		return nil, ErrTooLarge
	}
	captured, err := platformCapture(bounds)
	if err != nil {
		return nil, err
	}
	if err := checkCapturedImage(captured, bounds); err != nil {
		return nil, err
	}
	return captured, nil
}

// DisplayUnion returns the smallest rectangle containing every non-empty
// display, or the zero rectangle when there are none.
func DisplayUnion(displays []Display) image.Rectangle {
	var union image.Rectangle
	for _, display := range displays {
		bounds := canonicalRect(display.Bounds)
		if bounds.Empty() {
			continue
		}
		if union.Empty() {
			union = bounds
		} else {
			union = union.Union(bounds)
		}
	}
	return union
}

// DisplayAt returns the display containing screenPoint. Gaps between
// displays return false.
func DisplayAt(displays []Display, screenPoint image.Point) (Display, bool) {
	for _, display := range displays {
		if screenPoint.In(canonicalRect(display.Bounds)) {
			return display, true
		}
	}
	return Display{}, false
}

// At returns the display containing screenPoint and its local image point.
func (desktop *Desktop) At(screenPoint image.Point) (Display, image.Point, bool) {
	if desktop == nil {
		return Display{}, image.Point{}, false
	}
	display, ok := DisplayAt(desktop.Displays, screenPoint)
	if !ok {
		return Display{}, image.Point{}, false
	}
	return display, screenPoint.Sub(desktop.Bounds.Min), true
}

type captureFunc func(image.Rectangle) (*image.RGBA, error)

func captureAllWith(displays []Display, capture captureFunc) (*Desktop, error) {
	bounds := DisplayUnion(displays)
	if bounds.Empty() {
		return nil, ErrNoDisplays
	}
	captured, err := capture(bounds)
	if err != nil {
		return nil, err
	}
	if err := checkCapturedImage(captured, bounds); err != nil {
		return nil, err
	}
	return &Desktop{
		Image:    captured,
		Bounds:   bounds,
		Displays: append([]Display(nil), displays...),
	}, nil
}

func checkCapturedImage(image *image.RGBA, bounds image.Rectangle) error {
	if image == nil {
		return errors.New("capture: backend returned no image")
	}
	if image.Bounds().Dx() != bounds.Dx() || image.Bounds().Dy() != bounds.Dy() {
		return fmt.Errorf(
			"capture: backend returned %dx%d for a %dx%d request",
			image.Bounds().Dx(), image.Bounds().Dy(), bounds.Dx(), bounds.Dy(),
		)
	}
	return nil
}

func canonicalRect(bounds image.Rectangle) image.Rectangle {
	if bounds.Min.X > bounds.Max.X {
		bounds.Min.X, bounds.Max.X = bounds.Max.X, bounds.Min.X
	}
	if bounds.Min.Y > bounds.Max.Y {
		bounds.Min.Y, bounds.Max.Y = bounds.Max.Y, bounds.Min.Y
	}
	return bounds
}

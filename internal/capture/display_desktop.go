//go:build linux || (darwin && cgo)

package capture

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

// The platform library lives in the optional screenshot process. GoIM and
// Viva do not link it into their desktop executable.
func platformDisplays() ([]Display, error) {
	count := screenshot.NumActiveDisplays()
	if count == 0 {
		return nil, ErrNoDisplays
	}
	displays := make([]Display, 0, count)
	for index := 0; index < count; index++ {
		bounds := screenshot.GetDisplayBounds(index)
		if !bounds.Empty() {
			displays = append(displays, Display{
				Index: index, Bounds: bounds, Work: bounds,
				Primary: bounds.Min == (image.Point{}),
			})
		}
	}
	return displays, nil
}

func platformCapture(bounds image.Rectangle) (*image.RGBA, error) {
	pixels, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("desktop screenshot: %w", err)
	}
	return pixels, nil
}

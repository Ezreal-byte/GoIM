//go:build !windows && !linux && (!darwin || !cgo)

package capture

import "image"

func platformDisplays() ([]Display, error) {
	return nil, ErrUnsupported
}

func platformCapture(image.Rectangle) (*image.RGBA, error) {
	return nil, ErrUnsupported
}

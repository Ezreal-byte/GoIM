// Package video owns webcam discovery, native video capture, and compact JPEG
// frame encoding for GoIM direct video calls.
package video

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrUnsupported = errors.New("video: camera capture is not supported on this platform")
	ErrClosed      = errors.New("video: camera capture is closed")
	ErrNoDevice    = errors.New("video: no camera detected")
	ErrDevice      = errors.New("video: camera is unavailable")
)

// Device identifies one video capture endpoint.
type Device struct {
	ID      string
	Name    string
	Default bool
}

// Frame is a tightly packed, top-down BGRA image (4 bytes per pixel).
type Frame struct {
	Width, Height int
	BGRA          []byte
}

// Capture reads live frames from a local camera.
type Capture interface {
	ReadFrame(ctx context.Context) (Frame, error)
	Close() error
}

// Enumerate lists currently connected cameras.
func Enumerate() (devices []Device, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			devices, err = nil, fmt.Errorf("video: enumerate failed: %v", recovered)
		}
	}()
	return enumerate()
}

// OpenCapture opens the requested camera (or the default camera when id == "").
func OpenCapture(id string) (capture Capture, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			capture, err = nil, fmt.Errorf("%w: %v", ErrDevice, recovered)
		}
	}()
	return openCapture(id)
}

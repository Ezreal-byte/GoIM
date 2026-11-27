//go:build !windows && !linux && !(darwin && cgo)

package video

func enumerate() ([]Device, error) {
	return nil, ErrUnsupported
}

func openCapture(string) (Capture, error) {
	return nil, ErrUnsupported
}

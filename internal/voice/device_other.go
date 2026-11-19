//go:build !windows && !(linux && (amd64 || arm64)) && !darwin

package voice

func enumerate() Devices {
	return Devices{Problem: ErrUnsupported.Error()}
}

func openCapture(string) (Capture, error) {
	return nil, ErrUnsupported
}

func openPlayback(string) (Playback, error) {
	return nil, ErrUnsupported
}

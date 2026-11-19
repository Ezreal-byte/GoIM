// Package voice owns microphone capture, speaker playback, device selection,
// and the compact wire codec used by GoIM direct voice calls.
package voice

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupported = errors.New("voice: audio devices are not supported on this platform")
	ErrClosed      = errors.New("voice: audio stream is closed")
	ErrDevice      = errors.New("voice: audio device is unavailable")
)

const (
	SampleRate    = 16000
	FrameMillis   = 20
	FrameSamples  = SampleRate * FrameMillis / 1000
	PCMFrameBytes = FrameSamples * 2
)

// Device identifies one capture or playback endpoint. IDs are stable for the
// current boot/session and are the only device data persisted in settings.
type Device struct {
	ID      string
	Name    string
	Default bool
}

type Devices struct {
	Microphones []Device
	Speakers    []Device
	Problem     string
}

// Capture returns signed 16-bit little-endian mono PCM at 16 kHz.
type Capture interface {
	Read([]byte) (int, error)
	Close() error
}

// Playback accepts signed 16-bit little-endian mono PCM at 16 kHz.
type Playback interface {
	Write([]byte) (int, error)
	Close() error
}

func Enumerate() (devices Devices) {
	defer func() {
		if recovered := recover(); recovered != nil {
			devices = Devices{Problem: fmt.Sprintf("音频设备枚举失败: %v", recovered)}
		}
	}()
	return enumerate()
}

func OpenCapture(id string) (capture Capture, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			capture, err = nil, fmt.Errorf("%w: %v", ErrUnsupported, recovered)
		}
	}()
	return openCapture(id)
}

func OpenPlayback(id string) (playback Playback, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			playback, err = nil, fmt.Errorf("%w: %v", ErrUnsupported, recovered)
		}
	}()
	return openPlayback(id)
}

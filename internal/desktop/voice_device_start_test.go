package desktop

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goim/internal/domain"
	voiceaudio "goim/internal/voice"
)

func TestSpeakerChangeWhileCallDevicesAreOpening(t *testing.T) {
	for _, test := range []struct {
		name     string
		firstErr error
	}{
		{name: "old speaker opens"},
		{name: "old speaker becomes unavailable", firstErr: errors.New("speaker unplugged")},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := newFakeVoiceAudioBackend()
			commands := make(chan domain.Command, 4)
			r := newVoiceAudioRuntime(backend, func(c domain.Command) { commands <- c }, nil)
			defer r.close()
			opening, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			old, next := newFakeVoicePlayback(), newFakeVoicePlayback()
			nextOpening := make(chan struct{})
			var capture atomic.Pointer[fakeVoiceCapture]
			r.devices = voiceDevices{
				openCapture: func(string) (voiceaudio.Capture, error) {
					if previous := capture.Load(); previous != nil {
						select {
						case <-previous.closed:
						default:
							return nil, errors.New("microphone already in use")
						}
					}
					mic := newFakeVoiceCapture()
					capture.Store(mic)
					return mic, nil
				},
				openPlayback: func(id string) (voiceaudio.Playback, error) {
					if id == "headset" {
						close(nextOpening)
						return next, nil
					}
					close(opening)
					<-release
					if test.firstErr != nil {
						return nil, test.firstErr
					}
					return old, nil
				},
			}
			settings := domain.DefaultSettings("")
			call := domain.VoiceCall{ID: "starting", Status: domain.VoiceCallActive}
			r.sync(call, settings)
			waitForDeviceTransition(t, opening, commands, "opening the first speaker")
			call.Audio = domain.CallAudioOptions{HasSpeaker: true, Speaker: "headset"}
			r.sync(call, settings)
			unblock()
			waitForDeviceTransition(t, nextOpening, commands, "opening the requested speaker")
			if test.firstErr == nil {
				waitForDeviceTransition(t, old.closed, commands, "releasing the old speaker")
			}
			backend.audio <- []byte{42}
			receiveVoiceBytes(t, next.writes, "audio on the new speaker")
		})
	}
}

type unpluggedVoiceCapture struct {
	*fakeVoiceCapture
	reportUnplugged func()
}

func (c unpluggedVoiceCapture) Read(p []byte) (int, error) {
	n, err := c.fakeVoiceCapture.Read(p)
	if errors.Is(err, io.EOF) {
		c.reportUnplugged()
	}
	return n, err
}

func TestOldMicrophoneUnpluggedWhileReplacementOpens(t *testing.T) {
	backend := newFakeVoiceAudioBackend()
	commands := make(chan domain.Command, 4)
	r := newVoiceAudioRuntime(backend, func(c domain.Command) { commands <- c }, nil)
	defer r.close()
	opening, release, unplugged := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	old := unpluggedVoiceCapture{newFakeVoiceCapture(), sync.OnceFunc(func() { close(unplugged) })}
	next := newFakeVoiceCapture()
	r.devices = voiceDevices{
		openCapture: func(id string) (voiceaudio.Capture, error) {
			if id == "headset" {
				close(opening)
				<-release
				return next, nil
			}
			return old, nil
		},
		openPlayback: func(string) (voiceaudio.Playback, error) { return newFakeVoicePlayback(), nil },
	}
	settings := domain.DefaultSettings("")
	call := domain.VoiceCall{ID: "active", Status: domain.VoiceCallActive}
	r.sync(call, settings)
	waitVoiceSession(t, r)
	call.Audio = domain.CallAudioOptions{HasMicrophone: true, Microphone: "headset"}
	r.sync(call, settings)
	waitForDeviceTransition(t, opening, commands, "opening the selected microphone")
	old.Close()
	waitForDeviceTransition(t, unplugged, commands, "reading from the unplugged microphone")
	unblock()
	next.frames <- make([]byte, domain.VoiceFrameBytes)
	select {
	case <-backend.sent:
	case c := <-commands:
		t.Fatalf("replacing an unplugged microphone sent %s", c.Kind)
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not resume on the replacement microphone")
	}
}

func waitForDeviceTransition(t *testing.T, ready <-chan struct{}, commands <-chan domain.Command, stage string) {
	t.Helper()
	select {
	case <-ready:
	case c := <-commands:
		t.Fatalf("%s sent unexpected command %s", stage, c.Kind)
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out %s", stage)
	}
}

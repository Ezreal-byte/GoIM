package desktop

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"goim/internal/domain"
	voiceaudio "goim/internal/voice"
)

func TestIncomingRingtonePlaysOnceAndStopsOnEveryTransition(t *testing.T) {
	for _, status := range []domain.VoiceCallStatus{domain.VoiceCallActive, domain.VoiceCallEnded, ""} {
		t.Run(string(status), func(t *testing.T) {
			var ring incomingRingtone
			t.Cleanup(ring.close)
			playback := newFakeVoicePlayback()
			var opens atomic.Int32
			devices := voiceDevices{openPlayback: func(id string) (voiceaudio.Playback, error) {
				if id != "chosen-speaker" {
					t.Errorf("speaker=%q", id)
				}
				opens.Add(1)
				return playback, nil
			}}
			settings := domain.DefaultSettings(t.TempDir())
			settings.Sound, settings.VoiceSpeaker = true, "chosen-speaker"
			call := domain.VoiceCall{ID: "incoming", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging}
			for range 10 {
				ring.sync(call, settings, devices)
			}
			select {
			case pcm := <-playback.writes:
				if bytes.Equal(pcm, make([]byte, len(pcm))) {
					t.Fatal("ringtone is silent")
				}
			case <-time.After(time.Second):
				t.Fatal("ringtone did not start")
			}
			if opens.Load() != 1 {
				t.Fatalf("opened %d streams", opens.Load())
			}
			call.Status = status
			done := ring.sync(call, settings, devices)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("ringtone did not stop")
			}
			select {
			case <-playback.closed:
			default:
				t.Fatal("stream still open after stop")
			}
		})
	}
}

func TestRingtoneCancellationDuringDeviceOpenClosesLateStream(t *testing.T) {
	var ring incomingRingtone
	started, release := make(chan struct{}), make(chan struct{})
	playback := newFakeVoicePlayback()
	devices := voiceDevices{openPlayback: func(string) (voiceaudio.Playback, error) { close(started); <-release; return playback, nil }}
	settings := domain.DefaultSettings(t.TempDir())
	settings.Sound = true
	call := domain.VoiceCall{ID: "call", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging}
	ring.sync(call, settings, devices)
	<-started
	ring.close()
	close(release)
	select {
	case <-playback.closed:
	case <-time.After(time.Second):
		t.Fatal("late playback leaked")
	}
	select {
	case <-playback.writes:
		t.Fatal("canceled ringtone played")
	default:
	}
}

func TestRingtoneRespectsSoundSettingAndCallDirection(t *testing.T) {
	var ring incomingRingtone
	defer ring.close()
	devices := voiceDevices{openPlayback: func(string) (voiceaudio.Playback, error) {
		t.Error("unexpected playback")
		return nil, voiceaudio.ErrDevice
	}}
	settings := domain.DefaultSettings(t.TempDir())
	settings.Sound = false
	call := domain.VoiceCall{ID: "call", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging}
	ring.sync(call, settings, devices)
	if ring.session != nil {
		t.Fatal("disabled notification sound rang")
	}
	settings.Sound = true
	call.Direction = domain.VoiceCallOutgoing
	ring.sync(call, settings, devices)
	if ring.session != nil {
		t.Fatal("outgoing call rang")
	}
}

type delayedRingClose struct {
	voiceaudio.Playback
	started, release chan struct{}
}

func (p *delayedRingClose) Close() error {
	close(p.started)
	<-p.release
	return p.Playback.Close()
}

func TestRingtoneKeepsSpeakerBarrierWhenSoundIsDisabledBeforeAnswer(t *testing.T) {
	var ring incomingRingtone
	defer ring.close()
	base := newFakeVoicePlayback()
	playback := &delayedRingClose{Playback: base, started: make(chan struct{}), release: make(chan struct{})}
	devices := voiceDevices{openPlayback: func(string) (voiceaudio.Playback, error) { return playback, nil }}
	settings := domain.DefaultSettings(t.TempDir())
	settings.Sound = true
	call := domain.VoiceCall{ID: "call", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging}
	ring.sync(call, settings, devices)
	select {
	case <-base.writes:
	case <-time.After(time.Second):
		t.Fatal("no ring")
	}
	settings.Sound = false
	ring.sync(call, settings, devices)
	<-playback.started
	call.Status = domain.VoiceCallActive
	done := ring.sync(call, settings, devices)
	select {
	case <-done:
		t.Fatal("speaker barrier released before Close finished")
	default:
	}
	close(playback.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("speaker barrier never released")
	}
}

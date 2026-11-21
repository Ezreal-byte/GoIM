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
			// Real recordings can start with a short silence. Ensure the
			// playback stream delivers non-silent audio within the first second.
			deadline := time.After(time.Second)
			audible := false
			for !audible {
				select {
				case pcm := <-playback.writes:
					audible = !bytes.Equal(pcm, make([]byte, len(pcm)))
				case <-deadline:
					t.Fatal("ringtone did not become audible")
				}
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

func TestIncomingTransferChimeRespectsHistoryAndMutedChats(t *testing.T) {
	r := desktopRuntime{heardTransfers: map[string]bool{"old": true}, seen: map[string]bool{"chat-file": true}}
	s := domain.State{Conversations: []domain.Conversation{{ID: "quiet", Muted: true}}}
	// A print attachment has its own message ID and already uses chat notifications.
	if r.transferNeedsChime(&s, domain.Transfer{ID: "print-task", PrintJobID: "print-job", Direction: "receive", Status: "completed"}) {
		t.Fatal("printed attachment played a second notification")
	}
	for _, tc := range []struct {
		id, status, direction, conversation, file string
		want                                      bool
	}{
		{"old", "completed", "receive", "", "", false},
		{"upload", "offered", "receive", "", "", true},
		{"upload", "completed", "receive", "", "", false},
		{"auto", "transferring", "receive", "", "", false},
		{"auto", "completed", "receive", "", "", true},
		{"chat", "offered", "receive", "chat", "chat-file", false},
		{"silent", "offered", "receive", "quiet", "", false},
		{"out", "completed", "send", "", "", false},
	} {
		task := domain.Transfer{ID: tc.id, Status: tc.status, Direction: tc.direction, ConversationID: tc.conversation, FileID: tc.file}
		if got := r.transferNeedsChime(&s, task); got != tc.want {
			t.Fatalf("%s %s chime = %t, want %t", tc.id, tc.status, got, tc.want)
		}
	}
}

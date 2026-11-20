package desktop

import (
	_ "embed"
	"sync"
	"sync/atomic"
	"time"

	"goim/internal/domain"
	voiceaudio "goim/internal/voice"
)

// incomingRingtone has no idle worker. The one worker exists only while an
// incoming call is ringing and never requests UI frames or opens a microphone.
type incomingRingtone struct {
	mu       sync.Mutex
	closed   bool
	session  *ringtoneSession
	stopping <-chan struct{}
}

type ringtoneSession struct {
	id, speaker    string
	volume         atomic.Int32
	done, finished chan struct{}
	stopOnce       sync.Once
	mu             sync.Mutex
	playback       voiceaudio.Playback
}

func (r *incomingRingtone) sync(call domain.VoiceCall, settings domain.Settings, devices voiceDevices) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	ring := !r.closed && settings.Sound && call.ID != "" && call.Direction == domain.VoiceCallIncoming && call.Status == domain.VoiceCallRinging
	if session := r.session; session != nil && ring && session.id == call.ID && session.speaker == settings.VoiceSpeaker {
		session.volume.Store(int32(domain.ClampVoiceVolume(settings.VoiceSpeakerVolume)))
		return nil
	}
	finished := r.stopping
	if r.session != nil {
		r.session.stop()
		finished = r.session.finished
		r.stopping = finished
		r.session = nil
	}
	if ring {
		session := &ringtoneSession{id: call.ID, speaker: settings.VoiceSpeaker, done: make(chan struct{}), finished: make(chan struct{})}
		session.volume.Store(int32(domain.ClampVoiceVolume(settings.VoiceSpeakerVolume)))
		r.session = session
		go session.run(devices.openPlayback, finished, callMelody, true)
	}
	return finished
}

func (r *incomingRingtone) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.session != nil {
		r.session.stop()
		r.session = nil
	}
}

func (s *ringtoneSession) stop() {
	s.stopOnce.Do(func() { close(s.done); go s.closePlayback() })
}

func (s *ringtoneSession) closePlayback() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.playback != nil {
		_ = s.playback.Close()
		s.playback = nil
	}
}

func (s *ringtoneSession) run(open func(string) (voiceaudio.Playback, error), previous <-chan struct{}, cycle []byte, repeat bool) {
	defer close(s.finished)
	if previous != nil {
		// Keep the close barrier even if this queued ringtone is canceled.
		// An answered call must not open the speaker before an older ring exits.
		<-previous
	}
	select {
	case <-s.done:
		return
	default:
	}
	playback, err := open(s.speaker)
	if err != nil {
		return
	} // A missing speaker must not reject the incoming call.
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		_ = playback.Close()
		return
	default:
	}
	s.playback = playback
	s.mu.Unlock()
	defer s.closePlayback()
	var frame [voiceaudio.PCMFrameBytes]byte
	timer := time.NewTimer(0)
	defer timer.Stop()
	position := 0
	for {
		select {
		case <-s.done:
			return
		case <-timer.C:
		}
		copy(frame[:], cycle[position:position+len(frame)])
		voiceaudio.ScalePCM16LE(frame[:], int(s.volume.Load()))
		data := frame[:]
		for len(data) > 0 {
			n, err := playback.Write(data)
			if err != nil || n <= 0 || n > len(data) {
				return
			}
			data = data[n:]
		}
		position += len(frame)
		if position == len(cycle) {
			if !repeat {
				return
			}
			position = 0
		}
		timer.Reset(voiceaudio.FrameMillis * time.Millisecond)
	}
}

// Pre-rendered mono 16 kHz signed PCM, padded to complete playback frames.
// Source and attribution are recorded in THIRD_PARTY_NOTICES.md.
//
//go:embed sounds/phone-incoming-call.pcm
var callMelody []byte

//go:embed sounds/message-new-instant.pcm
var messageChime []byte

// A burst of messages shares one chime. This is called on the desktop thread.
func (r *desktopRuntime) playMessageChime(settings domain.Settings) {
	if last := r.messageSound; last != nil {
		select {
		case <-last.finished:
		default:
			return
		}
	}
	session := &ringtoneSession{speaker: settings.VoiceSpeaker, done: make(chan struct{}), finished: make(chan struct{})}
	session.volume.Store(int32(domain.ClampVoiceVolume(settings.VoiceSpeakerVolume)))
	r.messageSound = session
	go session.run(systemVoiceDevices.openPlayback, nil, messageChime, false)
}

package desktop

import (
	"encoding/binary"
	"math"
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
		go session.run(devices.openPlayback, finished)
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

func (s *ringtoneSession) run(open func(string) (voiceaudio.Playback, error), previous <-chan struct{}) {
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
	cycle := ringtonePCM()
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
		position = (position + len(frame)) % len(cycle)
		timer.Reset(voiceaudio.FrameMillis * time.Millisecond)
	}
}

// Two short, softly ramped tones followed by a pause. PCM is generated locally,
// so the ringtone has no platform sound-theme or external asset dependency.
func ringtonePCM() []byte {
	pcm := make([]byte, 2*voiceaudio.SampleRate*2)
	for sample := range 2 * voiceaudio.SampleRate {
		seconds := float64(sample) / voiceaudio.SampleRate
		pulse := seconds
		if seconds >= .55 {
			pulse = seconds - .55
		}
		if pulse < 0 || pulse >= .35 || seconds >= .90 {
			continue
		}
		ramp := min(1., pulse/.015, (.35-pulse)/.015)
		value := int16(6000 * ramp * (math.Sin(2*math.Pi*440*seconds) + math.Sin(2*math.Pi*480*seconds)))
		binary.LittleEndian.PutUint16(pcm[sample*2:], uint16(value))
	}
	return pcm
}

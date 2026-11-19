package desktop

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"goim/internal/domain"
	"goim/internal/i18n"
	voiceaudio "goim/internal/voice"
)

// voiceDevices keeps the native audio backend behind a small seam so call
// orchestration can be exercised without touching a physical microphone.
type voiceDevices struct {
	openCapture  func(string) (voiceaudio.Capture, error)
	openPlayback func(string) (voiceaudio.Playback, error)
}

var systemVoiceDevices = voiceDevices{
	openCapture:  voiceaudio.OpenCapture,
	openPlayback: voiceaudio.OpenPlayback,
}

// voiceAudioRuntime owns the microphone and speaker for the one active direct
// call. It is driven by detached snapshots on OnWake; it never polls the backend
// and never persists media.
type voiceAudioRuntime struct {
	mu               sync.Mutex
	backend          domain.VoiceAudioBackend
	devices          voiceDevices
	submit           func(domain.Command)
	invalidate       func()
	desiredID        string
	failedID         string
	generation       uint64
	starting         bool
	session          *voiceAudioSession
	muted            bool
	speakerMuted     bool
	microphoneVolume int
	speakerVolume    int
	pendingNotice    string
	closed           bool
}

func newVoiceAudioRuntime(backend domain.Backend, submit func(domain.Command), invalidate func()) *voiceAudioRuntime {
	runtime := &voiceAudioRuntime{
		devices: systemVoiceDevices, submit: submit, invalidate: invalidate,
	}
	if audio, ok := backend.(domain.VoiceAudioBackend); ok {
		runtime.backend = audio
	}
	return runtime
}

// sync starts the device streams only after a call becomes active and tears
// them down on every other transition. Opening devices happens on a worker so a
// slow driver cannot stall the UI wake path.
func (runtime *voiceAudioRuntime) sync(call domain.VoiceCall, settings domain.Settings) {
	if runtime == nil {
		return
	}
	var (
		stop             *voiceAudioSession
		start            bool
		generation       uint64
		devices          voiceDevices
		muted            bool
		speaker          bool
		microphoneVolume int
		speakerVolume    int
	)
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return
	}
	runtime.muted, runtime.speakerMuted = call.Muted, call.SpeakerMuted
	runtime.microphoneVolume = domain.ClampVoiceVolume(settings.VoiceMicrophoneVolume)
	runtime.speakerVolume = domain.ClampVoiceVolume(settings.VoiceSpeakerVolume)
	active := call.ID != "" && call.Status == domain.VoiceCallActive
	if !active {
		runtime.failedID = ""
		if runtime.desiredID != "" || runtime.session != nil || runtime.starting {
			runtime.desiredID = ""
			runtime.generation++
			runtime.starting = false
			stop, runtime.session = runtime.session, nil
		}
		runtime.mu.Unlock()
		if stop != nil {
			stop.stop()
		}
		return
	}
	if runtime.failedID != "" && runtime.failedID != call.ID {
		runtime.failedID = ""
	}
	if runtime.failedID == call.ID {
		if runtime.session != nil {
			runtime.session.setControls(runtime.muted, runtime.speakerMuted, runtime.microphoneVolume, runtime.speakerVolume)
		}
		runtime.mu.Unlock()
		return
	}
	if runtime.desiredID != call.ID {
		runtime.desiredID = call.ID
		runtime.generation++
		runtime.starting = false
		stop, runtime.session = runtime.session, nil
	}
	if runtime.session != nil && runtime.desiredID == call.ID {
		runtime.session.setControls(runtime.muted, runtime.speakerMuted, runtime.microphoneVolume, runtime.speakerVolume)
		runtime.mu.Unlock()
		if stop != nil {
			stop.stop()
		}
		return
	}
	if !runtime.starting {
		runtime.starting = true
		start = true
		generation = runtime.generation
		devices = runtime.devices
		muted, speaker = runtime.muted, runtime.speakerMuted
		microphoneVolume, speakerVolume = runtime.microphoneVolume, runtime.speakerVolume
	}
	runtime.mu.Unlock()
	if stop != nil {
		stop.stop()
	}
	if start {
		go runtime.open(call.ID, settings.VoiceMicrophone, settings.VoiceSpeaker, devices, generation, muted, speaker, microphoneVolume, speakerVolume)
	}
}

func (runtime *voiceAudioRuntime) open(callID, microphone, speaker string, devices voiceDevices, generation uint64, muted, speakerMuted bool, microphoneVolume, speakerVolume int) {
	if runtime.backend == nil {
		runtime.fail(callID, nil, i18n.T("当前版本无法建立语音音频通道"))
		return
	}
	capture, err := devices.openCapture(microphone)
	if err != nil {
		runtime.fail(callID, nil, i18n.T("无法打开麦克风：")+err.Error())
		return
	}
	playback, err := devices.openPlayback(speaker)
	if err != nil {
		_ = capture.Close()
		runtime.fail(callID, nil, i18n.T("无法打开扬声器：")+err.Error())
		return
	}
	audio := runtime.backend.VoiceAudio(callID)
	if audio == nil {
		_ = capture.Close()
		_ = playback.Close()
		runtime.fail(callID, nil, i18n.T("语音音频通道不可用"))
		return
	}
	session := &voiceAudioSession{
		callID: callID, capture: capture, playback: playback, audio: audio, done: make(chan struct{}),
	}
	session.muted.Store(muted)
	session.speakerMuted.Store(speakerMuted)
	session.microphoneVolume.Store(int32(microphoneVolume))
	session.speakerVolume.Store(int32(speakerVolume))

	runtime.mu.Lock()
	if runtime.closed || runtime.generation != generation || runtime.desiredID != callID {
		runtime.mu.Unlock()
		session.stop()
		return
	}
	runtime.session = session
	runtime.starting = false
	runtime.mu.Unlock()
	go session.run(runtime.backend, func(err error) { runtime.fail(callID, session, err.Error()) })
}

func (runtime *voiceAudioRuntime) fail(callID string, session *voiceAudioSession, message string) {
	runtime.mu.Lock()
	if runtime.closed || runtime.desiredID != callID || session != nil && runtime.session != session {
		runtime.mu.Unlock()
		return
	}
	runtime.desiredID = ""
	runtime.failedID = callID
	runtime.generation++
	runtime.starting = false
	if runtime.session == session {
		runtime.session = nil
	}
	if runtime.pendingNotice == "" {
		runtime.pendingNotice = message
	}
	submit, invalidate := runtime.submit, runtime.invalidate
	runtime.mu.Unlock()
	if session != nil {
		session.stop()
	}
	if submit != nil {
		submit(domain.Command{Kind: domain.EndVoiceCall, CallID: callID})
	}
	if invalidate != nil {
		invalidate()
	}
}

func (runtime *voiceAudioRuntime) takeNotice() string {
	if runtime == nil {
		return ""
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	notice := runtime.pendingNotice
	runtime.pendingNotice = ""
	return notice
}

func (runtime *voiceAudioRuntime) close() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return
	}
	runtime.closed = true
	runtime.desiredID = ""
	runtime.generation++
	runtime.starting = false
	session := runtime.session
	runtime.session = nil
	runtime.mu.Unlock()
	if session != nil {
		session.stop()
	}
}

type voiceAudioSession struct {
	callID           string
	capture          voiceaudio.Capture
	playback         voiceaudio.Playback
	audio            <-chan []byte
	done             chan struct{}
	stopOnce         sync.Once
	failOnce         sync.Once
	muted            atomic.Bool
	speakerMuted     atomic.Bool
	microphoneVolume atomic.Int32
	speakerVolume    atomic.Int32
}

func (session *voiceAudioSession) run(backend domain.VoiceAudioBackend, failed func(error)) {
	go session.captureLoop(backend, failed)
	go session.playbackLoop(failed)
}

func (session *voiceAudioSession) setControls(muted, speakerMuted bool, microphoneVolume, speakerVolume int) {
	session.muted.Store(muted)
	session.speakerMuted.Store(speakerMuted)
	session.microphoneVolume.Store(int32(microphoneVolume))
	session.speakerVolume.Store(int32(speakerVolume))
}

func (session *voiceAudioSession) captureLoop(backend domain.VoiceAudioBackend, failed func(error)) {
	pcm := make([]byte, domain.VoiceFrameBytes)
	encoded := make([]byte, domain.VoiceEncodedBytes)
	for {
		if _, err := io.ReadFull(session.capture, pcm); err != nil {
			if !session.stopped() {
				session.fail(failed, fmt.Errorf("%s%w", i18n.T("麦克风读取失败："), err))
			}
			return
		}
		if session.stopped() {
			return
		}
		if session.muted.Load() || session.microphoneVolume.Load() == 0 {
			continue
		}
		voiceaudio.ScalePCM16LE(pcm, int(session.microphoneVolume.Load()))
		frame := voiceaudio.EncodePCMUInto(encoded, pcm)
		if err := backend.SendVoiceAudio(session.callID, frame); err != nil {
			if !session.stopped() {
				session.fail(failed, fmt.Errorf("%s%w", i18n.T("语音发送中断："), err))
			}
			return
		}
	}
}

func (session *voiceAudioSession) playbackLoop(failed func(error)) {
	pcm := make([]byte, domain.VoiceMaxEncodedSize*2)
	for {
		select {
		case encoded, ok := <-session.audio:
			if !ok {
				return
			}
			if session.speakerMuted.Load() || session.speakerVolume.Load() == 0 {
				continue
			}
			decoded := voiceaudio.DecodePCMUInto(pcm, encoded)
			if len(decoded) == 0 {
				continue
			}
			voiceaudio.ScalePCM16LE(decoded, int(session.speakerVolume.Load()))
			if _, err := session.playback.Write(decoded); err != nil {
				if !session.stopped() {
					session.fail(failed, fmt.Errorf("%s%w", i18n.T("扬声器播放失败："), err))
				}
				return
			}
		case <-session.done:
			return
		}
	}
}

func (session *voiceAudioSession) stopped() bool {
	select {
	case <-session.done:
		return true
	default:
		return false
	}
}

func (session *voiceAudioSession) fail(failed func(error), err error) {
	session.failOnce.Do(func() {
		session.stop()
		failed(err)
	})
}

func (session *voiceAudioSession) stop() {
	session.stopOnce.Do(func() {
		close(session.done)
		go func() {
			_ = session.capture.Close()
			_ = session.playback.Close()
		}()
	})
}

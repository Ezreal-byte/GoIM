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
	ringtone                incomingRingtone
	mu                      sync.Mutex
	backend                 domain.VoiceAudioBackend
	devices                 voiceDevices
	submit                  func(domain.Command)
	invalidate              func()
	desiredID               string
	microphoneID, speakerID string
	failedID                string
	generation              uint64
	starting                bool
	session                 *voiceAudioSession
	muted                   bool
	speakerMuted            bool
	microphoneVolume        int
	speakerVolume           int
	pendingNotice           string
	closed                  bool
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
	settings = call.AudioSettings(settings)
	ringtoneStopped := runtime.ringtone.sync(call, settings, runtime.devices)
	var (
		stop       *voiceAudioSession
		start      bool
		generation uint64
		devices    voiceDevices
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
	runtime.microphoneID, runtime.speakerID = settings.VoiceMicrophone, settings.VoiceSpeaker
	if runtime.desiredID != call.ID {
		runtime.desiredID = call.ID
		runtime.generation++
		runtime.starting = false
		stop, runtime.session = runtime.session, nil
	}
	if runtime.session != nil && runtime.desiredID == call.ID {
		runtime.session.setControls(runtime.muted, runtime.speakerMuted, runtime.microphoneVolume, runtime.speakerVolume)
		runtime.syncDevicesLocked(runtime.session)
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
	}
	runtime.mu.Unlock()
	if stop != nil {
		stop.stop()
	}
	if start {
		go func() {
			if ringtoneStopped != nil {
				<-ringtoneStopped
			}
			runtime.mu.Lock()
			current := !runtime.closed && runtime.generation == generation && runtime.desiredID == call.ID
			runtime.mu.Unlock()
			if current {
				runtime.open(call.ID, devices, generation)
			}
		}()
	}
}

func (runtime *voiceAudioRuntime) syncDevicesLocked(session *voiceAudioSession) {
	switchCallDevice(runtime, session, &session.capture, runtime.microphoneID, runtime.devices.openCapture, domain.SetCallMicrophone)
	switchCallDevice(runtime, session, &session.playback, runtime.speakerID, runtime.devices.openPlayback, domain.SetCallSpeaker)
}

func (runtime *voiceAudioRuntime) open(callID string, devices voiceDevices, generation uint64) {
	if runtime.backend == nil {
		runtime.fail(callID, generation, nil, i18n.T("当前版本无法建立语音音频通道"))
		return
	}
	capture := openInitialDevice(runtime, callID, generation, &runtime.microphoneID, devices.openCapture, "无法打开麦克风：")
	if capture == nil {
		return
	}
	playback := openInitialDevice(runtime, callID, generation, &runtime.speakerID, devices.openPlayback, "无法打开扬声器：")
	if playback == nil {
		_ = capture.stream.Close()
		return
	}
	audio := runtime.backend.VoiceAudio(callID)
	if audio == nil {
		_ = capture.stream.Close()
		_ = playback.stream.Close()
		runtime.fail(callID, generation, nil, i18n.T("语音音频通道不可用"))
		return
	}
	session := &voiceAudioSession{
		callID: callID, audio: audio, done: make(chan struct{}),
	}
	session.capture.install(capture)
	session.playback.install(playback)
	runtime.mu.Lock()
	if runtime.closed || runtime.generation != generation || runtime.desiredID != callID {
		runtime.mu.Unlock()
		session.stop()
		return
	}
	// Device drivers can block while the user changes call controls. Apply the
	// latest values before either stream starts, especially microphone privacy.
	session.setControls(runtime.muted, runtime.speakerMuted, runtime.microphoneVolume, runtime.speakerVolume)
	runtime.session = session
	runtime.starting = false
	runtime.syncDevicesLocked(session)
	runtime.mu.Unlock()
	session.run(runtime.backend, func(err error) { runtime.fail(callID, generation, session, err.Error()) })
}

func (runtime *voiceAudioRuntime) fail(callID string, generation uint64, session *voiceAudioSession, message string) {
	runtime.mu.Lock()
	runtime.failLocked(callID, generation, session, message)
}

// Consumes runtime.mu and releases it before dispatching the UI and backend callbacks.
func (runtime *voiceAudioRuntime) failLocked(callID string, generation uint64, session *voiceAudioSession, message string) {
	if runtime.closed || runtime.generation != generation || runtime.desiredID != callID || session != nil && runtime.session != session {
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
	runtime.ringtone.close()
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
	capture          callDevice[voiceaudio.Capture]
	playback         callDevice[voiceaudio.Playback]
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
		device := session.capture.active.Load()
		_, err := io.ReadFull(device.stream, pcm)
		if device != session.capture.active.Load() {
			continue
		}
		if err != nil {
			if session.capture.waitForReplacement(device, session.done) {
				continue
			}
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
			device := session.playback.active.Load()
			if _, err := device.stream.Write(decoded); err != nil {
				if session.playback.waitForReplacement(device, session.done) {
					continue
				}
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
		failed(err)
	})
}

func (session *voiceAudioSession) stop() {
	session.stopOnce.Do(func() {
		close(session.done)
		go func() {
			_ = session.capture.active.Load().stream.Close()
			_ = session.playback.active.Load().stream.Close()
		}()
	})
}

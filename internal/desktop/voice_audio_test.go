package desktop

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/i18n"
	voiceaudio "goim/internal/voice"
)

type fakeVoiceAudioBackend struct {
	mu       sync.Mutex
	commands []domain.Command
	updates  chan struct{}
	audio    chan []byte
	sent     chan []byte
}

func newFakeVoiceAudioBackend() *fakeVoiceAudioBackend {
	return &fakeVoiceAudioBackend{
		updates: make(chan struct{}, 8),
		audio:   make(chan []byte, 8),
		sent:    make(chan []byte, 8),
	}
}

func (backend *fakeVoiceAudioBackend) Snapshot() domain.State { return domain.State{} }

func (backend *fakeVoiceAudioBackend) Submit(command domain.Command) {
	backend.mu.Lock()
	backend.commands = append(backend.commands, command)
	backend.mu.Unlock()
}

func (backend *fakeVoiceAudioBackend) Updates() <-chan struct{} { return backend.updates }

func (backend *fakeVoiceAudioBackend) VoiceAudio(string) <-chan []byte { return backend.audio }

func (backend *fakeVoiceAudioBackend) SendVoiceAudio(_ string, encoded []byte) error {
	backend.sent <- append([]byte(nil), encoded...)
	return nil
}

type fakeVoiceCapture struct {
	frames  chan []byte
	done    chan struct{}
	closed  chan struct{}
	closeMu sync.Once
}

func newFakeVoiceCapture() *fakeVoiceCapture {
	return &fakeVoiceCapture{
		frames: make(chan []byte, 4), done: make(chan struct{}), closed: make(chan struct{}),
	}
}

func (capture *fakeVoiceCapture) Read(pcm []byte) (int, error) {
	select {
	case frame := <-capture.frames:
		return copy(pcm, frame), nil
	case <-capture.done:
		return 0, io.EOF
	}
}

func (capture *fakeVoiceCapture) Close() error {
	capture.closeMu.Do(func() {
		close(capture.done)
		close(capture.closed)
	})
	return nil
}

type fakeVoicePlayback struct {
	writes  chan []byte
	done    chan struct{}
	closed  chan struct{}
	closeMu sync.Once
}

func newFakeVoicePlayback() *fakeVoicePlayback {
	return &fakeVoicePlayback{
		writes: make(chan []byte, 8), done: make(chan struct{}), closed: make(chan struct{}),
	}
}

func (playback *fakeVoicePlayback) Write(pcm []byte) (int, error) {
	frame := append([]byte(nil), pcm...)
	select {
	case playback.writes <- frame:
		return len(pcm), nil
	case <-playback.done:
		return 0, io.EOF
	}
}

func (playback *fakeVoicePlayback) Close() error {
	playback.closeMu.Do(func() {
		close(playback.done)
		close(playback.closed)
	})
	return nil
}

func receiveVoiceBytes(t *testing.T, stream <-chan []byte, description string) []byte {
	t.Helper()
	select {
	case frame := <-stream:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func requireNoVoiceBytes(t *testing.T, stream <-chan []byte, description string) {
	t.Helper()
	select {
	case frame := <-stream:
		t.Fatalf("%s unexpectedly delivered %x", description, frame)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestVoiceAudioRuntimeCarriesCapturePlaybackAndMutes(t *testing.T) {
	backend := newFakeVoiceAudioBackend()
	capture := newFakeVoiceCapture()
	playback := newFakeVoicePlayback()
	openedCapture := make(chan string, 1)
	openedPlayback := make(chan string, 1)
	runtime := newVoiceAudioRuntime(backend, backend.Submit, func() {})
	t.Cleanup(runtime.close)
	runtime.devices = voiceDevices{
		openCapture: func(id string) (voiceaudio.Capture, error) {
			openedCapture <- id
			return capture, nil
		},
		openPlayback: func(id string) (voiceaudio.Playback, error) {
			openedPlayback <- id
			return playback, nil
		},
	}
	settings := domain.Settings{
		VoiceMicrophone: "mic:studio", VoiceSpeaker: "speaker:room",
		VoiceMicrophoneVolume: 50, VoiceSpeakerVolume: 25,
	}
	call := domain.VoiceCall{ID: "call-audio", PeerID: "peer", Status: domain.VoiceCallActive}
	runtime.sync(call, settings)
	if id := <-openedCapture; id != "mic:studio" {
		t.Fatalf("capture device = %q", id)
	}
	if id := <-openedPlayback; id != "speaker:room" {
		t.Fatalf("playback device = %q", id)
	}

	pcm := make([]byte, domain.VoiceFrameBytes)
	for index := 0; index < len(pcm); index += 2 {
		sample := int16((index*7919)%30000 - 15000)
		pcm[index], pcm[index+1] = byte(uint16(sample)), byte(uint16(sample)>>8)
	}
	capture.frames <- pcm
	encoded := receiveVoiceBytes(t, backend.sent, "encoded microphone frame")
	scaledPCM := append([]byte(nil), pcm...)
	voiceaudio.ScalePCM16LE(scaledPCM, 50)
	if want := voiceaudio.EncodePCMU(scaledPCM); !bytes.Equal(encoded, want) {
		t.Fatalf("encoded microphone frame differs: got %x want %x", encoded, want)
	}

	remote := []byte{0x00, 0x40, 0x80, 0xc0, 0xff}
	backend.audio <- remote
	played := receiveVoiceBytes(t, playback.writes, "decoded remote frame")
	wantPlayed := voiceaudio.DecodePCMU(remote)
	voiceaudio.ScalePCM16LE(wantPlayed, 25)
	if !bytes.Equal(played, wantPlayed) {
		t.Fatalf("decoded remote frame differs: got %x want %x", played, wantPlayed)
	}

	settings.VoiceMicrophoneVolume, settings.VoiceSpeakerVolume = 20, 75
	runtime.sync(call, settings)
	capture.frames <- pcm
	encoded = receiveVoiceBytes(t, backend.sent, "adjusted microphone frame")
	scaledPCM = append(scaledPCM[:0], pcm...)
	voiceaudio.ScalePCM16LE(scaledPCM, 20)
	if want := voiceaudio.EncodePCMU(scaledPCM); !bytes.Equal(encoded, want) {
		t.Fatalf("adjusted microphone frame differs: got %x want %x", encoded, want)
	}
	backend.audio <- remote
	played = receiveVoiceBytes(t, playback.writes, "adjusted playback frame")
	wantPlayed = voiceaudio.DecodePCMU(remote)
	voiceaudio.ScalePCM16LE(wantPlayed, 75)
	if !bytes.Equal(played, wantPlayed) {
		t.Fatalf("adjusted playback frame differs: got %x want %x", played, wantPlayed)
	}

	call.Muted = true
	runtime.sync(call, settings)
	capture.frames <- pcm
	requireNoVoiceBytes(t, backend.sent, "muted microphone")

	call.Muted, call.SpeakerMuted = false, true
	runtime.sync(call, settings)
	backend.audio <- remote
	requireNoVoiceBytes(t, playback.writes, "muted speaker")

	call.SpeakerMuted = false
	runtime.sync(call, settings)
	capture.frames <- pcm
	_ = receiveVoiceBytes(t, backend.sent, "microphone after unmute")
	backend.audio <- remote
	_ = receiveVoiceBytes(t, playback.writes, "speaker after unmute")

	settings.VoiceMicrophoneVolume, settings.VoiceSpeakerVolume = 0, 0
	runtime.sync(call, settings)
	capture.frames <- pcm
	requireNoVoiceBytes(t, backend.sent, "zero-volume microphone")
	backend.audio <- remote
	requireNoVoiceBytes(t, playback.writes, "zero-volume speaker")

	runtime.sync(domain.VoiceCall{}, settings)
	select {
	case <-capture.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("microphone was not closed after the call")
	}
	select {
	case <-playback.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("speaker was not closed after the call")
	}
}

type plainVoiceBackend struct {
	mu       sync.Mutex
	commands []domain.Command
	updates  chan struct{}
}

func newPlainVoiceBackend() *plainVoiceBackend {
	return &plainVoiceBackend{updates: make(chan struct{}, 4)}
}

func (*plainVoiceBackend) Snapshot() domain.State { return domain.State{} }

func (backend *plainVoiceBackend) Submit(command domain.Command) {
	backend.mu.Lock()
	backend.commands = append(backend.commands, command)
	backend.mu.Unlock()
}

func (backend *plainVoiceBackend) Updates() <-chan struct{} { return backend.updates }

func (backend *plainVoiceBackend) commandCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.commands)
}

func TestVoiceAudioRuntimeEndsUnsupportedCallOnce(t *testing.T) {
	backend := newPlainVoiceBackend()
	runtime := newVoiceAudioRuntime(backend, backend.Submit, func() {})
	t.Cleanup(runtime.close)
	call := domain.VoiceCall{ID: "call-unsupported", Status: domain.VoiceCallActive}
	runtime.sync(call, domain.Settings{})
	deadline := time.Now().Add(2 * time.Second)
	for backend.commandCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if backend.commandCount() != 1 {
		t.Fatalf("unsupported audio commands = %d, want 1", backend.commandCount())
	}
	backend.mu.Lock()
	command := backend.commands[0]
	backend.mu.Unlock()
	if command.Kind != domain.EndVoiceCall || command.CallID != call.ID {
		t.Fatalf("unsupported audio command = %+v", command)
	}
	if notice := runtime.takeNotice(); notice == "" {
		t.Fatal("unsupported audio did not report a notice")
	}
	runtime.sync(call, domain.Settings{})
	time.Sleep(20 * time.Millisecond)
	if backend.commandCount() != 1 {
		t.Fatalf("unsupported audio was retried: %d commands", backend.commandCount())
	}
}

type failingVoiceCapture struct{}

func (failingVoiceCapture) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingVoiceCapture) Close() error             { return nil }

type idleVoicePlayback struct{}

func (idleVoicePlayback) Write(pcm []byte) (int, error) { return len(pcm), nil }
func (idleVoicePlayback) Close() error                  { return nil }

func TestVoiceAudioRuntimeReportsDeviceFailure(t *testing.T) {
	backend := newFakeVoiceAudioBackend()
	runtime := newVoiceAudioRuntime(backend, backend.Submit, func() {})
	t.Cleanup(runtime.close)
	runtime.devices = voiceDevices{
		openCapture: func(string) (voiceaudio.Capture, error) {
			return failingVoiceCapture{}, nil
		},
		openPlayback: func(string) (voiceaudio.Playback, error) {
			return idleVoicePlayback{}, nil
		},
	}
	call := domain.VoiceCall{ID: "call-failure", Status: domain.VoiceCallActive}
	runtime.sync(call, domain.Settings{})
	deadline := time.Now().Add(2 * time.Second)
	for {
		backend.mu.Lock()
		count := len(backend.commands)
		backend.mu.Unlock()
		if count > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("device failure did not end the call")
		}
		time.Sleep(time.Millisecond)
	}
	if notice := runtime.takeNotice(); !bytes.Contains([]byte(notice), []byte(i18n.T("麦克风读取失败："))) {
		t.Fatalf("device failure notice = %q", notice)
	}
	backend.mu.Lock()
	command := backend.commands[0]
	backend.mu.Unlock()
	if command.Kind != domain.EndVoiceCall || command.CallID != call.ID {
		t.Fatalf("device failure command = %+v", command)
	}
}

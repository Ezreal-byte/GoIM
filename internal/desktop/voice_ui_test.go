package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"

	"goim/internal/domain"
	voiceaudio "goim/internal/voice"
)

func voiceUIState(t *testing.T, call domain.VoiceCall) domain.State {
	t.Helper()
	state := shellTestState(t)
	state.Settings.LastSection = "messages"
	for index := range state.Peers {
		state.Peers[index].VoiceSupported = state.Peers[index].Online
	}
	state.VoiceCall = call
	return state
}

func openVoiceQA(t *testing.T, state domain.State, width, height int) (*UI, *vivaheadless.Session) {
	t.Helper()
	backend := newShellTestBackend(state)
	shell := New(backend)
	// Visual QA must never open a physical microphone or speaker.
	shell.voiceAudio = nil
	config := shell.Config()
	config.Width, config.Height = width, height
	session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	t.Cleanup(shell.Close)
	return shell, session
}

func TestVoiceCallOutgoingOverlayAndControls(t *testing.T) {
	call := domain.VoiceCall{
		ID: "call-outgoing", PeerID: "peer-a", PeerName: "Alice",
		Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging,
		StartedAt: time.Now(),
	}
	h := newShellTestHarness(t, voiceUIState(t, call))
	if !h.hasText("等待对方接听…") {
		t.Fatal("outgoing call does not show the waiting state")
	}
	for _, id := range []struct {
		name  string
		value gfx.ID
	}{
		{"mute", h.ui.ids.voiceMute},
		{"hang up", h.ui.ids.voiceHangup},
		{"speaker", h.ui.ids.voiceSpeaker},
	} {
		if !h.hasHit(id.value) {
			t.Fatalf("%s control is not on screen", id.name)
		}
	}
	h.clickID(h.ui.ids.voiceMute)
	command := h.lastCommand()
	if command.Kind != domain.SetVoiceMuted || command.CallID != call.ID || !command.Bool {
		t.Fatalf("mute command = %+v", command)
	}

	h.clickID(h.ui.ids.voiceSpeaker)
	command = h.lastCommand()
	if command.Kind != domain.SetVoiceSpeakerMuted || command.CallID != call.ID || !command.Bool {
		t.Fatalf("speaker command = %+v", command)
	}

	h.clickID(h.ui.ids.voiceHangup)
	command = h.lastCommand()
	if command.Kind != domain.EndVoiceCall || command.CallID != call.ID {
		t.Fatalf("hang-up command = %+v", command)
	}
}

func TestVoiceCallIncomingOverlayAcceptsAndDeclines(t *testing.T) {
	base := domain.VoiceCall{
		ID: "call-incoming", PeerID: "peer-a", PeerName: "Alice",
		Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging,
		StartedAt: time.Now(),
	}
	for _, action := range []struct {
		name string
		kind domain.CommandKind
	}{
		{"accept", domain.AcceptVoiceCall},
		{"decline", domain.RejectVoiceCall},
	} {
		t.Run(action.name, func(t *testing.T) {
			h := newShellTestHarness(t, voiceUIState(t, base))
			id := h.ui.ids.voiceAccept
			if action.name == "decline" {
				id = h.ui.ids.voiceReject
			}
			if !h.hasText("邀请你进行语音通话") {
				t.Fatal("incoming call does not show the invitation")
			}
			if !h.hasHit(id) {
				t.Fatalf("%s control is not on screen", action.name)
			}
			h.clickID(id)
			command := h.lastCommand()
			if command.Kind != action.kind || command.CallID != base.ID {
				t.Fatalf("%s command = %+v", action.name, command)
			}
		})
	}
}

func TestVoiceCallActiveOverlayShowsDurationAndSpeakerControl(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{
		ID: "call-active", PeerID: "peer-a", PeerName: "Alice",
		Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallActive,
		StartedAt: time.Now().Add(-time.Minute), ConnectedAt: time.Now().Add(-65 * time.Second),
	})
	h := newShellTestHarness(t, state)
	h.settle()
	h.backend.clearCommands()
	if !h.hasText("通话中") || !h.hasText("01:05") {
		t.Fatalf("active call does not show its duration: %#v", h.snapshot().Texts)
	}
	if !h.hasHit(h.ui.ids.voiceMute) || !h.hasHit(h.ui.ids.voiceSpeaker) || !h.hasHit(h.ui.ids.voiceHangup) {
		t.Fatal("active call controls are incomplete")
	}
	h.clickID(h.ui.ids.voiceSpeaker)
	command := h.lastCommand()
	if command.Kind != domain.SetVoiceSpeakerMuted || command.CallID != "call-active" || !command.Bool {
		t.Fatalf("speaker command = %+v", command)
	}
}

func TestChatHeaderStartsVoiceCallForSupportedOnlinePeer(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.LastSection = "messages"
	state.Peers = []domain.Peer{{
		ID: "peer-a", Name: "Alice", Online: true, VoiceSupported: true,
		Address: "192.168.1.20:7331", LastSeen: time.Now(),
	}}
	state.Conversations = []domain.Conversation{{
		ID: "conv-alice", Kind: "direct", PeerID: "peer-a", Title: "Alice", Opened: true,
		UpdatedAt: time.Now(),
	}}
	h := newShellTestHarness(t, state)
	if !h.hasHit(h.ui.ids.chatHeaderCall) {
		t.Fatal("direct chat header does not expose the call action")
	}
	h.clickID(h.ui.ids.chatHeaderCall)
	command := h.lastCommand()
	if command.Kind != domain.StartVoiceCall || command.PeerID != "peer-a" {
		t.Fatalf("start call command = %+v", command)
	}
}

func TestVoiceCallScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	calls := []struct {
		name string
		call domain.VoiceCall
	}{
		{
			name: "outgoing",
			call: domain.VoiceCall{
				ID: "qa-outgoing", PeerID: "peer-a", PeerName: "Alice",
				Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging,
				StartedAt: time.Now(),
			},
		},
		{
			name: "incoming",
			call: domain.VoiceCall{
				ID: "qa-incoming", PeerID: "peer-a", PeerName: "Alice",
				Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging,
				StartedAt: time.Now(),
			},
		},
		{
			name: "active",
			call: domain.VoiceCall{
				ID: "qa-active", PeerID: "peer-a", PeerName: "Alice",
				Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallActive,
				StartedAt: time.Now().Add(-95 * time.Second), ConnectedAt: time.Now().Add(-95 * time.Second),
			},
		},
	}
	for _, theme := range []string{"light", "dark"} {
		for _, test := range calls {
			t.Run(test.name+"-"+theme, func(t *testing.T) {
				state := voiceUIState(t, test.call)
				state.Settings.Theme = theme
				_, session := openVoiceQA(t, state, 1000, 700)
				shellQAShot(t, session, directory, filepath.Join("voice-"+test.name+"-"+theme+".png"))
			})
		}
	}

	for _, test := range []struct {
		theme, name   string
		width, height int
	}{
		{"light", "settings-voice-light.png", 1200, 900},
		{"dark", "settings-voice-dark.png", 1200, 900},
		{"light", "settings-voice-minimum-light.png", 1000, 700},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := shellTestState(t)
			state.Settings.Theme = test.theme
			shell, session := openShellQA(t, state, test.width, test.height)
			shell.panels.settingsExtra.voiceDevices = panelVoiceDeviceState{
				known: true,
				devices: voiceaudio.Devices{
					Microphones: []voiceaudio.Device{
						{ID: "mic:builtin", Name: "Built-in Microphone", Default: true},
						{ID: "mic:studio", Name: "Studio Microphone"},
					},
					Speakers: []voiceaudio.Device{
						{ID: "speaker:display", Name: "Display Speakers", Default: true},
						{ID: "speaker:studio", Name: "Studio Monitor Speakers"},
					},
				},
			}
			shellQAClick(t, session, shell.ids.navSettings)
			shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsVoice)))
			shellQAShot(t, session, directory, test.name)
		})
	}
}

package desktop

import (
	"github.com/vivaui/viva/app"
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
	shell := newUIShell(nil, false)
	shell.backend = newShellTestBackend(state)
	shell.voiceBoundID = state.VoiceCall.ID
	shell.voiceInvitation = incomingVoiceInvitation(state.VoiceCall)
	shell.refreshVoice()
	config := shell.voiceConfig()
	if incomingVoiceInvitation(state.VoiceCall) {
		config = shell.invitationConfig()
	} else {
		config.Width, config.Height = width, height
	}
	session, err := vivaheadless.Open(config, nil, vivaheadless.Options{Scale: 1})
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	t.Cleanup(shell.Close)
	shell.invalidator.Invalidate()
	return shell, session
}

func TestVoiceCallOutgoingOverlayAndControls(t *testing.T) {
	call := domain.VoiceCall{
		ID: "call-outgoing", PeerID: "peer-a", PeerName: "Alice",
		Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging,
		StartedAt: time.Now(),
	}
	h := newVoiceWindowHarness(t, voiceUIState(t, call))
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

func TestVoiceCallIncomingNotificationAcceptsAndDeclines(t *testing.T) {
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
			h := newVoiceWindowHarness(t, voiceUIState(t, base))
			id := h.ui.ids.voiceAccept
			label := "接听"
			if action.name == "decline" {
				id = h.ui.ids.voiceReject
				label = "拒绝"
			}
			if !h.hasText("邀请你进行语音通话") {
				t.Fatal("incoming call does not show the invitation")
			}
			hit, ok := h.findHit(id)
			if !ok || hit.Role != gfx.RoleButton || hit.Label != label || h.hasText(label) ||
				hit.Bounds.Width < 44 || hit.Bounds.Width != hit.Bounds.Height {
				t.Fatalf("expected icon-only %s button with a usable circular target: %+v", action.name, hit)
			}
			if kind := h.driver.PressAt(12, 12); kind != app.PressMove {
				t.Fatalf("invitation background is not draggable: %v", kind)
			}
			if kind := h.driver.PressAt(hit.Bounds.X+22, hit.Bounds.Y+22); kind != app.PressControl {
				t.Fatalf("invitation action is not a click target: %v", kind)
			}
			h.clickID(id)
			command := h.lastCommand()
			if command.Kind != action.kind || command.CallID != base.ID {
				t.Fatalf("%s command = %+v", action.name, command)
			}
		})
	}
}

func TestVoiceInvitationDoesNotAnswerStaleOrLockedCalls(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{ID: "one", PeerID: "missing", PeerName: "Private caller", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging})
	state.Settings.HideNotificationText = true
	h := newVoiceWindowHarness(t, state)
	if !h.hasText("Private caller") {
		t.Fatal("incoming call did not fall back to the caller's name")
	}
	unlock := 0
	h.ui.unlockForVoice = func() { unlock++ }
	state.Locked = true
	h.backend.setState(state)
	h.clickID(h.ui.ids.voiceAccept)
	if unlock != 1 || hasCommandKind(h.backend.commandLog(), domain.AcceptVoiceCall) {
		t.Fatal("locked invitation opened the microphone")
	}
	h.wake()
	if h.hasText("Private caller") || !h.hasText("收到一个语音通话邀请") {
		t.Fatal("locked invitation exposed the caller")
	}
	state.Locked = false
	state.VoiceCall.ID = "two"
	h.backend.setState(state)
	h.clickID(h.ui.ids.voiceReject)
	if hasCommandKind(h.backend.commandLog(), domain.RejectVoiceCall) {
		t.Fatal("stale invitation rejected a later call")
	}
}

func TestVoiceInvitationFallbackAndTrayActions(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{ID: "call", PeerID: "missing", PeerName: "Guest", Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging})
	state.Settings.DesktopNotifications = false
	h := newShellTestHarness(t, state)
	r := &desktopRuntime{ui: h.ui, backend: h.backend}
	h.ui.inlineVoiceID = state.VoiceCall.ID
	h.wake()
	if !h.hasHit(h.ui.ids.voiceAccept) || !h.hasHit(h.ui.ids.voiceReject) {
		t.Fatal("disabled notifications removed call actions")
	}
	h.clickID(h.ui.ids.voiceAccept)
	if c := h.lastCommand(); c.Kind != domain.AcceptVoiceCall || c.CallID != "call" {
		t.Fatalf("inline answer: %+v", c)
	}
	actions := r.voiceTrayItems()
	if len(actions) < 2 || actions[1].OnSelect == nil {
		t.Fatal("tray did not expose the incoming call")
	}
	actions[1].OnSelect()
	if c := h.lastCommand(); c.Kind != domain.RejectVoiceCall || c.CallID != "call" {
		t.Fatalf("tray decline: %+v", c)
	}
	state.VoiceCall.Status = domain.VoiceCallEnded
	h.backend.setState(state)
	h.backend.clearCommands()
	actions[0].OnSelect()
	if len(h.backend.commandLog()) != 0 || len(r.voiceTrayItems()) != 0 {
		t.Fatal("expired call retained actionable tray items")
	}
}

func TestVoiceWindowLoadsLatestContactAvatar(t *testing.T) {
	for _, direction := range []domain.VoiceCallDirection{domain.VoiceCallIncoming, domain.VoiceCallOutgoing} {
		t.Run(string(direction), func(t *testing.T) {
			state := voiceUIState(t, domain.VoiceCall{ID: "call", PeerID: "peer", PeerName: "Old", Direction: direction, Status: domain.VoiceCallRinging})
			path := writeExtraTestPNG(t, "peer.png")
			state.Peers = []domain.Peer{{ID: "peer", Name: "Alice", Remark: "Real contact", Avatar: path}}
			state.Settings.HideNotificationText = true
			h := newVoiceWindowHarness(t, state)
			h.waitFor(func() bool { return bitmapReady(h.ui.avatarBitmap(path)) })
			if !h.hasText("Real contact") {
				t.Fatal("call did not use current peer presentation")
			}
		})
	}
}

func TestVoiceWindowRightClickAdjustsOnlyThisCall(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{ID: "call", PeerID: "peer", Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallActive, ConnectedAt: time.Now()})
	for _, tc := range []struct {
		kind           string
		device, volume domain.CommandKind
	}{
		{"麦克风", domain.SetCallMicrophone, domain.SetCallMicrophoneVolume}, {"扬声器", domain.SetCallSpeaker, domain.SetCallSpeakerVolume},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			h := newVoiceWindowHarness(t, state)
			h.ui.voicePicker.enumerate = func() voiceaudio.Devices {
				devices := []voiceaudio.Device{{ID: "test", Name: "Test Device"}}
				return voiceaudio.Devices{Microphones: devices, Speakers: devices}
			}
			id := h.ui.ids.voiceMute
			if tc.kind == "扬声器" {
				id = h.ui.ids.voiceSpeaker
			}
			h.rightClickID(id)
			h.waitFor(func() bool { return h.ui.voicePicker.loading == nil })
			if !h.hasText("仅用于本次通话") {
				t.Fatal("secondary click did not open call controls")
			}
			selectHit, ok := h.findHitLabel(tc.kind)
			if !ok {
				t.Fatal("missing device select")
			}
			h.clickHit(selectHit)
			option, ok := h.findHitLabel("Test Device")
			if !ok {
				t.Fatal("missing enumerated device")
			}
			h.clickHit(option)
			if c := h.lastCommand(); c.Kind != tc.device || c.CallID != "call" || c.Text != "test" {
				t.Fatalf("device command = %+v", c)
			}
			level, ok := h.findHitLabel(tc.kind + "音量")
			if !ok {
				t.Fatal("missing call volume slider")
			}
			h.clickHit(level)
			if c := h.lastCommand(); c.Kind != tc.volume || c.CallID != "call" {
				t.Fatalf("volume command = %+v", c)
			}
			if hasCommandKind(h.backend.commandLog(), domain.SetSetting) {
				t.Fatal("call controls wrote preferences")
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
	h := newVoiceWindowHarness(t, state)
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

func newVoiceWindowHarness(t *testing.T, state domain.State) *shellTestHarness {
	t.Helper()
	backend := newShellTestBackend(state)
	shell := newUIShell(nil, false)
	shell.backend = backend
	shell.voiceBoundID = state.VoiceCall.ID
	shell.voiceInvitation = incomingVoiceInvitation(state.VoiceCall)
	shell.refreshVoice()
	config := shell.voiceConfig()
	if incomingVoiceInvitation(state.VoiceCall) {
		config = shell.invitationConfig()
		if config.WindowMode != app.WindowNotification {
			t.Fatal("incoming call did not use a notification window")
		}
	}
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		shell.Close()
		t.Fatal(err)
	}
	shell.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	harness := &shellTestHarness{t: t, ui: shell, backend: backend, driver: driver}
	t.Cleanup(driver.Close)
	t.Cleanup(shell.Close)
	return harness
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
				if incomingVoiceInvitation(test.call) {
					state.Peers[0].Remark = "林晓 · 设计部"
					state.Peers[0].Avatar = filepath.Join(t.TempDir(), "avatar.png")
					writePromoAvatar(t, state.Peers[0].Avatar, 1)
					state.Settings.HideNotificationText = true
				}
				shell, session := openVoiceQA(t, state, 430, 540)
				if incomingVoiceInvitation(test.call) {
					if err := session.WaitFor(func(_ app.Snapshot) bool {
						return bitmapReady(shell.avatarBitmap(state.Peers[0].Avatar))
					}, time.Second); err != nil {
						t.Fatal(err)
					}
					if err := session.Expect("layout", "clean"); err != nil {
						t.Fatal(err)
					}
				}
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

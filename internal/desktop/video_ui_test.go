package desktop

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestVideoCallButtonRightOfVoiceStartsVideoCall(t *testing.T) {
	state := voiceUIState(t, domain.VoiceCall{})
	state.Peers[0].VideoSupported = true
	state.Conversations[1].Opened = true
	h := newShellTestHarness(t, state)
	h.ui.selected = "conv-alice"
	h.ui.invalidator.Invalidate()
	if err := h.driver.Pump(); err != nil {
		t.Fatal(err)
	}
	voice, ok := h.findHit(h.ui.ids.chatHeaderCall)
	if !ok {
		t.Fatal("voice call button is missing")
	}
	videoBtn, ok := h.findHit(h.ui.ids.chatHeaderVideoCall)
	if !ok {
		t.Fatal("video call button is missing")
	}
	if videoBtn.Bounds.X <= voice.Bounds.Right() || videoBtn.Bounds.Y != voice.Bounds.Y {
		t.Fatalf("video button is not placed to the right of voice button: voice=%+v video=%+v", voice.Bounds, videoBtn.Bounds)
	}
	h.clickHit(videoBtn)
	if cmd := h.lastCommand(); cmd.Kind != domain.StartVideoCall || cmd.PeerID != state.Peers[0].ID {
		t.Fatalf("clicking video button submitted %+v, want StartVideoCall for %s", cmd, state.Peers[0].ID)
	}
}

func TestVideoCallIncomingNotificationMatchesVoiceStyle(t *testing.T) {
	voiceState := voiceUIState(t, domain.VoiceCall{
		ID: "call-voice", PeerID: "peer-a", PeerName: "Alice",
		Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging,
		StartedAt: time.Now(),
	})
	videoState := voiceUIState(t, domain.VoiceCall{
		ID: "call-video", PeerID: "peer-a", PeerName: "Alice", Video: true,
		Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging,
		StartedAt: time.Now(),
	})

	voiceShell := newUIShell(nil, false)
	voiceShell.backend = newShellTestBackend(voiceState)
	voiceShell.voiceBoundID = "call-voice"
	voiceShell.refreshVoice()
	t.Cleanup(voiceShell.Close)
	voiceCfg := voiceShell.invitationConfig()

	videoShell := newUIShell(nil, false)
	videoBackend := newShellTestBackend(videoState)
	videoShell.backend = videoBackend
	videoShell.voiceBoundID = "call-video"
	videoShell.refreshVoice()
	t.Cleanup(videoShell.Close)
	videoCfg := videoShell.invitationConfig()

	if videoCfg.WindowMode != app.WindowNotification || videoCfg.Width != voiceCfg.Width || videoCfg.Height != voiceCfg.Height {
		t.Fatalf("video invitation popup geometry (%dx%d mode=%v) does not match voice invitation (%dx%d mode=%v)",
			videoCfg.Width, videoCfg.Height, videoCfg.WindowMode, voiceCfg.Width, voiceCfg.Height, voiceCfg.WindowMode)
	}

	driver, err := app.NewDriver(videoCfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	snap := driver.Snapshot()
	requireShellLayout(t, snap)
	text := strings.Join(printTexts(snap), " ")
	if !strings.Contains(text, i18n.T("邀请你进行视频通话")) {
		t.Fatalf("video invitation text missing: %q", text)
	}
	for _, hit := range snap.Hits {
		if hit.ID == videoShell.ids.voiceAccept {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	cmds := videoBackend.commandLog()
	if len(cmds) != 1 || cmds[0].Kind != domain.AcceptVoiceCall || cmds[0].CallID != "call-video" {
		t.Fatalf("accepting video invitation submitted %+v", cmds)
	}
}

func TestVideoCallWindowWaitingAndActiveControls(t *testing.T) {
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, language := range []string{i18n.Chinese, i18n.English} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLanguage(language)
			call := domain.VoiceCall{
				ID: "video-1", PeerID: "peer-a", PeerName: "Alice", Video: true,
				Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging,
				StartedAt: time.Now(),
			}
			state := voiceUIState(t, call)
			backend := newShellTestBackend(state)
			view := newUIShell(nil, false)
			view.backend = backend
			view.voiceBoundID, view.voiceBoundVideo = call.ID, true
			view.refreshVoice()
			t.Cleanup(view.Close)

			driver, err := app.NewDriver(view.videoConfig(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(driver.Close)
			requireShellLayout(t, driver.Snapshot())

			// Transition to active video call
			state.VoiceCall.Status = domain.VoiceCallActive
			state.VoiceCall.ConnectedAt = time.Now().Add(-5 * time.Second)
			state.Revision++
			backend.setState(state)
			view.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
			requireShellLayout(t, driver.Snapshot())

			// Toggle camera off
			var camHit, hangupHit bool
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == view.ids.videoCamera {
					camHit = true
					if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if !camHit {
				t.Fatal("video camera toggle button missing")
			}
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == view.ids.voiceHangup {
					hangupHit = true
					if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if !hangupHit {
				t.Fatal("video hangup button missing")
			}
			cmds := backend.commandLog()
			if len(cmds) < 2 || cmds[0].Kind != domain.SetVideoCameraOff || !cmds[0].Bool || cmds[1].Kind != domain.EndVoiceCall {
				t.Fatalf("unexpected video control commands: %+v", cmds)
			}
		})
	}
}

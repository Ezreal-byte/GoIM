package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	vivaheadless "github.com/vivaui/viva/headless"

	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/video"
)

func syntheticVideoFrame(w, h int, rBase, gBase, bBase uint8) video.Frame {
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			fx := float32(x) / float32(max(w-1, 1))
			fy := float32(y) / float32(max(h-1, 1))
			// Soft studio-like vignette gradient with subtle geometric accent
			cx, cy := fx-0.5, fy-0.45
			dist := cx*cx + cy*cy
			shade := max(float32(0.45), 1.0-dist*0.9)
			b := uint8(min(255, int(float32(bBase)*shade+fx*36)))
			g := uint8(min(255, int(float32(gBase)*shade+fy*28)))
			r := uint8(min(255, int(float32(rBase)*shade+(1-fx)*24)))
			pix[off], pix[off+1], pix[off+2], pix[off+3] = b, g, r, 255
		}
	}
	return video.Frame{Width: w, Height: h, BGRA: pix}
}

func TestVideoAndRemoteAssistScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	i18n.SetLanguage(i18n.Chinese)

	// 1 & 2: Chat toolbar (voice + video + remote assist split button + expanded popover) in light & dark themes.
	for _, tc := range []struct {
		theme, file string
	}{
		{"light", "01-chat-toolbar-popover-light.png"},
		{"dark", "02-chat-toolbar-popover-dark.png"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			state := remoteDesktopTestState(t)
			state.Settings.Language = i18n.Chinese
			state.Settings.Theme = tc.theme
			state.Settings.LastSection = "messages"
			state.Peers[0].VoiceSupported = true
			state.Peers[0].VideoSupported = true
			state.Peers[0].Avatar = filepath.Join(t.TempDir(), "alice.png")
			writePromoAvatar(t, state.Peers[0].Avatar, 1)

			shell, session := openShellQA(t, state, 1180, 760)
			shell.selected = "conv-alice"
			shell.invalidator.Invalidate()
			_ = session.Wait(200 * time.Millisecond)
			shellQAClick(t, session, shell.ids.chatRemoteDesktopMore)
			requireShellLayout(t, session.Snapshot())
			shellQAShot(t, session, directory, tc.file)
		})
	}

	// 3: Video call waiting window (outgoing ringing).
	t.Run("video-waiting", func(t *testing.T) {
		state := voiceUIState(t, domain.VoiceCall{
			ID: "vid-wait", PeerID: "peer-a", PeerName: "Alice", Video: true,
			Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallRinging,
			StartedAt: time.Now(),
		})
		state.Peers[0].VideoSupported = true
		state.Peers[0].Avatar = filepath.Join(t.TempDir(), "alice.png")
		writePromoAvatar(t, state.Peers[0].Avatar, 1)

		view := newUIShell(nil, false)
		view.backend = newShellTestBackend(state)
		view.voiceBoundID, view.voiceBoundVideo = "vid-wait", true
		view.refreshVoice()
		t.Cleanup(view.Close)

		session, err := vivaheadless.Open(view.videoConfig(), nil, vivaheadless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		_ = session.WaitFor(func(_ app.Snapshot) bool {
			view.applyMediaResults()
			return bitmapReady(view.avatarBitmap(state.Peers[0].Avatar))
		}, time.Second)
		view.invalidator.Invalidate()
		requireShellLayout(t, session.Snapshot())
		shellQAShot(t, session, directory, "03-video-call-waiting.png")
	})

	// 4: Video call incoming floating notification popup.
	t.Run("video-incoming", func(t *testing.T) {
		state := voiceUIState(t, domain.VoiceCall{
			ID: "vid-in", PeerID: "peer-a", PeerName: "Alice", Video: true,
			Direction: domain.VoiceCallIncoming, Status: domain.VoiceCallRinging,
			StartedAt: time.Now(),
		})
		state.Peers[0].Avatar = filepath.Join(t.TempDir(), "alice.png")
		writePromoAvatar(t, state.Peers[0].Avatar, 1)

		view := newUIShell(nil, false)
		view.backend = newShellTestBackend(state)
		view.voiceBoundID, view.voiceBoundVideo, view.voiceInvitation = "vid-in", true, true
		view.refreshVoice()
		t.Cleanup(view.Close)

		session, err := vivaheadless.Open(view.invitationConfig(), nil, vivaheadless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		_ = session.WaitFor(func(_ app.Snapshot) bool {
			view.applyMediaResults()
			return bitmapReady(view.avatarBitmap(state.Peers[0].Avatar))
		}, time.Second)
		view.invalidator.Invalidate()
		requireShellLayout(t, session.Snapshot())
		shellQAShot(t, session, directory, "04-video-call-incoming-popup.png")
	})

	// 5: Video call active window with remote video + local PiP preview.
	t.Run("video-active", func(t *testing.T) {
		state := voiceUIState(t, domain.VoiceCall{
			ID: "vid-active", PeerID: "peer-a", PeerName: "Alice", Video: true,
			Direction: domain.VoiceCallOutgoing, Status: domain.VoiceCallActive,
			StartedAt: time.Now().Add(-95 * time.Second), ConnectedAt: time.Now().Add(-87 * time.Second),
		})
		state.Peers[0].VideoSupported = true
		state.Peers[0].Avatar = filepath.Join(t.TempDir(), "alice.png")
		writePromoAvatar(t, state.Peers[0].Avatar, 1)
		view := newUIShell(nil, false)
		view.backend = newShellTestBackend(state)
		view.voiceBoundID, view.voiceBoundVideo = "vid-active", true
		view.videoMedia = newVideoCallMedia(nil, view.invalidator.Invalidate)
		view.videoMedia.openCamera = func(string) (video.Capture, error) { return nil, video.ErrUnsupported }
		view.refreshVoice()
		remoteFrame := syntheticVideoFrame(640, 360, 56, 98, 152)
		localFrame := syntheticVideoFrame(320, 180, 96, 138, 118)
		view.videoMedia.remotePend.Store(&remoteFrame)
		view.videoMedia.localPending.Store(&localFrame)
		view.videoMedia.mu.Lock()
		view.videoMedia.cameraError = ""
		view.videoMedia.mu.Unlock()
		t.Cleanup(view.Close)

		session, err := vivaheadless.Open(view.videoConfig(), nil, vivaheadless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		view.invalidator.Invalidate()
		requireShellLayout(t, session.Snapshot())
		shellQAShot(t, session, directory, "05-video-call-active.png")
	})

	// 6: Incoming reverse-assistance floating popup.
	t.Run("remote-assist-incoming", func(t *testing.T) {
		state := remoteDesktopTestState(t)
		state.Settings.Language, state.Settings.Theme = i18n.Chinese, "light"
		state.RemoteDesktop = domain.RemoteDesktopSession{
			ID: "assist-in", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending,
			Incoming: true, Host: false,
		}
		captureRemoteDesktopNotification(t, state, directory, "06-remote-assist-incoming-popup.png")
	})

	// 7 & 8: Controlled-side bottom-right floating status overlay (expanded & collapsed).
	t.Run("remote-controlled-expanded-and-collapsed", func(t *testing.T) {
		state := remoteDesktopTestState(t)
		state.Settings.Language, state.Settings.Theme = i18n.Chinese, "light"
		state.Peers[0].Avatar = filepath.Join(t.TempDir(), "alice.png")
		writePromoAvatar(t, state.Peers[0].Avatar, 1)
		state.RemoteDesktop = domain.RemoteDesktopSession{
			ID: "controlled", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopActive,
			Incoming: false, Host: true, Control: true,
		}
		shell := newUIShell(newShellTestBackend(state), false)
		t.Cleanup(shell.Close)
		view := newRemoteDesktopView(state)
		view.avatarShell = shell
		view.stop = func() {}
		t.Cleanup(view.dispose)

		session, err := vivaheadless.Open(view.config(), nil, vivaheadless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		_ = session.WaitFor(func(_ app.Snapshot) bool {
			shell.applyMediaResults()
			return bitmapReady(shell.avatarBitmap(view.peerAvatar))
		}, time.Second)
		view.wake.Invalidate()
		requireShellLayout(t, session.Snapshot())
		shellQAShot(t, session, directory, "07-remote-controlled-expanded.png")

		// Collapse overlay and capture the compact collapsed pill
		collapsedW, collapsedH := sharingCollapsedSize(view.theme)
		view.collapsed = true
		if err := session.Resize(collapsedW, collapsedH); err != nil {
			t.Fatal(err)
		}
		view.wake.Invalidate()
		requireShellLayout(t, session.Snapshot())
		shellQAShot(t, session, directory, "08-remote-controlled-collapsed.png")
	})
}

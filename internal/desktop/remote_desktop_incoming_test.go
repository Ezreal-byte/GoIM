package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	vivaheadless "github.com/vivaui/viva/headless"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func TestRemoteDesktopConsentOpensInDesktopNotification(t *testing.T) {
	state := remoteDesktopTestState(t)
	state.RemoteDesktop = domain.RemoteDesktopSession{
		ID: "incoming", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending, Incoming: true,
	}
	view := newRemoteDesktopView(state)
	t.Cleanup(view.dispose)
	rejected := false
	view.stop = func() { rejected = true }
	view.accept = func() { t.Fatal("rejecting the invitation granted consent") }
	config := view.config()
	if config.WindowMode != app.WindowNotification || config.Origin != nil {
		t.Fatalf("expected desktop notification placement, got mode=%v origin=%v", config.WindowMode, config.Origin)
	}

	driver := newRemoteDesktopTestDriver(t, config)
	clickRemoteDesktopControl(t, driver, "拒绝")
	if !rejected || view.decisionSent {
		t.Fatal("the notification did not reject the pending request")
	}
}

func TestRemoteDesktopConsentGrantsControlWithoutAudio(t *testing.T) {
	state := remoteDesktopTestState(t)
	state.RemoteDesktop = domain.RemoteDesktopSession{
		ID: "incoming", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending, Incoming: true,
	}
	runtime := &desktopRuntime{backend: newShellTestBackend(state), ui: &UI{state: state}}
	backend := runtime.backend.(*shellTestBackend)
	view := newRemoteDesktopView(state)
	t.Cleanup(view.dispose)
	view.accept = func() { runtime.acceptRemoteDesktopInvitation(view.session.ID) }
	driver := newRemoteDesktopTestDriver(t, view.config())
	buttons := 0
	for _, hit := range driver.Snapshot().Hits {
		switch hit.Role {
		case gfx.RoleButton:
			buttons++
		case gfx.RoleCheckbox:
			t.Fatal("system audio choice remained in the compact invitation")
		}
	}
	if buttons != 2 || len(backend.commandLog()) != 0 {
		t.Fatalf("expected two choices and no automatic consent, got %d buttons and %+v", buttons, backend.commandLog())
	}

	runtime.acceptRemoteDesktopInvitation("obsolete")
	locked := state
	locked.Locked = true
	backend.setState(locked)
	runtime.acceptRemoteDesktopInvitation(view.session.ID)
	if len(backend.commandLog()) != 0 {
		t.Fatal("obsolete or locked invitation granted access")
	}
	backend.setState(state)

	clickRemoteDesktopControl(t, driver, "允许控制")
	clickRemoteDesktopControl(t, driver, "允许控制")
	commands := backend.commandLog()
	if len(commands) != 1 || commands[0].Kind != domain.AcceptRemoteDesktop ||
		commands[0].RemoteDesktopID != view.session.ID || !commands[0].AllowRemoteControl || commands[0].ShareRemoteAudio {
		t.Fatalf("unexpected permission granted by the green action: %+v", commands)
	}
}

func TestRemoteDesktopConsentScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(previous) })
	for _, tc := range []struct {
		name, lang, theme string
		font              int
	}{
		{"light-zh", i18n.Chinese, "light", 15},
		{"dark-zh", i18n.Chinese, "dark", 15},
		{"large-en", i18n.English, "light", 28},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := remoteDesktopTestState(t)
			state.Settings.Language, state.Settings.Theme, state.Settings.InterfaceFontSize = tc.lang, tc.theme, tc.font
			state.Peers[0].Avatar = filepath.Join(t.TempDir(), "avatar.png")
			writePromoAvatar(t, state.Peers[0].Avatar, 1)
			state.RemoteDesktop = domain.RemoteDesktopSession{
				ID: "incoming", PeerID: state.Peers[0].ID, Status: domain.RemoteDesktopPending, Incoming: true,
			}
			shell := newUIShell(newShellTestBackend(state), false)
			t.Cleanup(shell.Close)
			view := newRemoteDesktopView(state)
			view.avatarShell = shell
			t.Cleanup(view.dispose)
			session, err := vivaheadless.Open(view.config(), nil, vivaheadless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(session.Close)
			if err := session.WaitFor(func(_ app.Snapshot) bool {
				shell.applyMediaResults()
				return bitmapReady(shell.avatarBitmap(view.peerAvatar))
			}, time.Second); err != nil {
				t.Fatal(err)
			}
			view.wake.Invalidate()
			requireShellLayout(t, session.Snapshot())
			shellQAShot(t, session, directory, "remote-incoming-"+tc.name+".png")
		})
	}
}

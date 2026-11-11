package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func openShellQA(t *testing.T, state domain.State, width, height int) (*UI, *vivaheadless.Session) {
	t.Helper()
	backend := newShellTestBackend(state)
	shell := New(backend)
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

func shellQAShot(t *testing.T, session *vivaheadless.Session, directory, name string) {
	t.Helper()
	if err := session.Wait(700 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(directory, name)); err != nil {
		t.Fatal(err)
	}
}

func shellQAHit(t *testing.T, session *vivaheadless.Session, id ui.ID) ui.Hit {
	t.Helper()
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("control %v is not on screen", id)
	return ui.Hit{}
}

func shellQAClick(t *testing.T, session *vivaheadless.Session, id ui.ID) {
	t.Helper()
	hit := shellQAHit(t, session, id)
	if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}

func shellQARightClick(t *testing.T, session *vivaheadless.Session, id ui.ID) {
	t.Helper()
	hit := shellQAHit(t, session, id)
	if err := session.RightClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}

func shellQAEscape(t *testing.T, session *vivaheadless.Session) {
	t.Helper()
	if err := session.Driver().Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
}

func TestShellContextScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_QA_SCREENSHOTS to write offscreen screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	background := writeShellTestPNG(t, "chat-background.png", 640, 400)
	state := shellTestState(t)
	state.Self.Avatar = writeShellTestPNG(t, "self-avatar.png", 300, 200)
	state.Settings.Avatar = state.Self.Avatar
	state.Settings.Background = background
	state.Settings.BackgroundShade = 35
	state.NeiwangtongStatus = domain.CompatibilityStatus{Unavailable: true, Problem: "内网通原生文字、文件和头像尚未完成真机验收"}
	shell, session := openShellQA(t, state, 1440, 960)

	shellQAClick(t, session, shell.ids.navContacts)
	shellQAClick(t, session, shell.peerRowIDs["peer-a"])
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "shell-contacts-light.png")
	shellQAClick(t, session, shell.convRowIDs["conv-group"])
	if err := session.Expect("text", "群聊资料"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "shell-group-profile-light.png")
	shellQARightClick(t, session, shell.peerRowIDs["peer-a"])
	shellQAShot(t, session, directory, "shell-contact-menu-light.png")
	shellQAClick(t, session, shell.ids.shellMenuOrganize)
	shellQAShot(t, session, directory, "shell-organizer-light.png")
	shellQAEscape(t, session)

	shellQAClick(t, session, shell.ids.navMessages)
	shellQAShot(t, session, directory, "shell-chat-background-light.png")
	shellQARightClick(t, session, shell.convRowIDs["conv-alice"])
	shellQAShot(t, session, directory, "shell-conversation-menu-light.png")
	shellQAClick(t, session, shell.ids.shellMenuDelete)
	shellQAShot(t, session, directory, "shell-delete-confirm-light.png")
	shellQAEscape(t, session)
	shellQAClick(t, session, shell.ids.navLock)
	shellQAShot(t, session, directory, "shell-lock-setup-light.png")
	shellQAEscape(t, session)

	darkState := shellTestState(t)
	darkState.Settings.Theme = "dark"
	darkState.Settings.Background = background
	darkState.Settings.BackgroundShade = 55
	darkShell, darkSession := openShellQA(t, darkState, 1440, 960)
	shellQAClick(t, darkSession, darkShell.ids.navContacts)
	shellQAClick(t, darkSession, darkShell.peerRowIDs["peer-a"])
	if err := darkSession.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, darkSession, directory, "shell-contacts-dark.png")
	shellQARightClick(t, darkSession, darkShell.peerRowIDs["peer-a"])
	shellQAShot(t, darkSession, directory, "shell-contact-menu-dark.png")
	shellQAClick(t, darkSession, darkShell.ids.shellMenuOrganize)
	shellQAShot(t, darkSession, directory, "shell-organizer-dark.png")
	shellQAEscape(t, darkSession)
	shellQAClick(t, darkSession, darkShell.ids.navMessages)
	shellQAShot(t, darkSession, directory, "shell-chat-background-dark.png")

	minimumState := shellTestState(t)
	minimumShell, minimumSession := openShellQA(t, minimumState, 1000, 700)
	shellQAClick(t, minimumSession, minimumShell.ids.navMessages)
	shellQARightClick(t, minimumSession, minimumShell.convRowIDs["conv-alice"])
	shellQAShot(t, minimumSession, directory, "shell-conversation-menu-minimum.png")
	shellQAEscape(t, minimumSession)
	shellQAClick(t, minimumSession, minimumShell.ids.navContacts)
	shellQARightClick(t, minimumSession, minimumShell.peerRowIDs["peer-a"])
	shellQAClick(t, minimumSession, minimumShell.ids.shellMenuOrganize)
	shellQAShot(t, minimumSession, directory, "shell-organizer-minimum.png")

	selfState := shellMinimalState(t)
	selfState.Settings.Nickname = "本机用户"
	selfShell, selfSession := openShellQA(t, selfState, 1000, 700)
	shellQAClick(t, selfSession, selfShell.ids.navContacts)
	shellQAClick(t, selfSession, selfShell.ids.selfRow)
	shellQAShot(t, selfSession, directory, "shell-self-only-minimum.png")
	shellQAClick(t, session, shell.ids.navSettings)
	shellQAClick(t, session, shell.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	if err := session.Expect("text", "内网通互通"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("text", "飞鸽／飞秋互通"); err != nil {
		t.Fatal(err)
	}
	if err := session.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	shellQAShot(t, session, directory, "shell-network-compat-light.png")
}

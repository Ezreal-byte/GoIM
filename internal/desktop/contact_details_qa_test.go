//go:build vivahead

package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"goim/internal/domain"
)

func TestContactProfileScreenshots(t *testing.T) {
	dir := os.Getenv("GOIM_CONTACT_QA")
	if dir == "" {
		t.Skip("set GOIM_CONTACT_QA to save the contact screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := shellTestState(t)
	state.Settings.Theme = "light"
	state.Peers[0] = domain.Peer{
		ID: state.Peers[0].ID, Name: "林悠", Online: true, Presence: domain.PresenceAway,
		ClientType: domain.ClientNeiwangtong, Transport: domain.TransportNeiwangtong, Version: "3.4.3055",
		Signature: "讨论设计稿请直接留言。", ContactGroup: "产品与设计", Address: "192.0.2.21:9012",
		Details: domain.ProfileDetails{
			Company: "北岭工作室", Department: "体验设计", Phone: "010-5550100", Email: "design@example.com",
		},
	}
	backend := newShellTestBackend(state)
	shell := New(backend)
	defer shell.Close()
	cfg := shell.Config()
	cfg.Width, cfg.Height = 1280, 800
	session, err := headless.Open(cfg, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := findSessionHit(session.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing contact control %v", id)
		}
		if err := session.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	shot := func(name, label string) {
		t.Helper()
		if err := session.Move(580, 42); err != nil {
			t.Fatal(err)
		}
		if err := session.WaitFor(func(snapshot app.Snapshot) bool {
			for _, text := range snapshot.Texts {
				if text.Value == label {
					return true
				}
			}
			return false
		}, 2*time.Second); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := session.Expect("layout", "clean"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := session.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}

	click(shell.ids.navContacts)
	shot("contacts-empty-light", "选择联系人或群聊")
	click(shell.peerRowIDs[state.Peers[0].ID])
	shot("contact-profile-light", "联系人资料")
	if err := session.Resize(1000, 620); err != nil {
		t.Fatal(err)
	}
	shot("contact-profile-minimum", "联系人资料")
	if err := session.Resize(1280, 800); err != nil {
		t.Fatal(err)
	}
	state.Settings.Theme = "dark"
	state.Revision++
	backend.setState(state)
	shell.invalidator.Invalidate()
	if err := session.WaitFor(func(_ app.Snapshot) bool { return shell.isDark() }, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	shot("contact-profile-dark", "联系人资料")
	shell.contactPeer = ""
	shell.invalidator.Invalidate()
	shot("contacts-empty-dark", "选择联系人或群聊")
}

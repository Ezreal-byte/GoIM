package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/domain"
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
	view.accept = func(bool, bool) { t.Fatal("rejecting the invitation granted consent") }
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

package core

import (
	"context"
	"net"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/remotedesktop"
)

func TestRemoteDesktopRequestAssistanceReversesRoles(t *testing.T) {
	inputCh := make(chan domain.RemoteDesktopInput, 1)
	engine := func(ctx context.Context, tunnel net.Conn, grant remotedesktop.Start, input *remotedesktop.InputQueue, events remotedesktop.Events) error {
		events.Ready()
		if grant.Role == "viewer" {
			events.Frame(domain.RemoteDesktopFrame{Width: 2, Height: 2, BGRA: make([]byte, 16)})
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-input.Ready():
						if in, ok := input.Pop(); ok {
							inputCh <- in
							return
						}
					}
				}
			}()
		}
		<-ctx.Done()
		return nil
	}

	requester := remoteTestNode(t, "requester", engine)
	helper := remoteTestNode(t, "helper", engine)
	connectNodes(t, requester, helper)
	requesterID, helperID := requester.Snapshot().Self.ID, helper.Snapshot().Self.ID
	if !requester.Snapshot().Peer(helperID).RemoteDesktopAssistSupported {
		t.Fatal("expected peer to advertise RemoteDesktopAssistSupported")
	}

	// Requester asks helper for assistance -> requester is Host, helper becomes Viewer.
	if err := requester.execute(domain.Command{Kind: domain.RequestRemoteAssistance, PeerID: helperID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pending reverse assist", func() bool {
		rs, hs := requester.Snapshot().RemoteDesktop, helper.Snapshot().RemoteDesktop
		return rs.Status == domain.RemoteDesktopPending && !rs.Incoming && rs.Host &&
			hs.Status == domain.RemoteDesktopPending && hs.Incoming && !hs.Host
	})

	sessionID := helper.Snapshot().RemoteDesktop.ID
	if err := helper.execute(domain.Command{Kind: domain.AcceptRemoteDesktop, RemoteDesktopID: sessionID, AllowRemoteControl: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "active reverse assist", func() bool {
		rs, hs := requester.Snapshot().RemoteDesktop, helper.Snapshot().RemoteDesktop
		return rs.Status == domain.RemoteDesktopActive && rs.Host && rs.Control &&
			hs.Status == domain.RemoteDesktopActive && !hs.Host && hs.Control
	})

	// Helper (viewer) receives frames and can send control input; requester (host) cannot.
	frames := helper.RemoteDesktopFrames(sessionID)
	if frames == nil {
		t.Fatal("expected helper viewer to receive frames")
	}
	if requester.RemoteDesktopFrames(sessionID) != nil {
		t.Fatal("host should not expose viewer frame channel")
	}
	frame := waitFrame(t, frames)
	if frame.Width != 2 || frame.Height != 2 {
		t.Fatalf("unexpected frame %+v", frame)
	}

	if err := helper.SendRemoteDesktopInput(sessionID, domain.RemoteDesktopInput{Action: "move", X: 100, Y: 200}); err != nil {
		t.Fatalf("helper SendRemoteDesktopInput: %v", err)
	}
	select {
	case got := <-inputCh:
		if got.Action != "move" || got.X != 100 || got.Y != 200 {
			t.Fatalf("unexpected input %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for helper input")
	}
	if err := requester.SendRemoteDesktopInput(sessionID, domain.RemoteDesktopInput{Action: "move", X: 1, Y: 1}); err == nil {
		t.Fatal("controlled host must not send viewer input")
	}

	// Requester (controlled host) terminates at any time.
	if err := requester.execute(domain.Command{Kind: domain.EndRemoteDesktop, RemoteDesktopID: sessionID}); err != nil {
		t.Fatal(err)
	}
	waitRemote(t, requester, domain.RemoteDesktopEnded)
	waitRemote(t, helper, domain.RemoteDesktopEnded)

	msgs := requester.Snapshot().ChatMessages(directID(requesterID, helperID))
	var found bool
	for _, m := range msgs {
		if m.RemoteDesktop != nil && m.RemoteDesktop.RequestedHelp && m.RemoteDesktop.Host && m.RemoteDesktop.Connected {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected requested-help history record in %+v", msgs)
	}
}

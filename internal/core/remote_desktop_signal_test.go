package core

import (
	"context"
	"net"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/remotedesktop"
)

func TestRemoteDesktopCanCancelWhileSignalingIsBlocked(t *testing.T) {
	engine := func(context.Context, net.Conn, remotedesktop.Start, *remotedesktop.InputQueue, remotedesktop.Events) error {
		t.Error("helper started after a cancellation before consent was sent")
		return nil
	}
	for _, action := range []domain.CommandKind{domain.StartRemoteDesktop, domain.AcceptRemoteDesktop} {
		t.Run(string(action), func(t *testing.T) {
			viewer, host := remoteTestNode(t, "viewer", engine), remoteTestNode(t, "host", engine)
			connectNodes(t, viewer, host)
			hostID := host.Snapshot().Self.ID
			sender := viewer
			command := domain.Command{Kind: action, PeerID: hostID}
			if action == domain.AcceptRemoteDesktop {
				if err := viewer.startRemoteDesktop(hostID); err != nil {
					t.Fatal(err)
				}
				command.RemoteDesktopID = waitRemote(t, host, domain.RemoteDesktopPending).ID
				command.PeerID, sender = viewer.Snapshot().Self.ID, host
			}

			sender.mu.Lock()
			peer := sender.connections[command.PeerID]
			sender.mu.Unlock()
			peer.write.Lock()
			defer peer.write.Unlock()

			submit := func(command domain.Command) {
				t.Helper()
				done := make(chan error, 1)
				command.OnResult = func(err error) { done <- err }
				sender.Submit(command)
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("%s waited for the peer's socket writer", command.Kind)
				}
			}
			submit(command)
			id := sender.Snapshot().RemoteDesktop.ID
			if id == "" {
				t.Fatal("missing pending session")
			}
			submit(domain.Command{Kind: domain.EndRemoteDesktop, RemoteDesktopID: id})
			if session := sender.Snapshot().RemoteDesktop; session.Status != domain.RemoteDesktopEnded || session.Reason != "cancelled" {
				t.Fatalf("cancel left a live session: %+v", session)
			}
		})
	}
}

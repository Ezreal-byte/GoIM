package core

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"goim/internal/domain"
	"goim/internal/remotedesktop"
)

func waitRemote(t *testing.T, n *Node, status domain.RemoteDesktopStatus) domain.RemoteDesktopSession {
	t.Helper()
	var session domain.RemoteDesktopSession
	eventually(t, string(status), func() bool {
		session = n.Snapshot().RemoteDesktop
		return session.Status == status
	})
	return session
}

func remoteTestNode(t *testing.T, name string, engine remotedesktop.Engine) *Node {
	t.Helper()
	n, err := newNode(Options{
		DataDir: t.TempDir(), Listen: "127.0.0.1:0", Name: name,
		DisableDiscovery: true, DisableCompatibility: true, Version: "test",
	}, engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func waitFrame(t *testing.T, frames <-chan domain.RemoteDesktopFrame) domain.RemoteDesktopFrame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("frame stream ended early")
		}
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("no frame")
		return domain.RemoteDesktopFrame{}
	}
}

func TestRemoteDesktopRequiresConsentAndUsesDirectTLS(t *testing.T) {
	var starts atomic.Int32
	stopped := make(chan struct{}, 2)
	viewer := remoteTestNode(t, "viewer", func(ctx context.Context, conn net.Conn, start remotedesktop.Start, input <-chan domain.RemoteDesktopInput, events remotedesktop.Events) error {
		starts.Add(1)
		defer func() { stopped <- struct{}{} }()
		if start.Role != "viewer" || start.Control || !start.Audio {
			t.Error("viewer did not receive the exact local grant", start)
		}
		var pixels [4]byte
		if _, err := io.ReadFull(conn, pixels[:]); err != nil {
			return err
		}
		events.Ready()
		events.Frame(domain.RemoteDesktopFrame{Width: 1, Height: 1, BGRA: pixels[:]})
		select {
		case <-ctx.Done():
		case command := <-input:
			if command.Action != "audio" || command.Volume != 63 {
				t.Error("input permission leaked or volume corrupted", command)
			}
			<-ctx.Done()
		}
		return nil
	})
	host := remoteTestNode(t, "host", func(ctx context.Context, conn net.Conn, start remotedesktop.Start, _ <-chan domain.RemoteDesktopInput, events remotedesktop.Events) error {
		starts.Add(1)
		defer func() { stopped <- struct{}{} }()
		if start.Role != "host" || start.Control || !start.Audio {
			t.Error("host did not receive the exact local grant", start)
		}
		events.Ready()
		if _, err := conn.Write([]byte{5, 7, 9, 255}); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	})
	connectNodes(t, viewer, host)
	hostID := host.Snapshot().Self.ID
	if !host.Snapshot().RemoteDesktopAvailable || !viewer.Snapshot().Peer(hostID).RemoteDesktopSupported {
		t.Fatal("verified direct peers did not negotiate remote desktop")
	}
	if err := viewer.execute(domain.Command{Kind: domain.StartRemoteDesktop, PeerID: hostID}); err != nil {
		t.Fatal(err)
	}
	pending := waitRemote(t, host, domain.RemoteDesktopPending)
	if !pending.Incoming || pending.PeerName != "viewer" || starts.Load() != 0 {
		t.Fatalf("engine started or requested incorrect consent: %+v", pending)
	}
	assertRemoteStreamRejected(t, viewer, host, pending.ID, newID())
	if err := host.execute(domain.Command{Kind: domain.RejectRemoteDesktop, RemoteDesktopID: pending.ID}); err != nil {
		t.Fatal(err)
	}
	if ended := waitRemote(t, viewer, domain.RemoteDesktopEnded); ended.Reason != "declined" || starts.Load() != 0 {
		t.Fatalf("rejected invitation started an engine: %+v", ended)
	}

	if err := viewer.startRemoteDesktop(hostID); err != nil {
		t.Fatal(err)
	}
	pending = waitRemote(t, host, domain.RemoteDesktopPending)
	if err := host.execute(domain.Command{Kind: domain.AcceptRemoteDesktop, RemoteDesktopID: pending.ID, ShareRemoteAudio: true}); err != nil {
		t.Fatal(err)
	}
	active := waitRemote(t, viewer, domain.RemoteDesktopActive)
	waitRemote(t, host, domain.RemoteDesktopActive)
	frames := viewer.RemoteDesktopFrames(active.ID)
	if got := waitFrame(t, frames); got.Width != 1 || got.BGRA[2] != 9 {
		t.Fatal("dedicated TLS connection did not carry the helper's data", got)
	}
	if err := viewer.SendRemoteDesktopInput(active.ID, domain.RemoteDesktopInput{Action: "key", Key: "a"}); err == nil {
		t.Fatal("view-only permission allowed remote input")
	}
	if err := viewer.SendRemoteDesktopInput(active.ID, domain.RemoteDesktopInput{Action: "audio", Volume: 63}); err != nil {
		t.Fatal(err)
	}
	assertRemoteStreamRejected(t, viewer, host, pending.ID, newID())
	if err := host.execute(domain.Command{Kind: domain.SetLockPassword, Text: "test-lock-password", Bool: true}); err != nil {
		t.Fatal(err)
	}
	if ended := waitRemote(t, host, domain.RemoteDesktopEnded); ended.Reason != "locked" {
		t.Fatal("locking did not end remote desktop", ended)
	}
	waitRemote(t, viewer, domain.RemoteDesktopEnded)
	for range 2 {
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("engine kept running after the session ended")
		}
	}
	if _, ok := <-frames; ok || starts.Load() != 2 {
		t.Fatal("ended stream retained a frame or extra connection started a helper")
	}
}

func assertRemoteStreamRejected(t *testing.T, viewer, host *Node, id, grant string) {
	t.Helper()
	conn := openRemoteTestStream(t, viewer, host, id, grant)
	defer conn.Close()
	if _, err := readFrame(conn); err == nil {
		t.Fatal("a tunnel was admitted without a valid, unused grant")
	} else if deadline, ok := err.(net.Error); ok && deadline.Timeout() {
		t.Fatal("unauthorized connection was not closed promptly", err)
	}
}

func openRemoteTestStream(t *testing.T, viewer, host *Node, id, grant string) net.Conn {
	t.Helper()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", host.Address(), viewer.tls.Clone())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := readFrame(conn); err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(conn, envelope{
		Type: "remote_desktop_stream", Peer: domain.Peer{ID: viewer.Snapshot().Self.ID},
		RemoteDesktop: &remoteDesktopSignal{ID: id, Grant: grant},
	}); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestRemoteDesktopGrantIsPeerBoundAndSingleUse(t *testing.T) {
	stopped := make(chan struct{}, 2)
	host := remoteTestNode(t, "host", func(ctx context.Context, _ net.Conn, _ remotedesktop.Start, _ <-chan domain.RemoteDesktopInput, events remotedesktop.Events) error {
		events.Ready()
		<-ctx.Done()
		stopped <- struct{}{}
		return nil
	})
	viewer, outsider := remoteTestNode(t, "viewer", nil), remoteTestNode(t, "outsider", nil)
	connectNodes(t, viewer, host)
	connectNodes(t, outsider, host)
	viewerID := viewer.Snapshot().Self.ID
	id, grant := newID(), newID()
	host.mu.Lock()
	peer := host.connections[viewerID]
	session := host.newRemoteDesktopLocked(id, peer, true)
	session.view.Status = domain.RemoteDesktopConnecting
	session.grant = grant
	host.remoteDeadlineLocked(session, remoteSetupTimeout, "failed")
	host.mu.Unlock()

	assertRemoteStreamRejected(t, outsider, host, id, grant)
	stream := openRemoteTestStream(t, viewer, host, id, grant)
	defer stream.Close()
	if hello, err := readFrame(stream); err != nil || hello.Type != "remote_desktop_ready" {
		t.Fatal("authorized direct peer did not connect", err)
	}
	waitRemote(t, host, domain.RemoteDesktopActive)
	assertRemoteStreamRejected(t, viewer, host, id, grant)

	// Losing the messaging connection invalidates its grant even while
	// the dedicated media connection is still open.
	_ = peer.conn.Close()
	if ended := waitRemote(t, host, domain.RemoteDesktopEnded); ended.Reason != "disconnected" {
		t.Fatal(ended)
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("host helper remained active after losing the direct peer")
	}
}

func TestRemoteDesktopDoesNotUseLegacyOrRelayedContacts(t *testing.T) {
	withEngine := remoteTestNode(t, "with-engine", func(context.Context, net.Conn, remotedesktop.Start, <-chan domain.RemoteDesktopInput, remotedesktop.Events) error {
		t.Error("unsupported session started the engine")
		return nil
	})
	old := remoteTestNode(t, "old", nil)
	connectNodes(t, withEngine, old)
	if err := withEngine.startRemoteDesktop(old.Snapshot().Self.ID); err == nil {
		t.Fatal("an older peer accepted a remote desktop request")
	}
	if relayType("remote_desktop_signal") || relayType("remote_desktop_stream") {
		t.Fatal("remote desktop exposed through mesh relay")
	}
	withEngine.receiveRemoteDesktop(old.Snapshot().Self.ID, nil, &remoteDesktopSignal{ID: newID(), Action: "invite"})
	if withEngine.Snapshot().RemoteDesktop.ID != "" {
		t.Fatal("unauthenticated envelope created an invitation")
	}
}

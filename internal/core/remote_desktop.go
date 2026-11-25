package core

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
	"goim/internal/remotedesktop"
)

const (
	remoteDesktopVersion = 1
	remoteInviteTimeout  = 60 * time.Second
	remoteSetupTimeout   = 30 * time.Second
)

type remoteDesktopSignal struct {
	ID      string
	Action  string
	Grant   string `json:",omitempty"`
	Control bool   `json:",omitempty"`
	Audio   bool   `json:",omitempty"`
	Reason  string `json:",omitempty"`
}

// Session state is protected by Node.mu. The channels, context and input
// queue can also be used by the helper worker.
type remoteSession struct {
	view        domain.RemoteDesktopSession
	connectedAt time.Time
	peer        *peerConn
	ctx         context.Context
	cancel      context.CancelFunc
	timer       *time.Timer
	expires     time.Time
	grant       string
	attached    bool
	setupSent   chan struct{}
	frames      chan domain.RemoteDesktopFrame
	cursors     chan domain.RemoteDesktopCursor
	input       *remotedesktop.InputQueue
}

func validRemoteID(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (n *Node) newRemoteDesktopLocked(id string, peer *peerConn, incoming bool) *remoteSession {
	name := peer.peer.Name
	if remark := n.data.PeerRemarks[peer.peer.ID]; remark != "" {
		name = remark
	}
	ctx, cancel := context.WithCancel(n.ctx)
	s := &remoteSession{
		view: domain.RemoteDesktopSession{ID: id, PeerID: peer.peer.ID, PeerName: name, Incoming: incoming, Status: domain.RemoteDesktopPending},
		peer: peer, ctx: ctx, cancel: cancel,
		frames: make(chan domain.RemoteDesktopFrame, 1),
	}
	if !incoming {
		s.cursors = make(chan domain.RemoteDesktopCursor, 1)
		s.input = remotedesktop.NewInputQueue()
	}
	n.remoteDesktop = s
	n.remoteDeadlineLocked(s, remoteInviteTimeout, "no_answer")
	return s
}

func (n *Node) remoteDeadlineLocked(s *remoteSession, duration time.Duration, reason string) {
	if s.timer != nil {
		s.timer.Stop()
	}
	status := s.view.Status
	s.expires = time.Now().Add(duration)
	s.timer = time.AfterFunc(duration, func() {
		n.mu.Lock()
		expired := n.remoteDesktop == s && s.view.Status == status
		if expired {
			n.endRemoteDesktopLocked(reason)
		}
		n.mu.Unlock()
		if expired {
			n.sendRemoteStop(s, reason)
		}
	})
}

func remoteLive(s *remoteSession) bool {
	return s != nil && s.view.Status != domain.RemoteDesktopEnded
}

func (n *Node) startRemoteDesktop(peerID string) error {
	n.mu.Lock()
	if n.closing || n.remoteEngine == nil || n.data.State.Locked {
		n.mu.Unlock()
		return errors.New("当前无法发起远程桌面")
	}
	peer := n.connections[peerID]
	if peer == nil || !peer.peer.RemoteDesktopSupported {
		n.mu.Unlock()
		return errors.New("远程桌面需要支持该功能的 GoIM 联系人直连")
	}
	if remoteLive(n.remoteDesktop) {
		n.mu.Unlock()
		return errors.New("当前已有远程桌面会话")
	}
	s := n.newRemoteDesktopLocked(newID(), peer, false)
	n.sendRemoteSetupLocked(s, remoteDesktopSignal{ID: s.view.ID, Action: "invite"})
	n.mu.Unlock()
	n.signal()
	return nil
}

func (n *Node) acceptRemoteDesktop(id string, control, audio bool) error {
	n.mu.Lock()
	s := n.remoteDesktop
	if s == nil || s.view.ID != id || !s.view.Incoming || s.view.Status != domain.RemoteDesktopPending ||
		n.closing || n.data.State.Locked || n.connections[s.view.PeerID] != s.peer || !time.Now().Before(s.expires) {
		n.mu.Unlock()
		return errors.New("远程桌面邀请已失效")
	}
	s.grant = newID()
	s.view.Control, s.view.Audio = control, audio
	s.view.Status = domain.RemoteDesktopConnecting
	n.remoteDeadlineLocked(s, remoteSetupTimeout, "failed")
	n.sendRemoteSetupLocked(s, remoteDesktopSignal{ID: id, Action: "accept", Grant: s.grant, Control: control, Audio: audio})
	n.mu.Unlock()
	n.signal()
	return nil
}

func (n *Node) sendRemoteSetupLocked(s *remoteSession, signal remoteDesktopSignal) {
	// Socket writes must not block the command loop behind an invite or grant.
	done := make(chan struct{})
	s.setupSent = done
	n.workers.Add(1)
	go func() {
		defer n.workers.Done()
		defer close(done)
		if err := s.peer.sendContext(s.ctx, envelope{Type: "remote_desktop_signal", RemoteDesktop: &signal}); err != nil {
			n.finishRemoteDesktop(s, "disconnected", false)
		}
	}()
}

func (n *Node) receiveRemoteDesktop(peerID string, source *peerConn, msg *remoteDesktopSignal) {
	if source == nil || msg == nil || !validRemoteID(msg.ID) {
		return
	}
	n.mu.Lock()
	if n.closing || n.remoteEngine == nil || n.data.State.Locked ||
		n.connections[peerID] != source || !source.peer.RemoteDesktopSupported {
		n.mu.Unlock()
		return
	}
	s := n.remoteDesktop
	switch msg.Action {
	case "invite":
		// Duplicate invitations cannot extend a consent window or reopen it.
		if s != nil && s.view.ID == msg.ID && s.peer == source {
			n.mu.Unlock()
			return
		}
		if remoteLive(s) {
			n.mu.Unlock()
			reply := remoteDesktopSignal{ID: msg.ID, Action: "stop", Reason: "busy"}
			_ = source.send(envelope{Type: "remote_desktop_signal", RemoteDesktop: &reply})
			return
		}
		n.newRemoteDesktopLocked(msg.ID, source, true)
		n.mu.Unlock()
		n.signal()
	case "accept":
		if s == nil || s.view.ID != msg.ID || s.peer != source || s.view.Incoming ||
			s.view.Status != domain.RemoteDesktopPending || !validRemoteID(msg.Grant) || !time.Now().Before(s.expires) {
			n.mu.Unlock()
			return
		}
		s.view.Status = domain.RemoteDesktopConnecting
		s.view.Control, s.view.Audio = msg.Control, msg.Audio
		n.remoteDeadlineLocked(s, remoteSetupTimeout, "failed")
		n.mu.Unlock()
		n.signal()
		if !n.async(func() { n.dialRemoteDesktop(s, msg.Grant) }) {
			n.finishRemoteDesktop(s, "disconnected", false)
		}
	case "stop":
		if remoteLive(s) && s.view.ID == msg.ID && s.peer == source {
			n.endRemoteDesktopLocked(safeRemoteReason(msg.Reason))
		}
		n.mu.Unlock()
	default:
		n.mu.Unlock()
	}
}

func safeRemoteReason(reason string) string {
	switch reason {
	case "ended", "declined", "no_answer", "disconnected", "permission", "unsupported", "failed", "busy", "locked":
		return reason
	}
	return "failed"
}

func (n *Node) stopRemoteDesktop(id, reason string) error {
	n.mu.Lock()
	s := n.remoteDesktop
	if !remoteLive(s) || s.view.ID != id ||
		reason == "declined" && (!s.view.Incoming || s.view.Status != domain.RemoteDesktopPending) {
		n.mu.Unlock()
		return nil
	}
	n.endRemoteDesktopLocked(reason)
	n.mu.Unlock()
	n.sendRemoteStop(s, reason)
	return nil
}

func (n *Node) endRemoteDesktopForLock() {
	n.mu.Lock()
	s := n.remoteDesktop
	if !remoteLive(s) {
		n.mu.Unlock()
		return
	}
	n.endRemoteDesktopLocked("locked")
	n.mu.Unlock()
	n.sendRemoteStop(s, "locked")
}

func (n *Node) sendRemoteStop(s *remoteSession, reason string) {
	n.async(func() {
		if s.setupSent != nil {
			// A stop cannot overtake an invite or grant that already started writing.
			select {
			case <-s.setupSent:
			case <-n.ctx.Done():
				return
			}
		}
		stop := remoteDesktopSignal{ID: s.view.ID, Action: "stop", Reason: reason}
		_ = s.peer.sendContext(n.ctx, envelope{Type: "remote_desktop_signal", RemoteDesktop: &stop})
	})
}

func (n *Node) finishRemoteDesktop(s *remoteSession, reason string, notify bool) {
	n.mu.Lock()
	if n.remoteDesktop != s || !remoteLive(s) {
		n.mu.Unlock()
		return
	}
	n.endRemoteDesktopLocked(reason)
	n.mu.Unlock()
	if notify {
		n.sendRemoteStop(s, reason)
	}
}

func (n *Node) remotePeerDisconnectedLocked(peer *peerConn) {
	if s := n.remoteDesktop; remoteLive(s) && s.peer == peer {
		n.endRemoteDesktopLocked("disconnected")
	}
}

func (n *Node) endRemoteDesktopLocked(reason string) {
	s := n.remoteDesktop
	if !remoteLive(s) {
		return
	}
	s.view.Status = domain.RemoteDesktopEnded
	if reason == "ended" && s.connectedAt.IsZero() {
		reason = "cancelled"
	}
	s.view.Reason = reason
	s.grant = ""
	if s.timer != nil {
		s.timer.Stop()
	}
	s.cancel()
	select {
	case <-s.frames:
	default:
	}
	close(s.frames)
	if s.cursors != nil {
		select {
		case <-s.cursors:
		default:
		}
		close(s.cursors)
	}
	n.recordRemoteDesktopHistoryLocked(s)
	n.signal()
}

func (n *Node) dialRemoteDesktop(s *remoteSession, grant string) {
	dial := net.Dialer{Timeout: 5 * time.Second}
	raw, err := dial.DialContext(s.ctx, "tcp", s.peer.peer.Address)
	if err != nil {
		n.finishRemoteDesktop(s, "disconnected", true)
		return
	}
	defer raw.Close()
	if !n.allowedConnection(raw.LocalAddr(), raw.RemoteAddr()) {
		n.finishRemoteDesktop(s, "disconnected", true)
		return
	}
	conn := tls.Client(raw, n.tls.Clone())
	stop := watchConnection(s.ctx, conn)
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err = conn.HandshakeContext(s.ctx); err != nil {
		n.finishRemoteDesktop(s, "disconnected", true)
		return
	}
	hello, err := readFrame(conn)
	if err != nil || hello.Type != "hello" || hello.RemoteDesktopVersion != remoteDesktopVersion ||
		hello.Peer.ID != s.view.PeerID || !remoteTLSIdentity(conn, s.view.PeerID) {
		n.finishRemoteDesktop(s, "failed", true)
		return
	}
	self := keyID(n.key.Public().(ed25519.PublicKey))
	req := envelope{
		Type: "remote_desktop_stream", Peer: domain.Peer{ID: self},
		RemoteDesktop: &remoteDesktopSignal{ID: s.view.ID, Grant: grant},
	}
	if err := writeFrame(conn, req); err != nil {
		n.finishRemoteDesktop(s, "disconnected", true)
		return
	}
	reply, err := readFrame(conn)
	if err != nil || reply.Type != "remote_desktop_ready" || reply.RemoteDesktop == nil || reply.RemoteDesktop.ID != s.view.ID {
		n.finishRemoteDesktop(s, "failed", true)
		return
	}
	n.runRemoteDesktop(s, conn, "viewer")
}

func remoteTLSIdentity(conn *tls.Conn, expected string) bool {
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) != 1 {
		return false
	}
	key, ok := certs[0].PublicKey.(ed25519.PublicKey)
	return ok && keyID(key) == expected
}

func (n *Node) serveRemoteDesktop(conn *tls.Conn, peerID string, msg *remoteDesktopSignal) {
	if msg == nil || !validRemoteID(msg.ID) || !validRemoteID(msg.Grant) {
		return
	}
	n.mu.Lock()
	s := n.remoteDesktop
	if s == nil || s.view.ID != msg.ID || !s.view.Incoming || s.view.PeerID != peerID ||
		s.view.Status != domain.RemoteDesktopConnecting || s.attached || n.closing || n.data.State.Locked ||
		n.connections[peerID] != s.peer || !time.Now().Before(s.expires) ||
		subtle.ConstantTimeCompare([]byte(msg.Grant), []byte(s.grant)) != 1 {
		n.mu.Unlock()
		return
	}
	s.attached, s.grant = true, ""
	delete(n.incoming, conn)
	n.mu.Unlock()
	stop := watchConnection(s.ctx, conn)
	defer stop()
	if err := writeFrame(conn, envelope{Type: "remote_desktop_ready", RemoteDesktop: &remoteDesktopSignal{ID: s.view.ID}}); err != nil {
		n.finishRemoteDesktop(s, "disconnected", true)
		return
	}
	n.runRemoteDesktop(s, conn, "host")
}

func (n *Node) runRemoteDesktop(s *remoteSession, conn net.Conn, role string) {
	n.mu.Lock()
	if n.remoteDesktop != s || s.view.Status != domain.RemoteDesktopConnecting ||
		n.data.State.Locked || n.closing || n.connections[s.view.PeerID] != s.peer || !time.Now().Before(s.expires) {
		n.mu.Unlock()
		return
	}
	start := remotedesktop.Start{Role: role, Control: s.view.Control, Audio: s.view.Audio}
	n.mu.Unlock()
	_ = conn.SetDeadline(time.Time{})
	onReady := func() {
		n.mu.Lock()
		if n.remoteDesktop == s && s.view.Status == domain.RemoteDesktopConnecting &&
			!n.data.State.Locked && !n.closing && time.Now().Before(s.expires) {
			s.view.Status = domain.RemoteDesktopActive
			s.connectedAt = time.Now()
			s.timer.Stop()
			n.signal()
		}
		n.mu.Unlock()
	}
	onFrame := func(frame domain.RemoteDesktopFrame) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.remoteDesktop != s || s.view.Status != domain.RemoteDesktopActive || s.view.Incoming {
			return
		}
		select {
		case <-s.frames:
		default:
		}
		s.frames <- frame
	}
	onCursor := func(cursor domain.RemoteDesktopCursor) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.remoteDesktop != s || s.view.Status != domain.RemoteDesktopActive || s.view.Incoming {
			return
		}
		select {
		case <-s.cursors:
		default:
		}
		s.cursors <- cursor
	}
	onDevices := func(devices []string) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.remoteDesktop == s && s.view.Status == domain.RemoteDesktopActive && !s.view.Incoming && s.view.Audio {
			s.view.OutputDevices = slices.Clone(devices)
			n.signal()
		}
	}
	result := n.remoteEngine(s.ctx, conn, start, s.input, remotedesktop.Events{
		Ready: onReady, Frame: onFrame, Cursor: onCursor, AudioDevices: onDevices,
	})
	n.finishRemoteDesktop(s, remotedesktop.Reason(result), true)
}

func (n *Node) RemoteDesktopFrames(id string) <-chan domain.RemoteDesktopFrame {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s := n.remoteDesktop; s != nil && s.view.ID == id && s.view.Status == domain.RemoteDesktopActive && !s.view.Incoming {
		return s.frames
	}
	return nil
}

func (n *Node) RemoteDesktopCursors(id string) <-chan domain.RemoteDesktopCursor {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s := n.remoteDesktop; s != nil && s.view.ID == id && s.view.Status == domain.RemoteDesktopActive && !s.view.Incoming {
		return s.cursors
	}
	return nil
}

func (n *Node) SendRemoteDesktopInput(id string, input domain.RemoteDesktopInput) error {
	n.mu.Lock()
	s := n.remoteDesktop
	if s == nil || s.view.ID != id || s.view.Status != domain.RemoteDesktopActive ||
		s.view.Incoming || n.data.State.Locked || n.closing {
		n.mu.Unlock()
		return errors.New("远程桌面会话未连接")
	}
	if !validRemoteInput(s.view, input) {
		n.mu.Unlock()
		return errors.New("当前远程桌面会话不允许此操作")
	}
	if s.input.Push(input) || input.Action == "move" {
		n.mu.Unlock()
		return nil
	}
	n.endRemoteDesktopLocked("failed")
	n.mu.Unlock()
	n.sendRemoteStop(s, "failed")
	return errors.New("远程输入队列已满，会话已结束")
}

func validRemoteInput(view domain.RemoteDesktopSession, input domain.RemoteDesktopInput) bool {
	if input.Action == "audio" {
		return view.Audio && input.Volume >= 0 && input.Volume <= 100 &&
			(input.Device == "" || slices.Contains(view.OutputDevices, input.Device))
	}
	if !view.Control {
		return false
	}
	switch input.Action {
	case "move", "button", "wheel":
		return input.X >= 0 && input.X <= 65535 && input.Y >= 0 && input.Y <= 65535 &&
			(input.Action != "button" || input.Button >= 1 && input.Button <= 3) &&
			input.DeltaX >= -4096 && input.DeltaX <= 4096 && input.DeltaY >= -4096 && input.DeltaY <= 4096
	case "key":
		return input.Key != "" && len(input.Key) <= 32 && utf8.ValidString(input.Key) &&
			!strings.ContainsRune(input.Key, 0) && input.Modifiers >= 0 && input.Modifiers <= 15
	case "shortcut":
		return input.Key == "switch-app" || input.Key == "close-window"
	case "release":
		return true
	default:
		return false
	}
}

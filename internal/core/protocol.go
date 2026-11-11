package core

import (
	"crypto/ed25519"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"goim/internal/domain"
)

const protocolVersion = 1
const maxFrame = 4 << 20

type envelope struct {
	RecallSupported bool `json:",omitempty"`
	Version         int
	ID              string
	Type            string
	Peer            domain.Peer
	Message         domain.Message
	Group           domain.Group
	Invitation      domain.Invitation
	File            domain.File
	Files           []domain.File
	CatalogRevision uint64
	TransferID      string
	Offset          int64
	Status          string
	Error           string
	Target          string
	Proof           domain.Ownership
	Avatar          []byte
	Peers           []domain.Peer
}
type peerConn struct {
	conn     *tls.Conn
	peer     domain.Peer
	write    sync.Mutex
	outbound bool
}

func writeFrame(w io.Writer, e envelope) error {
	e.Version = protocolVersion
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(b) > maxFrame {
		return errors.New("消息超出大小限制")
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	if _, err = w.Write(h[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
func readFrame(r io.Reader) (envelope, error) {
	var e envelope
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return e, err
	}
	size := binary.BigEndian.Uint32(h[:])
	if size == 0 || size > maxFrame {
		return e, errors.New("无效消息长度")
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return e, err
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return e, err
	}
	if e.Version != protocolVersion {
		return e, errors.New("应用协议版本不兼容")
	}
	return e, nil
}
func (p *peerConn) send(e envelope) error {
	p.write.Lock()
	defer p.write.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	return writeFrame(p.conn, e)
}
func (n *Node) hello() envelope {
	e := n.profileEnvelope()
	e.Type = "hello"
	e.RecallSupported = true
	return e
}
func (n *Node) advertisedAddress() string {
	_, port, _ := net.SplitHostPort(n.Address())
	return net.JoinHostPort("", port)
}
func (n *Node) establish(conn *tls.Conn, outbound bool, expected string) {
	if !n.allowedRemote(conn.RemoteAddr()) {
		conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err := conn.HandshakeContext(n.ctx); err != nil {
		conn.Close()
		return
	}
	if err := writeFrame(conn, n.hello()); err != nil {
		conn.Close()
		return
	}
	hello, err := readFrame(conn)
	if err != nil {
		conn.Close()
		return
	}
	key, ok := conn.ConnectionState().PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok || hello.Peer.ID != keyID(key) || hello.Peer.ID == keyID(n.key.Public().(ed25519.PublicKey)) || expected != "" && hello.Peer.ID != expected {
		conn.Close()
		return
	}
	if hello.Type == "file_request" {
		defer conn.Close()
		n.serveFile(conn, hello, hello.Peer.ID)
		return
	}
	if hello.Type != "hello" {
		conn.Close()
		return
	}
	hello.Peer.PublicKey = append([]byte(nil), key...)
	hello.Peer.Transport, hello.Peer.ClientType = domain.TransportGoIM, domain.ClientGoIM
	hello.Peer.Avatar = ""
	hello.Peer.RecallSupported = hello.RecallSupported
	if len(hello.Avatar) > 0 {
		path, err := n.cacheAvatar(hello.Peer.ID, hello.Avatar)
		if err != nil {
			conn.Close()
			return
		}
		hello.Peer.Avatar = path
	}
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	_, port, _ := net.SplitHostPort(hello.Peer.Address)
	if port != "" {
		hello.Peer.Address = net.JoinHostPort(host, port)
	} else {
		hello.Peer.Address = conn.RemoteAddr().String()
	}
	if len(hello.Peer.Name) > 160 || !validProfileDetails(hello.Peer.Signature, hello.Peer.Presence, true) ||
		!validContactDetails(hello.Peer.Details) || !validProfileLine(hello.Peer.Version, 40) {
		conn.Close()
		return
	}
	pc := &peerConn{conn: conn, peer: hello.Peer, outbound: outbound}
	n.mu.Lock()
	preferred := n.data.State.Self.ID < pc.peer.ID
	if old := n.connections[pc.peer.ID]; old != nil {
		if old.outbound == preferred || outbound != preferred {
			n.mu.Unlock()
			conn.Close()
			return
		}
		old.conn.Close()
	}
	if n.closing {
		n.mu.Unlock()
		conn.Close()
		return
	}
	n.connections[pc.peer.ID] = pc
	n.mu.Unlock()
	_ = conn.SetDeadline(time.Time{})
	n.change(func(s *domain.State) {
		i := peerIndex(s, pc.peer.ID)
		pc.peer.Online = true
		pc.peer.LastSeen = time.Now()
		if i < 0 {
			s.Peers = append(s.Peers, pc.peer)
		} else {
			pc.peer.ContactGroup = s.Peers[i].ContactGroup
			s.Peers[i] = pc.peer
		}
	})
	n.async(func() { n.replay(pc.peer.ID); n.sendCatalog(pc.peer.ID) })
	defer func() {
		conn.Close()
		n.mu.Lock()
		current := n.connections[pc.peer.ID] == pc
		if current {
			delete(n.connections, pc.peer.ID)
		}
		n.mu.Unlock()
		if current {
			n.change(func(s *domain.State) {
				if i := peerIndex(s, pc.peer.ID); i >= 0 {
					s.Peers[i].Online = false
				}
				for i := range s.Transfers {
					t := &s.Transfers[i]
					if t.PeerID == pc.peer.ID && t.Status == "transferring" {
						t.Status = "waiting"
						t.Speed = 0
					}
				}
			})
		}
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(25 * time.Second))
		e, err := readFrame(conn)
		if err != nil {
			return
		}
		if err = n.receive(pc.peer.ID, e); err != nil {
			n.report(err)
		}
	}
}
func (n *Node) send(peer string, e envelope) error {
	n.mu.Lock()
	pc := n.connections[peer]
	n.mu.Unlock()
	if pc == nil {
		return errors.New("等待对方上线")
	}
	if err := pc.send(e); err != nil {
		pc.conn.Close()
		return err
	}
	return nil
}
func (n *Node) reliable(peer string, e envelope) {
	_ = n.durable(func(*persisted) []pending { return []pending{{PeerID: peer, Envelope: e}} })
}
func (n *Node) replay(peer string) {
	n.flushPeer(peer)
	n.mu.Lock()
	var resume []string
	for _, t := range n.data.State.Transfers {
		if t.PeerID == peer && t.Direction == "receive" && t.Status == "waiting" {
			resume = append(resume, t.ID)
		}
	}
	n.mu.Unlock()
	for _, id := range resume {
		n.startReceive(id, "")
	}
}
func (n *Node) flushPeer(peer string) {
	n.flushMu.Lock()
	n.mu.Lock()
	var list []pending
	for _, p := range n.data.Outbox {
		if p.PeerID == peer {
			list = append(list, p)
		}
	}
	pTransport := n.data.State.Peer(peer).Transport
	legacy := pTransport == legacyIPMSG
	isNWT := pTransport == domain.TransportNeiwangtong
	n.mu.Unlock()
	sort.Slice(list, func(i, j int) bool {
		if list[i].Sequence == list[j].Sequence {
			return list[i].Envelope.ID < list[j].Envelope.ID
		}
		return list[i].Sequence < list[j].Sequence
	})
	if legacy {
		var done []pending
		for _, p := range list {
			if err := n.legacyPending(peer, p); err != nil {
				break
			}
			done = append(done, p)
		}
		if len(done) != 0 {
			_ = n.legacyCompleted(done)
		}
		n.flushMu.Unlock()
		return
	}
	if isNWT {
		var done []pending
		for _, p := range list {
			if err := n.neiwangtongPending(peer, p); err != nil {
				break
			}
			done = append(done, p)
		}
		if len(done) != 0 {
			_ = n.legacyCompleted(done)
		}
		n.flushMu.Unlock()
		return
	}
	for _, p := range list {
		if err := n.send(peer, p.Envelope); err != nil {
			n.flushMu.Unlock()
			return
		}
	}
	n.flushMu.Unlock()
}
func (n *Node) receive(peer string, e envelope) error {
	if e.Type == "ping" {
		return n.send(peer, envelope{Type: "pong"})
	}
	if e.Type == "pong" {
		return nil
	}
	if e.Type == "ack" {
		return n.durable(func(data *persisted) []pending {
			p, ok := data.Outbox[e.ID]
			if ok && p.PeerID == peer {
				delete(data.Outbox, e.ID)
				for i := range data.State.Messages {
					m := &data.State.Messages[i]
					if m.ID == p.Envelope.Message.ID {
						m.Status = "delivered"
						for _, remaining := range data.Outbox {
							if remaining.Envelope.Type == "message" && remaining.Envelope.Message.ID == m.ID {
								m.Status = "sent"
								break
							}
						}
					}
				}
			}
			return nil
		})
	}
	if e.ID != "" {
		n.mu.Lock()
		seen := n.data.Seen[peer+":"+e.ID]
		n.mu.Unlock()
		if seen {
			return n.send(peer, envelope{Type: "ack", ID: e.ID})
		}
	}
	var err error
	switch e.Type {
	case "message":
		err = n.receiveMessage(peer, e)
	case "message_recall":
		err = n.receiveMessageRecall(peer, e)
	case "profile":
		err = n.receiveProfile(peer, e)
	case "group_invite", "group_state", "invite_reply", "group_leave", "ownership_offer", "ownership_accept", "ownership_result", "group_nickname":
		err = n.receiveGroup(peer, e)
	case "file_offer":
		err = n.receiveOffer(peer, e)
	case "file_retry":
		err = n.receiveFileRetry(peer, e)
	case "file_status":
		err = n.receiveFileStatus(peer, e)
	case "catalog":
		err = n.durable(func(data *persisted) []pending {
			if e.CatalogRevision < data.CatalogRevisions[peer] {
				return nil
			}
			data.CatalogRevisions[peer] = e.CatalogRevision
			s := &data.State
			for i := range s.Files {
				if s.Files[i].SenderID == peer {
					s.Files[i].Shared = false
				}
			}
			for _, f := range e.Files {
				if f.SenderID == peer && f.Shared && validFile(f) {
					f.SourcePath = ""
					if i := fileIndex(s, f.ID); i >= 0 {
						if s.Files[i].SenderID == peer {
							s.Files[i].Shared = true
						}
					} else {
						s.Files = append(s.Files, f)
					}
				}
			}
			return nil
		})
	case "catalog_request":
		n.sendCatalog(peer)
	case "get_shared":
		err = n.offerShared(peer, e.File.ID)
	default:
		err = fmt.Errorf("不支持的消息类型: %s", e.Type)
	}
	if err != nil {
		return err
	}
	if e.ID != "" {
		err = n.durable(func(p *persisted) []pending { p.Seen[peer+":"+e.ID] = true; return nil })
		if err != nil {
			return err
		}
		return n.send(peer, envelope{Type: "ack", ID: e.ID})
	}
	return nil
}
func (n *Node) receiveMessage(peer string, e envelope) error {
	m := e.Message
	m.Deleted, m.Recalled = false, false
	if m.ID == "" || m.SenderID != peer || m.Kind != "text" || len(m.Text) > 256<<10 {
		return errors.New("无效聊天消息")
	}
	s := n.snapshot(false)
	if strings.HasPrefix(m.ConversationID, "group:") {
		g := s.Group(strings.TrimPrefix(m.ConversationID, "group:"))
		if g.ID == "" || g.Dissolved || g.Left || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return errors.New("群成员身份已失效")
		}
	} else {
		p := s.Peer(peer)
		m.ConversationID = directID(s.Self.ID, peer)
		n.change(func(st *domain.State) { ensureDirect(st, p) })
	}
	m.Status = "delivered"
	return n.durable(func(p *persisted) []pending {
		if p.MessageRecalls[messageRecallKey(peer, m.ConversationID, m.ID)] {
			return nil
		}
		s := &p.State
		for _, old := range s.Messages {
			if old.ID == m.ID {
				return nil
			}
		}
		s.Messages = append(s.Messages, m)
		if i := convIndex(s, m.ConversationID); i >= 0 {
			s.Conversations[i].Hidden = false
			s.Conversations[i].Unread++
			s.Conversations[i].Preview = m.Text
			s.Conversations[i].UpdatedAt = m.CreatedAt
		}
		return nil
	})
}

type announcement struct {
	Version   int
	Peer      domain.Peer
	Signature []byte
}

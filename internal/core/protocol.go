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
	"strings"
	"sync"
	"time"

	"goim/internal/domain"
)

const protocolVersion = 1
const maxFrame = 4 << 20

type envelope struct {
	Version    int
	ID         string
	Type       string
	Peer       domain.Peer
	Message    domain.Message
	Group      domain.Group
	Invitation domain.Invitation
	File       domain.File
	Files      []domain.File
	TransferID string
	Offset     int64
	Status     string
	Target     string
	Proof      domain.Ownership
	Avatar     []byte
	Peers      []domain.Peer
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
	s := n.Snapshot()
	p := s.Self
	p.Address = n.advertisedAddress()
	p.Avatar = ""
	return envelope{Type: "hello", Peer: p}
}
func (n *Node) advertisedAddress() string {
	_, port, _ := net.SplitHostPort(n.Address())
	return net.JoinHostPort("", port)
}
func (n *Node) establish(conn *tls.Conn, outbound bool, expected string) {
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
	hello.Peer.Avatar = ""
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	_, port, _ := net.SplitHostPort(hello.Peer.Address)
	if port != "" {
		hello.Peer.Address = net.JoinHostPort(host, port)
	} else {
		hello.Peer.Address = conn.RemoteAddr().String()
	}
	if len(hello.Peer.Name) > 128 {
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
	if e.ID == "" {
		e.ID = newID()
	}
	e.Version = protocolVersion
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return
	}
	n.data.Outbox[e.ID] = pending{PeerID: peer, Envelope: e}
	err := saveStore(n.db, &n.data)
	n.mu.Unlock()
	if err != nil {
		n.report(err)
		return
	}
	_ = n.send(peer, e)
}
func (n *Node) replay(peer string) {
	n.mu.Lock()
	var list []envelope
	for _, p := range n.data.Outbox {
		if p.PeerID == peer {
			list = append(list, p.Envelope)
		}
	}
	var resume []string
	for _, t := range n.data.State.Transfers {
		if t.PeerID == peer && t.Direction == "receive" && t.Status == "waiting" {
			resume = append(resume, t.ID)
		}
	}
	n.mu.Unlock()
	for _, e := range list {
		_ = n.send(peer, e)
	}
	for _, id := range resume {
		n.startReceive(id, "")
	}
}
func (n *Node) receive(peer string, e envelope) error {
	if e.Type == "ping" {
		return n.send(peer, envelope{Type: "pong"})
	}
	if e.Type == "pong" {
		return nil
	}
	if e.Type == "ack" {
		n.mu.Lock()
		p, ok := n.data.Outbox[e.ID]
		if ok && p.PeerID == peer {
			delete(n.data.Outbox, e.ID)
			for i := range n.data.State.Messages {
				m := &n.data.State.Messages[i]
				if m.ID == p.Envelope.Message.ID {
					m.Status = "delivered"
				}
			}
			_ = saveStore(n.db, &n.data)
		}
		n.mu.Unlock()
		n.signal()
		return nil
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
	case "group_invite", "group_state", "invite_reply", "group_leave", "ownership_offer", "ownership_accept":
		err = n.receiveGroup(peer, e)
	case "file_offer":
		err = n.receiveOffer(peer, e)
	case "file_status":
		n.mu.Lock()
		i := transferIndex(&n.data.State, e.TransferID)
		authorized := i >= 0 && n.data.State.Transfers[i].PeerID == peer
		if authorized && (e.Status == "cancelled" || e.Status == "rejected") {
			if cancel := n.transfers[e.TransferID]; cancel != nil {
				cancel()
			}
		}
		n.mu.Unlock()
		if authorized {
			n.change(func(s *domain.State) {
				if i := transferIndex(s, e.TransferID); i >= 0 {
					s.Transfers[i].Status = e.Status
					if e.Status == "completed" {
						s.Transfers[i].Done = s.Transfers[i].Size
					}
					s.Transfers[i].UpdatedAt = time.Now()
				}
			})
		}
	case "catalog":
		n.change(func(s *domain.State) {
			var files []domain.File
			for _, f := range s.Files {
				if f.SenderID != peer || !f.Shared {
					files = append(files, f)
				}
			}
			for _, f := range e.Files {
				if f.SenderID == peer && f.Shared && validFile(f) {
					f.SourcePath = ""
					files = append(files, f)
				}
			}
			s.Files = files
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
		n.mu.Lock()
		n.data.Seen[peer+":"+e.ID] = true
		err = saveStore(n.db, &n.data)
		n.mu.Unlock()
		if err != nil {
			return err
		}
		return n.send(peer, envelope{Type: "ack", ID: e.ID})
	}
	return nil
}
func (n *Node) receiveMessage(peer string, e envelope) error {
	m := e.Message
	if m.ID == "" || m.SenderID != peer || m.Kind != "text" || len(m.Text) > 256<<10 {
		return errors.New("无效聊天消息")
	}
	s := n.Snapshot()
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
	n.change(func(s *domain.State) {
		for _, old := range s.Messages {
			if old.ID == m.ID {
				return
			}
		}
		s.Messages = append(s.Messages, m)
		if i := convIndex(s, m.ConversationID); i >= 0 {
			s.Conversations[i].Unread++
			s.Conversations[i].Preview = m.Text
			s.Conversations[i].UpdatedAt = m.CreatedAt
		}
	})
	return nil
}

type announcement struct {
	Version   int
	Peer      domain.Peer
	Signature []byte
}

func (n *Node) discoveryLoop() {
	defer n.wg.Done()
	address := &net.UDPAddr{IP: net.IPv4(239, 192, 77, 77), Port: 47841}
	var iface *net.Interface
	if name := n.Snapshot().Settings.Network; name != "" && name != "auto" {
		iface, _ = net.InterfaceByName(name)
	}
	var conn *net.UDPConn
	var err error
	if n.discovery {
		conn, err = net.ListenMulticastUDP("udp4", iface, address)
	}
	if err != nil {
		n.notice("自动发现暂不可用，可输入 IP 连接")
	} else if conn != nil {
		defer conn.Close()
		_ = conn.SetReadBuffer(64 << 10)
		n.async(func() {
			buf := make([]byte, 8192)
			for {
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				size, remote, e := conn.ReadFromUDP(buf)
				if e != nil {
					if n.ctx.Err() != nil {
						return
					}
					continue
				}
				var a announcement
				if json.Unmarshal(buf[:size], &a) != nil || a.Version != protocolVersion || a.Peer.ID == n.Snapshot().Self.ID || len(a.Peer.PublicKey) != ed25519.PublicKeySize || a.Peer.ID != keyID(a.Peer.PublicKey) {
					continue
				}
				signature := a.Signature
				a.Signature = nil
				raw, _ := json.Marshal(a)
				if !ed25519.Verify(a.Peer.PublicKey, raw, signature) {
					continue
				}
				_, port, _ := net.SplitHostPort(a.Peer.Address)
				if port == "" {
					continue
				}
				n.mu.Lock()
				connected := n.connections[a.Peer.ID] != nil
				n.mu.Unlock()
				if !connected {
					n.Connect(net.JoinHostPort(remote.IP.String(), port))
				}
			}
		})
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	announce := func() {
		s := n.Snapshot()
		if n.discovery {
			p := s.Self
			p.Avatar = ""
			p.Address = n.advertisedAddress()
			a := announcement{Version: protocolVersion, Peer: p}
			raw, _ := json.Marshal(a)
			a.Signature = ed25519.Sign(n.key, raw)
			raw, _ = json.Marshal(a)
			if c, e := net.DialUDP("udp4", nil, address); e == nil {
				_, _ = c.Write(raw)
				c.Close()
			}
		}
		n.mu.Lock()
		var ids []string
		for id := range n.connections {
			ids = append(ids, id)
		}
		n.mu.Unlock()
		for _, id := range ids {
			_ = n.send(id, envelope{Type: "ping"})
			n.replay(id)
		}
	}
	announce()
	for {
		select {
		case <-ticker.C:
			announce()
		case <-n.ctx.Done():
			return
		}
	}
}

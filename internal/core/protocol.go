package core

import (
	"context"
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
	"sync/atomic"
	"time"

	"goim/internal/domain"
	"goim/internal/ipmsg"
	"goim/internal/neiwangtong"
)

const protocolVersion = 1
const maxFrame = 4 << 20
const maxPendingPeerConnections = 64

type envelope struct {
	DNSDirectory    bool         `json:",omitempty"`
	DNSMembers      []dnsMember  `json:",omitempty"`
	RelaySupported  bool         `json:",omitempty"`
	Relay           *relayPacket `json:",omitempty"`
	RecallSupported bool         `json:",omitempty"`
	PrintSupported  bool         `json:",omitempty"`
	Version         int
	ID              string
	Type            string
	Peer            domain.Peer
	Message         domain.Message
	Group           domain.Group
	Invitation      domain.Invitation
	File            domain.File
	Files           []domain.File
	MemberCatalog   *memberCatalog `json:",omitempty"`
	CatalogRevision uint64
	Printers        []domain.PrinterShare
	PrinterRevision uint64
	PrintJob        domain.PrintJob
	PrintJobID      string
	PrintStatus     string
	PrintAuth       printerAuthMessage `json:",omitempty"`
	PrintToken      string             `json:",omitempty"`
	TransferID      string
	ShareRequestID  string `json:",omitempty"`
	Offset          int64
	Status          string
	Error           string
	Target          string
	Proof           domain.Ownership
	Avatar          []byte
	GroupAvatar     []byte
	Peers           []domain.Peer
	Voice           domain.VoiceSignal
	CallID          string
	Audio           []byte
}
type peerConn struct {
	dnsRequestDirectory *dnsDirectory
	dnsCapable          bool
	dnsJoined           time.Time // Protected by Node.mu, valid only in dnsMembership.
	dnsMembership       *dnsDirectory
	dnsRequested        time.Time
	dnsReply            time.Time
	conn                *tls.Conn
	peer                domain.Peer
	write               sync.Mutex
	outbound            bool
	lastRead            atomic.Int64
	memberCatalogReady  bool // Guarded by Node.mu. Cleared for every new TLS connection.
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
	e.DNSDirectory = true
	e.RecallSupported = true
	e.PrintSupported = true
	e.RelaySupported = true
	return e
}
func (n *Node) advertisedAddress() string {
	_, port, _ := net.SplitHostPort(n.Address())
	return net.JoinHostPort("", port)
}
func (n *Node) establish(conn *tls.Conn, outbound bool, expected string) {
	n.establishWithResult(n.ctx, conn, outbound, expected, nil)
}

func (n *Node) establishWithResult(ctx context.Context, conn *tls.Conn, outbound bool, expected string, ready chan<- bool) {
	stop := watchConnection(n.ctx, conn)
	defer stop()
	notify := func(ok bool) {
		if ready != nil {
			ready <- ok
			ready = nil
		}
	}
	defer func() { notify(false) }()
	if !n.allowedConnection(conn.LocalAddr(), conn.RemoteAddr()) {
		conn.Close()
		return
	}
	deadline := time.Now().Add(8 * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = conn.SetDeadline(deadline)
	if err := conn.HandshakeContext(ctx); err != nil {
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
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 || ctx.Err() != nil {
		conn.Close()
		return
	}
	key, ok := certs[0].PublicKey.(ed25519.PublicKey)
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
	hello.Peer.PrintSupported = hello.PrintSupported
	hello.Peer.RelaySupported = hello.RelaySupported
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
	pc := &peerConn{conn: conn, peer: hello.Peer, outbound: outbound, dnsCapable: hello.DNSDirectory}
	pc.lastRead.Store(time.Now().UnixNano())
	n.mu.Lock()
	if outbound && n.dnsActiveLocked() {
		address := conn.RemoteAddr().String()
		if _, ok := n.dnsDirectory.seeds[address]; ok {
			n.dnsDirectory.seeds[address] = pc.peer.ID
		}
	}
	preferred := n.data.State.Self.ID < pc.peer.ID
	if old := n.connections[pc.peer.ID]; old != nil {
		if old.peer.Address == pc.peer.Address && (old.outbound == preferred || outbound != preferred) {
			n.mu.Unlock()
			conn.Close()
			notify(true)
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
	// Established messaging peers have their own lifetime and must not consume
	// the admission budget for handshakes and temporary file connections.
	delete(n.incoming, conn)
	n.mu.Unlock()
	_ = conn.SetDeadline(time.Time{})
	err = n.durable(func(data *persisted) []pending {
		s := &data.State
		i := peerIndex(s, pc.peer.ID)
		peer := pc.peer
		peer.Online = true
		peer.LastSeen = time.Now()
		if i < 0 {
			s.Peers = append(s.Peers, peer)
		} else {
			peer.ContactGroup = s.Peers[i].ContactGroup
			s.Peers[i] = peer
		}
		nativeID := nativeIDForGoIM(peer.ID)
		if i := peerIndex(s, keyIDFromNative(nativeID)); i >= 0 && s.Peers[i].Transport == transportNWT {
			s.Peers[i].CompatibilityAliasOf = peer.ID
			migrateContactPreferences(data, s.Peers[i].ID, peer.ID)
			s.Peers[i].Online = false
			delete(n.compatPeers, compatibilityEndpoint{transportNWT, s.Peers[i].ID})
		}
		data.restoreDeletedContact(peer.ID)
		return nil
	})
	defer func() {
		conn.Close()
		n.mu.Lock()
		current := n.connections[pc.peer.ID] == pc
		if current {
			delete(n.connections, pc.peer.ID)
			delete(n.relayRoutes, pc.peer.ID)
		}
		n.mu.Unlock()
		if current {
			n.change(func(s *domain.State) {
				if n.connections[pc.peer.ID] != nil {
					return
				}
				if i := peerIndex(s, pc.peer.ID); i >= 0 {
					s.Peers[i].Online = false
				}
				for i := range s.Transfers {
					t := &s.Transfers[i]
					if t.ShareProject == "" && t.PeerID == pc.peer.ID && t.Status == "transferring" {
						t.Status = "waiting"
						t.Speed = 0
					}
				}
			})
			n.voicePeerDisconnected(pc.peer.ID)
		}
	}()
	if err != nil {
		return
	}
	notify(true)
	n.async(func() {
		n.exchangePeerRoutes()
		n.replay(pc.peer.ID)
		n.sendCatalog(pc.peer.ID)
		if pc.peer.PrintSupported {
			n.sendPrinterCatalog(pc.peer.ID)
			n.sendPrintStatuses(pc.peer.ID)
		}
	})
	for {
		_ = conn.SetReadDeadline(time.Now().Add(25 * time.Second))
		e, err := readFrame(conn)
		if err != nil {
			return
		}
		pc.lastRead.Store(time.Now().UnixNano())
		if e.Type == "dns_join" || e.Type == "dns_members" || e.Type == "dns_leave" {
			n.handleDNSDirectory(pc, e)
			continue
		}
		if err = n.receiveFrom(pc.peer.ID, e, pc); err != nil {
			n.report(err)
		}
	}
}
func (n *Node) send(peer string, e envelope) error {
	n.mu.Lock()
	pc := n.connections[peer]
	n.mu.Unlock()
	if pc == nil {
		return n.sendRelayed(peer, e)
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
	n.restoreNeiwangtongFiles(peer)
	n.restoreIPMSGOffers(peer)
	n.flushPeer(peer, true)
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

type peerDelivery struct {
	running, queued, replay bool
	nativeSequence          uint64
}

func (n *Node) flushPeer(peer string, replay bool) {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return
	}
	if n.flushing == nil {
		n.flushing = make(map[string]*peerDelivery)
	}
	work := n.flushing[peer]
	if work == nil {
		work = &peerDelivery{}
		n.flushing[peer] = work
	}
	work.replay = work.replay || replay
	if work.running {
		work.queued = true
		n.mu.Unlock()
		return
	}
	work.running = true
	n.workers.Add(1)
	n.mu.Unlock()
	go func() {
		defer n.workers.Done()
		for {
			n.mu.Lock()
			if work.replay {
				work.nativeSequence = 0
			}
			work.queued, work.replay = false, false
			n.mu.Unlock()
			n.deliverPeerOutbox(peer, &work.nativeSequence)
			n.mu.Lock()
			if n.closing || !work.queued {
				work.running = false
				if work.nativeSequence == 0 || n.closing {
					delete(n.flushing, peer)
				}
				n.mu.Unlock()
				return
			}
			n.mu.Unlock()
		}
	}()
}

func (n *Node) deliverPeerOutbox(peer string, nativeSequence *uint64) {
	n.mu.Lock()
	var list []pending
	for _, p := range n.data.Outbox {
		if p.PeerID == peer {
			list = append(list, p)
		}
	}
	n.mu.Unlock()
	sort.Slice(list, func(i, j int) bool {
		if list[i].Sequence == list[j].Sequence {
			return list[i].Envelope.ID < list[j].Envelope.ID
		}
		return list[i].Sequence < list[j].Sequence
	})
	for _, p := range list {
		n.mu.Lock()
		current, exists := n.data.Outbox[p.Envelope.ID]
		permitted := exists && deliveryAllowed(&n.data, current)
		transport, wireID, online := n.pendingRouteLocked(current)
		n.mu.Unlock()
		if !exists {
			continue
		}
		if !permitted {
			_ = n.durable(func(data *persisted) []pending {
				if entry, ok := data.Outbox[p.Envelope.ID]; ok && !deliveryAllowed(data, entry) {
					delete(data.Outbox, p.Envelope.ID)
				}
				return nil
			})
			continue
		}
		p = current
		if transport == domain.TransportGoIM {
			// A new local message can arrive before the receiver commits and
			// acknowledges the previous one. Only an explicit replay needs to
			// resend the older TLS frames, including those sent via a relay.
			if p.Sequence != 0 && p.Sequence <= *nativeSequence {
				continue
			}
			if n.send(peer, p.Envelope) != nil {
				return
			}
			*nativeSequence = p.Sequence
			continue
		}
		if !online {
			if current.WireTransport != "" || p.Envelope.Type == "file_status" || p.Envelope.Type == "file_retry" {
				continue
			}
			return
		}
		first := current.WireTransport == ""
		if first {
			prepared := false
			err := n.durable(func(data *persisted) []pending {
				if entry, ok := data.Outbox[p.Envelope.ID]; ok && deliveryAllowed(data, entry) {
					entry.WireTransport, entry.WirePeerID = transport, wireID
					data.Outbox[p.Envelope.ID] = entry
					prepared = true
				}
				return nil
			})
			if err != nil || !prepared {
				return
			}
		}
		var err error
		if transport == transportNWT {
			err = n.neiwangtongPending(wireID, p)
		} else {
			err = n.legacyPending(wireID, p)
		}
		if errors.Is(err, neiwangtong.ErrNotConnected) {
			if first {
				_ = n.durable(func(data *persisted) []pending {
					if entry, ok := data.Outbox[p.Envelope.ID]; ok {
						entry.WireTransport, entry.WirePeerID = "", ""
						data.Outbox[p.Envelope.ID] = entry
					}
					return nil
				})
			}
			return
		}
		if err != nil {
			if transport == legacyIPMSG && p.Envelope.Type == "file_offer" && errors.Is(err, ipmsg.ErrReceiptTimeout) {
				// Some clients confirm only by downloading. Keep the invitation
				// queued without holding later messages until the user accepts.
				continue
			}
			if n.ctx.Err() != nil || transport == transportNWT && errors.Is(err, neiwangtong.ErrDeliveryUncertain) ||
				transport == legacyIPMSG && (errors.Is(err, ipmsg.ErrReceiptTimeout) || errors.Is(err, ipmsg.ErrPeerOffline)) {
				return // preserve order and the original identifiers on replay
			}
			n.failCompatibilityDelivery(p, err)
			continue
		}
		if n.legacyCompleted([]pending{p}) != nil {
			return
		}
	}
}

func (n *Node) pendingRouteLocked(p pending) (transport, wireID string, online bool) {
	peer := n.data.State.Peer(p.PeerID)
	transport, wireID = p.WireTransport, p.WirePeerID
	if transport == "" {
		transport = peer.Transport
		if peer.IsLegacyNeiwangtong() {
			transport = transportNWT
		}
		if i := transferIndex(&n.data.State, p.Envelope.TransferID); i >= 0 {
			t := n.data.State.Transfers[i]
			if t.LegacyTransport != "" && t.RemotePacket != "" {
				transport, wireID = t.LegacyTransport, transferWireID(&n.data, t)
			} else if id, ok := strings.CutPrefix(t.ConversationID, "group:"); ok && n.data.NativeGroups[id].WireID != "" {
				transport = transportNWT
			}
		}
		if id, ok := strings.CutPrefix(p.Envelope.Message.ConversationID, "group:"); ok && n.data.NativeGroups[id].WireID != "" {
			transport = transportNWT
		}
		if p.Envelope.Type == nativeGroupRosterDelivery || p.Envelope.Type == nativeGroupPhotoDelivery {
			transport = transportNWT
		}
	}
	if transport == domain.TransportGoIM {
		return transport, p.PeerID, n.connections[p.PeerID] != nil
	}
	if wireID == "" {
		wireID = wireCompatibilityID(&n.data, p.PeerID, transport)
	}
	online = n.compatPeers[compatibilityEndpoint{transport, wireID}].Online
	return
}

func (n *Node) failCompatibilityDelivery(p pending, failure error) {
	_ = n.durable(func(data *persisted) []pending {
		if _, exists := data.Outbox[p.Envelope.ID]; !exists {
			return nil
		}
		delete(data.Outbox, p.Envelope.ID)
		for i := range data.State.Messages {
			if data.State.Messages[i].ID == p.Envelope.Message.ID {
				data.State.Messages[i].Status = "failed"
				break
			}
		}
		if i := transferIndex(&data.State, p.Envelope.TransferID); i >= 0 {
			t := &data.State.Transfers[i]
			if !terminalTransfer(t.Status) {
				t.Status, t.Error = "failed", failure.Error()
			}
		}
		data.State.Error = failure.Error()
		return nil
	})
}
func (n *Node) receive(peer string, e envelope) error {
	return n.receiveFrom(peer, e, nil)
}

func (n *Node) receiveFrom(peer string, e envelope, source *peerConn) error {
	if e.Type == "peer_routes" {
		return n.receivePeerRoutes(peer, e.Peers)
	}
	if e.Type == "relay" {
		return n.receiveRelayed(peer, e.Relay)
	}
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
		accepted := false
		err = n.durable(func(data *persisted) []pending {
			if source != nil && n.connections[peer] != source || e.CatalogRevision < data.CatalogRevisions[peer] {
				return nil
			}
			accepted = true
			data.CatalogRevisions[peer] = e.CatalogRevision
			data.updateMemberProjects(peer, e.MemberCatalog)
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
			for i := range s.Transfers {
				t := &s.Transfers[i]
				if t.ShareRequest && t.Status == "requesting" && t.PeerID == peer && !s.File(t.FileID).Shared {
					updateTransferProgress(t, "failed", 0, 0, "分享已失效")
				}
			}
			return nil
		})
		// Catalog data must commit before the first URL becomes actionable. The
		// UI caches rows by revision, so publish the new readiness with a revision too.
		if err == nil && accepted && source != nil && e.MemberCatalog != nil {
			n.mu.Lock()
			if n.connections[peer] == source && !source.memberCatalogReady {
				source.memberCatalogReady = true
				n.data.State.Revision++
				n.signal()
			}
			n.mu.Unlock()
		}
	case "catalog_request":
		n.sendCatalog(peer)
		if n.snapshot(false).Peer(peer).PrintSupported {
			n.sendPrinterCatalog(peer)
			n.sendPrintStatuses(peer)
		}
	case "printer_catalog":
		err = n.receivePrinterCatalog(peer, e)
	case "print_status":
		err = n.receivePrintStatus(peer, e)
	case "printer_auth":
		err = n.receivePrinterAuth(peer, e)
	case "printer_auth_result":
		n.receivePrinterAuthResult(peer, e)
	case "get_shared":
		err = n.offerShared(peer, e.File.ID, e.TransferID)
	case "voice_signal":
		err = n.receiveVoiceSignal(peer, e.Voice)
	case "voice_audio":
		n.receiveVoiceAudio(peer, e)
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
	// Text envelopes cannot attach authority over file or print records.
	m.FileID, m.PrintJobID = "", ""
	if m.ID == "" || m.SenderID != peer || m.Kind != "text" || len(m.Text) > 256<<10 {
		return errors.New("无效聊天消息")
	}
	m.Status = "delivered"
	var denied error
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		if !conversationAllows(s, m.ConversationID, peer) {
			denied = errors.New("群成员身份已失效")
			return nil
		}
		direct := !strings.HasPrefix(m.ConversationID, "group:")
		if direct {
			m.ConversationID = directID(s.Self.ID, peer)
		}
		if p.MessageRecalls[messageRecallKey(peer, m.ConversationID, m.ID)] {
			return nil
		}
		for _, old := range s.Messages {
			if old.ID == m.ID {
				return nil
			}
		}
		if direct {
			ensureDirect(s, s.Peer(peer))
		}
		addMessageState(s, m)
		return nil
	})
	return errors.Join(err, denied)
}

type announcement struct {
	Version   int
	Peer      domain.Peer
	Signature []byte
}

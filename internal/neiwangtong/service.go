package neiwangtong

import (
	"context"
	"crypto/md5" // native avatar version tag, not an authentication check
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Peer struct {
	ID, Name, Group, Sign, Address, Version           string
	Gender, Company, Department, Phone, Mobile, Email string
	Status                                            uint32
	Online, GoIM                                      bool
}

type Callbacks struct {
	Peer func(Peer)
	Text func(peerID string, packetID uint64, text string)
	// Message commits received chat content before the application receipt.
	Message        func(peerID string, message DirectMessage) error
	GroupRoster    func(peerID string, roster GroupRoster)
	GroupDeparture func(peerID, groupID string, kind uint32)
	GroupText      func(peerID string, message GroupMessage) error
	// TextAcknowledged reports the application's receipt from an established peer.
	// An ENet acknowledgement alone only confirms transport delivery.
	TextAcknowledged func(peerID string, packetID uint64)
	Files            func(peerID string, offer XMLSendFile)
	FileCancelled    func(peerID string, taskID uint64) // the sender withdrew an incoming offer
	FileProgress     func(peerID string, taskID uint64, done, total int64, complete bool, err error)
	Folders          func(peerID string, offer FolderOffer)
	FolderCancelled  func(peerID, taskID string)
	FolderProgress   func(peerID, taskID string, done, total int64, complete bool, err error)
	AvatarUpdated    func(peerID string, revision uint64, png []byte)
}

type receiptKey struct {
	peer   string
	packet uint64
}

type Config struct {
	ID                [16]byte
	Name              string
	Group             string
	Sign              string
	Profile           Peer
	GoIM              bool
	DiscoveryPort     int
	ControlPort       int
	RemoteControlPort int
	TransferPort      int
	EnableFolders     bool
	BindIP            netip.Addr
	AvatarPNG         []byte
	DisableBroadcast  bool
	AllowRemote       func(netip.AddrPort) bool
	CanServeGroupFile func(peerID, groupID string) bool
	Interface         func() string
	Trace             func(string) // optional developer diagnostics, no message bodies
	TraceEvent        func(kind uint32, payload []byte)
	Callbacks         Callbacks
}

type nativePeer struct {
	peer           Peer
	control        netip.AddrPort
	discovery      netip.AddrPort
	tcp            netip.AddrPort
	reverseTCP     netip.AddrPort
	folderTCP      netip.AddrPort
	reverse        map[uint64]*reverseChannel
	ready          chan struct{}
	avatar         wireAvatar
	avatarRevision uint64
	avatarCached   uint64
	avatarFetching bool
}

type Service struct {
	receipts         map[receiptKey]chan struct{} // protected by mu
	groupFiles       map[groupFileKey]groupFileOffer
	groupFileReplies map[receiptKey]chan groupFileResponse
	groupFileWorkers chan struct{}
	cfg              Config
	ctx              context.Context
	cancel           context.CancelFunc
	enet             *ENetTransport
	profileENet      *ENetTransport
	transfer         *TransferServer
	directories      *directoryTransport
	selfID           string
	messageSeq       atomic.Uint32

	discMu    sync.Mutex
	discConn  *net.UDPConn
	closeOnce sync.Once

	mu          sync.Mutex
	broadcast   bool
	local       wireProfile
	selfDisc    Discovery
	peers       map[string]*nativePeer
	byControl   map[netip.AddrPort]string
	verified    map[netip.AddrPort]string
	handshakes  map[netip.AddrPort]bool
	profileSent map[netip.AddrPort]*enetSession
	files       map[uint64]fileOffer
}

type reverseChannel struct{ cancel context.CancelFunc }

type fileOffer struct {
	peerID   string
	fileID   uint64
	size     int64
	accepted bool
}

func Start(cfg Config) (*Service, error) {
	if cfg.DiscoveryPort == 0 {
		cfg.DiscoveryPort = int(DiscoveryPort)
	}
	if cfg.ControlPort == 0 {
		cfg.ControlPort = int(ControlPort)
	}
	if cfg.RemoteControlPort == 0 {
		cfg.RemoteControlPort = cfg.ControlPort
	}
	if cfg.TransferPort == 0 {
		cfg.TransferPort = int(AvatarPort)
	}
	for _, port := range []int{cfg.DiscoveryPort, cfg.ControlPort, cfg.RemoteControlPort, cfg.TransferPort} {
		if port < 1 || port > 65535 {
			return nil, errors.New("invalid native service port")
		}
	}
	if cfg.BindIP.IsValid() && !cfg.BindIP.Is4() {
		return nil, errors.New("native service requires IPv4")
	}
	if cfg.ID == [16]byte{} {
		_, _ = rand.Read(cfg.ID[:])
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{cfg: cfg, ctx: ctx, cancel: cancel, selfID: keyForDiscovery(cfg.ID),
		peers: make(map[string]*nativePeer), byControl: make(map[netip.AddrPort]string),
		verified:   make(map[netip.AddrPort]string),
		handshakes: make(map[netip.AddrPort]bool), profileSent: make(map[netip.AddrPort]*enetSession),
		files:      make(map[uint64]fileOffer),
		groupFiles: make(map[groupFileKey]groupFileOffer), groupFileReplies: make(map[receiptKey]chan groupFileResponse),
		groupFileWorkers: make(chan struct{}, 16),
		broadcast:        !cfg.DisableBroadcast}
	s.selfDisc = Discovery{Kind: Announce, ID: cfg.ID, Broadcast: netip.AddrFrom4([4]byte{255, 255, 255, 255}), Trailer: uint16(cfg.ControlPort)}
	interfaceName := ""
	if cfg.Interface != nil {
		interfaceName = cfg.Interface()
	}
	if targets := broadcastTargets(cfg.BindIP, interfaceName); len(targets) > 0 {
		s.selfDisc.Broadcast = targets[0]
	}
	_, _ = rand.Read(s.selfDisc.ProfileTag[:])
	profile := cfg.Profile
	if profile.Name == "" {
		profile.Name, profile.Group, profile.Sign = cfg.Name, cfg.Group, cfg.Sign
	}
	s.local = localWireProfile(profile, cfg.AvatarPNG)
	var err error
	s.transfer, err = startTransferServer(cfg.BindIP, cfg.TransferPort, s.onFileStream, s.avatarAllowed)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("内网通 TCP 文件端口: %w", err)
	}
	s.transfer.SetAvatar(cfg.AvatarPNG)
	if cfg.EnableFolders {
		s.directories, err = startDirectories(cfg.BindIP)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("内网通 TCP 文件夹端口: %w", err)
		}
	}
	s.enet, err = newENetTransport(cfg.BindIP, cfg.ControlPort, s.onENetCommand, s.onLost)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("内网通 UDP 控制端口: %w", err)
	}
	s.enet.trace = cfg.Trace
	s.profileENet, err = newENetTransport(cfg.BindIP, 0, s.onProfileControl, nil)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("内网通 UDP 资料端口: %w", err)
	}
	s.profileENet.trace = cfg.Trace
	s.selfDisc.Trailer = uint16(s.profileENet.localAddr.Port)
	s.discConn, err = s.PrepareDiscovery(cfg.DiscoveryPort)
	if err != nil {
		s.Close()
		return nil, err
	}
	go s.enet.readLoop()
	go s.profileENet.readLoop()
	go s.readDiscovery(s.discConn)
	go s.maintain()
	return s, nil
}

func keyForDiscovery(id [16]byte) string { return hex.EncodeToString(id[:]) }

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.discMu.Lock()
		if s.discConn != nil {
			_ = s.discConn.Close()
			s.discConn = nil
		}
		s.discMu.Unlock()
		if s.enet != nil {
			_ = s.enet.Close()
		}
		if s.profileENet != nil {
			_ = s.profileENet.Close()
		}
		if s.transfer != nil {
			_ = s.transfer.Close()
		}
		if s.directories != nil {
			_ = s.directories.listener.Close()
		}
	})
	return nil
}

// PrepareDiscovery/AdoptDiscovery let the owner save the new port before it
// swaps discovery sockets. Existing ENet sessions and TCP transfers stay live.
func (s *Service) PrepareDiscovery(port int) (*net.UDPConn, error) {
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid native discovery port")
	}
	bind := s.cfg.BindIP
	if !bind.IsValid() {
		bind = netip.IPv4Unspecified()
	}
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(bind, uint16(port))))
	if err != nil {
		return nil, fmt.Errorf("内网通 UDP 发现端口 %d: %w", port, err)
	}
	return conn, nil
}

func (s *Service) AdoptDiscovery(conn *net.UDPConn) {
	s.discMu.Lock()
	old := s.discConn
	if s.ctx.Err() != nil {
		s.discMu.Unlock()
		_ = conn.Close()
		return
	}
	s.discConn = conn
	s.discMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go s.readDiscovery(conn)
	go s.broadcastAnnounce()
}

func (s *Service) SetBroadcastEnabled(enabled bool) {
	s.mu.Lock()
	was := s.broadcast
	s.broadcast = enabled
	s.mu.Unlock()
	if enabled && !was {
		s.broadcastAnnounce()
	}
}

func (s *Service) allows(remote netip.AddrPort) bool {
	return validUnicast(remote) && (s.cfg.AllowRemote == nil || s.cfg.AllowRemote(remote))
}

func (s *Service) readDiscovery(conn *net.UDPConn) {
	var buf [discoverySize + 1]byte
	for {
		n, remote, err := conn.ReadFromUDPAddrPort(buf[:])
		if err != nil {
			return
		}
		if !s.allows(remote) {
			continue
		}
		d, err := ParseDiscovery(buf[:n])
		if err != nil || d.ID == s.cfg.ID || d.Kind == ControlProfile {
			continue
		}
		s.mu.Lock()
		hidden := s.local.Status == StatusInvisible
		announce := s.selfDisc
		s.mu.Unlock()
		if hidden {
			continue
		}
		if d.Kind == Announce {
			announce.Kind = Reply
			// The record carries the sender's own interface broadcast address.
			// Native 3.4 does not echo the requester's address.
			if packet, err := announce.MarshalBinary(); err == nil {
				_, _ = conn.WriteToUDPAddrPort(packet, remote)
			}
		}
		target := netip.AddrPortFrom(remote.Addr(), uint16(s.cfg.RemoteControlPort))
		id, ok := s.recordDiscovery(d, remote, target, AvatarPort)
		if ok {
			profileTarget := netip.AddrPortFrom(remote.Addr(), profileTrailerPort(d.Trailer, s.cfg.RemoteControlPort))
			s.kickHandshake(id, profileTarget)
		}
	}
}

func profileTrailerPort(trailer uint16, fallback int) uint16 {
	if trailer != 0 {
		return trailer
	}
	return uint16(fallback)
}

func (s *Service) recordDiscovery(d Discovery, discovery, control netip.AddrPort, tcpPort uint16) (string, bool) {
	id := keyForDiscovery(d.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.peers[id]
	if p == nil {
		if len(s.peers) >= maxSessions {
			return "", false
		}
		p = &nativePeer{peer: Peer{ID: id}, ready: make(chan struct{})}
		s.peers[id] = p
	}
	if p.peer.Online && p.control.Addr() != control.Addr() {
		return "", false
	}
	if other := s.byControl[control]; other != "" && other != id {
		if prior := s.peers[other]; prior != nil && prior.peer.Online {
			return "", false
		}
	}
	s.byControl[control] = id
	if discovery.IsValid() {
		p.discovery = discovery
	}
	if !p.peer.Online {
		p.control = control
		p.tcp = netip.AddrPortFrom(control.Addr(), tcpPort)
	}
	return id, true
}

func (s *Service) broadcastAnnounce() {
	s.mu.Lock()
	if !s.broadcast || s.local.Status == StatusInvisible {
		s.mu.Unlock()
		return
	}
	announce := s.selfDisc
	s.mu.Unlock()
	s.discMu.Lock()
	conn := s.discConn
	s.discMu.Unlock()
	if conn == nil {
		return
	}
	port := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	name := s.interfaceName()
	for _, addr := range broadcastTargets(s.cfg.BindIP, name) {
		announce.Broadcast = addr
		if packet, err := announce.MarshalBinary(); err == nil {
			_, _ = conn.WriteToUDPAddrPort(packet, netip.AddrPortFrom(addr, port))
		}
	}
}

func (s *Service) interfaceName() string {
	if s.cfg.Interface == nil {
		return ""
	}
	return s.cfg.Interface()
}

func broadcastTargets(bind netip.Addr, name string) []netip.Addr {
	var targets []netip.Addr
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagBroadcast == 0 || name != "" && iface.Name != name {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, raw := range addresses {
			prefix, err := netip.ParsePrefix(raw.String())
			if err != nil || !prefix.Addr().Is4() || prefix.Bits() >= 31 ||
				bind.IsValid() && bind != prefix.Addr() {
				continue
			}
			bytes := prefix.Addr().As4()
			for i := prefix.Bits(); i < 32; i++ {
				bytes[i/8] |= 1 << uint(7-i%8)
			}
			addr := netip.AddrFrom4(bytes)
			if !slices.Contains(targets, addr) {
				targets = append(targets, addr)
			}
		}
	}
	if len(targets) == 0 && name == "" && !bind.IsValid() {
		targets = append(targets, netip.AddrFrom4([4]byte{255, 255, 255, 255}))
	}
	return targets
}

func (s *Service) maintain() {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	s.broadcastAnnounce()
	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-tick.C:
			s.enet.expire(now)
			s.profileENet.expire(now)
			s.broadcastAnnounce()
			s.mu.Lock()
			var targets []netip.AddrPort
			for _, p := range s.peers {
				if p.peer.Online {
					targets = append(targets, p.control)
				}
			}
			s.mu.Unlock()
			for _, target := range targets {
				go func() { _ = s.enet.Ping(target) }()
			}
		}
	}
}

func (s *Service) kickHandshake(id string, target netip.AddrPort) {
	s.mu.Lock()
	p := s.peers[id]
	// The advertised profile port is temporary. Once the main control
	// session is online, another discovery reply must not open a second one.
	if s.ctx.Err() != nil || s.local.Status == StatusInvisible || s.handshakes[target] ||
		p == nil || p.peer.Online {
		s.mu.Unlock()
		return
	}
	s.handshakes[target] = true
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); delete(s.handshakes, target); s.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		defer cancel()
		if err := s.profileENet.Connect(ctx, target); err != nil {
			if s.cfg.Trace != nil {
				s.cfg.Trace(fmt.Sprintf("profile bootstrap connect %s: %v", target, err))
			}
			return
		}
		packet, err := s.profileControlPacket(netip.Addr{})
		if err == nil {
			err = s.profileENet.SendAppCommand(target, uint32(ControlProfile), packet)
		}
		if s.cfg.Trace != nil && err != nil {
			s.cfg.Trace(fmt.Sprintf("profile bootstrap response %s: %v", target, err))
		}
	}()
}

func (s *Service) profileControlPacket(broadcast netip.Addr) ([]byte, error) {
	s.mu.Lock()
	record, profile := s.selfDisc, s.local
	s.mu.Unlock()
	if profile.Status == StatusInvisible {
		return nil, ErrNotConnected
	}
	record.Kind = ControlProfile
	if broadcast.Is4() {
		record.Broadcast = broadcast
	}
	return record.MarshalBinary()
}

func (s *Service) sendInitialProfile(target netip.AddrPort, includeDiscovery bool) error {
	session := s.enet.activeSession(target)
	if session == nil {
		return ErrNotConnected
	}
	s.mu.Lock()
	profile := s.local
	if profile.Status == StatusInvisible {
		s.mu.Unlock()
		return ErrNotConnected
	}
	if s.profileSent[target] == session {
		s.mu.Unlock()
		return nil
	}
	// Both the main and temporary ports can see discovery retries for the
	// same contact. Replaying the full handshake on an established main
	// connection causes the original client to disconnect it.
	s.profileSent[target] = session
	s.mu.Unlock()
	if includeDiscovery {
		packet, err := s.profileControlPacket(netip.Addr{})
		if err != nil {
			return err
		}
		if err := s.enet.SendAppCommand(target, uint32(ControlProfile), packet); err != nil {
			return err
		}
	}
	var folderPort uint16
	if s.directories != nil {
		folderPort = s.directories.port()
	}
	payload := formatProfileWithFolder(profile, uint16(s.transfer.Port()), uint16(s.transfer.ReversePort()), 0, folderPort, s.cfg.GoIM)
	return s.enet.SendAppCommand(target, CmdHandshark, payload)
}

func (s *Service) onProfileControl(remote netip.AddrPort, kind uint32, plain []byte) {
	if !s.allows(remote) || kind != uint32(ControlProfile) {
		return
	}
	d, err := ParseDiscovery(plain)
	if err != nil || d.ID == s.cfg.ID {
		return
	}
	// The advertised temporary port serves profile requests. It does not
	// authorize chat or file operations on a different ENet connection.
	go func() {
		// Native 3.4 replies to a temporary-port profile bootstrap from its
		// fixed control port and sends the full profile on that same main
		// connection. Replying on profileENet keeps this direction working
		// only for the side that initiated it and leaves the native peer
		// unable to complete the reverse contact bootstrap.
		target := netip.AddrPortFrom(remote.Addr(), uint16(s.cfg.RemoteControlPort))
		if _, ok := s.recordDiscovery(d, netip.AddrPort{}, target, AvatarPort); !ok {
			if s.cfg.Trace != nil {
				s.cfg.Trace(fmt.Sprintf("profile bootstrap identity conflict for %s", target))
			}
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		defer cancel()
		if err := s.enet.Connect(ctx, target); err != nil {
			if s.cfg.Trace != nil {
				s.cfg.Trace(fmt.Sprintf("profile bootstrap connect %s: %v", target, err))
			}
			return
		}
		err := s.sendInitialProfile(target, true)
		if s.cfg.Trace != nil {
			s.cfg.Trace(fmt.Sprintf("profile response %s: %v", remote, err))
		}
	}()
}

func (s *Service) onENetCommand(remote netip.AddrPort, kind uint32, plain []byte) {
	if !s.allows(remote) {
		return
	}
	if s.cfg.TraceEvent != nil {
		s.cfg.TraceEvent(kind, append([]byte(nil), plain...))
	}
	if s.cfg.Trace != nil {
		s.cfg.Trace(fmt.Sprintf("received native event %d (%d bytes)", kind, len(plain)))
	}
	if kind == uint32(ControlProfile) {
		d, err := ParseDiscovery(plain)
		if err != nil || d.ID == s.cfg.ID {
			return
		}
		control := remote
		if remote.Port() != uint16(s.cfg.RemoteControlPort) {
			control = netip.AddrPortFrom(remote.Addr(), uint16(s.cfg.RemoteControlPort))
			if s.cfg.Trace != nil {
				s.cfg.Trace(fmt.Sprintf("normalizing profile control %s to %s", remote, control))
			}
			if !s.enet.connected(control) {
				go func() {
					ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
					defer cancel()
					if err := s.enet.Connect(ctx, control); err != nil {
						if s.cfg.Trace != nil {
							s.cfg.Trace(fmt.Sprintf("profile main-control connect %s: %v", control, err))
						}
						return
					}
					if _, ok := s.recordDiscovery(d, netip.AddrPort{}, control, AvatarPort); ok {
						_ = s.sendInitialProfile(control, false)
					}
				}()
				return
			}
		}
		_, ok := s.recordDiscovery(d, netip.AddrPort{}, control, AvatarPort)
		if ok {
			go func() { _ = s.sendInitialProfile(control, false) }()
		}
		return
	}
	s.mu.Lock()
	id := s.byControl[remote]
	p := s.peers[id]
	ready := p != nil && p.peer.Online && s.verified[remote] == id
	s.mu.Unlock()
	if id == "" {
		return
	} // no IP-only attribution or arbitrary peer fallback
	if kind == CmdHandshark {
		hs, err := ParseHandshark(plain)
		if err != nil || !validWireProfile(hs.Info) {
			if s.cfg.Trace != nil {
				s.cfg.Trace(fmt.Sprintf("received profile rejected: parse=%v, has name=%t", err, hs.Info.Name != ""))
			}
			return
		}
		s.receiveProfile(id, remote, hs.Info, &hs)
		return
	}
	if !ready {
		return
	}
	switch kind {
	case CmdChangeInfo:
		var info profileChange
		if decodeNativeXML(plain, "X_CHANGE_INFO", &info) == nil && validWireProfile(info.wireProfile) {
			s.receiveProfile(id, remote, info.wireProfile, nil)
		}
	case CmdChangeStatus:
		if status, err := ParseChangeStatus(plain); err == nil && status <= StatusAway {
			s.mu.Lock()
			if p.peer.Status == status {
				s.mu.Unlock()
				return
			}
			p.peer.Status = status
			value := p.peer
			s.mu.Unlock()
			s.emitPeer(value)
		}
	case CmdHeartbeat:
		return
	case CmdQuit:
		var quit struct{}
		if decodeNativeXML(plain, "X_QUIT", &quit) == nil {
			s.onLost(remote)
		}
	case CmdSendMsg:
		message, err := ParseDirectMessage(plain)
		if err == nil && message.PacketID != 0 && (len(message.Parts) != 0 || message.RecallID != "") {
			if notify := s.cfg.Callbacks.Message; notify != nil {
				if err := notify(id, message); err != nil {
					return
				}
			}
			if text := message.Text(); text != "" && s.cfg.Callbacks.Text != nil {
				s.cfg.Callbacks.Text(id, message.PacketID, text)
			}
			// Do not wait for ENet reliability inside the receive callback:
			// the peer cannot acknowledge a new frame until this callback has
			// returned and the incoming packet itself has been acknowledged.
			go s.ackChatMessage(remote, message.PacketID)
		}
	case CmdGroupRoster:
		if roster, err := ParseGroupRoster(plain); err == nil && s.cfg.Callbacks.GroupRoster != nil {
			s.cfg.Callbacks.GroupRoster(id, roster)
		}
	case CmdGroupExit, CmdGroupDismiss, CmdGroupKick:
		if group, err := parseGroupDeparture(plain, kind); err == nil && s.cfg.Callbacks.GroupDeparture != nil {
			s.cfg.Callbacks.GroupDeparture(id, group, kind)
		}
	case cmdGroupFileRequest:
		s.onGroupFileRequest(remote, id, plain)
	case cmdGroupFileResponse:
		s.onGroupFileResponse(id, plain)
	case CmdGroupText:
		if message, err := ParseGroupMessage(plain); err == nil && s.cfg.Callbacks.GroupText != nil {
			if err := s.cfg.Callbacks.GroupText(id, message); err == nil {
				go s.ackChatMessage(remote, message.Message.PacketID)
			}
		}
	case CmdSendMsgAck:
		if packetID, err := ParseSendMsgAck(plain); err == nil && packetID != 0 {
			key := receiptKey{id, packetID}
			s.mu.Lock()
			if done := s.receipts[key]; done != nil {
				delete(s.receipts, key)
				close(done)
			}
			s.mu.Unlock()
			if notify := s.cfg.Callbacks.TextAcknowledged; notify != nil {
				notify(id, packetID)
			}
		}
	case CmdSendFile:
		offer, err := ParseSendFile(plain)
		if err == nil && offer.TaskID != 0 &&
			offer.FileID != 0 && offer.FileID <= maxFileSelector && offer.Pwd == "" && offer.Size >= 0 &&
			s.cfg.Callbacks.Files != nil {
			s.cfg.Callbacks.Files(id, offer)
		}
	case CmdSendFolder:
		if offer, err := ParseFolderOffer(plain); err == nil && s.directories != nil && s.cfg.Callbacks.Folders != nil {
			s.cfg.Callbacks.Folders(id, offer)
		}
	case CmdOperateSendFolder, CmdOperateRecvFolder:
		s.onDirectoryOperation(id, kind, plain)
	case CmdFileReverseReq:
		if selector, err := ParseReverseFileReq(plain); err == nil {
			s.startReverseChannel(id, selector)
		}
	case CmdOperateSendFile:
		taskID, op, err := ParseOperateSendFile(plain)
		if err == nil && taskID != 0 && op == fileSendCancel && s.cfg.Callbacks.FileCancelled != nil {
			s.cfg.Callbacks.FileCancelled(id, taskID)
		}
	case CmdOperateRecvFile:
		taskID, op, err := ParseOperateRecvFile(plain)
		if err == nil {
			s.receiveFileOperation(id, taskID, op)
		}
	case CmdProgressRecvFile:
		p, err := ParseProgressRecvFile(plain)
		if err == nil {
			s.mu.Lock()
			offer, ok := s.files[p.TaskID]
			s.mu.Unlock()
			if ok && offer.peerID == id && p.TotalSize == offer.size && p.RecvdSize >= 0 && p.RecvdSize <= offer.size {
				s.reportFile(id, p.TaskID, p.RecvdSize, p.TotalSize, p.RecvdSize == p.TotalSize, nil)
			}
		}
	}
}

func validWireProfile(p wireProfile) bool {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 || !utf8.ValidString(p.Name) || p.Status > StatusAway {
		return false
	}
	for _, field := range []string{p.Group, p.Sign, p.Company, p.Department, p.Phone, p.Mobile, p.Email, p.UserFace.Hash} {
		if len(field) > 512 || !utf8.ValidString(field) {
			return false
		}
	}
	return true
}

func (s *Service) receiveProfile(id string, remote netip.AddrPort, info wireProfile, hs *XMLHandshark) {
	s.mu.Lock()
	p := s.peers[id]
	if p == nil || s.byControl[remote] != id || s.ctx.Err() != nil || s.local.Status == StatusInvisible ||
		(p.peer.Online && p.control != remote && hs == nil) {
		s.mu.Unlock()
		return
	}
	first := !p.peer.Online
	if first {
		p.control = remote
		p.peer.Address = remote.String()
	}
	if hs != nil {
		s.verified[remote] = id
		port := hs.Net.FileTranTCPPort
		if port == bits.ReverseBytes16(AvatarPort) {
			port = AvatarPort // native 3.4.3055 serializes the stock port in network byte order
		}
		if port == 0 {
			port = AvatarPort
		}
		if first || p.control == remote {
			p.tcp = netip.AddrPortFrom(remote.Addr(), port)
			folderPort := bits.ReverseBytes16(hs.Net.FolderTranTCPPort)
			p.folderTCP = netip.AddrPort{}
			if folderPort != 0 {
				p.folderTCP = netip.AddrPortFrom(remote.Addr(), folderPort)
			}
			reversePort := bits.ReverseBytes16(hs.Net.FileTranTCPReversePort)
			if reversePort != 0 {
				p.reverseTCP = netip.AddrPortFrom(remote.Addr(), reversePort)
			}
		}
		p.peer.GoIM = hs.Features.GoIM
		if hs.Features.Major != 0 {
			p.peer.Version = fmt.Sprintf("%d.%d.%d", hs.Features.Major, hs.Features.Minor, hs.Features.Build)
		}
	}
	p.peer.Name, p.peer.Group, p.peer.Sign = info.Name, info.Group, info.Sign
	p.peer.Company, p.peer.Department, p.peer.Phone, p.peer.Mobile, p.peer.Email = info.Company, info.Department, info.Phone, info.Mobile, info.Email
	p.peer.Gender = ""
	if info.Sex == 1 {
		p.peer.Gender = "male"
	} else if info.Sex == 2 {
		p.peer.Gender = "female"
	}
	p.peer.Status, p.peer.Online = info.Status, true
	if p.avatarRevision == 0 || p.avatar != info.UserFace {
		p.avatar = info.UserFace
		p.avatarRevision++
	}
	revision, avatar, value := p.avatarRevision, p.avatar, p.peer
	s.mu.Unlock()
	s.emitPeer(value)
	if first {
		s.mu.Lock()
		if p.peer.Online {
			select {
			case <-p.ready:
			default:
				close(p.ready)
			}
		}
		s.mu.Unlock()
		go s.sendReadySequence(remote)
	}
	if avatar.ID == 0 && avatar.Hash == "" {
		if s.cfg.Callbacks.AvatarUpdated != nil {
			s.cfg.Callbacks.AvatarUpdated(id, revision, nil)
		}
	} else {
		s.fetchAvatar(id, false)
	}
}

func (s *Service) sendReadySequence(remote netip.AddrPort) {
	_ = s.enet.SendAppCommand(remote, CmdReady, formatReady(s.selfID))
}

func (s *Service) emitPeer(p Peer) {
	if s.cfg.Callbacks.Peer != nil {
		s.cfg.Callbacks.Peer(p)
	}
}

func (s *Service) onLost(remote netip.AddrPort) {
	s.mu.Lock()
	id := s.byControl[remote]
	delete(s.byControl, remote)
	delete(s.verified, remote)
	delete(s.profileSent, remote)
	p := s.peers[id]
	if p == nil || !p.peer.Online || p.control != remote {
		s.mu.Unlock()
		return
	}
	p.peer.Online = false
	stopReverseChannels(p)
	p.ready = make(chan struct{})
	value := p.peer
	s.mu.Unlock()
	s.emitPeer(value)
}

func (s *Service) Probe(ctx context.Context, target netip.AddrPort, controlPort, transferPort int) error {
	_, err := s.ProbePeer(ctx, target, controlPort, transferPort)
	return err
}

func (s *Service) ProbePeer(ctx context.Context, target netip.AddrPort, controlPort, transferPort int) (Peer, error) {
	if !s.allows(target) {
		return Peer{}, errors.New("内网通目标不在当前网络接口")
	}
	s.mu.Lock()
	announce, hidden := s.selfDisc, s.local.Status == StatusInvisible
	s.mu.Unlock()
	if hidden {
		return Peer{}, errors.New("隐身状态下不可探测内网通")
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	reply, err := Probe(ctx, s.cfg.BindIP, target, announce)
	if err != nil {
		return Peer{}, fmt.Errorf("内网通发现 %s: %w", target, err)
	}
	if controlPort == 0 {
		controlPort = int(ControlPort)
	}
	if transferPort == 0 {
		transferPort = int(AvatarPort)
	}
	control := netip.AddrPortFrom(target.Addr(), uint16(controlPort))
	id, ok := s.recordDiscovery(reply, target, control, uint16(transferPort))
	if !ok {
		return Peer{}, errors.New("内网通控制地址和已知设备冲突")
	}
	s.mu.Lock()
	p := s.peers[id]
	wait := p.ready
	s.mu.Unlock()
	profileTarget := netip.AddrPortFrom(target.Addr(), profileTrailerPort(reply.Trailer, s.cfg.RemoteControlPort))
	s.kickHandshake(id, profileTarget)
	select {
	case <-wait:
		s.mu.Lock()
		peer := p.peer
		s.mu.Unlock()
		if !peer.Online {
			return Peer{}, ErrNotConnected
		}
		return peer, nil
	case <-ctx.Done():
		if s.enet.connected(control) {
			return Peer{}, fmt.Errorf("内网通资料握手 %s: %w", control, ctx.Err())
		}
		return Peer{}, fmt.Errorf("内网通控制握手 %s: %w", control, ctx.Err())
	case <-s.ctx.Done():
		return Peer{}, s.ctx.Err()
	}
}

func (s *Service) control(peerID string) (netip.AddrPort, netip.AddrPort, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.peers[peerID]; p != nil && p.peer.Online {
		return p.control, p.tcp, nil
	}
	return netip.AddrPort{}, netip.AddrPort{}, ErrNotConnected
}

func (s *Service) SendText(peerID, text string) error {
	return s.SendTextWithID(peerID, 0, text)
}

// SendQuit notifies a connected peer that this endpoint is going offline.
func (s *Service) SendQuit(peerID string) error {
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdQuit, []byte("<X_QUIT docver=\"1\"/>"))
}

func (s *Service) SendTextWithID(peerID string, packetID uint64, text string) error {
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	if packetID == 0 || packetID > uint64(^uint32(0)) {
		packetID = uint64(s.messageSeq.Add(1))
	}
	payload, err := FormatSendMsg(packetID, text)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdSendMsg, payload)
}

// SendTextConfirmed uses the same application identifiers on every delivery
// attempt. The caller can retain its outbox entry on an uncertain receipt.
func (s *Service) SendTextConfirmed(ctx context.Context, peerID string, packetID uint64, id, text string, queuedAt time.Time) error {
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	payload, err := formatMessage(packetID, id, []MsgDataTxt{{Txt: &MsgTxt{T: "normal", V: text}}}, queuedAt)
	if err != nil {
		return err
	}
	return s.sendWithReceipt(ctx, peerID, control, CmdSendMsg, packetID, payload)
}

func (s *Service) SendTextRecall(ctx context.Context, peerID string, packetID uint64, id, targetID string) error {
	if packetID == 0 {
		return errors.New("invalid native recall packet")
	}
	body, err := formatRecallPayload(id, targetID)
	if err != nil {
		return err
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	return s.sendWithReceipt(ctx, peerID, control, CmdSendMsg, packetID, formatDirectBody(packetID, body, time.Time{}))
}

func (s *Service) ackChatMessage(remote netip.AddrPort, packetID uint64) {
	if err := s.enet.SendAppCommand(remote, CmdSendMsgAck, FormatSendMsgAck(packetID)); err != nil && s.cfg.Trace != nil {
		s.cfg.Trace("send native message acknowledgement: " + err.Error())
	}
}

func (s *Service) sendWithReceipt(ctx context.Context, peerID string, control netip.AddrPort, kind uint32, packetID uint64, payload []byte) error {
	key := receiptKey{peerID, packetID}
	done := make(chan struct{})
	s.mu.Lock()
	if s.receipts[key] != nil {
		s.mu.Unlock()
		return ErrDeliveryUncertain
	}
	if s.receipts == nil {
		s.receipts = make(map[receiptKey]chan struct{})
	}
	s.receipts[key] = done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.receipts[key] == done {
			delete(s.receipts, key)
		}
		s.mu.Unlock()
	}()
	if err := s.enet.SendAppCommand(control, kind, payload); err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		return err
	}
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
	case <-ctx.Done():
	case <-s.ctx.Done():
	}
	return ErrDeliveryUncertain
}

func (s *Service) SendFileOffer(peerID string, taskID uint64, name string, size, lastMod int64, path string) error {
	return s.SendFileOfferWithIDs(peerID, taskID, taskID, name, size, lastMod, path)
}

func (s *Service) SendFileOfferWithIDs(peerID string, taskID, fileID uint64, name string, size, lastMod int64, path string) error {
	control, err := s.prepareFileOffer(peerID, taskID, fileID, size, path)
	if err != nil {
		return err
	}
	err = s.enet.SendAppCommand(control, CmdSendFile, FormatSendFile(taskID, fileID, name, size, lastMod))
	if err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		s.dropFile(taskID)
	}
	return err
}

func (s *Service) SendChatImage(ctx context.Context, peerID string, packetID uint64, id string, taskID, fileID uint64, size int64, digest [16]byte, path string) error {
	part, err := chatImagePart(fileID, size, digest)
	if err != nil {
		return err
	}
	payload, err := formatMessage(packetID, id, []MsgDataTxt{part}, time.Time{})
	if err != nil {
		return err
	}
	control, err := s.prepareFileOffer(peerID, taskID, fileID, size, path)
	if err != nil {
		return err
	}
	err = s.sendWithReceipt(ctx, peerID, control, CmdSendMsg, packetID, payload)
	if err != nil && !errors.Is(err, ErrDeliveryUncertain) {
		s.dropFile(taskID)
	}
	return err
}

func chatImagePart(fileID uint64, size int64, digest [16]byte) (MsgDataTxt, error) {
	if size <= 0 || size > MaxChatImageBytes || fileID == avatarSelector || fileID == 0 || fileID > maxFileSelector {
		return MsgDataTxt{}, errors.New("invalid native chat image")
	}
	return MsgDataTxt{Img: &MsgTxt{T: "feihu", V: fmt.Sprintf("%d|%x", fileID, digest)}}, nil
}

func (s *Service) prepareFileOffer(peerID string, taskID, fileID uint64, size int64, path string) (netip.AddrPort, error) {
	return s.prepareFileOfferWithAccess(peerID, taskID, fileID, size, path, nil)
}

func (s *Service) prepareFileOfferWithAccess(peerID string, taskID, fileID uint64, size int64, path string, allowed func() bool) (netip.AddrPort, error) {
	control, _, err := s.control(peerID)
	if err != nil {
		return netip.AddrPort{}, err
	}
	s.mu.Lock()
	p := s.peers[peerID]
	chunked := p != nil && !p.peer.GoIM
	if taskID == 0 || taskID > maxFileSelector || fileID == 0 || fileID > maxFileSelector || !validFileSize(size, chunked) {
		s.mu.Unlock()
		return netip.AddrPort{}, errors.New("内网通文件超出此版本的传输范围")
	}
	old, exists := s.files[taskID]
	if exists && (old.peerID != peerID || old.fileID != fileID || old.size != size) {
		s.mu.Unlock()
		return netip.AddrPort{}, errors.New("内网通任务编号已被占用")
	}
	if err := s.transfer.registerFileWithAccess(fileID, taskID, path, size, control.Addr(), chunked, allowed); err != nil {
		s.mu.Unlock()
		return netip.AddrPort{}, err
	}
	if !exists {
		s.files[taskID] = fileOffer{peerID: peerID, fileID: fileID, size: size}
	}
	s.mu.Unlock()
	return control, nil
}

func (s *Service) receiveFileOperation(peer string, task uint64, op uint32) {
	s.mu.Lock()
	offer, ok := s.files[task]
	if !ok || offer.peerID != peer {
		s.mu.Unlock()
		return
	}
	if op == fileReceiveAccept {
		offer.accepted = true
		s.files[task] = offer
		s.mu.Unlock()
		return
	}
	p := s.peers[peer]
	if op != fileReceiveStop && !(op == fileReceiveStopLegacy && p != nil && p.peer.GoIM) {
		s.mu.Unlock()
		return
	}
	reason := ErrFileRejected
	if offer.accepted {
		reason = ErrFileCancelled
	}
	s.dropFileLocked(task)
	s.mu.Unlock()
	s.reportFile(peer, task, 0, offer.size, false, reason)
}

func (s *Service) dropFile(task uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropFileLocked(task)
}

func (s *Service) dropFileLocked(task uint64) {
	offer := s.files[task]
	delete(s.files, task)
	for key, item := range s.groupFiles {
		if item.task == task {
			delete(s.groupFiles, key)
		}
	}
	if p := s.peers[offer.peerID]; p != nil && s.offerCountLocked(offer.peerID) == 0 {
		stopReverseChannels(p)
	}
	if offer.fileID != 0 {
		s.transfer.UnregisterFile(offer.fileID)
	}
}

func (s *Service) offerCountLocked(peerID string) int {
	count := 0
	for _, offer := range s.files {
		if offer.peerID == peerID {
			count++
		}
	}
	return count
}

func stopReverseChannels(p *nativePeer) {
	for selector, channel := range p.reverse {
		delete(p.reverse, selector)
		channel.cancel()
	}
}

// The receiver chooses the reverse selector independently of TASK_ID and
// FILE_ID. The bootstrap precedes the receiver's separate TCP file request.
// Keep it scoped to a peer with outstanding offers and leave file access
// checks to the normal transfer listener.
func (s *Service) startReverseChannel(peerID string, selector uint64) {
	if selector == 0 || selector > maxFileSelector {
		return
	}
	s.mu.Lock()
	p := s.peers[peerID]
	if p == nil || !p.peer.Online || !validUnicast(p.reverseTCP) ||
		p.reverse[selector] != nil || len(p.reverse) >= min(8, s.offerCountLocked(peerID)) {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	channel := &reverseChannel{cancel: cancel}
	if p.reverse == nil {
		p.reverse = make(map[uint64]*reverseChannel)
	}
	p.reverse[selector] = channel
	target := p.reverseTCP
	s.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			if p.reverse[selector] == channel {
				delete(p.reverse, selector)
			}
			s.mu.Unlock()
		}()
		conn, err := openReverseFileChannel(ctx, s.cfg.BindIP, target, selector)
		if err != nil {
			if s.cfg.Trace != nil && ctx.Err() == nil {
				s.cfg.Trace("open native file reverse channel: " + err.Error())
			}
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		// No file data uses this connection. Hold it until the peer
		// disconnects or we no longer have outstanding offers for it.
		var unexpected [1]byte
		_, _ = io.ReadFull(conn, unexpected[:])
	}()
}

func (s *Service) onFileStream(task uint64, done, total int64, failure error) {
	s.mu.Lock()
	offer, ok := s.files[task]
	complete := failure == nil && done == total
	if ok {
		if complete {
			s.dropFileLocked(task)
		} else if done > 0 && !offer.accepted {
			offer.accepted = true
			s.files[task] = offer
		}
	}
	s.mu.Unlock()
	if ok {
		if failure != nil {
			failure = fmt.Errorf("内网通文件发送中断: %w", failure)
		}
		s.reportFile(offer.peerID, task, done, total, complete, failure)
	}
}

func (s *Service) reportFile(peer string, task uint64, done, total int64, complete bool, err error) {
	if s.cfg.Callbacks.FileProgress != nil {
		s.cfg.Callbacks.FileProgress(peer, task, done, total, complete, err)
	}
}

func (s *Service) StopReceiving(peerID string, taskID uint64) error {
	if taskID == 0 {
		return errors.New("invalid native file task")
	}
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdOperateRecvFile, FormatOperateRecvFile(taskID, fileReceiveStop))
}

func (s *Service) CancelSend(peerID string, taskID uint64) error {
	if taskID == 0 {
		return errors.New("invalid native file task")
	}
	s.mu.Lock()
	for key, item := range s.groupFiles {
		if item.task == taskID {
			if key.peer != peerID {
				s.mu.Unlock()
				return errors.New("内网通任务与联系人不匹配")
			}
			s.dropFileLocked(taskID)
			s.mu.Unlock()
			return nil
		}
	}
	if offer, ok := s.files[taskID]; ok {
		if offer.peerID != peerID {
			s.mu.Unlock()
			return errors.New("内网通任务与联系人不匹配")
		}
		s.dropFileLocked(taskID)
	}
	s.mu.Unlock()
	control, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(control, CmdOperateSendFile, FormatOperateSendFile(taskID, fileSendCancel))
}

func (s *Service) AcceptFile(ctx context.Context, peerID string, taskID, fileID uint64, total int64, destPath string, progress func(int64)) error {
	return s.receiveFile(ctx, peerID, taskID, fileID, total, destPath, false, progress)
}

// ResumeFile appends to a private partial file for this exact offer. It keeps
// the received prefix if the connection is interrupted.
func (s *Service) ResumeFile(ctx context.Context, peerID string, taskID, fileID uint64, total int64, partial string, progress func(int64)) error {
	return s.receiveFile(ctx, peerID, taskID, fileID, total, partial, true, progress)
}

func (s *Service) receiveFile(ctx context.Context, peerID string, taskID, fileID uint64, total int64,
	destPath string, resume bool, progress func(int64)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	control, tcp, err := s.control(peerID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	p := s.peers[peerID]
	stockClient := p != nil && !p.peer.GoIM
	s.mu.Unlock()
	if taskID == 0 || fileID == 0 || fileID > maxFileSelector || !validFileSize(total, stockClient) {
		return errors.New("invalid native file transfer")
	}
	selector := fileID // Older GoIM peers expect their offered file ID.
	if stockClient {
		selector = uint64(max(1, s.messageSeq.Add(1)))
	}
	key := reverseKey{remote: tcp.Addr(), selector: selector}
	reverseReady, err := s.transfer.expectReverse(key)
	if err != nil {
		return err
	}
	defer s.transfer.finishReverse(key)
	if err = s.enet.SendAppCommand(control, CmdFileReverseReq, FormatReverseFileReq(selector)); err != nil {
		return err
	}
	if err = s.enet.SendAppCommand(control, CmdOperateRecvFile, FormatOperateRecvFile(taskID, fileReceiveAccept)); err != nil {
		return err
	}
	// The native sender normally connects to our reverse listener before it
	// serves cmd 3. Keep the two-second fallback for older native builds and
	// synthetic peers that only implement the direct transfer path.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-reverseReady:
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	err = downloadFileTo(ctx, s.cfg.BindIP, tcp, fileID, total, destPath, stockClient, resume, func(done, total int64) {
		if progress != nil {
			progress(done)
		}
	})
	if err == nil {
		_ = s.enet.SendAppCommand(control, CmdProgressRecvFile, FormatProgressRecvFile(taskID, total, total, "0KB/s"))
	}
	return err
}

func localWireProfile(p Peer, png []byte) wireProfile {
	info := wireProfile{Name: p.Name, Group: p.Group, Sign: p.Sign, Status: p.Status,
		Company: p.Company, Department: p.Department, Phone: p.Phone, Mobile: p.Mobile, Email: p.Email}
	if p.Gender == "male" {
		info.Sex = 1
	} else if p.Gender == "female" {
		info.Sex = 2
	}
	if len(png) != 0 {
		digest := md5.Sum(png)
		info.UserFace = wireAvatar{ID: avatarSelector, Hash: hex.EncodeToString(digest[:])}
	}
	return info
}

func (s *Service) UpdateProfile(p Peer, png []byte) {
	info := localWireProfile(p, png)
	s.mu.Lock()
	previous := s.local
	if info == previous {
		s.mu.Unlock()
		return
	}
	s.transfer.SetAvatar(png)
	s.local = info
	_, _ = rand.Read(s.selfDisc.ProfileTag[:])
	var targets []netip.AddrPort
	for _, peer := range s.peers {
		if peer.peer.Online {
			targets = append(targets, peer.control)
		}
	}
	s.mu.Unlock()
	for _, target := range targets {
		go func() {
			_ = s.enet.SendAppCommand(target, CmdChangeInfo, formatProfileChange(info))
		}()
	}
	if previous.Status == StatusInvisible && info.Status != StatusInvisible {
		s.broadcastAnnounce()
	}
}

func (s *Service) avatarAllowed(ip netip.Addr) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.peers {
		// A directed avatar pull can arrive after the authenticated control
		// session and before the full profile marks the peer online. Discovery
		// or the verified control endpoint is enough to bind the request to a
		// known native identity.
		if p.control.Addr() == ip || p.discovery.Addr() == ip {
			return true
		}
	}
	return false
}

func (s *Service) AvatarCurrent(peerID string, revision uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.peers[peerID]
	return p != nil && p.peer.Online && p.avatarRevision == revision
}

func (s *Service) FetchAvatar(peerID string) error {
	_, _, err := s.control(peerID)
	if err != nil {
		return err
	}
	s.fetchAvatar(peerID, true)
	return nil
}

const MaxChatImageBytes = 24 << 20

func (s *Service) FetchChatImage(ctx context.Context, peerID string, ref ChatImageRef) ([]byte, error) {
	_, tcp, err := s.control(peerID)
	if err != nil {
		return nil, err
	}
	if ref.Selector == 0 || ref.Selector == avatarSelector {
		return nil, errors.New("invalid native chat image selector")
	}
	data, err := fetchMedia(ctx, s.cfg.BindIP, tcp, ref.Selector, MaxChatImageBytes, 15*time.Second)
	if err == nil && md5.Sum(data) != ref.Digest {
		return nil, errors.New("native chat image did not match its digest")
	}
	return data, err
}

func (s *Service) fetchAvatar(peerID string, force bool) {
	s.mu.Lock()
	p := s.peers[peerID]
	if p == nil || !p.peer.Online || p.avatarFetching || p.avatar.ID == 0 && p.avatar.Hash == "" ||
		!force && p.avatarCached == p.avatarRevision {
		s.mu.Unlock()
		return
	}
	p.avatarFetching = true
	s.mu.Unlock()
	go func() {
		for {
			s.mu.Lock()
			rev, tcp, avatar, online := p.avatarRevision, p.tcp, p.avatar, p.peer.Online
			s.mu.Unlock()
			var png []byte
			if online && (avatar.ID != 0 || avatar.Hash != "") {
				selector := avatar.ID
				if selector == 0 {
					selector = avatarSelector
				}
				if selector <= maxFileSelector {
					var err error
					png, err = fetchOfferedAvatar(s.ctx, s.cfg.BindIP, tcp, uint32(selector))
					if s.cfg.Trace != nil && err != nil {
						s.cfg.Trace("avatar transfer: " + err.Error())
					}
				}
			}
			s.mu.Lock()
			if rev != p.avatarRevision {
				s.mu.Unlock()
				continue
			}
			p.avatarFetching = false
			current := p.peer.Online && len(png) > 0
			if current {
				p.avatarCached = rev
			}
			s.mu.Unlock()
			if current && s.cfg.Callbacks.AvatarUpdated != nil {
				s.cfg.Callbacks.AvatarUpdated(peerID, rev, png)
			}
			return
		}
	}()
}

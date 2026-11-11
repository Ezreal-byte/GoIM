package neiwangtong

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

type Peer struct {
	ID       string
	Name     string
	Group    string
	Sign     string
	Address  string
	Online   bool
	AvatarID uint64
}

type Callbacks struct {
	Peer          func(Peer)
	Text          func(peerID string, packetID uint64, text string)
	Files         func(peerID string, taskID uint64, name string, size int64)
	FileProgress  func(taskID uint64, done, total int64, complete bool, err error)
	AvatarUpdated func(peerID string, png []byte)
	Error         func(error)
}

type Config struct {
	ID               [16]byte
	Name             string
	Group            string
	Sign             string
	DiscoveryPort    int
	ControlPort      int
	TransferPort     int
	BindIP           netip.Addr
	AvatarPNG        []byte
	DisableBroadcast bool
	Callbacks        Callbacks
}

type Service struct {
	cfg              Config
	bindIP           netip.Addr
	discConn         *net.UDPConn
	enet             *ENetTransport
	transfer         *TransferServer
	peers            map[string]*nativePeer
	peersMu          sync.RWMutex
	closing          atomic.Bool
	broadcastEnabled atomic.Bool
	stopCh           chan struct{}
	selfDisc         Discovery
	markerHex        string
}

type nativePeer struct {
	peer       Peer
	udp9011    *net.UDPAddr
	udp9012    *net.UDPAddr
	tcp9013    netip.AddrPort
	profileTag [16]byte
	lastSeen   time.Time
}

func Start(cfg Config) (*Service, error) {
	if cfg.DiscoveryPort <= 0 {
		cfg.DiscoveryPort = int(DiscoveryPort)
	}
	if cfg.ControlPort <= 0 {
		cfg.ControlPort = int(ControlPort)
	}
	if cfg.TransferPort <= 0 {
		cfg.TransferPort = int(AvatarPort)
	}
	if cfg.ID == [16]byte{} {
		_, _ = rand.Read(cfg.ID[:])
	}

	var randomTag [16]byte
	_, _ = rand.Read(randomTag[:])

	s := &Service{
		cfg:       cfg,
		bindIP:    cfg.BindIP,
		peers:     make(map[string]*nativePeer),
		stopCh:    make(chan struct{}),
		markerHex: hex.EncodeToString(cfg.ID[:]),
		selfDisc: Discovery{
			Kind:       Announce,
			ID:         cfg.ID,
			ProfileTag: randomTag,
			Broadcast:  netip.AddrFrom4([4]byte{255, 255, 255, 255}),
			Trailer:    uint16(cfg.ControlPort),
		},
	}

	transferSrv, err := StartTransferServer(cfg.BindIP, cfg.TransferPort, func(taskID uint64, done, total int64) {
		if cfg.Callbacks.FileProgress != nil {
			cfg.Callbacks.FileProgress(taskID, done, total, done >= total, nil)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("neiwangtong tcp transfer: %w", err)
	}
	if len(cfg.AvatarPNG) > 0 {
		transferSrv.SetAvatar(cfg.AvatarPNG)
	}
	s.transfer = transferSrv

	enetTrans, err := NewENetTransport(cfg.BindIP, cfg.ControlPort, s.onENetCommand)
	if err != nil {
		_ = transferSrv.Close()
		return nil, fmt.Errorf("neiwangtong enet: %w", err)
	}
	s.enet = enetTrans

	var discLAddr *net.UDPAddr
	if cfg.BindIP.IsValid() && cfg.BindIP.Is4() {
		discLAddr = &net.UDPAddr{IP: cfg.BindIP.AsSlice(), Port: cfg.DiscoveryPort}
	} else {
		discLAddr = &net.UDPAddr{Port: cfg.DiscoveryPort}
	}
	discConn, err := net.ListenUDP("udp4", discLAddr)
	if err != nil {
		_ = enetTrans.Close()
		_ = transferSrv.Close()
		return nil, fmt.Errorf("neiwangtong discovery: %w", err)
	}
	s.discConn = discConn

	go s.discoveryReadLoop()
	s.broadcastEnabled.Store(!cfg.DisableBroadcast)
	go s.discoveryAnnounceLoop()

	return s, nil
}

func (s *Service) Close() error {
	s.closing.Store(true)
	close(s.stopCh)
	var errs []error
	if s.discConn != nil {
		_ = s.discConn.Close()
	}
	if s.enet != nil {
		_ = s.enet.Close()
	}
	if s.transfer != nil {
		_ = s.transfer.Close()
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *Service) SetBroadcastEnabled(enabled bool) {
	prev := s.broadcastEnabled.Swap(enabled)
	if enabled && !prev {
		s.broadcastAnnounce()
	}
}

func (s *Service) SetAvatar(png []byte) {
	if s.transfer != nil {
		s.transfer.SetAvatar(png)
	}
}

func (s *Service) discoveryReadLoop() {
	var buf [1024]byte
	for {
		n, remote, err := s.discConn.ReadFromUDP(buf[:])
		if err != nil {
			if s.closing.Load() {
				return
			}
			continue
		}
		if n != discoverySize {
			continue
		}
		d, err := ParseDiscovery(buf[:n])
		if err != nil || d.ID == s.cfg.ID {
			continue
		}

		peerID := hex.EncodeToString(d.ID[:])
		remoteIP := remote.IP.To4()
		if remoteIP == nil {
			continue
		}

		ctrlPort := s.cfg.ControlPort
		if d.Trailer != 0 {
			ctrlPort = int(d.Trailer)
		}

		s.peersMu.Lock()
		p, exists := s.peers[peerID]
		if !exists {
			p = &nativePeer{
				peer: Peer{
					ID:      peerID,
					Name:    "内网通用户",
					Address: netip.AddrPortFrom(netip.AddrFrom4([4]byte(remoteIP)), uint16(ctrlPort)).String(),
					Online:  true,
				},
				udp9011: remote,
				udp9012: &net.UDPAddr{IP: remoteIP, Port: ctrlPort},
				tcp9013: netip.AddrPortFrom(netip.AddrFrom4([4]byte(remoteIP)), uint16(s.cfg.TransferPort)),
			}
			s.peers[peerID] = p
		} else if p.udp9012.Port != ctrlPort {
			p.udp9012 = &net.UDPAddr{IP: remoteIP, Port: ctrlPort}
		}
		p.lastSeen = time.Now()
		p.profileTag = d.ProfileTag
		peerCopy := p.peer
		udpTarget := p.udp9012
		s.peersMu.Unlock()

		if s.cfg.Callbacks.Peer != nil {
			s.cfg.Callbacks.Peer(peerCopy)
		}

		if d.Kind == Announce {
			s.replyDiscovery(remote)
		}

		go s.handshakePeer(udpTarget)
	}
}

func (s *Service) replyDiscovery(to *net.UDPAddr) {
	reply := s.selfDisc
	reply.Kind = Reply
	b, err := reply.MarshalBinary()
	if err == nil {
		_, _ = s.discConn.WriteToUDP(b, to)
	}
}

func (s *Service) discoveryAnnounceLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	if s.broadcastEnabled.Load() {
		s.broadcastAnnounce()
	}

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.broadcastEnabled.Load() {
				s.broadcastAnnounce()
			}
		}
	}
}

func (s *Service) broadcastAnnounce() {
	if !s.broadcastEnabled.Load() {
		return
	}
	payload, err := s.selfDisc.MarshalBinary()
	if err != nil {
		return
	}
	bcastAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: s.cfg.DiscoveryPort}
	_, _ = s.discConn.WriteToUDP(payload, bcastAddr)
}

func (s *Service) handshakePeer(target *net.UDPAddr) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := s.enet.Connect(ctx, target)
	if err != nil {
		return
	}

	hs := FormatHandshark(s.cfg.Name, s.cfg.Group, s.cfg.Sign, 0, uint16(s.transfer.Port()), uint16(s.enet.localAddr.Port), 1, "avatar")
	_ = s.enet.SendAppCommand(target, CmdHandshark, hs)
}

func (s *Service) onENetCommand(remote *net.UDPAddr, appKind uint32, plain []byte) {
	if remote.Port == s.enet.localAddr.Port && remote.IP.IsLoopback() {
		return
	}

	peerID := s.findPeerIDByUDP(remote)

	switch appKind {
	case CmdHandshark:
		hs, err := ParseHandshark(plain)
		if err == nil {
			var callbackPeer Peer
			var hasCallback bool
			s.peersMu.Lock()
			if peerID == "" && len(s.peers) == 1 {
				for id := range s.peers {
					peerID = id
					break
				}
			}
			if p, ok := s.peers[peerID]; ok {
				if hs.Info.Name != "" {
					p.peer.Name = hs.Info.Name
				}
				p.peer.Group = hs.Info.Group
				p.peer.Sign = hs.Info.Sign
				p.peer.AvatarID = hs.Info.UserFace.ID
				if hs.Net.FileTranTCPPort != 0 {
					p.tcp9013 = netip.AddrPortFrom(p.tcp9013.Addr(), hs.Net.FileTranTCPPort)
				}
				if hs.Net.FileTranENetPort != 0 {
					p.udp9012 = &net.UDPAddr{IP: p.udp9012.IP, Port: int(hs.Net.FileTranENetPort)}
				}
				callbackPeer = p.peer
				hasCallback = true
			}
			s.peersMu.Unlock()

			if hasCallback && s.cfg.Callbacks.Peer != nil {
				s.cfg.Callbacks.Peer(callbackPeer)
			}
			if (hs.Info.UserFace.ID != 0 || hs.Info.UserFace.Hash != "") && peerID != "" {
				go s.FetchAvatar(peerID)
			}
		}
	case CmdSendMsg:
		msgID, text, _, err := ParseSendMsg(plain)
		if err == nil && s.cfg.Callbacks.Text != nil {
			if peerID == "" {
				peerID = s.anyPeerID()
			}
			if peerID != "" {
				s.cfg.Callbacks.Text(peerID, msgID, text)
			}
		}
	case CmdSendFile:
		sf, err := ParseSendFile(plain)
		if err == nil && s.cfg.Callbacks.Files != nil {
			if peerID == "" {
				peerID = s.anyPeerID()
			}
			if peerID != "" {
				s.cfg.Callbacks.Files(peerID, sf.TaskID, sf.Name, sf.Size)
			}
		}
	case CmdOperateRecvFile:
		_, op, err := ParseOperateRecvFile(plain)
		if err == nil && op == 2 {
		}
	case CmdProgressRecvFile:
		prog, err := ParseProgressRecvFile(plain)
		if err == nil && s.cfg.Callbacks.FileProgress != nil {
			s.cfg.Callbacks.FileProgress(prog.TaskID, prog.RecvdSize, prog.TotalSize, prog.RecvdSize >= prog.TotalSize, nil)
		}
	case CmdAvatarOffer:
		if peerID == "" {
			peerID = s.anyPeerID()
		}
		if peerID != "" {
			go s.FetchAvatar(peerID)
		}
	}
}

func (s *Service) anyPeerID() string {
	s.peersMu.RLock()
	defer s.peersMu.RUnlock()
	for id := range s.peers {
		return id
	}
	return ""
}

func (s *Service) findPeerIDByUDP(addr *net.UDPAddr) string {
	s.peersMu.RLock()
	defer s.peersMu.RUnlock()
	for id, p := range s.peers {
		if p.udp9012.IP.Equal(addr.IP) && (len(s.peers) == 1 || p.udp9012.Port == addr.Port) {
			return id
		}
	}
	return ""
}

func (s *Service) SendText(peerID, text string) error {
	s.peersMu.RLock()
	p, ok := s.peers[peerID]
	if !ok {
		s.peersMu.RUnlock()
		return fmt.Errorf("unknown neiwangtong peer %s", peerID)
	}
	udp := p.udp9012
	s.peersMu.RUnlock()

	payload, err := FormatSendMsg(uint64(time.Now().UnixNano()), text)
	if err != nil {
		return err
	}
	return s.enet.SendAppCommand(udp, CmdSendMsg, payload)
}

func (s *Service) SendFileOffer(peerID string, taskID uint64, name string, size, lastMod int64, localPath string) error {
	s.peersMu.RLock()
	p, ok := s.peers[peerID]
	if !ok {
		s.peersMu.RUnlock()
		return fmt.Errorf("unknown neiwangtong peer %s", peerID)
	}
	udp := p.udp9012
	s.peersMu.RUnlock()

	s.transfer.RegisterFile(taskID, localPath, size)

	payload := FormatSendFile(taskID, taskID, name, size, lastMod)
	return s.enet.SendAppCommand(udp, CmdSendFile, payload)
}

func (s *Service) RejectFile(peerID string, taskID uint64) error {
	s.peersMu.RLock()
	p, ok := s.peers[peerID]
	if !ok {
		s.peersMu.RUnlock()
		return fmt.Errorf("unknown neiwangtong peer %s", peerID)
	}
	udp := p.udp9012
	s.peersMu.RUnlock()

	payload := FormatOperateRecvFile(taskID, 3)
	return s.enet.SendAppCommand(udp, CmdOperateRecvFile, payload)
}

func (s *Service) AcceptFile(ctx context.Context, peerID string, taskID uint64, totalSize int64, destPath string) error {
	s.peersMu.RLock()
	p, ok := s.peers[peerID]
	if !ok {
		s.peersMu.RUnlock()
		return fmt.Errorf("unknown neiwangtong peer %s", peerID)
	}
	udp := p.udp9012
	tcp := p.tcp9013
	s.peersMu.RUnlock()

	payload := FormatOperateRecvFile(taskID, 2)
	_ = s.enet.SendAppCommand(udp, CmdOperateRecvFile, payload)

	err := DownloadFile(ctx, tcp, taskID, totalSize, destPath, func(done, total int64) {
		if s.cfg.Callbacks.FileProgress != nil {
			s.cfg.Callbacks.FileProgress(taskID, done, total, done >= total, nil)
		}
		progPayload := FormatProgressRecvFile(taskID, total, done, "0KB/s")
		_ = s.enet.SendAppCommand(udp, CmdProgressRecvFile, progPayload)
	})

	if s.cfg.Callbacks.FileProgress != nil {
		s.cfg.Callbacks.FileProgress(taskID, totalSize, totalSize, err == nil, err)
	}
	return err
}

func (s *Service) RegisterFile(taskID uint64, path string, size int64) {
	if s.transfer != nil {
		s.transfer.RegisterFile(taskID, path, size)
	}
}

func (s *Service) SetPeerTransferPort(peerID string, tcpPort netip.AddrPort) {
	s.peersMu.Lock()
	defer s.peersMu.Unlock()
	if p, ok := s.peers[peerID]; ok {
		p.tcp9013 = tcpPort
	}
}

func (s *Service) FetchAvatar(peerID string) {
	s.peersMu.RLock()
	p, ok := s.peers[peerID]
	if !ok {
		s.peersMu.RUnlock()
		return
	}
	tcp := p.tcp9013
	s.peersMu.RUnlock()
	if !tcp.IsValid() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	png, err := FetchOfferedAvatar(ctx, s.bindIP, tcp)
	if err == nil && len(png) > 0 && s.cfg.Callbacks.AvatarUpdated != nil {
		s.cfg.Callbacks.AvatarUpdated(peerID, png)
	}
}

func (s *Service) Probe(ctx context.Context, target netip.AddrPort, controlPort, transferPort int) error {
	if controlPort <= 0 {
		controlPort = s.cfg.ControlPort
	}
	if transferPort <= 0 {
		transferPort = s.cfg.TransferPort
	}

	reply, err := Probe(ctx, s.bindIP, target, s.selfDisc)
	if err != nil {
		return err
	}
	peerID := hex.EncodeToString(reply.ID[:])
	ip := target.Addr().AsSlice()
	s.peersMu.Lock()
	p, ok := s.peers[peerID]
	if !ok {
		p = &nativePeer{
			peer: Peer{
				ID:      peerID,
				Name:    "内网通用户",
				Address: target.String(),
				Online:  true,
			},
			udp9011: &net.UDPAddr{IP: ip, Port: int(target.Port())},
			udp9012: &net.UDPAddr{IP: ip, Port: controlPort},
			tcp9013: netip.AddrPortFrom(target.Addr(), uint16(transferPort)),
		}
		s.peers[peerID] = p
	}
	p.lastSeen = time.Now()
	peerCopy := p.peer
	udpTarget := p.udp9012
	s.peersMu.Unlock()

	if s.cfg.Callbacks.Peer != nil {
		s.cfg.Callbacks.Peer(peerCopy)
	}
	go s.handshakePeer(udpTarget)
	return nil
}

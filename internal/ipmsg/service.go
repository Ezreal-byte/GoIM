package ipmsg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type Peer struct {
	ID          string
	Name        string
	Host        string
	Address     string
	Online      bool
	Away        bool
	Neiwangtong bool
}

type Callbacks struct {
	Peer          func(Peer)
	Message       func(peerID string, packetNumber uint64, text string, files []Attachment) error
	FileRequested func(transferID string)
	FileProgress  func(packetNumber uint64, fileID uint32, done, total int64, complete bool, err error)
	AllowFile     func(transferID string) bool
	Error         func(error)
}

type Config struct {
	Host             string
	User             string // stable logon identity; Name is the display nickname
	Name             string
	Version          string
	Port             int
	BindIP           netip.Addr // optional explicit IPv4 address
	Hidden           bool
	Away             bool
	DisableBroadcast bool
	RequireFiles     bool
	Callbacks        Callbacks
}

type peerState struct {
	peer    Peer
	addr    *net.UDPAddr
	seen    time.Time
	classic bool
}

type servingFile struct {
	path       string
	size       int64
	source     os.FileInfo
	digest     string
	recipient  net.IP
	peerID     string
	transferID string
}

type fileKey struct {
	packet uint64
	file   uint32
}

type localProfile struct {
	name         string
	hidden, away bool
}

const goIMDiscoveryMarker = "\nGOIM:1"

type Service struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cfg       Config
	udp       *net.UDPConn
	tcp       net.Listener
	packet    atomic.Uint64
	profile   atomic.Pointer[localProfile]
	broadcast atomic.Bool

	mu       sync.Mutex
	peers    map[string]*peerState
	files    map[fileKey]servingFile
	probes   map[netip.AddrPort][]chan Peer
	receipts map[messageReceiptKey]messageReceipt
	wg       sync.WaitGroup
	active   sync.Map // accepted file connections, closed during shutdown
	slots    chan struct{}
}

func Start(cfg Config) (*Service, error) {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("IPMSG port must be between 1 and 65535")
	}
	cfg.Host = headerField(cfg.Host)
	cfg.Name = displayNickname(cfg.Name)
	if strings.TrimSpace(cfg.User) == "" {
		cfg.User = cfg.Name
	}
	cfg.User = headerField(cfg.User)
	if cfg.BindIP.IsValid() && !cfg.BindIP.Is4() {
		return nil, errors.New("IPMSG bind address must be IPv4")
	}
	bind := netip.IPv4Unspecified()
	if cfg.BindIP.IsValid() {
		bind = cfg.BindIP
	}
	endpoint := netip.AddrPortFrom(bind, uint16(cfg.Port)).String()
	packet, err := listenUDP("udp4", endpoint)
	if err != nil {
		return nil, fmt.Errorf("IPMSG UDP %d: %w", cfg.Port, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		ctx: ctx, cancel: cancel, cfg: cfg, udp: packet,
		peers: make(map[string]*peerState), files: make(map[fileKey]servingFile),
		probes:   make(map[netip.AddrPort][]chan Peer),
		receipts: make(map[messageReceiptKey]messageReceipt),
		slots:    make(chan struct{}, 16),
	}
	service.packet.Store(uint64(time.Now().Unix()))
	service.profile.Store(&localProfile{name: cfg.Name, hidden: cfg.Hidden, away: cfg.Away})
	service.broadcast.Store(!cfg.DisableBroadcast)
	service.tcp, err = net.Listen("tcp4", endpoint)
	if err != nil && cfg.RequireFiles {
		cancel()
		_ = packet.Close()
		return nil, fmt.Errorf("IPMSG TCP %d: %w", cfg.Port, err)
	}
	service.wg.Add(1)
	go service.readUDP()
	if service.tcp != nil {
		service.wg.Add(1)
		go service.acceptFiles()
	} else {
		service.report(fmt.Errorf("IPMSG file listener is unavailable; text remains active"))
	}
	service.wg.Add(1)
	go service.heartbeat()
	service.announce(commandEntry)
	return service, nil
}

func (s *Service) FilesAvailable() bool { return s != nil && s.tcp != nil }

func (s *Service) SetBroadcastEnabled(enabled bool) {
	previous := s.broadcast.Swap(enabled)
	if enabled && !previous {
		s.announce(commandEntry)
	}
}

func (s *Service) UpdateProfile(name string, hidden, away bool) {
	old := s.profile.Swap(&localProfile{name: displayNickname(name), hidden: hidden, away: away})
	if hidden {
		if !old.hidden {
			s.announce(commandExit)
		}
		return
	}
	if !old.hidden && old.away != away {
		s.announce(commandAbsence)
		return
	}
	s.announce(commandEntry)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.cancel()
	if !s.profile.Load().hidden {
		s.announce(commandExit)
	}
	_ = s.udp.Close()
	if s.tcp != nil {
		_ = s.tcp.Close()
	}
	s.active.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
	s.wg.Wait()
}

func (s *Service) SendText(peerID, text string) error {
	state := s.peer(peerID)
	if state == nil {
		return errors.New("IPMSG peer is offline")
	}
	return s.sendFileMessage(state, s.nextPacket(), text, "")
}

// FileOffer identifies one attachment across receipt retries and restarts.
type FileOffer struct {
	Packet                 uint64
	ID                     uint32
	TransferID, Path, Name string
	Size                   int64
	Source                 os.FileInfo // handle-based metadata retained from hashing the source
	Digest                 string
}

// RememberFile restores a verified outgoing attachment without repeating the
// invitation. The core persists its identifiers before calling this method.
func (s *Service) RememberFile(peer string, offer FileOffer) error {
	if !s.FilesAvailable() {
		return errors.New("IPMSG TCP file listener is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.peers[peer]
	if state == nil || !state.peer.Online {
		return ErrPeerOffline
	}
	key := fileKey{packet: offer.Packet, file: offer.ID}
	if old, ok := s.files[key]; ok && (old.transferID != offer.TransferID || old.peerID != peer) {
		return errors.New("IPMSG file invitation identifiers already in use")
	}
	s.files[key] = servingFile{
		path: offer.Path, size: offer.Size, source: offer.Source, digest: offer.Digest,
		recipient: append(net.IP(nil), state.addr.IP...), peerID: peer, transferID: offer.TransferID,
	}
	return nil
}

// VerifiedFile can reuse the digest only for this same transfer, path, and
// version of the file that was previously opened and hashed in this process.
func (s *Service) VerifiedFile(peer string, offer FileOffer) bool {
	s.mu.Lock()
	entry, ok := s.files[fileKey{packet: offer.Packet, file: offer.ID}]
	s.mu.Unlock()
	return ok && entry.transferID == offer.TransferID && entry.peerID == peer && entry.path == offer.Path &&
		entry.digest == offer.Digest && sameFileVersion(entry.source, offer.Source)
}

func sameFileVersion(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}

func (s *Service) SendFileOffer(ctx context.Context, peer string, offer FileOffer, caption string) error {
	if err := s.RememberFile(peer, offer); err != nil {
		return err
	}
	if caption == "" {
		caption = offer.Name
	}
	attachment := Attachment{ID: offer.ID, Name: offer.Name, Size: offer.Size, ModTime: offer.Source.ModTime().Unix(), Kind: fileRegular}
	return s.sendConfirmed(ctx, peer, offer.Packet, caption, encodeAttachments([]Attachment{attachment}), offer.TransferID)
}

// DownloadFile requests a visible attachment and streams its remaining bytes
// into dst. The caller owns destination creation, validation and durable state.
func (s *Service) DownloadFile(ctx context.Context, peerID string, packetNumber uint64, fileID uint32,
	size, offset int64, dst io.Writer, progress func(done int64)) (string, error) {
	state := s.peer(peerID)
	if state == nil {
		return "", errors.New("IPMSG peer is offline")
	}
	if size < 0 || offset < 0 || offset > size {
		return "", errors.New("invalid IPMSG transfer range")
	}
	dialer := net.Dialer{Timeout: 8 * time.Second}
	if ip := s.cfg.BindIP; ip.IsValid() && !ip.IsUnspecified() {
		dialer.LocalAddr = &net.TCPAddr{IP: net.IP(ip.AsSlice())}
	}
	conn, err := dialer.DialContext(ctx, "tcp4", state.addr.String())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	request := packetString(s.nextPacket(), s.wireUser(state.classic), s.cfg.Host, commandGetFile,
		fmt.Sprintf("%x:%x:%x:", packetNumber, fileID, offset))
	_ = conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
	if _, err := conn.Write(request); err != nil {
		return "", err
	}
	hash := sha256.New()
	done := offset
	buffer := make([]byte, 256<<10)
	last := time.Now()
	for done < size {
		count := int64(len(buffer))
		if remain := size - done; remain < count {
			count = remain
		}
		_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		n, readErr := conn.Read(buffer[:count])
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return "", writeErr
			}
			_, _ = hash.Write(buffer[:n])
			done += int64(n)
			if progress != nil && time.Since(last) >= 200*time.Millisecond {
				progress(done)
				last = time.Now()
			}
		}
		if readErr != nil {
			if readErr == io.EOF && done == size {
				break
			}
			return "", readErr
		}
	}
	if progress != nil {
		progress(done)
	}
	// RELEASEFILES applies to the whole offer, which can have more attachments.
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *Service) SendFileMessage(peerID, path, text, transferID string) error {
	state := s.peer(peerID)
	if state == nil {
		return errors.New("IPMSG peer is offline")
	}
	return s.sendFileMessage(state, s.nextPacket(), text, "")
}

func (s *Service) sendFileMessage(state *peerState, number uint64, text, attachments string) error {
	data, err := s.messagePacket(state, number, text, attachments)
	if err != nil {
		return err
	}
	return s.sendToLogin(state, data)
}

// A route can change after an offer or retry was prepared. Check and send
// together with respect to the endpoint changes in the receive loop.
func (s *Service) sendToLogin(state *peerState, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.peers[state.peer.ID]
	if current == nil || !current.peer.Online || current.addr.Port != state.addr.Port || !current.addr.IP.Equal(state.addr.IP) {
		return ErrPeerOffline
	}
	return s.sendPacket(state, data)
}

func (s *Service) messagePacket(state *peerState, number uint64, text, attachments string) ([]byte, error) {
	extra := text
	command := uint32(commandSend) | optionUTF8 | optionSendCheck
	if attachments != "" {
		extra += "\x00" + attachments
		command |= optionFile
	}
	if state.classic {
		// Older Chinese clients do not advertise UTF-8, or explicitly use GBK.
		var err error
		extra, err = simplifiedchinese.GBK.NewEncoder().String(extra)
		if err != nil {
			return nil, errors.New("旧版兼容编码不支持此消息中的部分字符")
		}
		command &^= optionUTF8
	}
	return packetString(number, s.wireUser(state.classic), s.cfg.Host, command, extra), nil
}

func (s *Service) wireUser(classic bool) string {
	name := s.cfg.User
	if !classic {
		return name
	}
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(name)
	if err != nil {
		return "GoIM"
	}
	return encoded
}

func (s *Service) readUDP() {
	defer s.wg.Done()
	buffer := make([]byte, 128<<10)
	for {
		count, from, err := s.udp.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		payload := append([]byte(nil), buffer[:count]...)
		s.handleDatagram(payload, from)
	}
}

func (s *Service) handleDatagram(data []byte, from *net.UDPAddr) {
	message, err := parsePacket(data)
	if err != nil {
		return
	}
	if strings.EqualFold(message.Host, s.cfg.Host) && message.Sender == s.cfg.User {
		return
	}
	if strings.Contains(message.Extra, goIMDiscoveryMarker) &&
		(message.baseCommand() == commandEntry || message.baseCommand() == commandAnswer || message.baseCommand() == commandExit || message.baseCommand() == commandAbsence) {
		return
	}
	base := message.baseCommand()
	if base == commandReceived {
		s.receiveMessageReceipt(message, from)
		return
	}
	switch base {
	case commandEntry, commandAnswer, commandAbsence, commandExit, commandSend:
	default:
		return
	}
	id := peerID(message.Host, message.Sender)
	peer := Peer{ID: id, Name: message.Sender, Host: message.Host,
		Address: from.String(),
		Online:  true, Away: message.has(optionAbsence), Neiwangtong: message.isNeiwangtong()}
	presence := base == commandEntry || base == commandAnswer || base == commandAbsence
	if presence {
		if nickname, _ := decodeMessage(message.Extra, message.has(optionUTF8)); strings.TrimSpace(nickname) != "" {
			peer.Name = nickname
		}
		if _, _, extra := strings.Cut(message.Extra, "\x00"); extra && message.has(optionCapUTF8) {
			for line := range strings.SplitSeq(message.Extra, "\n") {
				if name, ok := strings.CutPrefix(line, "NN:"); ok && utf8.ValidString(name) && strings.TrimSpace(name) != "" {
					peer.Name = name
					break
				}
			}
		}
	}
	s.mu.Lock()
	var previous []Peer
	if base != commandExit {
		for otherID, other := range s.peers {
			if otherID != id && other.peer.Online && other.addr.Port == from.Port && other.addr.IP.Equal(from.IP) {
				other.peer.Online = false
				previous = append(previous, other.peer)
			}
		}
	}
	state := s.peers[id]
	wasOnline := state != nil && state.peer.Online
	address := from.AddrPort()
	address = netip.AddrPortFrom(address.Addr().Unmap(), address.Port())
	probing := len(s.probes[address]) != 0
	if state == nil {
		state = &peerState{addr: new(net.UDPAddr)}
		s.peers[id] = state
	} else if !presence {
		// A text packet names the login, not necessarily the display nickname.
		peer.Name, peer.Away = state.peer.Name, state.peer.Away
		peer.Neiwangtong = peer.Neiwangtong || state.peer.Neiwangtong
	}
	if base == commandExit {
		peer.Online = false
	}
	state.peer = peer
	state.addr.IP = append(net.IP(nil), from.IP...)
	state.addr.Port = from.Port
	state.seen = time.Now()
	if base == commandEntry || base == commandAnswer || base == commandAbsence {
		state.classic = !message.has(optionCapUTF8) && !message.has(optionUTF8)
	}
	state.classic = state.classic || strings.HasPrefix(message.rawVersion, "1_lbt") || message.isNeiwangtong()
	s.mu.Unlock()
	if s.cfg.Callbacks.Peer != nil {
		for _, peer := range previous {
			s.cfg.Callbacks.Peer(peer)
		}
		s.cfg.Callbacks.Peer(peer)
	}
	// Keep discovery requests flag-free for classic peers. Advertise file
	// support in the reply, including when a peer first answers our discovery.
	if base == commandEntry || base == commandAnswer && (!wasOnline || probing) && s.FilesAvailable() {
		s.answer(s.peer(id))
	}
	if presence {
		s.mu.Lock()
		for _, done := range s.probes[address] {
			done <- peer
		}
		delete(s.probes, address)
		s.mu.Unlock()
	}

	switch message.baseCommand() {
	case commandSend:
		if s.cfg.Callbacks.Message == nil {
			return
		}
		text, files := decodeMessage(message.Extra, message.has(optionUTF8))
		var attachments []Attachment
		if files != "" && message.has(optionFile) {
			attachments = decodeAttachments(files)
			if len(attachments) == 0 {
				return
			}
		}
		if text == "" && len(attachments) == 0 || s.cfg.Callbacks.Message(id, message.Number, text, attachments) != nil {
			return
		}
		if message.has(optionSendCheck) {
			state := s.peer(id)
			if state == nil {
				return
			}
			_ = s.sendPacket(state, packetString(s.nextPacket(), s.wireUser(state.classic), s.cfg.Host,
				commandReceived, strconv.FormatUint(uint64(message.Number), 10)))
		}
	}
}

func (s *Service) answer(state *peerState) {
	profile := s.profile.Load()
	if profile.hidden {
		return
	}
	_ = s.sendPacket(state, s.presencePacket(profile, s.statusCommand(commandAnswer, profile)))
}

func (s *Service) announce(command uint32) {
	if s == nil || s.udp == nil {
		return
	}
	profile := s.profile.Load()
	if profile.hidden && command != commandExit {
		return
	}
	// Some classic clients only answer an entry with no capability flags.
	if command == commandAbsence {
		command = s.statusCommand(command, profile)
	}
	packet := s.presencePacket(profile, command)
	if command&0xff == commandAbsence || command == commandExit {
		// A known peer can be on another subnet or a non-default port.
		s.mu.Lock()
		var targets []netip.AddrPort
		for _, state := range s.peers {
			if state.peer.Online {
				targets = append(targets, state.addr.AddrPort())
			}
		}
		s.mu.Unlock()
		for _, target := range targets {
			_, _ = s.udp.WriteToUDPAddrPort(packet, target)
		}
	}
	if !s.broadcast.Load() {
		return
	}
	var withFiles []byte
	if command == commandEntry && s.FilesAvailable() {
		withFiles = s.presencePacket(profile, s.statusCommand(command, profile))
	}
	for _, address := range broadcastAddresses(s.cfg.Port, s.cfg.BindIP) {
		_, _ = s.udp.WriteToUDP(packet, address)
		if withFiles != nil {
			_, _ = s.udp.WriteToUDP(withFiles, address)
		}
	}
}

func (s *Service) statusCommand(command uint32, profile *localProfile) uint32 {
	command |= optionCapUTF8
	if s.FilesAvailable() {
		command |= optionFile
	}
	if profile.away {
		command |= optionAbsence
	}
	return command
}

func (s *Service) presencePacket(profile *localProfile, command uint32) []byte {
	name, err := simplifiedchinese.GBK.NewEncoder().String(profile.name)
	if err != nil {
		name = "GoIM"
	}
	// Empty legacy group, followed by the UTF-8 nickname extension. The
	// marker keeps native GoIM peers out of the legacy contact list.
	user := s.wireUser(true)
	return packetString(s.nextPacket(), user, s.cfg.Host, command, name+"\x00\x00\nNN:"+profile.name+goIMDiscoveryMarker)
}

// Probe sends a unicast entry from the actual legacy service port. Some
// clients ignore otherwise valid discovery packets from an ephemeral port.
func (s *Service) Probe(ctx context.Context, address netip.AddrPort) (Peer, error) {
	ip := address.Addr().Unmap()
	if !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) || address.Port() == 0 {
		return Peer{}, errors.New("IPMSG requires a unicast IPv4 address")
	}
	address = netip.AddrPortFrom(ip, address.Port())
	profile := s.profile.Load()
	if profile.hidden {
		return Peer{}, errors.New("IPMSG discovery is unavailable while invisible")
	}
	done := make(chan Peer, 1)
	s.mu.Lock()
	s.probes[address] = append(s.probes[address], done)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		waiters := slices.DeleteFunc(s.probes[address], func(ch chan Peer) bool { return ch == done })
		if len(waiters) == 0 {
			delete(s.probes, address)
		} else {
			s.probes[address] = waiters
		}
		s.mu.Unlock()
	}()
	for range 3 {
		if _, err := s.udp.WriteToUDPAddrPort(s.presencePacket(profile, commandEntry), address); err != nil {
			return Peer{}, err
		}
		timer := time.NewTimer(700 * time.Millisecond)
		select {
		case peer := <-done:
			timer.Stop()
			return peer, nil
		case <-ctx.Done():
			timer.Stop()
			return Peer{}, ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return Peer{}, s.ctx.Err()
		case <-timer.C:
		}
	}
	return Peer{}, context.DeadlineExceeded
}

func (s *Service) heartbeat() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.announce(commandEntry)
			s.markStalePeersOffline()
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Service) markStalePeersOffline() {
	now := time.Now()
	var offline []Peer
	s.mu.Lock()
	for _, state := range s.peers {
		if state.peer.Online && now.Sub(state.seen) > 3*time.Minute {
			state.peer.Online = false
			offline = append(offline, state.peer)
		}
	}
	s.mu.Unlock()
	if s.cfg.Callbacks.Peer != nil {
		for _, peer := range offline {
			s.cfg.Callbacks.Peer(peer)
		}
	}
}

func (s *Service) peer(id string) *peerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.peers[id]
	if state == nil || !state.peer.Online {
		return nil
	}
	return &peerState{peer: state.peer, classic: state.classic, addr: &net.UDPAddr{
		IP: append(net.IP(nil), state.addr.IP...), Port: state.addr.Port,
	}}
}

func (s *Service) sendPacket(state *peerState, data []byte) error {
	if state == nil || state.addr == nil || state.addr.IP == nil {
		return errors.New("IPMSG peer address is unavailable")
	}
	_, err := s.udp.WriteToUDP(data, state.addr)
	return err
}

func (s *Service) nextPacket() uint64 {
	for {
		number := s.packet.Add(1)
		if number != 0 {
			return number
		}
	}
}

func (s *Service) acceptFiles() {
	defer s.wg.Done()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
			s.active.Store(conn, struct{}{})
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { s.active.Delete(conn); <-s.slots }()
				defer conn.Close()
				if s.ctx.Err() == nil {
					s.serveFile(conn)
				}
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (s *Service) serveFile(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	request, err := readRequest(conn)
	if err != nil {
		return
	}
	packet, err := parsePacket(request)
	if err != nil || packet.baseCommand() != commandGetFile {
		return
	}
	number, fileID, offset, err := parseFileRequest(packet.Extra)
	if err != nil {
		return
	}
	s.mu.Lock()
	entry, ok := s.files[fileKey{packet: number, file: fileID}]
	s.mu.Unlock()
	address, isTCP := conn.RemoteAddr().(*net.TCPAddr)
	if !ok || offset > entry.size || !isTCP || !entry.recipient.Equal(address.IP) || !s.fileRecipientOnline(entry) {
		return
	}
	if s.cfg.Callbacks.AllowFile != nil && !s.cfg.Callbacks.AllowFile(entry.transferID) {
		return
	}
	progress := s.cfg.Callbacks.FileProgress
	report := func(done int64, complete bool, err error) {
		if progress != nil {
			progress(number, fileID, done, entry.size, complete, err)
		}
	}
	file, err := os.Open(entry.path)
	if err != nil {
		report(offset, false, errors.New("无法打开兼容传输的源文件"))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !sameFileVersion(entry.source, info) {
		report(offset, false, errors.New("源文件已变化，请重新发送"))
		return
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		report(offset, false, errors.New("无法定位兼容传输的文件偏移"))
		return
	}
	// Some clients start downloading without sending the UDP delivery receipt.
	s.confirmFileRequest(number, entry)
	if requested := s.cfg.Callbacks.FileRequested; requested != nil {
		requested(entry.transferID)
	}
	done := offset
	if done < entry.size {
		report(done, false, nil)
	}
	last := time.Now()
	for done < entry.size {
		if s.ctx.Err() != nil || !s.fileRecipientOnline(entry) || s.cfg.Callbacks.AllowFile != nil && !s.cfg.Callbacks.AllowFile(entry.transferID) {
			return
		}
		// Limit idle writes, not total duration: a large file can take minutes.
		_ = conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
		written, err := io.CopyN(conn, file, min(256<<10, entry.size-done))
		done += written
		if err != nil {
			report(done, false, errors.New("对方断开了文件连接，可重新接收"))
			return
		}
		if done < entry.size && time.Since(last) >= 200*time.Millisecond {
			report(done, false, nil)
			last = time.Now()
		}
	}
	report(done, true, nil)
}

func (s *Service) fileRecipientOnline(entry servingFile) bool {
	peer := s.peer(entry.peerID)
	return peer != nil && entry.recipient.Equal(peer.addr.IP)
}

func readRequest(conn net.Conn) ([]byte, error) {
	buffer := make([]byte, 0, 512)
	chunk := make([]byte, 512)
	for len(buffer) < 64<<10 {
		count, err := conn.Read(chunk)
		if count > 0 {
			buffer = append(buffer, chunk[:count]...)
			if i := bytes.IndexAny(buffer, "\x00\n\r"); i >= 0 {
				return buffer[:i], nil
			}
			// Some legacy clients (including Neiwangtong 3.4) omit the NUL
			// after the trailing colon and leave the socket open for the data.
			if buffer[len(buffer)-1] == ':' {
				if p, err := parsePacket(buffer); err == nil && p.baseCommand() == commandGetFile {
					if _, _, _, err := parseFileRequest(p.Extra); err == nil {
						return buffer, nil
					}
				}
			}
		}
		if err != nil {
			return buffer, err
		}
	}
	return nil, errors.New("IPMSG file request is too large")
}

func (s *Service) report(err error) {
	if err != nil && s.cfg.Callbacks.Error != nil {
		s.cfg.Callbacks.Error(err)
	}
}

func broadcastAddresses(port int, bind netip.Addr) []*net.UDPAddr {
	byHost := make(map[string]*net.UDPAddr)
	if !bind.IsValid() || bind.IsUnspecified() {
		byHost["255.255.255.255"] = &net.UDPAddr{IP: net.IPv4bcast, Port: port}
	}
	interfaces, _ := net.Interfaces()
	for _, network := range interfaces {
		if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := network.Addrs()
		for _, address := range addresses {
			ip, network4, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil {
				continue
			}
			if bind.IsValid() && !bind.IsUnspecified() && !ip.Equal(net.IP(bind.AsSlice())) {
				continue
			}
			broadcast := make(net.IP, len(network4.IP))
			for i := range network4.IP {
				broadcast[i] = network4.IP[i] | ^network4.Mask[i]
			}
			byHost[broadcast.String()] = &net.UDPAddr{IP: broadcast, Port: port}
		}
	}
	out := make([]*net.UDPAddr, 0, len(byHost))
	for _, address := range byHost {
		out = append(out, address)
	}
	return out
}

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
	"path/filepath"
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
	Peer         func(Peer)
	Text         func(peerID string, packetNumber uint64, text string)
	Files        func(peerID string, packetNumber uint64, message string, files []Attachment)
	FilePrepared func(transferID string, packetNumber uint64, fileID uint32) error
	FileProgress func(packetNumber uint64, fileID uint32, done, total int64, complete bool, err error)
	AllowFile    func(transferID string) bool
	Error        func(error)
}

type Config struct {
	Host             string
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
	modified   time.Time
	recipient  net.IP
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
	fileID    atomic.Uint32
	profile   atomic.Pointer[localProfile]
	broadcast atomic.Bool

	mu     sync.Mutex
	peers  map[string]*peerState
	files  map[fileKey]servingFile
	probes map[netip.AddrPort][]chan struct{}
	wg     sync.WaitGroup
}

func Start(cfg Config) (*Service, error) {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("IPMSG port must be between 1 and 65535")
	}
	cfg.Host = headerField(cfg.Host)
	cfg.Name = headerField(cfg.Name)
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
		probes: make(map[netip.AddrPort][]chan struct{}),
	}
	service.packet.Store(uint64(time.Now().Unix()))
	service.fileID.Store(1)
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
	old := s.profile.Swap(&localProfile{name: headerField(name), hidden: hidden, away: away})
	if hidden {
		if !old.hidden {
			s.announce(commandExit)
		}
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
	s.wg.Wait()
}

func (s *Service) SendText(peerID, text string) error {
	state := s.peer(peerID)
	if state == nil {
		return errors.New("IPMSG peer is offline")
	}
	return s.sendFileMessage(state, s.nextPacket(), text, "")
}

// OfferFile advertises one explicit local file. Only the registered path can
// later be read by a file request, never an arbitrary path supplied remotely.
func (s *Service) OfferFile(peerID, path, transferID, message string) (uint64, uint32, error) {
	if !s.FilesAvailable() {
		return 0, 0, errors.New("IPMSG TCP file listener is unavailable")
	}
	state := s.peer(peerID)
	if state == nil {
		return 0, 0, errors.New("IPMSG peer is offline")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		return 0, 0, errors.New("IPMSG source file is unavailable")
	}
	number := s.nextPacket()
	fileID := s.fileID.Add(1)
	file := Attachment{ID: fileID, Name: filepath.Base(path), Size: info.Size(),
		ModTime: info.ModTime().Unix(), Kind: fileRegular}
	if prepare := s.cfg.Callbacks.FilePrepared; prepare != nil {
		if err := prepare(transferID, number, fileID); err != nil {
			return 0, 0, err
		}
	}
	s.mu.Lock()
	for key, entry := range s.files {
		if entry.transferID == transferID {
			delete(s.files, key)
		}
	}
	s.files[fileKey{packet: number, file: fileID}] = servingFile{
		path: path, size: file.Size, modified: info.ModTime(),
		recipient: state.addr.IP, transferID: transferID,
	}
	s.mu.Unlock()
	if message == "" {
		message = file.Name
	}
	if err := s.sendFileMessage(state, number, message, encodeAttachments([]Attachment{file})); err != nil {
		s.mu.Lock()
		delete(s.files, fileKey{packet: number, file: fileID})
		s.mu.Unlock()
		return 0, 0, err
	}
	return number, fileID, nil
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
	request := packetString(s.nextPacket(), s.wireName(state), s.cfg.Host, commandGetFile,
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
			return errors.New("旧版兼容编码不支持此消息中的部分字符")
		}
		command &^= optionUTF8
	}
	data := packetString(number, s.wireName(state), s.cfg.Host, command, extra)
	return s.sendPacket(state, data)
}

func (s *Service) wireName(state *peerState) string {
	name := s.profile.Load().name
	if !state.classic {
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
	if strings.EqualFold(message.Host, s.cfg.Host) {
		return
	}
	if strings.Contains(message.Extra, goIMDiscoveryMarker) &&
		(message.baseCommand() == commandEntry || message.baseCommand() == commandAnswer || message.baseCommand() == commandExit) {
		return
	}
	base := message.baseCommand()
	switch base {
	case commandEntry, commandAnswer, commandAbsence, commandExit, commandSend:
	default:
		return
	}
	id := peerID(message.Host, from.IP.String())
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
			close(done)
		}
		delete(s.probes, address)
		s.mu.Unlock()
	}

	switch message.baseCommand() {
	case commandSend:
		text, files := decodeMessage(message.Extra, message.has(optionUTF8))
		var attachments []Attachment
		if s.cfg.Callbacks.Files != nil && files != "" && message.has(optionFile) {
			attachments = decodeAttachments(files)
		}
		// A single-file offer often repeats the filename as its text body.
		filenameOnly := len(attachments) == 1 && strings.TrimSpace(text) == attachments[0].Name
		if s.cfg.Callbacks.Text != nil && text != "" && !filenameOnly {
			s.cfg.Callbacks.Text(id, message.Number, text)
		}
		if len(attachments) != 0 {
			s.cfg.Callbacks.Files(id, message.Number, text, attachments)
		}
		if message.has(optionSendCheck) {
			_ = s.sendPacket(s.peer(id), packetString(s.nextPacket(), s.cfg.Name, s.cfg.Host,
				commandReceived, strconv.FormatUint(uint64(message.Number), 10)))
		}
	}
}

func (s *Service) answer(state *peerState) {
	profile := s.profile.Load()
	if profile.hidden {
		return
	}
	command := uint32(commandAnswer | optionCapUTF8)
	if s.FilesAvailable() {
		command |= optionFile
	}
	if profile.away {
		command |= optionAbsence
	}
	_ = s.sendPacket(state, s.presencePacket(profile, command))
}

func (s *Service) announce(command uint32) {
	if s == nil || s.udp == nil || !s.broadcast.Load() {
		return
	}
	profile := s.profile.Load()
	if profile.hidden && command != commandExit {
		return
	}
	// Some classic clients only answer an entry with no capability flags.
	packet := s.presencePacket(profile, command)
	var withFiles []byte
	if command == commandEntry && s.FilesAvailable() {
		withFiles = s.presencePacket(profile, command|optionFile|optionCapUTF8)
	}
	for _, address := range broadcastAddresses(s.cfg.Port, s.cfg.BindIP) {
		_, _ = s.udp.WriteToUDP(packet, address)
		if withFiles != nil {
			_, _ = s.udp.WriteToUDP(withFiles, address)
		}
	}
}

func (s *Service) presencePacket(profile *localProfile, command uint32) []byte {
	name, err := simplifiedchinese.GBK.NewEncoder().String(profile.name)
	if err != nil {
		name = "GoIM"
	}
	// Empty legacy group, followed by the UTF-8 nickname extension. The
	// marker keeps native GoIM peers out of the legacy contact list.
	return packetString(s.nextPacket(), name, s.cfg.Host, command, name+"\x00\x00\nNN:"+profile.name+goIMDiscoveryMarker)
}

// Probe sends a unicast entry from the actual legacy service port. Some
// clients ignore otherwise valid discovery packets from an ephemeral port.
func (s *Service) Probe(ctx context.Context, address netip.AddrPort) error {
	ip := address.Addr().Unmap()
	if !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) || address.Port() == 0 {
		return errors.New("IPMSG requires a unicast IPv4 address")
	}
	address = netip.AddrPortFrom(ip, address.Port())
	profile := s.profile.Load()
	if profile.hidden {
		return errors.New("IPMSG discovery is unavailable while invisible")
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.probes[address] = append(s.probes[address], done)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		waiters := slices.DeleteFunc(s.probes[address], func(ch chan struct{}) bool { return ch == done })
		if len(waiters) == 0 {
			delete(s.probes, address)
		} else {
			s.probes[address] = waiters
		}
		s.mu.Unlock()
	}()
	for range 3 {
		if _, err := s.udp.WriteToUDPAddrPort(s.presencePacket(profile, commandEntry), address); err != nil {
			return err
		}
		timer := time.NewTimer(700 * time.Millisecond)
		select {
		case <-done:
			timer.Stop()
			return nil
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-s.ctx.Done():
			timer.Stop()
			return s.ctx.Err()
		case <-timer.C:
		}
	}
	return context.DeadlineExceeded
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
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.serveFile(conn)
		}()
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
	if !ok || offset > entry.size || !isTCP || !entry.recipient.Equal(address.IP) {
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
	if err != nil || !info.Mode().IsRegular() || info.Size() != entry.size || !info.ModTime().Equal(entry.modified) {
		report(offset, false, errors.New("源文件已变化，请重新发送"))
		return
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		report(offset, false, errors.New("无法定位兼容传输的文件偏移"))
		return
	}
	done := offset
	if done < entry.size {
		report(done, false, nil)
	}
	last := time.Now()
	for done < entry.size {
		if s.ctx.Err() != nil || s.cfg.Callbacks.AllowFile != nil && !s.cfg.Callbacks.AllowFile(entry.transferID) {
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

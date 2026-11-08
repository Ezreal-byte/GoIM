package ipmsg

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type Peer struct {
	ID      string
	Name    string
	Host    string
	Address string
	Online  bool
	Away    bool
}

type Callbacks struct {
	Peer         func(Peer)
	Text         func(peerID string, packetNumber uint64, text string)
	Files        func(peerID string, packetNumber uint64, message string, files []Attachment)
	FileProgress func(packetNumber uint64, fileID uint32, done, total int64, complete bool)
	AllowFile    func(transferID string) bool
	Error        func(error)
}

type Config struct {
	Host      string
	Name      string
	Version   string
	Port      int
	Hidden    bool
	Away      bool
	Callbacks Callbacks
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

const goIMDiscoveryMarker = "\nGOIM:1\n"

type Service struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cfg     Config
	udp     *net.UDPConn
	tcp     net.Listener
	packet  atomic.Uint64
	fileID  atomic.Uint32
	profile atomic.Pointer[localProfile]

	mu    sync.Mutex
	peers map[string]*peerState
	files map[fileKey]servingFile
	wg    sync.WaitGroup
}

func Start(cfg Config) (*Service, error) {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("IPMSG port must be between 1 and 65535")
	}
	cfg.Host = headerField(cfg.Host)
	cfg.Name = headerField(cfg.Name)
	packet, err := listenUDPShared("udp4", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("IPMSG UDP %d: %w", cfg.Port, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		ctx: ctx, cancel: cancel, cfg: cfg, udp: packet,
		peers: make(map[string]*peerState), files: make(map[fileKey]servingFile),
	}
	service.packet.Store(uint64(time.Now().Unix()))
	service.fileID.Store(1)
	service.profile.Store(&localProfile{name: cfg.Name, hidden: cfg.Hidden, away: cfg.Away})
	service.tcp, _ = net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
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
	conn, err := dialer.DialContext(ctx, "tcp4", state.addr.String())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	request := packetString(s.nextPacket(), s.cfg.Name, s.cfg.Host, commandGetFile,
		fmt.Sprintf("%x:%x:%x", packetNumber, fileID, offset))
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
	_ = s.sendPacket(state, packetString(s.nextPacket(), s.cfg.Name, s.cfg.Host,
		commandRelease, fmt.Sprintf("%x", packetNumber)))
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
	command := uint32(commandSend) | optionUTF8
	if attachments != "" {
		extra += "\x00" + attachments
		command |= optionFile
	}
	name := s.cfg.Name
	if state.classic {
		// Older Chinese clients do not advertise UTF-8, or explicitly use GBK.
		var err error
		name, err = simplifiedchinese.GBK.NewEncoder().String(headerField(name))
		if err != nil {
			name = "GoIM"
		}
		extra, err = simplifiedchinese.GBK.NewEncoder().String(extra)
		if err != nil {
			return errors.New("旧版兼容编码不支持此消息中的部分字符")
		}
		command &^= optionUTF8
	}
	data := packetString(number, name, s.cfg.Host, command, extra)
	return s.sendPacket(state, data)
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
	id := peerID(message.Host, from.IP.String())
	peer := Peer{ID: id, Name: message.Sender, Host: message.Host,
		Address: from.String(),
		Online:  true, Away: message.has(optionAbsence)}
	base := message.baseCommand()
	if base == commandEntry || base == commandAnswer || base == commandAbsence {
		if nickname, _ := decodeMessage(message.Extra, message.has(optionUTF8)); strings.TrimSpace(nickname) != "" {
			peer.Name = nickname
		}
	}
	s.mu.Lock()
	state := s.peers[id]
	if state == nil {
		state = &peerState{addr: new(net.UDPAddr)}
		s.peers[id] = state
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
	state.classic = state.classic || strings.HasPrefix(message.rawVersion, "1_lbt")
	s.mu.Unlock()
	if s.cfg.Callbacks.Peer != nil {
		s.cfg.Callbacks.Peer(peer)
	}

	switch message.baseCommand() {
	case commandEntry:
		s.answer(s.peer(id))
	case commandSend:
		text, files := decodeMessage(message.Extra, message.has(optionUTF8))
		if s.cfg.Callbacks.Text != nil && text != "" {
			s.cfg.Callbacks.Text(id, message.Number, text)
		}
		if s.cfg.Callbacks.Files != nil && files != "" && message.has(optionFile) {
			if decoded := decodeAttachments(files); len(decoded) != 0 {
				s.cfg.Callbacks.Files(id, message.Number, text, decoded)
			}
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
	command := uint32(commandAnswer | optionCapUTF8 | optionFile)
	if profile.away {
		command |= optionAbsence
	}
	extra := profile.name + "\x00\nNN:" + profile.name + goIMDiscoveryMarker
	_ = s.sendPacket(state, packetString(s.nextPacket(), s.cfg.Name, s.cfg.Host, command, extra))
}

func (s *Service) announce(command uint32) {
	if s == nil || s.udp == nil {
		return
	}
	profile := s.profile.Load()
	if profile.hidden && command != commandExit {
		return
	}
	name, err := simplifiedchinese.GBK.NewEncoder().String(profile.name)
	if err != nil {
		name = "GoIM"
	}
	// Some classic clients only answer an entry with no capability flags.
	packet := packetString(s.nextPacket(), s.cfg.Name, s.cfg.Host, command, name+"\x00"+goIMDiscoveryMarker)
	for _, address := range broadcastAddresses(s.cfg.Port) {
		_, _ = s.udp.WriteToUDP(packet, address)
	}
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
	file, err := os.Open(entry.path)
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != entry.size || !info.ModTime().Equal(entry.modified) {
		return
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	written, err := io.CopyN(conn, file, entry.size-offset)
	complete := err == nil && written == entry.size-offset
	if s.cfg.Callbacks.FileProgress != nil {
		s.cfg.Callbacks.FileProgress(number, fileID, offset+written, entry.size, complete)
	}
}

func readRequest(conn net.Conn) ([]byte, error) {
	buffer := make([]byte, 0, 512)
	chunk := make([]byte, 512)
	for len(buffer) < 64<<10 {
		count, err := conn.Read(chunk)
		if count > 0 {
			buffer = append(buffer, chunk[:count]...)
			if strings.ContainsAny(string(buffer), "\x00\n\r") {
				return []byte(strings.TrimRight(string(buffer), "\x00\r\n")), nil
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

func broadcastAddresses(port int) []*net.UDPAddr {
	byHost := map[string]*net.UDPAddr{
		"255.255.255.255": {IP: net.IPv4bcast, Port: port},
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

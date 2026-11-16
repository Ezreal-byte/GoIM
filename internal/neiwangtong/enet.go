package neiwangtong

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// ENet 1.x command sizes and sequencing follow the upstream protocol. This
// transport implements the reliable channels used by the native IM adapter.
const (
	enetAck        = 1
	enetConnect    = 2
	enetVerify     = 3
	enetDisconnect = 4
	enetPing       = 5
	enetReliable   = 6
	enetFragment   = 8
	enetAckFlag    = 0x80
	enetTimed      = 0x8000
	enetNoPeer     = 0x0fff

	maxAppFrame   = 512 << 10
	maxSessions   = 64
	maxENetWindow = 512
	enetTimeout   = 80 * time.Second
)

var (
	ErrNotConnected      = errors.New("内网通控制会话未连接")
	ErrDeliveryUncertain = errors.New("内网通未确认收到，发送结果不明")
)

type fragmentKey struct {
	channel byte
	start   uint16
}

type fragmentPart struct {
	offset uint32
	data   []byte
}

type enetFragments struct {
	parts []fragmentPart
	total uint32
	seen  int
	at    time.Time
}

type reliableFrame struct {
	body  []byte
	count uint16
}

type reliableChannel struct {
	received uint16
	waiting  map[uint16]reliableFrame
}

type enetSession struct {
	target     netip.AddrPort
	localSlot  uint16
	remoteSlot uint16
	incoming   byte
	outgoing   byte
	cookie     [4]byte
	connecting bool
	ready      chan struct{}
	done       chan struct{}
	active     bool
	closed     bool
	lastSeen   time.Time
	mtu        int
	verify     []byte

	mu        sync.Mutex
	sendMu    sync.Mutex
	seq       uint16
	ctrlSeq   uint16
	ackWake   chan struct{}
	pending   map[fragmentKey]struct{}
	receive   [2]reliableChannel
	fragments map[fragmentKey]*enetFragments
}

type ENetTransport struct {
	conn      *net.UDPConn
	localAddr *net.UDPAddr
	mu        sync.Mutex
	sessions  map[netip.AddrPort]*enetSession
	nextSlot  uint16
	stopped   chan struct{}
	stopOnce  sync.Once
	onCommand func(netip.AddrPort, uint32, []byte)
	onLost    func(netip.AddrPort)
	trace     func(string)
}

func newENetTransport(bind netip.Addr, port int, onCommand func(netip.AddrPort, uint32, []byte), onLost func(netip.AddrPort)) (*ENetTransport, error) {
	if !bind.IsValid() {
		bind = netip.IPv4Unspecified()
	}
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(bind, uint16(port))))
	if err != nil {
		return nil, err
	}
	return &ENetTransport{
		conn: conn, localAddr: conn.LocalAddr().(*net.UDPAddr),
		sessions: make(map[netip.AddrPort]*enetSession), stopped: make(chan struct{}),
		onCommand: onCommand, onLost: onLost,
	}, nil
}

func (t *ENetTransport) Close() error {
	t.stopOnce.Do(func() {
		close(t.stopped)
		_ = t.conn.Close()
	})
	return nil
}

func (t *ENetTransport) session(remote netip.AddrPort, create bool) *enetSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sessions[remote]
	if s == nil && create && len(t.sessions) < maxSessions {
		// Slots are local, scoped to an endpoint. Cycling also keeps stale
		// packets from a previous connection out of the replacement session.
		t.nextSlot = (t.nextSlot + 1) % enetNoPeer
		s = &enetSession{
			target: remote, localSlot: t.nextSlot, mtu: 1400, ctrlSeq: 1,
			ready: make(chan struct{}), done: make(chan struct{}), ackWake: make(chan struct{}, 1),
			lastSeen: time.Now(), pending: make(map[fragmentKey]struct{}),
			fragments: make(map[fragmentKey]*enetFragments),
		}
		t.sessions[remote] = s
	}
	return s
}

func (t *ENetTransport) drop(s *enetSession) {
	t.mu.Lock()
	if t.sessions[s.target] != s {
		t.mu.Unlock()
		return
	}
	delete(t.sessions, s.target)
	s.mu.Lock()
	wasActive := s.active
	s.active, s.closed = false, true
	close(s.done)
	s.mu.Unlock()
	t.mu.Unlock()
	if wasActive && t.onLost != nil {
		t.onLost(s.target)
	}
}

func (t *ENetTransport) expire(now time.Time) {
	t.mu.Lock()
	list := make([]*enetSession, 0, len(t.sessions))
	for _, s := range t.sessions {
		list = append(list, s)
	}
	t.mu.Unlock()
	for _, s := range list {
		s.mu.Lock()
		timeout := enetTimeout
		if !s.active {
			timeout = 5 * time.Second
		}
		stale := now.Sub(s.lastSeen) > timeout
		for key, fragments := range s.fragments {
			if now.Sub(fragments.at) > 8*time.Second {
				delete(s.fragments, key)
			}
		}
		s.mu.Unlock()
		if stale {
			t.drop(s)
		}
	}
}

func (t *ENetTransport) readLoop() {
	var buf [4096]byte
	for {
		n, remote, err := t.conn.ReadFromUDPAddrPort(buf[:])
		if err != nil {
			return
		}
		if n >= 2 {
			t.handlePacket(remote, buf[:n])
		}
	}
}

func (t *ENetTransport) handlePacket(remote netip.AddrPort, packet []byte) {
	header := binary.BigEndian.Uint16(packet)
	if header&0x4000 != 0 { // No negotiated compressor.
		return
	}
	off := 2
	if header&enetTimed != 0 {
		off = 4
	}
	if len(packet) < off+4 {
		return
	}
	if t.trace != nil {
		t.trace(fmt.Sprintf("ENet input %s header=%04x command=%d bytes=%d hex=%x",
			remote, header, packet[off]&15, len(packet), packet[:min(len(packet), 96)]))
	}
	if header&enetNoPeer == enetNoPeer {
		if len(packet) == off+48 && packet[off] == enetAckFlag|enetConnect && packet[off+1] == 255 && off == 4 {
			t.acceptConnect(remote, packet[off:])
		}
		return
	}
	s := t.session(remote, false)
	if s == nil {
		return
	}
	var payloads [][]byte
	for off+4 <= len(packet) {
		kind := packet[off] & 15
		if int(kind) >= len(enetLengths) {
			break
		}
		length := enetLengths[kind]
		if length == 0 || off+length > len(packet) {
			break
		}
		c := packet[off : off+length]
		channel, seq := c[1], binary.BigEndian.Uint16(c[2:])
		var data []byte
		switch kind {
		case enetReliable, 7, enetFragment, 9, 12:
			size := int(binary.BigEndian.Uint16(c[lengthField(kind):]))
			if size == 0 || size > len(packet)-off-length {
				return
			}
			data = packet[off+length : off+length+size]
		}
		off += length + len(data)
		s.mu.Lock()
		if s.closed || header&enetNoPeer != s.localSlot ||
			(!s.connecting && byte(header>>12)&3 != s.incoming) {
			s.mu.Unlock()
			break
		}
		ack := false
		lost := false
		switch {
		case kind == enetVerify && s.connecting && channel == 255 && c[6] < 4 && c[7] < 4 &&
			bytes.Equal(c[40:44], s.cookie[:]) && binary.BigEndian.Uint16(c[4:]) < enetNoPeer &&
			binary.BigEndian.Uint32(c[16:]) >= 1 && binary.BigEndian.Uint32(c[16:]) <= 255:
			s.remoteSlot = binary.BigEndian.Uint16(c[4:])
			s.incoming, s.outgoing = c[6], c[7]
			s.mtu = int(min(max(binary.BigEndian.Uint32(c[8:]), 576), 1400))
			s.connecting = false
			s.activateLocked()
			ack = true
		case kind == enetAck && channel == 255 && !s.active && len(s.verify) != 0 &&
			binary.BigEndian.Uint16(c[4:]) == 1:
			s.verify = nil
			s.activateLocked()
		case s.active:
			s.lastSeen = time.Now()
			switch kind {
			case enetAck:
				key := fragmentKey{channel, binary.BigEndian.Uint16(c[4:])}
				if _, ok := s.pending[key]; ok {
					delete(s.pending, key)
					select {
					case s.ackWake <- struct{}{}:
					default:
					}
				}
			case enetVerify:
				ack = channel == 255 && bytes.Equal(c[40:44], s.cookie[:])
			case enetReliable:
				if channel < 2 && len(data) >= 8 {
					ack, payloads = s.acceptReliable(channel, seq, 1, data, payloads)
				}
			case enetFragment:
				if channel < 2 {
					ack, payloads = s.acceptFragment(channel, seq, c, data, payloads)
				}
			case enetDisconnect:
				lost, ack = true, true
			case enetPing, 10, 11:
				ack = true
			}
		}
		peer := s.remoteSlot | uint16(s.outgoing)<<12
		s.mu.Unlock()
		if ack && c[0]&enetAckFlag != 0 && header&enetTimed != 0 {
			t.ack(remote, peer, channel, seq, binary.BigEndian.Uint16(packet[2:]))
		}
		if lost {
			t.drop(s)
			return
		}
	}
	for _, payload := range payloads {
		t.dispatchAppData(remote, payload)
	}
}

var enetLengths = [...]int{0, 8, 48, 44, 8, 4, 6, 8, 24, 8, 12, 16, 24}

func lengthField(kind byte) int {
	if kind == enetReliable {
		return 4
	}
	return 6
}

func (s *enetSession) activateLocked() {
	if !s.active && !s.closed {
		s.active = true
		s.lastSeen = time.Now()
		close(s.ready)
	}
}

func (s *enetSession) acceptReliable(channel byte, seq, count uint16, data []byte, out [][]byte) (bool, [][]byte) {
	r := &s.receive[channel]
	delta := int16(seq - (r.received + 1))
	if delta < 0 {
		return true, out
	}
	if delta >= maxENetWindow {
		return false, out
	}
	if delta > 0 {
		if _, ok := r.waiting[seq]; ok {
			return true, out
		}
		if len(r.waiting) >= 8 {
			return false, out
		}
		if r.waiting == nil {
			r.waiting = make(map[uint16]reliableFrame)
		}
		r.waiting[seq] = reliableFrame{append([]byte(nil), data...), count}
		return true, out
	}
	out = append(out, append([]byte(nil), data...))
	r.received += count
	for {
		start := r.received + 1
		frame, ok := r.waiting[start]
		if !ok {
			break
		}
		delete(r.waiting, start)
		out = append(out, frame.body)
		r.received += frame.count
	}
	return true, out
}

func (s *enetSession) acceptFragment(channel byte, seq uint16, c, data []byte, out [][]byte) (bool, [][]byte) {
	start := binary.BigEndian.Uint16(c[4:])
	count, index := binary.BigEndian.Uint32(c[8:]), binary.BigEndian.Uint32(c[12:])
	total, offset := binary.BigEndian.Uint32(c[16:]), binary.BigEndian.Uint32(c[20:])
	if count < 2 || count > maxENetWindow || index >= count || seq != start+uint16(index) ||
		total < 8 || total > maxAppFrame || offset >= total || uint32(len(data)) > total-offset {
		return false, out
	}
	r := &s.receive[channel]
	delta := int16(start - (r.received + 1))
	if delta < 0 {
		return true, out
	}
	if delta >= maxENetWindow || (delta > 0 && len(r.waiting) >= 8) {
		return false, out
	}
	if _, ready := r.waiting[start]; ready {
		return true, out
	}
	key := fragmentKey{channel, start}
	f := s.fragments[key]
	if f == nil {
		if len(s.fragments) >= 2 {
			return false, out
		}
		f = &enetFragments{parts: make([]fragmentPart, count), total: total, at: time.Now()}
		s.fragments[key] = f
	}
	if f.total != total || len(f.parts) != int(count) {
		return false, out
	}
	if f.parts[index].data != nil {
		return true, out
	}
	// Offsets cannot overlap, and the sum of stored fragments is bounded by total.
	for _, part := range f.parts {
		if len(part.data) != 0 && offset < part.offset+uint32(len(part.data)) &&
			part.offset < offset+uint32(len(data)) {
			return false, out
		}
	}
	f.parts[index] = fragmentPart{offset, append([]byte(nil), data...)}
	f.seen++
	if f.seen != len(f.parts) {
		return true, out
	}
	delete(s.fragments, key)
	full := make([]byte, 0, total)
	for _, part := range f.parts {
		if part.offset != uint32(len(full)) {
			return false, out
		}
		full = append(full, part.data...)
	}
	if len(full) != int(total) {
		return false, out
	}
	return s.acceptReliable(channel, start, uint16(count), full, out)
}

func (t *ENetTransport) acceptConnect(remote netip.AddrPort, c []byte) {
	slot, channels := binary.BigEndian.Uint16(c[4:]), binary.BigEndian.Uint32(c[16:])
	if slot >= enetNoPeer || channels < 1 || channels > 255 {
		return
	}
	s := t.session(remote, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.active || (s.connecting && bytes.Compare(s.cookie[:], c[40:44]) < 0) {
		s.mu.Unlock()
		return
	}
	if len(s.verify) != 0 && bytes.Equal(c[40:44], s.cookie[:]) {
		verify := s.verify
		s.mu.Unlock()
		_, _ = t.conn.WriteToUDPAddrPort(verify, remote)
		return
	}
	s.connecting = false
	copy(s.cookie[:], c[40:44])
	s.remoteSlot = slot
	s.outgoing, s.incoming = (c[6]+1)&3, (c[7]+1)&3
	s.mtu = int(min(max(binary.BigEndian.Uint32(c[8:]), 576), 1400))
	packet := make([]byte, 48)
	timedHeader(packet, slot|uint16(s.outgoing)<<12)
	copy(packet[4:], c[:44])
	packet[4] = enetAckFlag | enetVerify
	binary.BigEndian.PutUint16(packet[6:], 1)
	binary.BigEndian.PutUint16(packet[8:], s.localSlot)
	packet[10], packet[11] = s.outgoing, s.incoming
	binary.BigEndian.PutUint32(packet[12:], uint32(s.mtu))
	binary.BigEndian.PutUint32(packet[20:], 1)
	s.verify = packet
	s.mu.Unlock()
	_, _ = t.conn.WriteToUDPAddrPort(packet, remote)
}

func timedHeader(packet []byte, peer uint16) {
	binary.BigEndian.PutUint16(packet, peer|enetTimed)
	binary.BigEndian.PutUint16(packet[2:], uint16(time.Now().UnixMilli()))
}

func (t *ENetTransport) ack(remote netip.AddrPort, peer uint16, channel byte, seq, timestamp uint16) {
	var packet [10]byte
	binary.BigEndian.PutUint16(packet[:], peer)
	packet[2], packet[3] = enetAck, channel
	binary.BigEndian.PutUint16(packet[4:], seq)
	binary.BigEndian.PutUint16(packet[6:], seq)
	binary.BigEndian.PutUint16(packet[8:], timestamp)
	if t.trace != nil {
		t.trace(fmt.Sprintf("ENet output %s type=%d bytes=%d hex=%x", remote, enetAck, len(packet), packet[:]))
	}
	_, _ = t.conn.WriteToUDPAddrPort(packet[:], remote)
}

func (t *ENetTransport) dispatchAppData(remote netip.AddrPort, frame []byte) {
	if len(frame) < 8 || len(frame) > maxAppFrame || binary.BigEndian.Uint32(frame) != uint32(len(frame)) {
		if t.trace != nil {
			t.trace(fmt.Sprintf("ENet discarded app frame from %s (%d bytes)", remote, len(frame)))
		}
		return
	}
	kind := binary.BigEndian.Uint32(frame[4:])
	if kind == uint32(ControlProfile) {
		if d, err := ParseDiscovery(frame); err == nil && d.Kind == ControlProfile {
			t.onCommand(remote, kind, frame)
		} else if t.trace != nil {
			t.trace(fmt.Sprintf("ENet discarded profile frame from %s: %v", remote, err))
		}
		return
	}
	plain := make([]byte, len(frame)-8)
	if err := Decrypt(plain, frame[8:], Key); err == nil {
		t.onCommand(remote, kind, plain)
	} else if t.trace != nil {
		t.trace(fmt.Sprintf("ENet could not decrypt event %d from %s: %v", kind, remote, err))
	}
}

func (t *ENetTransport) Connect(ctx context.Context, target netip.AddrPort) error {
	if !validUnicast(target) {
		return ErrNotConnected
	}
	s := t.session(target, true)
	if s == nil {
		return errors.New("内网通控制连接数已达上限")
	}
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return nil
	}
	if !s.connecting && len(s.verify) == 0 {
		s.connecting = true
		_, _ = rand.Read(s.cookie[:])
	}
	s.mu.Unlock()
	for range 5 {
		s.mu.Lock()
		if s.active {
			s.mu.Unlock()
			return nil
		}
		if s.connecting && !s.closed {
			var packet [52]byte
			timedHeader(packet[:], enetNoPeer)
			packet[4], packet[5] = enetAckFlag|enetConnect, 255
			binary.BigEndian.PutUint16(packet[6:], 1)
			binary.BigEndian.PutUint16(packet[8:], s.localSlot)
			packet[10], packet[11] = 255, 255
			binary.BigEndian.PutUint32(packet[12:], 1400)
			binary.BigEndian.PutUint32(packet[16:], 65536)
			binary.BigEndian.PutUint32(packet[20:], 1)
			binary.BigEndian.PutUint32(packet[32:], 5000)
			binary.BigEndian.PutUint32(packet[36:], 2)
			binary.BigEndian.PutUint32(packet[40:], 2)
			copy(packet[44:], s.cookie[:])
			_, _ = t.conn.WriteToUDPAddrPort(packet[:], target)
		}
		s.mu.Unlock()
		timer := time.NewTimer(400 * time.Millisecond)
		select {
		case <-s.ready:
			timer.Stop()
			return nil
		case <-s.done:
			timer.Stop()
			return ErrNotConnected
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-t.stopped:
			timer.Stop()
			return context.Canceled
		case <-timer.C:
		}
	}
	return context.DeadlineExceeded
}

func (t *ENetTransport) connected(target netip.AddrPort) bool {
	return t.activeSession(target) != nil
}

func (t *ENetTransport) activeSession(target netip.AddrPort) *enetSession {
	s := t.session(target, false)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return nil
	}
	return s
}

func (t *ENetTransport) SendAppCommand(target netip.AddrPort, kind uint32, payload []byte) error {
	if len(payload) > maxAppFrame-8 {
		return errors.New("内网通消息超出大小限制")
	}
	if kind == uint32(ControlProfile) {
		err := t.sendReliable(target, payload, false)
		if t.trace != nil {
			t.trace(fmt.Sprintf("ENet send %s event %d (%d bytes): %v", target, kind, len(payload), err))
		}
		return err
	}
	cipher, err := Encrypt(nil, payload, Key)
	if err != nil {
		return err
	}
	frame := make([]byte, 8+len(cipher))
	binary.BigEndian.PutUint32(frame, uint32(len(frame)))
	binary.BigEndian.PutUint32(frame[4:], kind)
	copy(frame[8:], cipher)
	err = t.sendReliable(target, frame, false)
	if t.trace != nil {
		t.trace(fmt.Sprintf("ENet send %s event %d (%d bytes): %v", target, kind, len(frame), err))
	}
	return err
}

func (t *ENetTransport) Ping(target netip.AddrPort) error {
	return t.sendReliable(target, nil, true)
}

func (t *ENetTransport) sendReliable(target netip.AddrPort, frame []byte, ping bool) error {
	s := t.session(target, false)
	if s == nil {
		return ErrNotConnected
	}
	s.sendMu.Lock()
	s.mu.Lock()
	if !s.active || s.closed {
		s.mu.Unlock()
		s.sendMu.Unlock()
		return ErrNotConnected
	}
	peer := s.remoteSlot | uint16(s.outgoing)<<12
	chunk := s.mtu - 28
	channel, start := byte(0), s.seq+1
	count := max((len(frame)+chunk-1)/chunk, 1)
	if ping {
		channel, start = 255, s.ctrlSeq+1
		s.ctrlSeq++
	} else {
		if count > maxENetWindow {
			s.mu.Unlock()
			s.sendMu.Unlock()
			return errors.New("内网通消息超过此连接的分片容量")
		}
		s.seq += uint16(count)
	}
	packets := make([][]byte, 0, count)
	for index := range count {
		seq := start + uint16(index)
		var packet []byte
		switch {
		case ping:
			packet = make([]byte, 8)
			packet[4] = enetAckFlag | enetPing
		case count == 1:
			packet = make([]byte, 10+len(frame))
			packet[4] = enetAckFlag | enetReliable
			binary.BigEndian.PutUint16(packet[8:], uint16(len(frame)))
			copy(packet[10:], frame)
		default:
			offset := index * chunk
			body := frame[offset:min(offset+chunk, len(frame))]
			packet = make([]byte, 28+len(body))
			packet[4] = enetAckFlag | enetFragment
			binary.BigEndian.PutUint16(packet[8:], start)
			binary.BigEndian.PutUint16(packet[10:], uint16(len(body)))
			binary.BigEndian.PutUint32(packet[12:], uint32(count))
			binary.BigEndian.PutUint32(packet[16:], uint32(index))
			binary.BigEndian.PutUint32(packet[20:], uint32(len(frame)))
			binary.BigEndian.PutUint32(packet[24:], uint32(offset))
			copy(packet[28:], body)
		}
		packet[5] = channel
		binary.BigEndian.PutUint16(packet[6:], seq)
		s.pending[fragmentKey{channel, seq}] = struct{}{}
		packets = append(packets, packet)
	}
	s.mu.Unlock()
	s.sendMu.Unlock()
	keys := make([]fragmentKey, len(packets))
	for i := range keys {
		keys[i] = fragmentKey{channel, start + uint16(i)}
	}
	defer func() {
		s.mu.Lock()
		for _, key := range keys {
			delete(s.pending, key)
		}
		s.mu.Unlock()
	}()

	sent := false
	for _, pause := range []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, 800 * time.Millisecond} {
		for i, packet := range packets {
			s.mu.Lock()
			_, waiting := s.pending[keys[i]]
			closed := s.closed
			s.mu.Unlock()
			if closed {
				break
			}
			if !waiting {
				continue
			}
			timedHeader(packet, peer)
			if t.trace != nil {
				t.trace(fmt.Sprintf("ENet output %s type=%d bytes=%d hex=%x",
					target, packet[4]&15, len(packet), packet[:min(len(packet), 96)]))
			}
			if _, err := t.conn.WriteToUDPAddrPort(packet, target); err != nil {
				if sent {
					return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
				}
				return err
			}
			sent = true
		}
		timer := time.NewTimer(pause)
	wait:
		for {
			s.mu.Lock()
			finished := true
			for _, key := range keys {
				if _, waiting := s.pending[key]; waiting {
					finished = false
					break
				}
			}
			s.mu.Unlock()
			if finished {
				timer.Stop()
				return nil
			}
			select {
			case <-s.ackWake:
			case <-timer.C:
				break wait
			case <-t.stopped:
				timer.Stop()
				if sent {
					return ErrDeliveryUncertain
				}
				return ErrNotConnected
			case <-s.done:
				timer.Stop()
				if sent {
					return ErrDeliveryUncertain
				}
				return ErrNotConnected
			}
		}
	}
	t.drop(s)
	if sent {
		return ErrDeliveryUncertain
	}
	return ErrNotConnected
}

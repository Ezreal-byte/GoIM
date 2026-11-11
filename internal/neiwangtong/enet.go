package neiwangtong

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

const (
	enetCmdAcknowledge uint8 = 1
	enetCmdConnect     uint8 = 2
	enetCmdVerify      uint8 = 3
	enetCmdDisconnect  uint8 = 4
	enetCmdPing        uint8 = 5
	enetCmdSendReliable uint8 = 6
	enetCmdSendFragment uint8 = 8

	enetFlagAcknowledge uint8 = 0x80

	maxFragmentSize = 1372
)

type enetSession struct {
	remoteAddr    *net.UDPAddr
	remoteSlot    uint16
	toRemote      uint8
	fromRemote    uint8
	outgoing      uint16
	cookie        [4]byte
	established   atomic.Bool
	lastSeen      time.Time
	mu            sync.Mutex
	reliableSeq   uint16
	fragMap       map[uint32][][]byte
}

type ENetTransport struct {
	conn       *net.UDPConn
	localAddr  *net.UDPAddr
	sessions   map[string]*enetSession
	mu         sync.Mutex
	closing    atomic.Bool
	onCommand  func(addr *net.UDPAddr, appKind uint32, payload []byte)
}

func NewENetTransport(bind netip.Addr, port int, onCommand func(addr *net.UDPAddr, appKind uint32, payload []byte)) (*ENetTransport, error) {
	var laddr *net.UDPAddr
	if bind.IsValid() && bind.Is4() {
		laddr = &net.UDPAddr{IP: bind.AsSlice(), Port: port}
	} else {
		laddr = &net.UDPAddr{Port: port}
	}
	conn, err := net.ListenUDP("udp4", laddr)
	if err != nil {
		return nil, err
	}
	t := &ENetTransport{
		conn:      conn,
		localAddr: conn.LocalAddr().(*net.UDPAddr),
		sessions:  make(map[string]*enetSession),
		onCommand: onCommand,
	}
	go t.readLoop()
	return t, nil
}

func (t *ENetTransport) Close() error {
	t.closing.Store(true)
	return t.conn.Close()
}

func (t *ENetTransport) getOrCreateSession(addr *net.UDPAddr) *enetSession {
	key := addr.String()
	t.mu.Lock()
	defer t.mu.Unlock()
	sess, ok := t.sessions[key]
	if !ok {
		sess = &enetSession{
			remoteAddr: addr,
			fragMap:    make(map[uint32][][]byte),
			lastSeen:   time.Now(),
		}
		t.sessions[key] = sess
	}
	return sess
}

func (t *ENetTransport) readLoop() {
	var buf [4096]byte
	for {
		n, remote, err := t.conn.ReadFromUDP(buf[:])
		if err != nil {
			if t.closing.Load() {
				return
			}
			continue
		}
		if n < 4 {
			continue
		}
		t.handlePacket(remote, buf[:n])
	}
}

func (t *ENetTransport) handlePacket(remote *net.UDPAddr, b []byte) {
	first := binary.BigEndian.Uint16(b[0:2])
	timed := (first & 0x8000) != 0

	// Initial connect from peer
	if first == 0x8fff && len(b) == 52 && b[4] == 0x82 && b[5] == 255 {
		t.handleIncomingConnect(remote, b)
		return
	}

	sess := t.getOrCreateSession(remote)
	sess.mu.Lock()
	sess.lastSeen = time.Now()
	sess.mu.Unlock()

	i := 2
	if timed {
		i = 4
	}

	for i+4 <= len(b) {
		cmdWithFlag := b[i]
		cmd := cmdWithFlag & 0x0f
		channel := b[i+1]
		seq := binary.BigEndian.Uint16(b[i+2 : i+4])

		cmdLengths := map[uint8]int{1: 8, 2: 48, 3: 44, 4: 8, 5: 4, 6: 6, 8: 24}
		lenHead, ok := cmdLengths[cmd]
		if !ok || i+lenHead > len(b) {
			return
		}

		// Acknowledge if requested
		if (cmdWithFlag & enetFlagAcknowledge) != 0 {
			t.sendAck(sess, channel, seq, b)
		}

		switch cmd {
		case enetCmdAcknowledge:
			if channel == 255 && !sess.established.Load() {
				sess.established.Store(true)
			}
		case enetCmdVerify:
			// Verified connection
			sess.established.Store(true)
			t.sendAck(sess, channel, seq, b)
		case enetCmdSendReliable:
			ext := binary.BigEndian.Uint16(b[i+4 : i+6])
			off := i + lenHead
			if off+int(ext) <= len(b) && ext >= 8 {
				appData := b[off : off+int(ext)]
				t.dispatchAppData(remote, appData)
			}
			i += lenHead + int(ext)
			continue
		case enetCmdSendFragment:
			ext := binary.BigEndian.Uint16(b[i+6 : i+8])
			count := binary.BigEndian.Uint32(b[i+8 : i+12])
			index := binary.BigEndian.Uint32(b[i+12 : i+16])
			total := binary.BigEndian.Uint32(b[i+16 : i+20])
			_ = count
			off := i + lenHead
			if off+int(ext) <= len(b) {
				data := make([]byte, ext)
				copy(data, b[off:off+int(ext)])
				sess.mu.Lock()
				parts := sess.fragMap[total]
				if parts == nil {
					parts = make([][]byte, count)
					sess.fragMap[total] = parts
				}
				if index < uint32(len(parts)) {
					parts[index] = data
				}
				// Check if complete
				complete := true
				var fullLen int
				for _, p := range parts {
					if p == nil {
						complete = false
						break
					}
					fullLen += len(p)
				}
				if complete && uint32(fullLen) == total {
					delete(sess.fragMap, total)
					full := make([]byte, 0, total)
					for _, p := range parts {
						full = append(full, p...)
					}
					sess.mu.Unlock()
					t.dispatchAppData(remote, full)
				} else {
					sess.mu.Unlock()
				}
			}
			i += lenHead + int(ext)
			continue
		}
		i += lenHead
	}
}

func (t *ENetTransport) handleIncomingConnect(remote *net.UDPAddr, b []byte) {
	remoteSlot := binary.BigEndian.Uint16(b[8:10])
	if remoteSlot >= 0xfff {
		return
	}
	sess := t.getOrCreateSession(remote)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	copy(sess.cookie[:], b[44:48])
	sess.remoteSlot = remoteSlot
	if b[10] == 255 {
		sess.toRemote = 0
	} else {
		sess.toRemote = (b[10] + 1) & 3
	}
	if b[11] == 255 {
		sess.fromRemote = 0
	} else {
		sess.fromRemote = (b[11] + 1) & 3
	}
	sess.outgoing = remoteSlot | (uint16(sess.toRemote) << 12)

	// Send VERIFY_CONNECT (Command 3)
	packet := make([]byte, 48)
	binary.BigEndian.PutUint16(packet[0:2], 0x8000|sess.outgoing)
	binary.BigEndian.PutUint16(packet[2:4], uint16(time.Now().UnixMilli()&0xffff))
	packet[4] = 0x83 // VERIFY_CONNECT | ACK_FLAG
	packet[5] = 255
	binary.BigEndian.PutUint16(packet[6:8], 1)
	packet[10] = sess.toRemote
	packet[11] = sess.fromRemote
	copy(packet[12:48], b[12:48])

	_, _ = t.conn.WriteToUDP(packet, remote)
	sess.established.Store(true)
}

func (t *ENetTransport) sendAck(sess *enetSession, channel uint8, seq uint16, original []byte) {
	ack := make([]byte, 10)
	binary.BigEndian.PutUint16(ack[0:2], sess.outgoing)
	ack[2] = 1 // Acknowledge command
	ack[3] = channel
	binary.BigEndian.PutUint16(ack[4:6], seq)
	binary.BigEndian.PutUint16(ack[6:8], seq)
	if len(original) >= 4 {
		copy(ack[8:10], original[2:4])
	}
	_, _ = t.conn.WriteToUDP(ack, sess.remoteAddr)
}

func (t *ENetTransport) dispatchAppData(remote *net.UDPAddr, appData []byte) {
	if len(appData) < 8 {
		return
	}
	appLen := binary.BigEndian.Uint32(appData[0:4])
	appKind := binary.BigEndian.Uint32(appData[4:8])
	if int(appLen) > len(appData) {
		return
	}
	cipher := appData[8:appLen]
	plain := make([]byte, len(cipher))
	if err := Decrypt(plain, cipher, Key); err != nil {
		return
	}
	if t.onCommand != nil {
		t.onCommand(remote, appKind, plain)
	}
}

// Connect initiates ENet handshake to remote peer if not yet connected
func (t *ENetTransport) Connect(ctx context.Context, target *net.UDPAddr) (*enetSession, error) {
	sess := t.getOrCreateSession(target)
	if sess.established.Load() {
		return sess, nil
	}

	packet := make([]byte, 52)
	binary.BigEndian.PutUint16(packet[0:2], 0x8fff)
	binary.BigEndian.PutUint16(packet[2:4], uint16(time.Now().UnixMilli()&0xffff))
	packet[4] = 0x82 // Connect command | ACK
	packet[5] = 255  // Channel 255
	binary.BigEndian.PutUint16(packet[6:8], 1)
	binary.BigEndian.PutUint16(packet[8:10], 1) // outgoing peer ID
	packet[10] = 255
	packet[11] = 255
	binary.BigEndian.PutUint32(packet[12:16], 1400) // MTU
	binary.BigEndian.PutUint32(packet[16:20], 4096) // Window
	binary.BigEndian.PutUint32(packet[20:24], 2)    // Channels
	var cookie [4]byte
	_, _ = rand.Read(cookie[:])
	copy(packet[44:48], cookie[:])

	_, err := t.conn.WriteToUDP(packet, target)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sess.established.Load() {
			return sess, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return sess, nil
}

// SendAppCommand encrypts payload and sends via ENet reliable channel (fragmenting if needed)
func (t *ENetTransport) SendAppCommand(target *net.UDPAddr, appKind uint32, plainPayload []byte) error {
	cipher, err := Encrypt(nil, plainPayload, Key)
	if err != nil {
		return err
	}

	totalAppLen := 8 + len(cipher)
	appData := make([]byte, totalAppLen)
	binary.BigEndian.PutUint32(appData[0:4], uint32(totalAppLen))
	binary.BigEndian.PutUint32(appData[4:8], appKind)
	copy(appData[8:], cipher)

	sess := t.getOrCreateSession(target)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.reliableSeq++
	seq := sess.reliableSeq

	if len(appData) <= maxFragmentSize {
		// Single reliable packet (Command 6)
		pkt := make([]byte, 10+len(appData))
		binary.BigEndian.PutUint16(pkt[0:2], 0x8000|sess.outgoing)
		binary.BigEndian.PutUint16(pkt[2:4], uint16(time.Now().UnixMilli()&0xffff))
		pkt[4] = 0x86 // Send Reliable | ACK
		pkt[5] = 0    // Channel 0
		binary.BigEndian.PutUint16(pkt[6:8], seq)
		binary.BigEndian.PutUint16(pkt[8:10], uint16(len(appData)))
		copy(pkt[10:], appData)
		_, err = t.conn.WriteToUDP(pkt, target)
		return err
	}

	// Fragmented reliable packet (Command 8)
	fragCount := (len(appData) + maxFragmentSize - 1) / maxFragmentSize
	totalLen := uint32(len(appData))
	for idx := 0; idx < fragCount; idx++ {
		off := idx * maxFragmentSize
		end := off + maxFragmentSize
		if end > len(appData) {
			end = len(appData)
		}
		fragData := appData[off:end]

		headerLen := 28
		pkt := make([]byte, headerLen+len(fragData))
		binary.BigEndian.PutUint16(pkt[0:2], 0x8000|sess.outgoing)
		binary.BigEndian.PutUint16(pkt[2:4], uint16(time.Now().UnixMilli()&0xffff))
		pkt[4] = 0x88 // Send Fragment | ACK
		pkt[5] = 0
		binary.BigEndian.PutUint16(pkt[6:8], seq+uint16(idx))
		binary.BigEndian.PutUint16(pkt[8:10], seq)
		binary.BigEndian.PutUint16(pkt[10:12], uint16(len(fragData)))
		binary.BigEndian.PutUint32(pkt[12:16], uint32(fragCount))
		binary.BigEndian.PutUint32(pkt[16:20], uint32(idx))
		binary.BigEndian.PutUint32(pkt[20:24], totalLen)
		binary.BigEndian.PutUint32(pkt[24:28], uint32(off))
		copy(pkt[headerLen:], fragData)

		if _, err = t.conn.WriteToUDP(pkt, target); err != nil {
			return err
		}
	}
	return nil
}

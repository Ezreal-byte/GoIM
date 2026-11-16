package neiwangtong

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var loopback = netip.MustParseAddr("127.0.0.1")

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "tcp" {
		conn, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		return conn.Addr().(*net.TCPAddr).Port
	}
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(loopback, 0)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func waitEvent[T any](t *testing.T, events <-chan T) T {
	t.Helper()
	select {
	case value := <-events:
		return value
	case <-time.After(6 * time.Second):
		t.Fatal("timed out waiting for a native protocol event")
		var zero T
		return zero
	}
}

func TestNativeDiscoveryIsNotACompletedHandshake(t *testing.T) {
	peer, err := Start(Config{
		Name: "Receiver", BindIP: loopback, DisableBroadcast: true,
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	stub, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(loopback, 0)))
	if err != nil {
		t.Fatal(err)
	}
	defer stub.Close()
	target := stub.LocalAddr().(*net.UDPAddr).AddrPort()
	go func() {
		buf := make([]byte, 512)
		if _, addr, err := stub.ReadFromUDPAddrPort(buf); err == nil {
			reply := Discovery{Kind: Reply, ID: [16]byte{91}, Broadcast: loopback}
			packet, _ := reply.MarshalBinary()
			_, _ = stub.WriteToUDPAddrPort(packet, addr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := peer.Probe(ctx, target, int(target.Port()), 0); err == nil {
		t.Fatal("a discovery reply was treated as a verified control connection")
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if p := peer.peers[keyForDiscovery([16]byte{91})]; p == nil || p.peer.Online {
		t.Fatalf("unexpected peer readiness: %+v", p)
	}
}

func TestNativeDiscoveryReplyUsesRequestInterface(t *testing.T) {
	peer, err := Start(Config{
		Name: "Receiver", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{11},
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	requester, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(loopback, 0)))
	if err != nil {
		t.Fatal(err)
	}
	defer requester.Close()
	_ = requester.SetDeadline(time.Now().Add(2 * time.Second))
	request := Discovery{
		Kind: Announce, ID: [16]byte{12},
		Broadcast: netip.AddrFrom4([4]byte{127, 0, 0, 255}),
	}
	peer.mu.Lock()
	wantBroadcast := peer.selfDisc.Broadcast
	peer.mu.Unlock()
	packet, err := request.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	target := netip.AddrPortFrom(loopback, uint16(peer.cfg.DiscoveryPort))
	if _, err := requester.WriteToUDPAddrPort(packet, target); err != nil {
		t.Fatal(err)
	}
	var buf [discoverySize + 1]byte
	n, _, err := requester.ReadFromUDPAddrPort(buf[:])
	if err != nil {
		t.Fatal(err)
	}
	reply, err := ParseDiscovery(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if reply.Kind != Reply || reply.Broadcast != wantBroadcast {
		t.Fatalf("discovery reply = %+v, want Reply with sender broadcast %s", reply, wantBroadcast)
	}
}

func TestProfileBootstrapPromotesToMainControlConnection(t *testing.T) {
	peers := make(chan Peer, 16)
	trace := make(chan string, 256)
	callbacks := Callbacks{Peer: func(p Peer) { peers <- p }}
	traceLine := func(line string) {
		select {
		case trace <- line:
		default:
		}
	}
	first := Config{
		Name: "First", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{21},
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
		Callbacks: callbacks, Trace: traceLine,
	}
	second := Config{
		Name: "Second", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{22},
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
		Callbacks: callbacks, Trace: traceLine,
	}
	first.RemoteControlPort = second.ControlPort
	second.RemoteControlPort = first.ControlPort
	firstService, err := Start(first)
	if err != nil {
		t.Fatal(err)
	}
	defer firstService.Close()
	secondService, err := Start(second)
	if err != nil {
		t.Fatal(err)
	}
	defer secondService.Close()

	profileTarget := netip.AddrPortFrom(loopback, uint16(firstService.selfDisc.Trailer))
	discovery := firstService.selfDisc
	discovery.Kind = Reply
	controlTarget := netip.AddrPortFrom(loopback, uint16(second.RemoteControlPort))
	if _, ok := secondService.recordDiscovery(discovery, netip.AddrPort{}, controlTarget, AvatarPort); !ok {
		t.Fatal("could not seed profile bootstrap identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	secondService.kickHandshake(firstService.selfID, profileTarget)

	seen := make(map[string]bool)
	var traceLines []string
	for !seen[firstService.selfID] || !seen[secondService.selfID] {
		select {
		case peer := <-peers:
			seen[peer.ID] = peer.Online
		case line := <-trace:
			traceLines = append(traceLines, line)
			if len(traceLines) > 80 {
				traceLines = traceLines[len(traceLines)-80:]
			}
		case <-ctx.Done():
			t.Fatalf("reverse profile bootstrap did not confirm both peers: first=%t second=%t\ntrace:\n%s",
				seen[firstService.selfID], seen[secondService.selfID], strings.Join(traceLines, "\n"))
		}
	}
	if !secondService.profileENet.connected(profileTarget) {
		t.Fatal("profile bootstrap did not use the advertised temporary ENet endpoint")
	}
}

func TestServiceLocalLoopback(t *testing.T) {
	texts := make(chan string, 4)
	offers := make(chan XMLSendFile, 4)
	avatars := make(chan []byte, 8)
	peers := make(chan Peer, 16)
	aCfg := Config{
		Name: "Receiver", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{1},
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
		Callbacks: Callbacks{
			Text:          func(_ string, _ uint64, text string) { texts <- text },
			Files:         func(_ string, offer XMLSendFile) { offers <- offer },
			Peer:          func(p Peer) { peers <- p },
			AvatarUpdated: func(_ string, _ uint64, png []byte) { avatars <- png },
		},
	}
	bCfg := Config{
		Name: "Sender", BindIP: loopback, DisableBroadcast: true, ID: [16]byte{2},
		DiscoveryPort: freePort(t, "udp"), ControlPort: freePort(t, "udp"), TransferPort: freePort(t, "tcp"),
		AvatarPNG: createTestPNG(),
	}
	aCfg.RemoteControlPort = bCfg.ControlPort
	bCfg.RemoteControlPort = aCfg.ControlPort
	a, err := Start(aCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Start(bCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := a.Probe(ctx, netip.AddrPortFrom(loopback, uint16(bCfg.DiscoveryPort)), bCfg.ControlPort, bCfg.TransferPort); err != nil {
		t.Fatal(err)
	}
	first := waitEvent(t, peers)
	if first.ID != b.selfID || first.Name != "Sender" || !first.Online {
		t.Fatalf("first profile = %+v", first)
	}
	if got := waitEvent(t, avatars); !bytes.Equal(got, bCfg.AvatarPNG) {
		t.Fatal("first avatar differs")
	}
	// Wait for the other side's profile on the same verified control session.
	if err := b.Probe(ctx, netip.AddrPortFrom(loopback, uint16(aCfg.DiscoveryPort)), aCfg.ControlPort, aCfg.TransferPort); err != nil {
		t.Fatal(err)
	}
	message := "本机消息 & <文本> 😀 " + string(bytes.Repeat([]byte("a"), 3000))
	if err := b.SendText(a.selfID, message); err != nil {
		t.Fatal(err)
	}
	if got := waitEvent(t, texts); got != message {
		t.Fatal("reassembled message differs")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	payload := []byte("synthetic native file bytes")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := b.SendFileOffer(a.selfID, 888, "项目 & 文件.txt", int64(len(payload)), time.Now().Unix(), source); err != nil {
		t.Fatal(err)
	}
	offer := waitEvent(t, offers)
	if offer.FileID != 888 || offer.TaskID != 888 || offer.Name != "项目 & 文件.txt" {
		t.Fatalf("offer: %+v", offer)
	}
	dest := filepath.Join(dir, "received.txt")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("downloaded before acceptance: %v", err)
	}
	if err := a.AcceptFile(ctx, b.selfID, offer.TaskID, offer.FileID, offer.Size, dest, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("file data mismatch: %v", err)
	}
	b.UpdateProfile(Peer{Name: "Changed & ready", Sign: "在线", Status: StatusAway}, nil)
	var next Peer
	updateDeadline := time.After(6 * time.Second)
	for next.Name != "Changed & ready" {
		select {
		case next = <-peers:
		case <-updateDeadline:
			t.Fatalf("profile update did not arrive; last profile: %+v", next)
		}
	}
	if next.Name != "Changed & ready" || next.Sign != "在线" || next.Status != StatusAway {
		t.Fatalf("profile update: %+v", next)
	}
	avatarDeadline := time.After(6 * time.Second)
	for {
		select {
		case avatar := <-avatars:
			if len(avatar) == 0 {
				return
			}
		case <-avatarDeadline:
			t.Fatal("avatar removal did not clear the image")
		}
	}
}

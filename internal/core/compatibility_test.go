package core

import (
	"net"
	"net/netip"
	"testing"
	"time"

	"goim/internal/domain"
)

func freeCompatibilityPort(t *testing.T) int {
	t.Helper()
	for range 16 {
		packet, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		port := packet.LocalAddr().(*net.UDPAddr).Port
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		_ = packet.Close()
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	t.Fatal("no port available for both UDP and TCP")
	return 0
}

func TestCompatibilityPortsRollBackAndWaitForIPMSGTransfer(t *testing.T) {
	n, err := New(Options{DataDir: t.TempDir(), Listen: "127.0.0.1:0", DisableDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	initial := n.Snapshot()
	if initial.Settings.IPMSG.Enabled || initial.Settings.Neiwangtong.Enabled || initial.IPMSGStatus.Active {
		t.Fatal("fresh install started a compatibility service")
	}
	first := domain.CompatibilityConfig{Enabled: true, Port: freeCompatibilityPort(t)}
	if err := n.setCompatibility(domain.TransportIPMSG, first); err != nil {
		t.Fatal(err)
	}
	if s := n.Snapshot(); s.Settings.IPMSG != first || !s.IPMSGStatus.Active {
		t.Fatalf("IPMSG not active: %+v", s.IPMSGStatus)
	}
	blocker, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	blocked := domain.CompatibilityConfig{Enabled: true, Port: blocker.Addr().(*net.TCPAddr).Port}
	if err := n.setCompatibility(domain.TransportIPMSG, blocked); err == nil {
		t.Fatal("saved a port without a usable TCP listener")
	}
	if s := n.Snapshot(); s.Settings.IPMSG != first || !s.IPMSGStatus.Active || s.IPMSGStatus.Problem == "" {
		t.Fatalf("port conflict discarded the working listener or error: %+v", s.IPMSGStatus)
	}
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	// A classic IPMSG client sends discovery without GoIM's self-filter marker.
	if _, err := peer.WriteToUDPAddrPort([]byte("1:1:fixture:ipmsg-fixture:1:Fixture"), netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(first.Port))); err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	var reply [1024]byte
	if _, from, err := peer.ReadFromUDPAddrPort(reply[:]); err != nil || int(from.Port()) != first.Port {
		t.Fatalf("old IPMSG listener did not answer after rollback: %s, %v", from, err)
	}
	if err := n.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{
			ID: "active", LegacyTransport: domain.TransportIPMSG, Status: "transferring",
		})
	}); err != nil {
		t.Fatal(err)
	}
	next := domain.CompatibilityConfig{Enabled: true, Port: freeCompatibilityPort(t)}
	if err := n.setCompatibility(domain.TransportIPMSG, next); err != nil {
		t.Fatal(err)
	}
	if s := n.Snapshot(); s.Settings.IPMSG != first || s.IPMSGStatus.Pending == nil || *s.IPMSGStatus.Pending != next {
		t.Fatalf("active transfer did not defer rebind: %+v", s.IPMSGStatus)
	}
	n.setTransfer("active", "completed", 1, 0, "")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := n.Snapshot(); s.Settings.IPMSG == next && s.IPMSGStatus.Active && s.IPMSGStatus.Pending == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("deferred rebind did not finish: %+v", n.Snapshot().IPMSGStatus)
}

func TestGoIMConnectUsesTLSWithIPMSGActive(t *testing.T) {
	start := func() *Node {
		n, err := New(Options{DataDir: t.TempDir(), Listen: "127.0.0.1:0", DisableDiscovery: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = n.Close() })
		return n
	}
	a, b := start(), start()
	for _, n := range []*Node{a, b} {
		if err := n.setCompatibility(domain.TransportIPMSG, domain.CompatibilityConfig{Enabled: true, Port: freeCompatibilityPort(t)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.connectVia(domain.TransportGoIM, b.Address()); err != nil {
		t.Fatal(err)
	}
	want := b.Snapshot().Self.ID
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := a.Snapshot()
		p := s.Peer(want)
		if p.Online {
			if p.Transport != domain.TransportGoIM || p.ClientType != domain.ClientGoIM || len(s.Conversations) != 0 {
				t.Fatalf("TLS connection was misclassified or created a chat: %+v", s)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("GoIM TLS peer did not connect with the separate IPMSG adapter running")
}

func TestCompatibilityMigrationKeepsIPMSGPreferenceAndChatData(t *testing.T) {
	dir := t.TempDir()
	opts := Options{DataDir: dir, Listen: "127.0.0.1:0", DisableDiscovery: true, DisableIPMSG: true}
	n, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.change(func(s *domain.State) {
		s.Settings.CompatibilityConfigured = false
		s.Settings.IPMSG = domain.CompatibilityConfig{}
		s.Conversations = []domain.Conversation{
			{ID: "discovered", Kind: "direct"},
			{ID: "draft", Kind: "direct", Draft: "retain"},
			{ID: "opened", Kind: "direct", Opened: true},
			{ID: "history", Kind: "direct"},
		}
		s.Messages = []domain.Message{{ID: "text", ConversationID: "history", Text: "retain"}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n, err = New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	s := n.Snapshot()
	if !s.Settings.IPMSG.Enabled || s.Settings.IPMSG.Port != domain.IPMSGPort || s.IPMSGStatus.Active || !s.IPMSGStatus.Unavailable {
		t.Fatalf("legacy preference or override lost: %+v %+v", s.Settings.IPMSG, s.IPMSGStatus)
	}
	if !s.Conversation("discovered").Hidden || s.Conversation("draft").Hidden ||
		s.Conversation("opened").Hidden || s.Conversation("history").Hidden || len(s.Messages) != 1 {
		t.Fatalf("conversation upgrade was lossy: %+v", s.Conversations)
	}
}

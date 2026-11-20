package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

	"goim/internal/domain"
)

// This isolated DNS fixture answers only a single SRV owner and its A target.
// Neither name is resolvable via the system resolver.
func discoveryDNSFixture(t *testing.T, target net.TCPAddr) string {
	t.Helper()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { socket.Close() })
	go func() {
		request := make([]byte, 2048)
		for {
			n, remote, err := socket.ReadFromUDP(request)
			if err != nil {
				return
			}
			if n < 17 {
				continue
			}
			i := 12
			name := ""
			for i < n && request[i] > 0 {
				size := int(request[i])
				i++
				if i+size >= n {
					return
				}
				name += string(request[i:i+size]) + "."
				i += size
			}
			if i+5 > n {
				continue
			}
			qtype := binary.BigEndian.Uint16(request[i+1 : i+3])
			reply := append([]byte(nil), request[:i+5]...)
			binary.BigEndian.PutUint16(reply[2:4], 0x8180)
			clear(reply[6:12])
			var answer []byte
			switch {
			case name == "_goim._tcp.office.test." && qtype == 33:
				answer = []byte{0, 0, 0, 0, byte(target.Port >> 8), byte(target.Port), 5, 'e', 'n', 't', 'r', 'y', 6, 'o', 'f', 'f', 'i', 'c', 'e', 4, 't', 'e', 's', 't', 0}
			case name == "entry.office.test." && qtype == 1:
				answer = target.IP.To4()
			}
			if answer != nil {
				reply[7] = 1
				reply = append(reply, 0xc0, 12, 0, byte(qtype), 0, 1, 0, 0, 0, 60, 0, byte(len(answer)))
				reply = append(reply, answer...)
			}
			_, _ = socket.WriteToUDP(reply, remote)
		}
	}()
	return socket.LocalAddr().String()
}

func TestDNSDiscoveryIsOptIn(t *testing.T) {
	fresh := domain.DefaultSettings(t.TempDir())
	var upgraded domain.Settings
	if err := json.Unmarshal([]byte(`{"Network":"auto","DisableAutoDiscovery":false}`), &upgraded); err != nil {
		t.Fatal(err)
	}
	for _, settings := range []domain.Settings{fresh, upgraded} {
		n := &Node{ctx: t.Context(), discovery: true}
		n.data.State.Settings = settings
		if settings.DisableAutoDiscovery || n.dnsEnabledLocked() {
			t.Fatal("expected ordinary LAN discovery with DNS discovery off")
		}
		for _, force := range []bool{false, true} {
			n.tickDNSDiscovery("auto", force)
			if n.dnsDirectory != nil {
				t.Fatal("an unconfigured startup or refresh started DNS discovery")
			}
		}
	}
}

func TestDNSDiscoveryPreservesFixedInterfacePolicyUntilEnabled(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range addrs {
			local, ok := a.(*net.IPNet)
			if !ok || local.IP.To4() == nil {
				continue
			}
			n := &Node{discovery: true}
			s := &n.data.State.Settings
			s.Network = iface.Name
			dst := &net.TCPAddr{IP: local.IP, Port: 47842}
			remote := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 17), Port: 47842}
			if n.allowedConnection(dst, remote) || !n.allowedConnection(dst, dst) {
				t.Fatal("unconfigured DNS changed the selected interface's subnet policy")
			}
			source, err := n.selectedIPFor("[2001:db8::17]:47842")
			if err != nil || !source.Equal(local.IP) {
				t.Fatalf("default fixed-interface source = %v, %v; want its IPv4 address", source, err)
			}
			s.DNSDiscovery.Server = "192.0.2.53:53"
			if !n.allowedConnection(dst, remote) {
				t.Fatal("configured DNS did not enable routed peers on the selected interface")
			}
			s.DisableAutoDiscovery = true
			if n.allowedConnection(dst, remote) {
				t.Fatal("disabled discovery kept accepting routed peers")
			}
			return
		}
	}
	t.Skip("no IPv4 loopback interface")
}

func TestDNSDiscoverySettingsAndResolver(t *testing.T) {
	node := testNode(t, "settings")
	cfg := domain.DNSDiscoveryConfig{Server: discoveryDNSFixture(t, net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 48765}), Domain: " OFFICE.Test. "}
	set := func(c domain.DNSDiscoveryConfig) error {
		return node.execute(domain.Command{Kind: domain.SetSetting, Setting: "dns_discovery", DNSDiscovery: c})
	}
	if err := set(cfg); err != nil {
		t.Fatal(err)
	}
	got := node.Snapshot().Settings.DNSDiscovery
	if got.Server != cfg.Server || got.Domain != "office.test" {
		t.Fatalf("saved %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	seeds, err := lookupDNSSeeds(ctx, got, nil)
	if _, ok := seeds["127.0.0.1:48765"]; err != nil || !ok || len(seeds) != 1 {
		t.Fatalf("custom resolver returned %v, %v", seeds, err)
	}
	for _, bad := range []domain.DNSDiscoveryConfig{{Server: "bad name"}, {Server: "10.1.1.1:0"}, {Server: "10.1.1.1", Domain: "bad..name"}} {
		if err := set(bad); err == nil || node.Snapshot().Settings.DNSDiscovery != got {
			t.Fatal("invalid settings replaced saved DNS", bad)
		}
	}
	dir := node.DataDir()
	node.Close()
	reopened := testNodeAt(t, "settings", dir)
	if reopened.Snapshot().Settings.DNSDiscovery != got {
		t.Fatal("configuration lost on restart")
	}
	if err := reopened.setting(domain.Command{Setting: "dns_discovery"}); err != nil || reopened.Snapshot().Settings.DNSDiscovery.Server != "" {
		t.Fatalf("disable: %v", err)
	}
}

func TestDNSDirectoryRequiresActiveRegistrationAndVerifiedSeed(t *testing.T) {
	cfg := domain.DNSDiscoveryConfig{Server: "10.1.1.53:53", Domain: "office.test"}
	d := &dnsDirectory{config: cfg, seeds: map[string]string{"10.1.1.20:47842": "other-identity"}}
	pc := &peerConn{peer: domain.Peer{ID: "entry", Address: "10.1.1.20:47842"}}
	n := &Node{discovery: true, dnsDirectory: d, connections: map[string]*peerConn{"entry": pc}}
	n.data.State.Settings.DNSDiscovery = cfg
	msg := envelope{Type: "dns_members", Target: cfg.Domain}
	pc.dnsRequestDirectory = d
	pc.dnsRequested = time.Now()
	n.handleDNSDirectory(pc, msg)
	if !pc.dnsReply.IsZero() {
		t.Fatal("accepted a roster from an unverified peer")
	}
	d.seeds["10.1.1.20:47842"] = pc.peer.ID
	n.data.State.Self.Presence = domain.PresenceInvisible
	n.handleDNSDirectory(pc, msg)
	if !pc.dnsReply.IsZero() {
		t.Fatal("accepted roster while invisible")
	}
	n.data.State.Self.Presence = domain.PresenceOnline
	pc.dnsRequestDirectory = nil
	n.handleDNSDirectory(pc, msg)
	if !pc.dnsReply.IsZero() {
		t.Fatal("accepted unsolicited roster")
	}
	pc.dnsRequestDirectory = d
	n.handleDNSDirectory(pc, msg)
	accepted := pc.dnsReply
	if accepted.IsZero() {
		t.Fatal("ignored solicited roster from the verified seed")
	}
	n.handleDNSDirectory(pc, msg)
	if accepted != pc.dnsReply {
		t.Fatal("accepted the same reply twice")
	}
}

func TestDNSDiscoveryDefaultDomain(t *testing.T) {
	cfg, err := (domain.DNSDiscoveryConfig{Server: "10.0.0.53"}).Normalized()
	if err != nil || cfg.Server != "10.0.0.53:53" || cfg.Domain != domain.DefaultDiscoveryDomain {
		t.Fatal(cfg, err)
	}
}

package core

import (
	"crypto/ed25519"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"time"

	"goim/internal/domain"
)

const discoveryPort = 47841

func (n *Node) selectedInterface() *net.Interface {
	n.mu.Lock()
	name := n.data.State.Settings.Network
	n.mu.Unlock()
	if name == "" || name == "auto" {
		return nil
	}
	iface, _ := net.InterfaceByName(name)
	return iface
}
func (n *Node) selectedIP() net.IP {
	iface := n.selectedInterface()
	if iface == nil {
		if bound, ok := n.listener.Addr().(*net.TCPAddr); ok && !bound.IP.IsUnspecified() {
			return bound.IP
		}
		return nil
	}
	addresses, _ := iface.Addrs()
	for _, a := range addresses {
		if v, ok := a.(*net.IPNet); ok && v.IP.To4() != nil {
			return v.IP.To4()
		}
	}
	return nil
}
func (n *Node) allowedRemote(address net.Addr) bool {
	iface := n.selectedInterface()
	if iface == nil {
		return true
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	addresses, _ := iface.Addrs()
	for _, a := range addresses {
		if network, ok := a.(*net.IPNet); ok && network.Contains(ip) {
			return true
		}
	}
	return false
}
func (n *Node) readDiscovery(c *net.UDPConn) {
	buf := make([]byte, 8192)
	for {
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		size, remote, err := c.ReadFromUDP(buf)
		if err != nil {
			if n.ctx.Err() != nil {
				return
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return
		}
		n.handleDiscoveryMessage(buf[:size], remote.IP)
	}
}

func (n *Node) handleDiscoveryMessage(body []byte, remote net.IP) {
	var announcement announcement
	if json.Unmarshal(body, &announcement) != nil || announcement.Version != protocolVersion ||
		announcement.Peer.ID == keyID(n.key.Public().(ed25519.PublicKey)) || len(announcement.Peer.PublicKey) != ed25519.PublicKeySize ||
		announcement.Peer.ID != keyID(announcement.Peer.PublicKey) {
		return
	}
	signature := announcement.Signature
	announcement.Signature = nil
	raw, _ := json.Marshal(announcement)
	if !ed25519.Verify(announcement.Peer.PublicKey, raw, signature) {
		return
	}
	_, port, err := net.SplitHostPort(announcement.Peer.Address)
	if err != nil || port == "" {
		return
	}
	address := net.JoinHostPort(remote.String(), port)
	n.mu.Lock()
	connection := n.connections[announcement.Peer.ID]
	connected := connection != nil && (connection.peer.Address == address || time.Since(time.Unix(0, connection.lastRead.Load())) < 12*time.Second)
	invisible := n.data.State.Self.Presence == domain.PresenceInvisible
	n.mu.Unlock()
	if !connected && !invisible {
		n.dialTo(address, announcement.Peer.ID, true)
	}
}
func (n *Node) discoveryLoop() {
	defer n.wg.Done()
	address := &net.UDPAddr{IP: net.IPv4(239, 192, 77, 77), Port: discoveryPort}
	var connections []*net.UDPConn
	networkName := "__initial__"
	previousNetwork := ""
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()
	refresh := func(enabled bool, name string) {
		if !enabled {
			name = "\x00"
		}
		if name == networkName {
			return
		}
		networkName = name
		for _, connection := range connections {
			connection.Close()
		}
		connections = nil
		if !enabled {
			return
		}
		for _, iface := range n.discoveryInterfaces() {
			c, err := net.ListenMulticastUDP("udp4", &iface, address)
			if err != nil {
				continue
			}
			connections = append(connections, c)
			_ = c.SetReadBuffer(64 << 10)
			n.async(func() { n.readDiscovery(c) })
		}
		if len(connections) == 0 {
			networkName = "__initial__"
		}
	}
	announce := func() {
		s := n.snapshot(false)
		enabled := n.discovery && !s.Settings.DisableAutoDiscovery && s.Self.Presence != domain.PresenceInvisible
		network := s.Settings.Network + "|" + networkFingerprint(n.discoveryInterfaces())
		if previousNetwork != "" && previousNetwork != network {
			n.refreshNetworkConnections()
		}
		previousNetwork = network
		refresh(enabled, network)
		if enabled {
			p := s.Self
			p.Avatar = ""
			// Keep the signed announcement compatible with previous clients.
			p.Signature, p.Presence = "", ""
			p.Details, p.Version = domain.ProfileDetails{}, ""
			p.Address = n.advertisedAddress()
			p.RecallSupported = false
			p.PrintSupported = false
			p.RelaySupported = false
			a := announcement{Version: protocolVersion, Peer: p}
			raw, _ := json.Marshal(a)
			a.Signature = ed25519.Sign(n.key, raw)
			raw, _ = json.Marshal(a)
			for _, iface := range n.discoveryInterfaces() {
				addresses, _ := iface.Addrs()
				for _, local := range addresses {
					if network, ok := local.(*net.IPNet); ok && network.IP.To4() != nil {
						if c, err := net.DialUDP("udp4", &net.UDPAddr{IP: network.IP}, address); err == nil {
							_, _ = c.Write(raw)
							c.Close()
						}
					}
				}
			}
		}
		n.mu.Lock()
		var ids []string
		for id := range n.connections {
			ids = append(ids, id)
		}
		n.mu.Unlock()
		for _, id := range ids {
			_ = n.send(id, envelope{Type: "ping"})
			n.replay(id)
		}
		n.exchangePeerRoutes()
		n.mu.Lock()
		queued := make(map[string]bool)
		for _, delivery := range n.data.Outbox {
			transport, _, online := n.pendingRouteLocked(delivery)
			if transport == domain.TransportGoIM && n.connections[delivery.PeerID] == nil || transport != domain.TransportGoIM && online {
				queued[delivery.PeerID] = true
			}
		}
		n.mu.Unlock()
		for id := range queued {
			n.flushPeer(id)
		}
		for _, p := range s.Peers {
			if p.Transport == domain.TransportGoIM && !p.Online && p.Address != "" {
				n.dialTo(p.Address, p.ID, true)
			}
		}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	announce()
	for {
		select {
		case <-ticker.C:
			announce()
		case <-n.discoveryWake:
			networkName = "__initial__"
			announce()
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) discoveryInterfaces() []net.Interface {
	if selected := n.selectedInterface(); selected != nil {
		return []net.Interface{*selected}
	}
	interfaces, _ := net.Interfaces()
	var usable []net.Interface
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil {
				usable = append(usable, iface)
				break
			}
		}
	}
	return usable
}

func networkFingerprint(interfaces []net.Interface) string {
	var parts []string
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil {
				parts = append(parts, iface.Name+":"+address.String())
			}
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func (n *Node) refreshNetworkConnections() {
	valid := make(map[string]bool)
	for _, iface := range n.discoveryInterfaces() {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok {
				valid[network.IP.String()] = true
			}
		}
	}
	n.mu.Lock()
	var connections []*peerConn
	for _, connection := range n.connections {
		connections = append(connections, connection)
	}
	network := n.data.State.Settings.Network
	n.mu.Unlock()
	for _, connection := range connections {
		if local, ok := connection.conn.LocalAddr().(*net.TCPAddr); ok && !local.IP.IsLoopback() && !valid[local.IP.String()] {
			connection.conn.Close()
		}
	}
	if bound, ok := n.listener.Addr().(*net.TCPAddr); ok {
		ip := browserIP(bound, network).String()
		n.mu.Lock()
		changed := n.localIP != ip
		n.localIP = ip
		if changed {
			n.data.State.Revision++
		}
		n.mu.Unlock()
		if changed {
			n.signal()
		}
	}
}

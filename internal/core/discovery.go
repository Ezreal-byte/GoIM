package core

import (
	"crypto/ed25519"
	"encoding/json"
	"net"
	"time"

	"goim/internal/domain"
)

func (n *Node) selectedInterface() *net.Interface {
	name := n.snapshot(false).Settings.Network
	if name == "" || name == "auto" {
		return nil
	}
	iface, _ := net.InterfaceByName(name)
	return iface
}
func (n *Node) selectedIP() net.IP {
	iface := n.selectedInterface()
	if iface == nil {
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
		var a announcement
		if json.Unmarshal(buf[:size], &a) != nil || a.Version != protocolVersion || a.Peer.ID == keyID(n.key.Public().(ed25519.PublicKey)) || len(a.Peer.PublicKey) != ed25519.PublicKeySize || a.Peer.ID != keyID(a.Peer.PublicKey) {
			continue
		}
		signature := a.Signature
		a.Signature = nil
		raw, _ := json.Marshal(a)
		if !ed25519.Verify(a.Peer.PublicKey, raw, signature) {
			continue
		}
		_, port, _ := net.SplitHostPort(a.Peer.Address)
		if port == "" {
			continue
		}
		n.mu.Lock()
		connected := n.connections[a.Peer.ID] != nil
		invisible := n.data.State.Self.Presence == domain.PresenceInvisible
		n.mu.Unlock()
		if !connected && !invisible {
			n.dialTo(net.JoinHostPort(remote.IP.String(), port), a.Peer.ID, true)
		}
	}
}
func (n *Node) discoveryLoop() {
	defer n.wg.Done()
	address := &net.UDPAddr{IP: net.IPv4(239, 192, 77, 77), Port: 47841}
	var connection *net.UDPConn
	networkName := "__initial__"
	defer func() {
		if connection != nil {
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
		if connection != nil {
			connection.Close()
			connection = nil
		}
		if !enabled {
			return
		}
		c, err := net.ListenMulticastUDP("udp4", n.selectedInterface(), address)
		if err != nil {
			n.notice("自动发现暂不可用，可输入 IP 连接")
			return
		}
		connection = c
		_ = c.SetReadBuffer(64 << 10)
		n.async(func() { n.readDiscovery(c) })
	}
	announce := func() {
		s := n.snapshot(false)
		enabled := n.discovery && !s.Settings.DisableAutoDiscovery && s.Self.Presence != domain.PresenceInvisible
		refresh(enabled, s.Settings.Network)
		if enabled {
			p := s.Self
			p.Avatar = ""
			// Keep the signed announcement compatible with previous clients.
			p.Signature, p.Presence = "", ""
			p.Address = n.advertisedAddress()
			p.RecallSupported = false
			a := announcement{Version: protocolVersion, Peer: p}
			raw, _ := json.Marshal(a)
			a.Signature = ed25519.Sign(n.key, raw)
			raw, _ = json.Marshal(a)
			var local *net.UDPAddr
			if ip := n.selectedIP(); ip != nil {
				local = &net.UDPAddr{IP: ip}
			}
			if c, err := net.DialUDP("udp4", local, address); err == nil {
				_, _ = c.Write(raw)
				c.Close()
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
		for _, p := range s.Peers {
			if !p.Online && p.Address != "" {
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
		case <-n.ctx.Done():
			return
		}
	}
}

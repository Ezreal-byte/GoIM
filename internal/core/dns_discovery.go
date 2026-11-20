package core

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"goim/internal/domain"
)

const (
	maxDNSSeeds     = 32
	maxDNSMembers   = 256
	dnsJoinInterval = 15 * time.Second
)

type dnsMember struct {
	ID      string
	Address string
}

// A configuration or interface change retires the entire directory, including
// authenticated seeds, outstanding DNS results and membership leases.
type dnsDirectory struct {
	config     domain.DNSDiscoveryConfig
	network    string
	ctx        context.Context
	cancel     context.CancelFunc
	nextLookup time.Time
	lookingUp  bool
	seeds      map[string]string // Exact DNS endpoints -> identity verified by outbound TLS.
	localSeed  bool
	status     string
}

func (n *Node) dnsActiveLocked() bool {
	s := &n.data.State
	return !n.closing && n.dnsDirectory != nil && n.discovery && !s.Settings.DisableAutoDiscovery &&
		s.Self.Presence != domain.PresenceInvisible && n.dnsDirectory.config == s.Settings.DNSDiscovery
}

func (n *Node) tickDNSDiscovery(network string, force bool) {
	n.mu.Lock()
	s := &n.data.State
	cfg := s.Settings.DNSDiscovery
	enabled := !n.closing && n.discovery && !s.Settings.DisableAutoDiscovery && s.Self.Presence != domain.PresenceInvisible && cfg.Server != ""
	d := n.dnsDirectory
	var leave []*peerConn
	var previousZone string
	if d != nil && (!enabled || d.config != cfg || d.network != network) {
		for _, pc := range n.connections {
			if pc.dnsRequestDirectory == d {
				leave = append(leave, pc)
				pc.dnsRequestDirectory = nil
			}
			if pc.dnsMembership == d {
				pc.dnsMembership = nil
			}
		}
		previousZone = d.config.Domain
		d.cancel()
		n.dnsDirectory, d = nil, nil
	}
	if enabled && d == nil {
		ctx, cancel := context.WithCancel(n.ctx)
		d = &dnsDirectory{config: cfg, network: network, ctx: ctx, cancel: cancel, status: "正在查询 DNS 服务记录"}
		n.dnsDirectory = d
	}
	lookup := d != nil && !d.lookingUp && (force || !time.Now().Before(d.nextLookup))
	if lookup {
		d.lookingUp = true
	}
	n.mu.Unlock()
	for _, pc := range leave {
		_ = pc.send(envelope{Type: "dns_leave", Target: previousZone})
	}
	if d == nil {
		return
	}
	if lookup {
		n.async(func() { n.resolveDirectory(d) })
	}
	n.contactDNSSeeds(d)
}

func lookupDNSSeeds(ctx context.Context, cfg domain.DNSDiscoveryConfig, local net.IP) (map[string]string, error) {
	resolver := net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var dialer net.Dialer
		if local != nil {
			if strings.HasPrefix(network, "tcp") {
				dialer.LocalAddr = &net.TCPAddr{IP: local}
			} else {
				dialer.LocalAddr = &net.UDPAddr{IP: local}
			}
		}
		return dialer.DialContext(ctx, network, cfg.Server)
	}}
	_, records, err := resolver.LookupSRV(ctx, "goim", "tcp", cfg.Domain+".")
	if err != nil {
		return nil, err
	}
	seeds := make(map[string]string)
	var lookupErr error
	for i, record := range records {
		if i >= maxDNSSeeds || len(seeds) >= maxDNSSeeds || ctx.Err() != nil {
			break
		}
		if record.Port == 0 || record.Target == "." {
			continue
		}
		ips, err := resolver.LookupNetIP(ctx, "ip", record.Target)
		if err != nil {
			lookupErr = err
			continue
		}
		for _, ip := range ips {
			addr := netip.AddrPortFrom(ip.Unmap(), record.Port)
			if validDNSEndpoint(addr) {
				seeds[addr.String()] = ""
				if len(seeds) >= maxDNSSeeds {
					break
				}
			}
		}
	}
	if len(seeds) == 0 && lookupErr != nil {
		return nil, lookupErr
	}
	if len(seeds) == 0 && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return seeds, nil
}

func validDNSEndpoint(addr netip.AddrPort) bool {
	ip := addr.Addr().Unmap()
	return addr.IsValid() && addr.Port() != 0 && (ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

func (n *Node) localDNSEndpoints() map[string]bool {
	addr := n.listener.Addr().(*net.TCPAddr).AddrPort()
	local := make(map[string]bool)
	if !addr.Addr().IsUnspecified() {
		local[netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port()).String()] = true
		return local
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if prefix, err := netip.ParsePrefix(a.String()); err == nil {
			local[netip.AddrPortFrom(prefix.Addr().Unmap(), addr.Port()).String()] = true
		}
	}
	return local
}

func (n *Node) resolveDirectory(d *dnsDirectory) {
	ctx, cancel := context.WithTimeout(d.ctx, 8*time.Second)
	defer cancel()
	var seeds map[string]string
	localIP, err := n.selectedIPFor(d.config.Server)
	if err == nil {
		seeds, err = lookupDNSSeeds(ctx, d.config, localIP)
	}
	local := n.localDNSEndpoints()
	n.mu.Lock()
	d.lookingUp = false
	if n.dnsDirectory != d || !n.dnsActiveLocked() {
		n.mu.Unlock()
		return
	}
	d.nextLookup = time.Now().Add(time.Minute)
	status := ""
	if len(seeds) == 0 {
		status = "未找到 GoIM 服务记录"
		var dnsErr *net.DNSError
		if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
			status = "查询 DNS 失败，请检查服务器地址和网络"
		}
		d.nextLookup = time.Now().Add(20 * time.Second)
	}
	d.localSeed = false
	for address := range seeds {
		if local[address] {
			d.localSeed = true
			delete(seeds, address)
		} else {
			seeds[address] = d.seeds[address]
		}
	}
	if d.localSeed {
		status = "本机是发现入口"
	}
	d.seeds = seeds
	if status != "" {
		n.setDNSStatusLocked(d, status)
	}
	n.mu.Unlock()
	n.contactDNSSeeds(d)
}

func (n *Node) setDNSStatusLocked(d *dnsDirectory, status string) {
	if d.status != status {
		d.status = status
		n.data.State.Revision++
		n.signal()
	}
}

func (n *Node) contactDNSSeeds(d *dnsDirectory) {
	n.mu.Lock()
	if n.dnsDirectory != d || !n.dnsActiveLocked() {
		n.mu.Unlock()
		return
	}
	var dial []string
	var join []*peerConn
	now := time.Now()
	status := "已找到 GoIM 入口，正在连接"
	connected, compatible, registered := false, false, false
	for address, id := range d.seeds {
		// An already-open connection can predate the DNS configuration.
		if id == "" {
			for _, pc := range n.connections {
				if pc.outbound && pc.conn.RemoteAddr().String() == address {
					id = pc.peer.ID
					d.seeds[address] = id
					break
				}
			}
		}
		if pc := n.connections[id]; pc != nil && (pc.peer.Address == address || pc.outbound && pc.conn.RemoteAddr().String() == address) {
			connected = true
			compatible = compatible || pc.dnsCapable
			registered = registered || pc.dnsRequestDirectory == d && !pc.dnsReply.IsZero() && now.Sub(pc.dnsReply) < 3*dnsJoinInterval
			if pc.dnsCapable && (pc.dnsRequestDirectory != d || now.Sub(pc.dnsRequested) >= dnsJoinInterval) {
				if pc.dnsRequestDirectory != d {
					pc.dnsReply = time.Time{}
				}
				pc.dnsRequestDirectory = d
				pc.dnsRequested = now
				join = append(join, pc)
			}
		} else {
			dial = append(dial, address)
		}
	}
	if !d.localSeed && len(d.seeds) > 0 {
		switch {
		case registered:
			status = "已连接发现入口"
		case compatible:
			status = "已连接，等待入口确认发现域"
		case connected:
			status = "入口版本未支持跨网段发现"
		}
		n.setDNSStatusLocked(d, status)
	}
	n.mu.Unlock()
	for _, address := range dial {
		n.dialToContext(d.ctx, address, "", true)
	}
	for _, pc := range join {
		_ = pc.send(envelope{Type: "dns_join", Target: d.config.Domain})
	}
}

func (n *Node) handleDNSDirectory(pc *peerConn, e envelope) {
	n.mu.Lock()
	d := n.dnsDirectory
	if !n.dnsActiveLocked() || e.Target != d.config.Domain || n.connections[pc.peer.ID] != pc {
		n.mu.Unlock()
		return
	}
	if e.Type == "dns_leave" {
		pc.dnsMembership = nil
		n.mu.Unlock()
		return
	}
	if e.Type == "dns_join" {
		if !d.localSeed || pc.dnsMembership == d && time.Since(pc.dnsJoined) < dnsJoinInterval/2 {
			n.mu.Unlock()
			return
		}
		pc.dnsMembership, pc.dnsJoined = d, time.Now()
		members := make([]dnsMember, 0)
		for _, other := range n.connections {
			if other == pc || other.dnsMembership != d || time.Since(other.dnsJoined) > 3*dnsJoinInterval {
				continue
			}
			members = append(members, dnsMember{other.peer.ID, other.peer.Address})
			if len(members) == maxDNSMembers {
				break
			}
		}
		n.mu.Unlock()
		_ = pc.send(envelope{Type: "dns_members", Target: d.config.Domain, DNSMembers: members})
		return
	}
	trusted := false
	for address, id := range d.seeds {
		if id == pc.peer.ID && (pc.peer.Address == address || pc.outbound && pc.conn.RemoteAddr().String() == address) {
			trusted = true
			break
		}
	}
	if !trusted || pc.dnsRequestDirectory != d || pc.dnsRequested.IsZero() || time.Since(pc.dnsRequested) > dnsJoinInterval || len(e.DNSMembers) > maxDNSMembers {
		n.mu.Unlock()
		return
	}
	// Accept a single roster per poll, including an empty one.
	if !pc.dnsRequested.After(pc.dnsReply) {
		n.mu.Unlock()
		return
	}
	pc.dnsReply = time.Now()
	if !d.localSeed {
		n.setDNSStatusLocked(d, "已连接发现入口")
	}
	var candidates []dnsMember
	for _, member := range e.DNSMembers {
		if len(member.ID) != 32 || member.ID == n.data.State.Self.ID || n.connections[member.ID] != nil || n.dialing[member.Address] {
			continue
		}
		if _, err := hex.DecodeString(member.ID); err != nil {
			continue
		}
		addr, err := netip.ParseAddrPort(member.Address)
		if err != nil || !validDNSEndpoint(addr) || !addr.Addr().Unmap().IsGlobalUnicast() || addr.Addr().Zone() != "" {
			continue
		}
		candidates = append(candidates, member)
		if len(candidates) == 8 {
			break
		}
	}
	n.mu.Unlock()
	for _, member := range candidates {
		n.dialToContext(d.ctx, member.Address, member.ID, true)
	}
}

package localsend

import (
	"encoding/json"
	"net"
	"strings"
	"time"

	"golang.org/x/net/ipv6"
)

// IPv6 link-local multicast is advertised alongside IPv4 by the v2.2 client.
var groupIPv6 = net.ParseIP("ff12::fd3a:e420")

func (s *Service) startIPv6Discovery() {
	conn, err := s.listenMulticast("udp6")
	if err != nil {
		return
	}
	s.udp6 = conn
	packet := ipv6.NewPacketConn(conn)
	_ = packet.SetControlMessage(ipv6.FlagInterface, true)
	s.spawn(func() {
		buf := make([]byte, 8192)
		for {
			n, control, addr, err := packet.ReadFrom(buf)
			if err != nil {
				return
			}
			from, ok := addr.(*net.UDPAddr)
			if !ok {
				continue
			}
			if from.IP.IsLinkLocalUnicast() && from.Zone == "" && control != nil {
				if iface, err := net.InterfaceByIndex(control.IfIndex); err == nil {
					from.Zone = iface.Name
				}
			}
			host := from.IP.String()
			if from.Zone != "" {
				host += "%" + from.Zone
			}
			s.handleAnnouncement(buf[:n], host, false)
		}
	})
	s.spawn(func() {
		joined := make(map[int]net.Interface)
		ticker := time.NewTicker(18 * time.Second)
		defer ticker.Stop()
		for {
			raw, _ := json.Marshal(announcement{info: s.selfInfo(), Announce: true})
			current := make(map[int]net.Interface)
			for _, iface := range s.interfaces() {
				if iface.Flags&net.FlagMulticast == 0 {
					continue
				}
				if _, found := joined[iface.Index]; !found {
					if err := packet.JoinGroup(&iface, &net.UDPAddr{IP: groupIPv6}); err != nil {
						continue
					}
				}
				current[iface.Index] = iface
				_, _ = packet.WriteTo(raw, &ipv6.ControlMessage{IfIndex: iface.Index, HopLimit: 1},
					&net.UDPAddr{IP: groupIPv6, Port: Port, Zone: iface.Name})
			}
			for index, iface := range joined {
				if _, found := current[index]; !found {
					_ = packet.LeaveGroup(&iface, &net.UDPAddr{IP: groupIPv6})
				}
			}
			joined = current
			s.expireDevices()
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake6:
			case <-ticker.C:
			}
		}
	})
}

func (s *Service) handleAnnouncement(raw []byte, host string, legacyFallback bool) {
	var packet announcement
	if !s.originAllowed(host) || json.Unmarshal(raw, &packet) != nil || !validInfo(packet.info) ||
		strings.EqualFold(packet.Fingerprint, s.fingerprint) {
		return
	}
	fingerprint := packet.Fingerprint
	if packet.Protocol == "https" {
		fingerprint = strings.ToUpper(fingerprint)
	}
	s.mu.Lock()
	known, found := s.peers[packet.Protocol+":"+fingerprint]
	s.mu.Unlock()
	if packet.Announce || !found || known.Address != net.JoinHostPort(host, portString(packet.Port)) {
		s.queueProbe(host, packet.info, legacyFallback && packet.Announce)
	} else {
		s.remember(host, packet.info, packet.Protocol == "https", false)
	}
}

func (s *Service) expireDevices() {
	expired := false
	s.mu.Lock()
	for id, p := range s.peers {
		if time.Since(p.seen) > 2*time.Minute {
			delete(s.peers, id)
			expired = true
		}
	}
	s.mu.Unlock()
	if expired {
		s.announceChange()
	}
}

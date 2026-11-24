package localsend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"goim/internal/domain"
	"golang.org/x/net/ipv4"
)

type seenDevice struct {
	domain.LocalSendDevice
	fingerprint string
	seen        time.Time
}

var group = net.IPv4(224, 0, 0, 167)

func portString(port int) string { return strconv.Itoa(port) }

func (s *Service) interfaces() []net.Interface {
	s.mu.Lock()
	name := s.network
	s.mu.Unlock()
	all, _ := net.Interfaces()
	var result []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && (name == "" || name == "auto" || name == iface.Name) {
			result = append(result, iface)
		}
	}
	return result
}

func (s *Service) originAllowed(host string) bool {
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if s.options.NoDiscovery {
		return ip.IsLoopback()
	}
	if ip.IsLoopback() {
		return true
	}
	s.mu.Lock()
	preferred := s.network
	s.mu.Unlock()
	if (preferred == "" || preferred == "auto") && isLAN(ip) {
		return true
	}
	for _, iface := range s.interfaces() {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if network, ok := addr.(*net.IPNet); ok && network.Contains(ip.AsSlice()) {
				return true
			}
		}
	}
	return false
}

func (s *Service) client(expected string, observed *string, headerTimeout time.Duration) *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			TLSClientConfig:     clientTLS(s.cert, expected, observed),
			TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: headerTimeout,
			DisableKeepAlives: true,
		},
	}
}

func request(ctx context.Context, client *http.Client, method, target string, body io.Reader, contentType string, size int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	return client.Do(req)
}

func readResponse(res *http.Response, value any) error {
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return transferError(res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxMetadata)).Decode(value)
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	var peer info
	if !decode(w, r, &peer) {
		return
	}
	if !validInfo(peer) {
		fail(w, http.StatusBadRequest, "Invalid device information")
		return
	}
	cert := certFingerprint(r)
	verified := cert != "" && strings.EqualFold(cert, peer.Fingerprint) && peer.Protocol == "https"
	if peer.Protocol == "http" || verified {
		s.remember(remoteIP(r), peer, verified, false)
	} else if cert == "" {
		s.queueProbe(remoteIP(r), peer, false)
	}
	s.serveInfo(w, r)
}

func (s *Service) serveInfo(w http.ResponseWriter, _ *http.Request) {
	local := s.selfInfo()
	local.Port, local.Protocol = 0, ""
	reply(w, local)
}

func (s *Service) remember(host string, peer info, verified, trustChange bool) {
	if !validInfo(peer) || !s.originAllowed(host) || strings.EqualFold(peer.Fingerprint, s.fingerprint) {
		return
	}
	fp := peer.Fingerprint
	if peer.Protocol == "https" {
		if !verified {
			return
		}
		fp = strings.ToUpper(fp)
	}
	endpoint := net.JoinHostPort(host, portString(peer.Port))
	id := peer.Protocol + ":" + fp
	if host == "127.0.0.1" && peer.Port == s.port {
		return
	}
	now := time.Now()
	device := domain.LocalSendDevice{ID: id, Name: peer.Alias, Model: peer.Model, Type: peer.Type, Address: endpoint, Plain: peer.Protocol == "http"}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	previous := s.endpoints[peer.Protocol+":"+endpoint]
	if previous != "" && previous != id {
		if !trustChange {
			if old, ok := s.peers[previous]; ok {
				old.Changed = true
				old.seen = now
				s.peers[previous] = old
				s.mu.Unlock()
				s.announceChange()
				return
			}
			// Keep the last pin even if the device has gone offline.
			device.Changed = true
		} else {
			delete(s.peers, previous)
		}
	}
	if len(s.peers) >= 512 {
		if _, found := s.peers[id]; !found {
			s.mu.Unlock()
			return
		}
	}
	old := s.peers[id]
	s.peers[id] = seenDevice{LocalSendDevice: device, fingerprint: fp, seen: now}
	if !device.Changed && (len(s.endpoints) < 2048 || previous != "") {
		s.endpoints[peer.Protocol+":"+endpoint] = id
	}
	s.mu.Unlock()
	if old.LocalSendDevice != device {
		s.announceChange()
	}
}

func (s *Service) probe(ctx context.Context, host string, peer info, manual bool) error {
	if !s.originAllowed(host) {
		return errors.New("地址不在所选局域网接口上")
	}
	endpoint := net.JoinHostPort(host, portString(peer.Port))
	ctx, cancel := context.WithTimeout(ctx, 3200*time.Millisecond)
	defer cancel()
	var observed string
	client := s.client(peer.Fingerprint, &observed, 2*time.Second)
	payload, _ := json.Marshal(s.selfInfo())
	res, err := request(ctx, client, http.MethodPost, targetURL(peer.Protocol, endpoint, "register", nil), bytes.NewReader(payload), "application/json", int64(len(payload)))
	if err != nil {
		return err
	}
	var received info
	if err := readResponse(res, &received); err != nil {
		return err
	}
	received.Port, received.Protocol = peer.Port, peer.Protocol
	if observed != "" {
		received.Fingerprint = observed
	}
	if !validInfo(received) {
		return errors.New("对端返回了无效的 LocalSend 信息")
	}
	s.remember(host, received, observed != "", manual)
	return nil
}

func (s *Service) AddDevice(raw string, plain bool) error {
	endpoint, err := localAddress(raw, Port)
	if err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(endpoint)
	scheme := "https"
	if plain {
		scheme = "http"
	}
	value, _ := strconv.Atoi(port)
	return s.probe(s.ctx, host, info{Port: value, Protocol: scheme}, true)
}

func (s *Service) queueProbe(host string, peer info, fallback bool) {
	select {
	case s.workers <- struct{}{}:
	default:
		return
	}
	if !s.spawn(func() {
		defer func() { <-s.workers }()
		if err := s.probe(s.ctx, host, peer, false); err != nil && fallback {
			s.sendAnnouncement(false, &net.UDPAddr{IP: net.ParseIP(host), Port: Port})
		}
	}) {
		<-s.workers
	}
}

func (s *Service) Refresh() {
	select {
	case s.wake6 <- struct{}{}:
	default:
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Scan() {
	s.Refresh()
	if !s.scanning.CompareAndSwap(false, true) {
		return
	}
	if !s.spawn(func() {
		defer s.scanning.Store(false)
		// Fallback for Wi-Fi networks which suppress multicast. Limit to one
		// IPv4 /24 and constrain probes to the selected LAN.
		ip := net.ParseIP(s.linkIP()).To4()
		if ip == nil || ip.IsLoopback() {
			return
		}
		var tasks = make(chan string)
		var finished = make(chan struct{}, 12)
		ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
		defer cancel()
		for range cap(finished) {
			go func() {
				defer func() { finished <- struct{}{} }()
				for host := range tasks {
					_ = s.probe(ctx, host, info{Port: Port, Protocol: "https"}, false)
				}
			}()
		}
		for i := 1; i < 255; i++ {
			if byte(i) == ip[3] || ctx.Err() != nil {
				continue
			}
			host := net.IPv4(ip[0], ip[1], ip[2], byte(i)).String()
			select {
			case tasks <- host:
			case <-ctx.Done():
			}
		}
		close(tasks)
		for range cap(finished) {
			<-finished
		}
	}) {
		s.scanning.Store(false)
	}
}

func (s *Service) listenMulticast(family string) (*net.UDPConn, error) {
	cfg := reusableMulticastSocket()
	socket, err := cfg.ListenPacket(s.ctx, family, ":"+portString(Port))
	if err != nil {
		return nil, err
	}
	return socket.(*net.UDPConn), nil
}

func (s *Service) startDiscovery() {
	udp, err := s.listenMulticast("udp4")
	if err != nil {
		s.mu.Lock()
		s.problem = "UDP 53317 无法监听。仍可通过设备地址添加对方。"
		s.mu.Unlock()
		return
	}
	s.udp = udp
	s.spawn(func() {
		buffer := make([]byte, 8192)
		for {
			n, from, err := udp.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			s.handleAnnouncement(buffer[:n], from.IP.String(), true)
		}
	})
	s.spawn(func() {
		packet := ipv4.NewPacketConn(udp)
		joined := make(map[int]net.Interface)
		ticker := time.NewTicker(18 * time.Second)
		defer ticker.Stop()
		for {
			current := make(map[int]net.Interface)
			for _, iface := range s.interfaces() {
				if iface.Flags&net.FlagMulticast == 0 {
					continue
				}
				if _, found := joined[iface.Index]; !found {
					if err := packet.JoinGroup(&iface, &net.UDPAddr{IP: group}); err != nil {
						continue
					}
				}
				current[iface.Index] = iface
			}
			for index, iface := range joined {
				if _, found := current[index]; !found {
					_ = packet.LeaveGroup(&iface, &net.UDPAddr{IP: group})
				}
			}
			joined = current
			s.sendAnnouncement(true, nil)
			s.expireDevices()
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
		}
	})
}

func (s *Service) sendAnnouncement(announce bool, destination *net.UDPAddr) {
	if s.udp == nil || s.ctx.Err() != nil {
		return
	}
	raw, _ := json.Marshal(announcement{info: s.selfInfo(), Announce: announce})
	if destination != nil {
		_, _ = s.udp.WriteToUDP(raw, destination)
		// Stock clients on networks without HTTP discovery also listen on the group.
	}
	for _, iface := range s.interfaces() {
		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok || network.IP.To4() == nil {
				continue
			}
			udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: network.IP})
			if err != nil {
				continue
			}
			packet := ipv4.NewPacketConn(udp)
			_ = packet.SetMulticastTTL(1)
			_ = packet.SetMulticastInterface(&iface)
			_, _ = udp.WriteToUDP(raw, &net.UDPAddr{IP: group, Port: Port})
			_ = udp.Close()
		}
	}
}

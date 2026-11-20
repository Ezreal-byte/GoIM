package domain

import (
	"errors"
	"net/netip"
	"strings"
)

const DefaultDiscoveryDomain = "goim.internal"

type DNSDiscoveryConfig struct {
	Server string
	Domain string
}

// Normalized accepts a literal resolver address, so finding the resolver never
// requires consulting another DNS server. An empty server disables discovery.
func (c DNSDiscoveryConfig) Normalized() (DNSDiscoveryConfig, error) {
	server := strings.TrimSpace(c.Server)
	if server == "" {
		return DNSDiscoveryConfig{}, nil
	}
	endpoint, err := netip.ParseAddrPort(server)
	if addr, parseErr := netip.ParseAddr(server); parseErr == nil {
		endpoint, err = netip.AddrPortFrom(addr, 53), nil
	}
	if err != nil || endpoint.Port() == 0 || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() || endpoint.Addr().Unmap() == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return DNSDiscoveryConfig{}, errors.New("请输入有效的 DNS 服务器 IP 或 IP:端口")
	}
	zone := strings.TrimSpace(c.Domain)
	if zone == "" {
		zone = DefaultDiscoveryDomain
	}
	zone = strings.TrimSuffix(strings.ToLower(zone), ".")
	// Leave room for the service and protocol labels in the absolute owner.
	if len(zone) > 253-len("_goim._tcp.") {
		return DNSDiscoveryConfig{}, errors.New("发现域名无效")
	}
	for _, label := range strings.Split(zone, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return DNSDiscoveryConfig{}, errors.New("发现域名无效")
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return DNSDiscoveryConfig{}, errors.New("发现域名无效")
			}
		}
	}
	return DNSDiscoveryConfig{Server: endpoint.String(), Domain: zone}, nil
}

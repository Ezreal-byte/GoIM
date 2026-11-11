// Package neiwangtong contains wire formats validated against the native
// Neiwangtong 3.4.3055 client. It does not implement the IPMSG adapter.
package neiwangtong

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"time"
)

const (
	DiscoveryPort uint16 = 9011
	ControlPort uint16 = 9012
	AvatarPort uint16 = 9013
	discoverySize = 304
	discoveryMagic = 0x5fc1d8ec
	discoveryTag = "#3#4#"
)

type DiscoveryKind uint32

const (
	Announce DiscoveryKind = 1
	Reply DiscoveryKind = 2
	ControlProfile DiscoveryKind = 4
)

// Discovery is the public discovery record. ProfileTag changes when the test
// client changes its profile, but its individual fields are not a revision clock.
type Discovery struct {
	Kind DiscoveryKind
	ID [16]byte
	ProfileTag [16]byte
	Broadcast netip.Addr
	Trailer uint16
}

var errDiscovery = errors.New("invalid native discovery record")

func ParseDiscovery(p []byte) (Discovery, error) {
	if len(p) != discoverySize || binary.BigEndian.Uint32(p) != discoverySize ||
		binary.BigEndian.Uint32(p[8:]) != 1 || binary.BigEndian.Uint32(p[12:]) != discoveryMagic ||
		string(p[24:29]) != discoveryTag || p[61] != '0' {
		return Discovery{}, errDiscovery
	}
	d := Discovery{
		Kind: DiscoveryKind(binary.BigEndian.Uint32(p[4:])),
		Broadcast: netip.AddrFrom4([4]byte(p[20:24])),
		ProfileTag: [16]byte(p[95:111]),
		Trailer: binary.BigEndian.Uint16(p[165:167]),
	}
	if !d.validKind() {
		return Discovery{}, errDiscovery
	}
	if _, err := hex.Decode(d.ID[:], p[29:61]); err != nil || d.ID == [16]byte{} {
		return Discovery{}, errDiscovery
	}
	return d, nil
}

func (d Discovery) validKind() bool {
	return d.Kind == Announce || d.Kind == Reply || d.Kind == ControlProfile
}

func (d Discovery) MarshalBinary() ([]byte, error) {
	if !d.validKind() || !d.Broadcast.Is4() || d.ID == [16]byte{} {
		return nil, errDiscovery
	}
	p := make([]byte, discoverySize)
	binary.BigEndian.PutUint32(p, discoverySize)
	binary.BigEndian.PutUint32(p[4:], uint32(d.Kind))
	binary.BigEndian.PutUint32(p[8:], 1)
	binary.BigEndian.PutUint32(p[12:], discoveryMagic)
	copy(p[20:24], d.Broadcast.AsSlice())
	copy(p[24:29], discoveryTag)
	hex.Encode(p[29:61], d.ID[:])
	p[61] = '0'
	copy(p[95:111], d.ProfileTag[:])
	binary.BigEndian.PutUint16(p[162:164], 500)
	binary.BigEndian.PutUint16(p[165:167], d.Trailer)
	return p, nil
}

func validUnicast(endpoint netip.AddrPort) bool {
	ip := endpoint.Addr()
	return ip.Is4() && !ip.IsUnspecified() && !ip.IsMulticast() &&
		ip != netip.AddrFrom4([4]byte{255, 255, 255, 255}) && endpoint.Port() != 0
}

// Probe sends one directed discovery request. It accepts only a reply from the
// requested endpoint; the broadcast address inside the record never controls a dial.
func Probe(ctx context.Context, bind netip.Addr, target netip.AddrPort, self Discovery) (Discovery, error) {
	if !validUnicast(target) || bind.IsValid() && !bind.Is4() {
		return Discovery{}, errors.New("native discovery requires IPv4 endpoints")
	}
	self.Kind = Announce
	payload, err := self.MarshalBinary()
	if err != nil {
		return Discovery{}, err
	}
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(netip.AddrPortFrom(bind, 0)))
	if err != nil {
		return Discovery{}, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err = conn.WriteToUDPAddrPort(payload, target); err != nil {
		return Discovery{}, err
	}
	var packet [discoverySize + 1]byte
	for {
		n, from, err := conn.ReadFromUDPAddrPort(packet[:])
		if err != nil {
			if ctx.Err() != nil {
				return Discovery{}, ctx.Err()
			}
			return Discovery{}, err
		}
		if from != target {
			continue
		}
		if reply, err := ParseDiscovery(packet[:n]); err == nil && reply.Kind == Reply && reply.ID != self.ID {
			return reply, nil
		}
	}
}

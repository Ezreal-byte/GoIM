//go:build linux || darwin

package ipmsg

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func listenUDP(network, address string) (*net.UDPConn, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		return raw.Control(func(fd uintptr) {
			_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
		})
	}}
	packet, err := config.ListenPacket(context.Background(), network, address)
	if err != nil {
		return nil, err
	}
	return packet.(*net.UDPConn), nil
}

//go:build windows

package ipmsg

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

func listenUDP(network, address string) (*net.UDPConn, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		// Winsock2 defines SO_EXCLUSIVEADDRUSE as the complement of SO_REUSEADDR.
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			socketErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, ^windows.SO_REUSEADDR, 1)
			if socketErr == nil {
				socketErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
			}
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	packet, err := config.ListenPacket(context.Background(), network, address)
	if err != nil {
		return nil, err
	}
	return packet.(*net.UDPConn), nil
}

//go:build windows

package ipmsg

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

func listenUDPShared(network, address string) (*net.UDPConn, error) {
	config := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		return raw.Control(func(fd uintptr) {
			_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
			_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
		})
	}}
	packet, err := config.ListenPacket(context.Background(), network, address)
	if err != nil {
		return nil, err
	}
	return packet.(*net.UDPConn), nil
}

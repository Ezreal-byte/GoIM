//go:build linux || darwin

package localsend

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func reusableMulticastSocket() net.ListenConfig {
	return net.ListenConfig{Control: func(_, _ string, conn syscall.RawConn) error {
		var option error
		err := conn.Control(func(fd uintptr) {
			option = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if option == nil {
				option = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			}
		})
		if err != nil {
			return err
		}
		return option
	}}
}

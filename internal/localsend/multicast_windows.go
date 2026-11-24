package localsend

import (
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

// Membership filters multicast traffic. Reuse also lets stock LocalSend
// own a membership on this host when its TCP port is different.
func reusableMulticastSocket() net.ListenConfig {
	return net.ListenConfig{Control: func(_, _ string, conn syscall.RawConn) error {
		var option error
		err := conn.Control(func(fd uintptr) {
			option = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
		})
		if err != nil {
			return err
		}
		return option
	}}
}

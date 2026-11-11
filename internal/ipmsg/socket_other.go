//go:build !windows && !linux && !darwin

package ipmsg

import "net"

func listenUDP(network, address string) (*net.UDPConn, error) {
	return net.ListenUDP(network, mustResolve(address))
}

func mustResolve(address string) *net.UDPAddr {
	addressValue, _ := net.ResolveUDPAddr("udp4", address)
	return addressValue
}

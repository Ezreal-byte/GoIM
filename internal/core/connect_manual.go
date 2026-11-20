package core

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"syscall"

	"goim/internal/domain"
)

func (n *Node) connectManual(command domain.Command) error {
	address := strings.TrimSpace(command.Address)
	n.mu.Lock()
	if n.connectionFeedback.Pending || n.closing {
		n.mu.Unlock()
		return nil
	}
	n.connectionFeedback = domain.ConnectionFeedback{
		Sequence: n.connectionFeedback.Sequence + 1, Address: address, Pending: true,
	}
	n.data.State.Revision++
	n.mu.Unlock()
	n.signal()
	n.async(func() {
		err := n.manualConnection(command.Transport, address)
		if n.ctx.Err() != nil {
			return
		}
		n.mu.Lock()
		n.connectionFeedback.Sequence++
		n.connectionFeedback.Pending = false
		if err != nil {
			n.connectionFeedback.Problem = manualConnectionProblem(err)
		}
		n.data.State.Revision++
		n.mu.Unlock()
		n.signal()
	})
	return nil
}

func (n *Node) manualConnection(transport, address string) error {
	if address == "" {
		return errors.New("请输入 IP 或 IP:端口")
	}
	switch transport {
	case domain.TransportAuto:
		endpoint, err := parseAutoEndpoint(address)
		if err != nil {
			return err
		}
		return n.connectAuto(endpoint)
	case domain.TransportGoIM:
		if _, _, err := net.SplitHostPort(address); err != nil {
			address = net.JoinHostPort(strings.Trim(address, "[]"), "47842")
		}
		return n.connectGoIM(address, "")
	case domain.TransportIPMSG, domain.TransportNeiwangtong:
		if ip, err := netip.ParseAddr(strings.Trim(address, "[]")); err == nil && ip.Unmap().Is4() {
			state := n.snapshot(false)
			port := state.Settings.IPMSG.Port
			if transport == domain.TransportNeiwangtong {
				port = state.Settings.Neiwangtong.Port
			}
			address = netip.AddrPortFrom(ip.Unmap(), uint16(port)).String()
		}
		if transport == domain.TransportIPMSG {
			return n.connectLegacy(address)
		}
		return n.connectNeiwangtong(address)
	default:
		return errors.New("未知连接方式")
	}
}

func manualConnectionProblem(err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &networkError) && networkError.Timeout():
		return "连接超时，请检查双方网络、防火墙和目标地址后重试。"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "目标端口拒绝连接，请确认对方 GoIM 已启动且端口正确。"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return "无法到达目标网络，请检查双方是否在可互通的局域网。"
	default:
		if problem := platformConnectionProblem(err); problem != "" {
			return problem
		}
		return err.Error()
	}
}

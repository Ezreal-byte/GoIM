package core

import (
	"errors"

	"golang.org/x/sys/windows"
)

func platformConnectionProblem(err error) string {
	switch {
	case errors.Is(err, windows.WSAECONNREFUSED):
		return "目标端口拒绝连接，请确认对方 GoIM 已启动且端口正确。"
	case errors.Is(err, windows.WSAENETUNREACH), errors.Is(err, windows.WSAEHOSTUNREACH):
		return "无法到达目标网络，请检查双方是否在可互通的局域网。"
	default:
		return ""
	}
}

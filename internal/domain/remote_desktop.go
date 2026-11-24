package domain

import "fmt"

const (
	StartRemoteDesktop  CommandKind = "start_remote_desktop"
	AcceptRemoteDesktop CommandKind = "accept_remote_desktop"
	RejectRemoteDesktop CommandKind = "reject_remote_desktop"
	EndRemoteDesktop    CommandKind = "end_remote_desktop"
)

type RemoteDesktopStatus string

const (
	RemoteDesktopPending    RemoteDesktopStatus = "pending"
	RemoteDesktopConnecting RemoteDesktopStatus = "connecting"
	RemoteDesktopActive     RemoteDesktopStatus = "active"
	RemoteDesktopEnded      RemoteDesktopStatus = "ended"
)

type RemoteDesktopSession struct {
	ID            string
	PeerID        string
	PeerName      string
	Reason        string
	Status        RemoteDesktopStatus
	Incoming      bool
	Control       bool
	Audio         bool
	OutputDevices []string
}

// RemoteDesktopRecord is the local chat history of a finished session.
type RemoteDesktopRecord struct {
	Incoming        bool
	Reason          string
	Connected       bool
	DurationSeconds int
}

func (r RemoteDesktopRecord) Outcome() string {
	switch r.Reason {
	case "ended":
		return "协助已结束"
	case "cancelled":
		return "协助已取消"
	case "declined":
		return "请求已拒绝"
	case "no_answer":
		return "请求已超时"
	case "disconnected":
		return "远程桌面连接已断开"
	case "permission":
		return "系统未授予屏幕或控制权限"
	case "unsupported":
		return "当前桌面环境不支持远程协助"
	case "busy":
		return "对方正在进行远程协助"
	case "locked":
		return "应用锁定，远程协助已结束"
	default:
		return "未能建立远程桌面连接"
	}
}

func (r RemoteDesktopRecord) Duration() string {
	return fmt.Sprintf("%02d:%02d", r.DurationSeconds/60, r.DurationSeconds%60)
}

type RemoteDesktopFrame struct {
	Width, Height int
	// BGRA is a tightly packed, immutable, top-down pixel buffer.
	BGRA []byte
}

// RemoteDesktopInput is local viewer input. The helper's wire grammar is
// intentionally independent of desktop UI and framework types.
type RemoteDesktopInput struct {
	Action    string `json:"action"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	Button    int    `json:"button,omitempty"`
	Down      bool   `json:"down,omitempty"`
	DeltaX    int    `json:"delta_x,omitempty"`
	DeltaY    int    `json:"delta_y,omitempty"`
	Key       string `json:"key,omitempty"`
	Modifiers int    `json:"modifiers,omitempty"`
	Volume    int    `json:"volume"`
	Muted     bool   `json:"muted,omitempty"`
	Device    string `json:"device,omitempty"`
}

// RemoteDesktopBackend is optional, so the desktop can also run with
// backends that do not ship a remote desktop engine.
type RemoteDesktopBackend interface {
	RemoteDesktopFrames(id string) <-chan RemoteDesktopFrame
	SendRemoteDesktopInput(id string, input RemoteDesktopInput) error
}

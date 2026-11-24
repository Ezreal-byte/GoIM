package domain

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

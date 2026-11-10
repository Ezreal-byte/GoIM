// Package domain is the typed boundary between the peer engine and desktop UI.
package domain

import "time"

type Peer struct {
	RecallSupported bool `json:"RecallSupported,omitempty"`
	ID              string
	Name            string
	Address         string
	Avatar          string
	ContactGroup    string
	Online          bool
	LastSeen        time.Time
	PublicKey       []byte
	Signature       string         `json:",omitempty"`
	Presence        Presence       `json:",omitempty"`
	Details         ProfileDetails `json:",omitzero"`
	Version         string         `json:",omitempty"`
	Transport       string         `json:",omitempty"` // empty for GoIM TLS; "ipmsg" for IP Messenger-family peers
}

// ProfileDetails contains optional information shared with connected GoIM peers.
type ProfileDetails struct {
	Gender     string `json:",omitempty"` // empty, male, female
	Company    string `json:",omitempty"`
	Department string `json:",omitempty"`
	Phone      string `json:",omitempty"`
	Mobile     string `json:",omitempty"`
	Email      string `json:",omitempty"`
}

type Presence string

const (
	PresenceOnline    Presence = "online"
	PresenceAway      Presence = "away"
	PresenceBusy      Presence = "busy"
	PresenceInvisible Presence = "invisible"
	PresenceOffline   Presence = "offline" // peer-facing state of an invisible contact
)

func (p Peer) VisibleOnline() bool {
	return p.Online && p.Presence != PresenceInvisible && p.Presence != PresenceOffline
}

type Conversation struct {
	ID        string
	Title     string
	Kind      string // direct or group
	PeerID    string
	GroupID   string
	Pinned    bool
	Muted     bool
	Hidden    bool // local session removed; a new message can restore it
	Unread    int
	Draft     string
	Preview   string
	UpdatedAt time.Time
}

type Message struct {
	Recalled       bool `json:",omitempty"`
	Deleted        bool `json:",omitempty"`
	ID             string
	ConversationID string
	SenderID       string
	Kind           string // text, image, file, system
	Text           string
	FileID         string
	CreatedAt      time.Time
	Status         string // pending, sent, delivered, failed
	Nickname       string
}

type Group struct {
	ID        string
	Name      string
	OwnerID   string
	OwnerKey  []byte
	Members   []string
	Pending   []string
	Epoch     uint64
	Dissolved bool
	Left      bool
	Signature []byte
	RootKey   []byte
	Nonce     string
	Ownership []Ownership
}

type Ownership struct {
	FromID    string
	ToID      string
	ToKey     []byte
	Epoch     uint64
	Signature []byte
}

type Invitation struct {
	ID        string
	GroupID   string
	GroupName string
	FromID    string
	FromName  string
	Kind      string // join or ownership
	Status    string // pending, accepted, rejected, withdrawn
	CreatedAt time.Time
	ToID      string
}

type GroupNickname struct {
	GroupID string
	PeerID  string
	Name    string
}

type File struct {
	ID              string
	Name            string
	Size            int64
	Hash            string
	MIME            string
	InlineImage     bool `json:",omitempty"` // bounded image attachment delivered as a chat message
	SenderID        string
	ConversationID  string
	SourcePath      string // local only, never sent over the wire
	Shared          bool
	ShareFolder     bool `json:",omitempty"` // browser-only directory share
	CreatedAt       time.Time
	ShareURL        string `json:"-"` // snapshot only, never sent to peers
	ShareProtected  bool   `json:"-"`
	ShareQR         []byte `json:"-"`
	ShareEntryCount int    `json:"-"`
}

type Transfer struct {
	Revoked         bool
	FromShare       bool
	ID              string
	FileID          string
	ConversationID  string
	PeerID          string
	Direction       string // send or receive
	Status          string // offered, transferring, verifying, completed, rejected, waiting, cancelled, failed
	Name            string
	Size            int64
	Done            int64
	Speed           float64
	Path            string
	Error           string
	UpdatedAt       time.Time
	LegacyTransport string `json:",omitempty"`
	RemotePacket    string `json:",omitempty"`
	RemoteFile      string `json:",omitempty"`
}

type Shortcut struct {
	Action string
	Keys   string
	Scope  string
}

const ShortcutScreenshot = "截图标注"

type Settings struct {
	RecentEmoji          []string `json:",omitempty"`
	Language             string
	Theme                string
	Nickname             string
	Avatar               string
	Signature            string
	ProfileDetails       ProfileDetails `json:",omitzero"`
	Presence             Presence
	Background           string
	BackgroundShade      int
	HasLockPassword      bool
	LockOnStartup        bool
	HideNotificationText bool
	HideOfflineContacts  bool
	DisableAutoDiscovery bool
	AutoAcceptImages     bool
	DesktopNotifications bool
	Sound                bool
	DownloadDir          string
	DuplicatePolicy      string
	AutoAccept           bool
	Network              string
	PendingNetwork       string
	CloseToTray          bool
	SendKey              string
	Shortcuts            []Shortcut
}

func DefaultSettings(downloadDir string) Settings {
	return Settings{Language: "zh-CN", Theme: "light", Nickname: "我", DesktopNotifications: true, DownloadDir: downloadDir,
		DuplicatePolicy: "rename", CloseToTray: true, SendKey: "Enter", Shortcuts: []Shortcut{
			{Action: "会话内搜索", Keys: "Ctrl+F", Scope: "应用内"},
			{Action: "快速查找", Keys: "Ctrl+K", Scope: "应用内"},
			{Action: "创建群聊", Keys: "Ctrl+Shift+N", Scope: "应用内"},
			{Action: "打开设置", Keys: "Ctrl+,", Scope: "应用内"},
			{Action: "显示或隐藏窗口", Scope: "全局"},
			{Action: ShortcutScreenshot, Keys: "Alt+A", Scope: "全局"},
		}}
}

// EnsureDefaultShortcuts adds shortcuts introduced after an existing profile
// was created without replacing the user's configured or cleared values.
func (s *Settings) EnsureDefaultShortcuts() {
	present := make(map[string]bool, len(s.Shortcuts))
	for _, shortcut := range s.Shortcuts {
		present[shortcut.Action] = true
	}
	for _, shortcut := range DefaultSettings(s.DownloadDir).Shortcuts {
		if !present[shortcut.Action] {
			s.Shortcuts = append(s.Shortcuts, shortcut)
		}
	}
}

type State struct {
	Self           Peer
	Peers          []Peer
	ContactGroups  []string
	Conversations  []Conversation
	Messages       []Message
	Groups         []Group
	Invitations    []Invitation
	Files          []File
	Transfers      []Transfer
	Settings       Settings
	Error          string
	Notice         string
	Locked         bool
	LockResult     LockResult    `json:"-"`
	DeviceShare    DeviceShare   `json:"-"`
	ShareFeedback  ShareFeedback `json:"-"`
	ListenAddress  string
	LocalIP        string `json:"-"`
	Version        string
	Revision       uint64
	GroupNicknames []GroupNickname
}

type LockResult struct {
	Request uint64
	Error   string
}

const WebReceiveFolder = "网页接收"

type DeviceShare struct {
	URL, ReceiveDir string
	QR              []byte
}

// ShareFeedback is the latest ephemeral result of a file sharing action.
type ShareFeedback struct {
	Sequence      uint64
	Title, Detail string
	Failed        bool
}

func (s State) Peer(id string) Peer {
	if s.Self.ID == id {
		return s.Self
	}
	for _, p := range s.Peers {
		if p.ID == id {
			return p
		}
	}
	return Peer{ID: id, Name: "未知联系人"}
}
func (s State) Conversation(id string) Conversation {
	for _, v := range s.Conversations {
		if v.ID == id {
			return v
		}
	}
	return Conversation{}
}
func (s State) Group(id string) Group {
	for _, v := range s.Groups {
		if v.ID == id {
			return v
		}
	}
	return Group{}
}
func (s State) File(id string) File {
	for _, v := range s.Files {
		if v.ID == id {
			return v
		}
	}
	return File{}
}
func (s State) ChatMessages(id string) []Message {
	var out []Message
	for _, m := range s.Messages {
		if m.ConversationID == id && !m.Deleted {
			out = append(out, m)
		}
	}
	return out
}

type CommandKind string

const (
	Connect            CommandKind = "connect"
	StartChat          CommandKind = "start_chat"
	SendText           CommandKind = "send_text"
	SendFiles          CommandKind = "send_files"
	AcceptTransfer     CommandKind = "accept_transfer"
	RejectTransfer     CommandKind = "reject_transfer"
	CancelTransfer     CommandKind = "cancel_transfer"
	RetryTransfer      CommandKind = "retry_transfer"
	CreateGroup        CommandKind = "create_group"
	InviteMembers      CommandKind = "invite_members"
	AcceptInvite       CommandKind = "accept_invite"
	RejectInvite       CommandKind = "reject_invite"
	RenameGroup        CommandKind = "rename_group"
	RemoveMember       CommandKind = "remove_member"
	LeaveGroup         CommandKind = "leave_group"
	TransferOwner      CommandKind = "transfer_owner"
	DissolveGroup      CommandKind = "dissolve_group"
	SetSetting         CommandKind = "set_setting"
	SetProfile         CommandKind = "set_profile"
	SetContactGroup    CommandKind = "set_contact_group"
	CreateContactGroup CommandKind = "create_contact_group"
	RenameContactGroup CommandKind = "rename_contact_group"
	DeleteContactGroup CommandKind = "delete_contact_group"
	SetGroupNickname   CommandKind = "set_group_nickname"
	WithdrawInvite     CommandKind = "withdraw_invite"
	TogglePin          CommandKind = "toggle_pin"
	ToggleMute         CommandKind = "toggle_mute"
	DeleteConversation CommandKind = "delete_conversation"
	SetSharePassword   CommandKind = "set_share_password"
	SetLockPassword    CommandKind = "set_lock_password"
	LockApp            CommandKind = "lock_app"
	UnlockApp          CommandKind = "unlock_app"
	MarkRead           CommandKind = "mark_read"
	SaveDraft          CommandKind = "save_draft"
	ShareFiles         CommandKind = "share_files"
	SetDeviceShare     CommandKind = "set_device_share"
	RemoveShare        CommandKind = "remove_share"
	RefreshShares      CommandKind = "refresh_shares"
	GetShared          CommandKind = "get_shared"
	ClearError         CommandKind = "clear_error"
)

const (
	RecallMessage CommandKind = "recall_message"
	DeleteMessage CommandKind = "delete_message"
	RememberEmoji CommandKind = "remember_emoji"
)

type Command struct {
	MessageID      string
	Kind           CommandKind
	PeerID         string
	ConversationID string
	GroupID        string
	FileID         string
	TransferID     string
	InvitationID   string
	Text           string
	Name           string
	Address        string
	Paths          []string
	IDs            []string
	Setting        string
	Value          string
	Bool           bool
	Password       string // local secret, never serialized into app state or wire messages
	AvatarBGRA     []byte // optional immutable preview pixels in premultiplied BGRA
	AvatarSize     int
	Signature      string
	ProfileDetails *ProfileDetails
	Presence       Presence
	Request        uint64
}

// Backend snapshots are detached copies. Submit queues work, never blocks the UI.
type Backend interface {
	Snapshot() State
	Submit(Command)
	Updates() <-chan struct{}
}

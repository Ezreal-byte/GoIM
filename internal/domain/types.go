// Package domain is the typed boundary between the peer engine and desktop UI.
package domain

import "time"

type Peer struct {
	ID           string
	Name         string
	Address      string
	Avatar       string
	ContactGroup string
	Online       bool
	LastSeen     time.Time
	PublicKey    []byte
}

type Conversation struct {
	ID        string
	Title     string
	Kind      string // direct or group
	PeerID    string
	GroupID   string
	Pinned    bool
	Muted     bool
	Unread    int
	Draft     string
	Preview   string
	UpdatedAt time.Time
}

type Message struct {
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
	ID             string
	Name           string
	Size           int64
	Hash           string
	MIME           string
	SenderID       string
	ConversationID string
	SourcePath     string // local only, never sent over the wire
	Shared         bool
	CreatedAt      time.Time
}

type Transfer struct {
	ID             string
	FileID         string
	ConversationID string
	PeerID         string
	Direction      string // send or receive
	Status         string // offered, transferring, verifying, completed, rejected, waiting, cancelled, failed
	Name           string
	Size           int64
	Done           int64
	Speed          float64
	Path           string
	Error          string
	UpdatedAt      time.Time
}

type Shortcut struct {
	Action string
	Keys   string
	Scope  string
}
type Settings struct {
	Theme                string
	Nickname             string
	Avatar               string
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
	return Settings{Theme: "light", Nickname: "我", DesktopNotifications: true, DownloadDir: downloadDir,
		DuplicatePolicy: "rename", CloseToTray: true, SendKey: "Enter", Shortcuts: []Shortcut{
			{Action: "会话内搜索", Keys: "Ctrl+F", Scope: "应用内"},
			{Action: "快速查找", Keys: "Ctrl+K", Scope: "应用内"},
			{Action: "创建群聊", Keys: "Ctrl+Shift+N", Scope: "应用内"},
			{Action: "打开设置", Keys: "Ctrl+,", Scope: "应用内"},
			{Action: "显示或隐藏窗口", Scope: "全局"},
		}}
}

type State struct {
	Self           Peer
	Peers          []Peer
	Conversations  []Conversation
	Messages       []Message
	Groups         []Group
	Invitations    []Invitation
	Files          []File
	Transfers      []Transfer
	Settings       Settings
	Error          string
	Notice         string
	ListenAddress  string
	Version        string
	Revision       uint64
	GroupNicknames []GroupNickname
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
		if m.ConversationID == id {
			out = append(out, m)
		}
	}
	return out
}

type CommandKind string

const (
	Connect          CommandKind = "connect"
	StartChat        CommandKind = "start_chat"
	SendText         CommandKind = "send_text"
	SendFiles        CommandKind = "send_files"
	AcceptTransfer   CommandKind = "accept_transfer"
	RejectTransfer   CommandKind = "reject_transfer"
	CancelTransfer   CommandKind = "cancel_transfer"
	RetryTransfer    CommandKind = "retry_transfer"
	CreateGroup      CommandKind = "create_group"
	InviteMembers    CommandKind = "invite_members"
	AcceptInvite     CommandKind = "accept_invite"
	RejectInvite     CommandKind = "reject_invite"
	RenameGroup      CommandKind = "rename_group"
	RemoveMember     CommandKind = "remove_member"
	LeaveGroup       CommandKind = "leave_group"
	TransferOwner    CommandKind = "transfer_owner"
	DissolveGroup    CommandKind = "dissolve_group"
	SetSetting       CommandKind = "set_setting"
	SetProfile       CommandKind = "set_profile"
	SetContactGroup  CommandKind = "set_contact_group"
	SetGroupNickname CommandKind = "set_group_nickname"
	WithdrawInvite   CommandKind = "withdraw_invite"
	TogglePin        CommandKind = "toggle_pin"
	ToggleMute       CommandKind = "toggle_mute"
	MarkRead         CommandKind = "mark_read"
	SaveDraft        CommandKind = "save_draft"
	ShareFiles       CommandKind = "share_files"
	RemoveShare      CommandKind = "remove_share"
	RefreshShares    CommandKind = "refresh_shares"
	GetShared        CommandKind = "get_shared"
	ClearError       CommandKind = "clear_error"
)

type Command struct {
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
}

// Backend snapshots are detached copies. Submit queues work, never blocks the UI.
type Backend interface {
	Snapshot() State
	Submit(Command)
	Updates() <-chan struct{}
}

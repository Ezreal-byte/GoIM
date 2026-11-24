// Package domain is the typed boundary between the peer engine and desktop UI.
package domain

import (
	"encoding/hex"
	"strings"
	"time"
)

type Peer struct {
	Remark               string `json:"-"` // local presentation only; never sent to another peer
	ContactHidden        bool   `json:"-"` // keeps the original group while hidden locally
	ContactDeleted       bool   `json:"-"` // retained for history until this peer reappears
	RecallSupported      bool   `json:"RecallSupported,omitempty"`
	PrintCancelSupported bool   `json:"PrintCancelSupported,omitempty"`
	PrintSupported       bool   `json:"PrintSupported,omitempty"`
	VoiceSupported       bool   `json:",omitempty"`
	RelaySupported       bool   `json:",omitempty"`
	ID                   string
	Name                 string
	Address              string
	Avatar               string
	ContactGroup         string
	Online               bool
	LastSeen             time.Time
	PublicKey            []byte
	Signature            string         `json:",omitempty"`
	Presence             Presence       `json:",omitempty"`
	Details              ProfileDetails `json:",omitzero"`
	Version              string         `json:",omitempty"`
	Transport            string         `json:",omitempty"` // empty for GoIM TLS
	ClientType           ClientType     `json:",omitempty"`
	CompatibilityAliasOf string         `json:",omitempty"` // local contact linkage; never an authenticated identity
}

func (p Peer) DisplayName() string {
	if p.Remark != "" {
		return p.Remark
	}
	return p.Name
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

type ClientType string

const (
	ClientGoIM        ClientType = "goim"
	ClientNeiwangtong ClientType = "neiwangtong"
	ClientIPMSG       ClientType = "ipmsg"
)

const (
	TransportGoIM            = ""
	TransportAuto            = "auto" // connection request only; never a stored transport
	TransportIPMSG           = "ipmsg"
	TransportNeiwangtong     = "neiwangtong"
	IPMSGPort                = 2425
	LANDiscoveryPort         = 47841
	LANDiscoveryGroup        = "239.192.77.77"
	NeiwangtongDiscoveryPort = 9011
	NeiwangtongControlPort   = 9012
)

type CompatibilityConfig struct {
	Enabled bool
	Port    int
}

type CompatibilityStatus struct {
	Unavailable bool
	Active      bool
	Pending     *CompatibilityConfig
	Problem     string
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

// IsLegacyNeiwangtong identifies the 2425 advertisement sent by some
// Neiwangtong clients in addition to their native discovery.
func (p Peer) IsLegacyNeiwangtong() bool {
	return p.Transport == TransportIPMSG && p.ClientType == ClientNeiwangtong
}

// UnboundIPMSGPeerID identifies old records that did not retain the sender's login.
func UnboundIPMSGPeerID(id string) bool {
	suffix, ok := strings.CutPrefix(id, "ipmsg-")
	if !ok || len(suffix) != 16 {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}

// DirectReadOnlyReason is shared by the composer and the delivery admission check.
// Confirmed bridges between two compatibility transports can still send.
func (s State) DirectReadOnlyReason(peer Peer) string {
	switch {
	case peer.IsLegacyNeiwangtong():
		return "这条旧会话仅保留聊天记录。请从联系人列表打开内网通会话"
	case peer.Transport == TransportIPMSG && UnboundIPMSGPeerID(peer.ID):
		return "旧飞鸽／飞秋会话的登录身份未确认，请选择当前联系人重新发送"
	case peer.CompatibilityAliasOf != "":
		for _, canonical := range s.Peers {
			if canonical.ID == peer.CompatibilityAliasOf && canonical.Transport == TransportGoIM {
				return "此联系人已使用 GoIM 连接，请打开当前会话。"
			}
		}
	}
	return ""
}

func (p Peer) ListedContact() bool {
	return !p.ContactDeleted && p.CompatibilityAliasOf == "" && !p.IsLegacyNeiwangtong()
}

func (p Peer) SupportsGroups() bool {
	return p.ID != "" && p.ListedContact() && (p.Transport == TransportGoIM || p.Transport == TransportNeiwangtong)
}

func (p Peer) SupportsNeiwangtongRecall() bool {
	return p.Transport == TransportNeiwangtong && p.Version == "3.4.3055"
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
	Opened    bool `json:",omitempty"` // explicitly opened, even before the first message
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
	Kind           string // text, image, file, print, system
	Text           string
	FileID         string
	PrintJobID     string       `json:",omitempty"`
	Voice          *VoiceRecord `json:",omitempty"`
	CreatedAt      time.Time
	Status         string // pending, sent, delivered, failed
	Nickname       string
}

// VoiceRecord retains call facts after the live call has been cleared.
type VoiceRecord struct {
	Direction       VoiceCallDirection
	Reason          string
	Connected       bool
	DurationSeconds int
}

func (m Message) CanRecallNeiwangtongText(now time.Time) bool {
	return m.Kind == "text" && m.Status == "sent" && now.Before(m.CreatedAt.Add(2*time.Minute))
}

type Group struct {
	ID         string
	Name       string
	OwnerID    string
	OwnerKey   []byte
	Members    []string
	Pending    []string
	Epoch      uint64
	Dissolved  bool
	Left       bool
	Signature  []byte
	RootKey    []byte
	Nonce      string
	Ownership  []Ownership
	AvatarHash string `json:",omitempty"` // canonical PNG SHA-256, signed for GoIM groups
	AvatarMD5  string `json:",omitempty"` // native group roster version tag
	Avatar     string `json:"-"`          // local cached path, restored from AvatarHash
	Transport  string `json:",omitempty"` // empty for signed GoIM groups
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
	ID               string
	Name             string
	Size             int64
	Hash             string
	MIME             string
	InlineImage      bool `json:",omitempty"` // bounded image attachment delivered as a chat message
	Folder           bool `json:",omitempty"` // native peer directory, distinct from browser sharing
	SenderID         string
	ConversationID   string
	SourcePath       string // local only, never sent over the wire
	Shared           bool
	ShareFolder      bool `json:",omitempty"` // browser-only directory share
	CreatedAt        time.Time
	ShareUnavailable bool   `json:"-"`
	ShareVersion     uint64 `json:"-"`
	MemberURL        string `json:"-"` // snapshot only, for a member directory or password prompt
	ShareURL         string `json:"-"` // snapshot only, never sent to peers
	ShareProtected   bool   `json:"-"`
	ShareQR          []byte `json:"-"`
	ShareEntryCount  int    `json:"-"`
}

// ShareSession carries an unlocked member session only between local commands.
type ShareSession struct{ Address, Project, Token string }

// ShareDownload identifies an item in the peer's member catalog by its stable entry ID.
type ShareDownload struct {
	ProjectID, Name string
	Index           int
	Folder          bool
	Session         ShareSession
}

type Transfer struct {
	WebSender         string `json:",omitempty"` // Browser address, without its ephemeral port.
	WebAuto           bool   `json:",omitempty"` // Approved under the temporary unattended window.
	WebInbox          string `json:",omitempty"` // Pinned inbox for this upload.
	ShareProject      string `json:",omitempty"` // member catalog project, for HTTP downloads
	ShareIndex        int    `json:",omitempty"`
	ShareFolder       bool   `json:",omitempty"`
	ShareRequestID    string `json:",omitempty"` // original catalog request for a native offer
	ShareRequest      bool   `json:",omitempty"` // waiting for a native file offer
	ShareStage        string `json:",omitempty"` // incomplete member download, local to the receive directory
	Revoked           bool
	FromShare         bool
	ID                string
	FileID            string
	ConversationID    string
	PeerID            string
	Direction         string // send or receive
	Status            string // offered, transferring, verifying, completed, rejected, waiting, cancelled, failed
	Name              string
	Size              int64
	Done              int64
	Speed             float64
	Path              string
	Error             string
	UpdatedAt         time.Time
	LegacyTransport   string `json:",omitempty"`
	RemotePacket      string `json:",omitempty"`
	RemoteRevision    string `json:",omitempty"` // fingerprint of a stock direct-file source path, size and modification time
	RemoteOfferClosed bool   `json:",omitempty"` // sender explicitly withdrew a completed stock file invitation
	RemoteFile        string `json:",omitempty"`
	RemoteImageMD5    string `json:",omitempty"` // legacy inline image asset digest
	RemoteShareID     string `json:",omitempty"` // Neiwangtong group attachment
	RemoteFileModTime int64  `json:",omitempty"` // sender revision for a partially downloaded group file
	FolderMaxPath     int    `json:",omitempty"`
	FolderMaxItem     int64  `json:",omitempty"`
	FolderStage       string `json:",omitempty"` // private staging directory of a group folder download
	FileStage         string `json:",omitempty"` // resumable Neiwangtong file beside its destination
	RemotePeerID      string `json:",omitempty"` // wire identity of an existing compatibility transfer
	PrintJobID        string `json:",omitempty"` // local print task served by this transfer

	LocalSendPeer      string                  `json:",omitempty"`
	LocalSendNeedsPIN  bool                    `json:",omitempty"`
	LocalSendCanRetry  bool                    `json:",omitempty"`
	LocalSendDirectory bool                    `json:",omitempty"`
	LocalSendItems     []LocalSendTransferItem `json:",omitempty"`
}

func (t Transfer) CanRelocateFailedReceive() bool {
	return t.Direction == "receive" && t.Status == "failed" && t.LegacyTransport == TransportNeiwangtong &&
		!t.Revoked && t.PrintJobID == "" && t.RemoteImageMD5 == ""
}

// PrintCapabilities is the portable subset of printer options both sides can
// present consistently. An empty slice means the local driver did not report
// that capability; the platform defaults still apply.
type PrintCapabilities struct {
	Papers            []string `json:",omitempty"`
	Orientations      []string `json:",omitempty"`
	ColorModes        []string `json:",omitempty"`
	DuplexModes       []string `json:",omitempty"`
	SupportsPageRange bool     `json:",omitempty"`
}

const (
	PrintOrientationPortrait  = "portrait"
	PrintOrientationLandscape = "landscape"

	PrintColorColor      = "color"
	PrintColorMonochrome = "monochrome"

	PrintDuplexSimplex       = "simplex"
	PrintDuplexLongEdge      = "long-edge"
	PrintDuplexShortEdge     = "short-edge"
	PrintStatusSending       = "sending"
	PrintStatusReceiving     = "receiving"
	PrintStatusSubmitting    = "submitting"
	PrintStatusQueued        = "queued"
	PrintStatusCancelled     = "cancelled"
	PrintStatusCompleted     = "completed"
	PrintStatusFailed        = "failed"
	PrintPageRangeAll        = ""
	PrintPageRangeCustomized = "custom"
)

type PrintOptions struct {
	Paper       string
	Copies      int
	Orientation string
	PageRange   string
	Color       string
	Duplex      string
}

// LocalPrinter is a transient inventory entry read from the operating system.
// LocalPrinter.ID is the queue name and never enters a remote catalog.
type LocalPrinter struct {
	ID           string
	Name         string
	Available    bool
	Default      bool
	Status       string `json:",omitempty"`
	Capabilities PrintCapabilities
}

// PrinterShare is a peer-visible printer catalog entry. DeviceName is needed so
// the owner can address its local queue after a restart; remote peers cannot
// submit to anything except this record's authenticated OwnerID.
type PrinterShare struct {
	ID               string
	OwnerID          string
	OwnerName        string
	Name             string
	DeviceName       string
	Available        bool
	Capabilities     PrintCapabilities
	CreatedAt        time.Time
	PasswordRequired bool   `json:",omitempty"`
	AuthRevision     string `json:",omitempty"`
	AuthSalt         []byte `json:",omitempty"`
}

type PrinterAuthResult struct {
	Request uint64
	Pending bool
	Error   string
}

// PrintJob is the durable record kept on both peers. LocalJobID is populated
// only on the receiving machine and is never sent over the wire.
type PrintJob struct {
	ID               string
	MessageID        string
	TransferID       string
	FileID           string
	ConversationID   string
	PrinterID        string
	PrinterName      string
	PrinterDevice    string
	PrinterOwnerID   string
	PrinterOwnerName string
	SenderID         string
	SenderName       string
	FileName         string
	FileSize         int64
	MIME             string `json:",omitempty"`
	Options          PrintOptions
	Status           string // sending, receiving, submitting, queued, completed, failed
	Error            string `json:",omitempty"`
	LocalJobID       string `json:",omitempty"`
	CancelRequest    string `json:",omitempty"` // Sent request awaiting the printer owner’s decision.
	CancelError      string `json:",omitempty"`
	SubmitKey        string `json:",omitempty"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (j PrintJob) Terminal() bool {
	return j.Status == PrintStatusCompleted || j.Status == PrintStatusFailed || j.Status == PrintStatusCancelled
}

// CanCancel is also checked by the owner when a remote request arrives.
func (j PrintJob) CanCancel() bool {
	return j.CancelRequest == "" && (j.Status == PrintStatusSending || j.Status == PrintStatusReceiving || j.Status == PrintStatusQueued)
}

type Shortcut struct {
	Action string
	Keys   string
	Scope  string
}

const ShortcutScreenshot = "截图标注"

type Settings struct {
	LocalSend                    LocalSendConfig
	PinnedPrinters               []string `json:",omitempty"`
	IPMSG                        CompatibilityConfig
	Neiwangtong                  CompatibilityConfig
	CompatibilityConfigured      bool
	CompatibilityDefaultsVersion int      `json:",omitempty"`
	RecentEmoji                  []string `json:",omitempty"`
	LastSection                  string   `json:",omitempty"`
	Language                     string
	Theme                        string
	ColorScheme                  string
	InterfaceFont                string
	InterfaceFontSize            int
	Nickname                     string
	Avatar                       string
	Signature                    string
	ProfileDetails               ProfileDetails `json:",omitzero"`
	Presence                     Presence
	Background                   string
	BackgroundTransparency       int
	HasLockPassword              bool
	LockOnStartup                bool
	SystemUnlock                 bool
	HideNotificationText         bool
	HideOfflineContacts          bool
	DisableAutoDiscovery         bool
	DNSDiscovery                 DNSDiscoveryConfig `json:",omitzero"`
	AutoAcceptImages             bool
	ManualVideoReceive           bool
	// DisableAutoUpdateCheck preserves the zero value as the desired default:
	// existing profiles keep checking for updates unless the user opts out.
	DisableAutoUpdateCheck    bool
	DesktopNotifications      bool
	Sound                     bool
	VoiceMicrophone           string `json:",omitempty"` // empty selects the system default
	VoiceSpeaker              string `json:",omitempty"` // empty selects the system default
	VoiceMicrophoneVolume     int
	VoiceSpeakerVolume        int
	VoiceAudioDefaultsVersion int `json:",omitempty"`
	DownloadDir               string
	DuplicatePolicy           string
	AutoAccept                bool
	Network                   string
	PendingNetwork            string
	HideTitleGitHub           bool `json:",omitempty"`
	CloseToTray               bool
	SendKey                   string
	Shortcuts                 []Shortcut
}

// VoiceCallStatus is the lifecycle state of the one direct voice call this
// device participates in. Calls are ephemeral and never enter chat history.
type VoiceCallStatus string

const (
	VoiceCallRinging VoiceCallStatus = "ringing"
	VoiceCallActive  VoiceCallStatus = "active"
	VoiceCallEnded   VoiceCallStatus = "ended"
)

type VoiceCallDirection string

const (
	VoiceCallOutgoing VoiceCallDirection = "outgoing"
	VoiceCallIncoming VoiceCallDirection = "incoming"
)

const (
	VoiceCodecPCMU      = "pcmu"
	VoiceSampleRate     = 16000
	VoiceFrameMillis    = 20
	VoiceFrameSamples   = VoiceSampleRate * VoiceFrameMillis / 1000
	VoiceFrameBytes     = VoiceFrameSamples * 2
	VoiceEncodedBytes   = VoiceFrameSamples
	VoiceMaxEncodedSize = VoiceEncodedBytes * 4
)

const (
	VoiceVolumeDefault        = 100
	VoiceVolumeMaximum        = 100
	VoiceAudioDefaultsVersion = 1
)

const (
	InterfaceFontSizeMinimum = 9
	InterfaceFontSizeDefault = 14
	InterfaceFontSizeMaximum = 28
)

func NormalizeInterfaceFontSize(size int) int {
	if size < InterfaceFontSizeMinimum {
		return InterfaceFontSizeDefault
	}
	return min(size, InterfaceFontSizeMaximum)
}

func ClampVoiceVolume(volume int) int {
	return min(max(volume, 0), VoiceVolumeMaximum)
}

// EnsureVoiceDefaults distinguishes an old profile with no volume fields from
// a new profile where the user deliberately selected zero percent.
func (s *Settings) EnsureVoiceDefaults() {
	if s.VoiceAudioDefaultsVersion < VoiceAudioDefaultsVersion {
		s.VoiceMicrophoneVolume = VoiceVolumeDefault
		s.VoiceSpeakerVolume = VoiceVolumeDefault
		s.VoiceAudioDefaultsVersion = VoiceAudioDefaultsVersion
	}
	s.VoiceMicrophoneVolume = ClampVoiceVolume(s.VoiceMicrophoneVolume)
	s.VoiceSpeakerVolume = ClampVoiceVolume(s.VoiceSpeakerVolume)
}

// VoiceCall is a detached, UI-facing view of the live call. It is omitted from
// persisted state and becomes zero once the final status has been presented.
type VoiceCall struct {
	ID           string
	PeerID       string
	PeerName     string
	PeerAvatar   string
	Direction    VoiceCallDirection
	Status       VoiceCallStatus
	StartedAt    time.Time
	ConnectedAt  time.Time
	Muted        bool
	SpeakerMuted bool
	Reason       string
	Audio        CallAudioOptions
}

// CallAudioOptions contains only the local, temporary overrides for one call.
// The flags distinguish system-default devices and zero volume from an unset override.
type CallAudioOptions struct {
	Microphone, Speaker                                              string
	MicrophoneVolume, SpeakerVolume                                  int
	HasMicrophone, HasSpeaker, HasMicrophoneVolume, HasSpeakerVolume bool
}

func (c VoiceCall) AudioSettings(s Settings) Settings {
	if c.Audio.HasMicrophone {
		s.VoiceMicrophone = c.Audio.Microphone
	}
	if c.Audio.HasSpeaker {
		s.VoiceSpeaker = c.Audio.Speaker
	}
	if c.Audio.HasMicrophoneVolume {
		s.VoiceMicrophoneVolume = c.Audio.MicrophoneVolume
	}
	if c.Audio.HasSpeakerVolume {
		s.VoiceSpeakerVolume = c.Audio.SpeakerVolume
	}
	return s
}

func (c VoiceCall) Live() bool {
	return c.ID != "" && c.Status != VoiceCallEnded
}

type VoiceSignal struct {
	CallID       string
	Action       string // invite, accept, reject, busy, end
	Reason       string
	Codec        string
	SampleRate   int
	Muted        bool
	SpeakerMuted bool
}

func DefaultSettings(downloadDir string) Settings {
	return Settings{CompatibilityConfigured: true, CompatibilityDefaultsVersion: 1,
		IPMSG:       CompatibilityConfig{Enabled: true, Port: IPMSGPort},
		Neiwangtong: CompatibilityConfig{Enabled: true, Port: NeiwangtongDiscoveryPort},
		LastSection: "contacts",
		Language:    "zh-CN", Theme: "light", ColorScheme: "classic", Nickname: "我", DesktopNotifications: true, DownloadDir: downloadDir,
		BackgroundTransparency: 60,
		InterfaceFontSize:      InterfaceFontSizeDefault,
		VoiceMicrophoneVolume:  VoiceVolumeDefault, VoiceSpeakerVolume: VoiceVolumeDefault,
		VoiceAudioDefaultsVersion: VoiceAudioDefaultsVersion,
		DuplicatePolicy:           "rename", CloseToTray: true, SendKey: "Enter", Shortcuts: []Shortcut{
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
	LocalSend          LocalSendStatus     `json:"-"`
	ContactRefresh     ContactRefresh      `json:"-"`
	DiscoveryDisabled  bool                `json:"-"`
	IPMSGStatus        CompatibilityStatus `json:"-"`
	NeiwangtongStatus  CompatibilityStatus `json:"-"`
	Self               Peer
	Peers              []Peer
	ContactGroups      []string
	Conversations      []Conversation
	Messages           []Message
	Groups             []Group
	Invitations        []Invitation
	Files              []File
	Transfers          []Transfer
	LocalPrinters      []LocalPrinter `json:"-"`
	Printers           []PrinterShare
	PrintJobs          []PrintJob
	PrintersLoading    bool                         `json:"-"`
	PrintError         string                       `json:"-"`
	PrinterAuthorized  map[string]string            `json:"-"`
	PrinterAuth        map[string]PrinterAuthResult `json:"-"`
	Settings           Settings
	VoiceCall          VoiceCall `json:"-"`
	Error              string
	Notice             string
	Locked             bool
	LockResult         LockResult         `json:"-"`
	DeviceShare        DeviceShare        `json:"-"`
	ShareFeedback      ShareFeedback      `json:"-"`
	ConnectionFeedback ConnectionFeedback `json:"-"`
	DNSDiscoveryStatus string             `json:"-"`
	DataResetPending   bool               `json:"-"`
	ListenAddress      string
	LocalIP            string `json:"-"`
	Version            string
	Revision           uint64
	GroupNicknames     []GroupNickname
}

type LockResult struct {
	Request uint64
	Error   string
}

type ContactRefresh struct {
	Sequence uint64
	Pending  bool
}

type ConnectionFeedback struct {
	Sequence uint64
	Address  string
	Pending  bool
	Problem  string
}

const WebReceiveFolder = "网页接收"

type DeviceShare struct {
	AutoAcceptUntil time.Time
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
	if peer, ok := s.LookupPeer(id); ok || id == "" {
		return peer
	}
	return Peer{ID: id, Name: "未知联系人"}
}

// LookupPeer looks up a current contact, including local records kept for history.
func (s State) LookupPeer(id string) (Peer, bool) {
	if id == "" {
		return Peer{}, false
	}
	if s.Self.ID == id {
		return s.Self, true
	}
	for _, p := range s.Peers {
		if p.ID == id {
			return p, true
		}
	}
	return Peer{}, false
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
func (s State) Printer(id string) PrinterShare {
	for _, v := range s.Printers {
		if v.ID == id {
			return v
		}
	}
	return PrinterShare{}
}
func (s State) PrintJob(id string) PrintJob {
	for _, v := range s.PrintJobs {
		if v.ID == id {
			return v
		}
	}
	return PrintJob{}
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
	Connect              CommandKind = "connect"
	SetCompatibility     CommandKind = "set_compatibility"
	RefreshPeerAvatar    CommandKind = "refresh_peer_avatar"
	RefreshContacts      CommandKind = "refresh_contacts"
	StartChat            CommandKind = "start_chat"
	SendText             CommandKind = "send_text"
	SendFiles            CommandKind = "send_files"
	AcceptTransfer       CommandKind = "accept_transfer"
	RejectTransfer       CommandKind = "reject_transfer"
	CancelPrintJob       CommandKind = "cancel_print_job"
	CancelTransfer       CommandKind = "cancel_transfer"
	RetryTransfer        CommandKind = "retry_transfer"
	CreateGroup          CommandKind = "create_group"
	InviteMembers        CommandKind = "invite_members"
	AcceptInvite         CommandKind = "accept_invite"
	RejectInvite         CommandKind = "reject_invite"
	RenameGroup          CommandKind = "rename_group"
	SetGroupAvatar       CommandKind = "set_group_avatar"
	RemoveMember         CommandKind = "remove_member"
	LeaveGroup           CommandKind = "leave_group"
	TransferOwner        CommandKind = "transfer_owner"
	DissolveGroup        CommandKind = "dissolve_group"
	SetSetting           CommandKind = "set_setting"
	SetProfile           CommandKind = "set_profile"
	SetContactGroup      CommandKind = "set_contact_group"
	SetContactRemark     CommandKind = "set_contact_remark"
	SetContactHidden     CommandKind = "set_contact_hidden"
	DeleteContact        CommandKind = "delete_contact"
	CreateContactGroup   CommandKind = "create_contact_group"
	RenameContactGroup   CommandKind = "rename_contact_group"
	DeleteContactGroup   CommandKind = "delete_contact_group"
	SetGroupNickname     CommandKind = "set_group_nickname"
	WithdrawInvite       CommandKind = "withdraw_invite"
	TogglePin            CommandKind = "toggle_pin"
	ToggleMute           CommandKind = "toggle_mute"
	DeleteConversation   CommandKind = "delete_conversation"
	ReplaceShareSource   CommandKind = "replace_share_source"
	SetSharePassword     CommandKind = "set_share_password"
	SetLockPassword      CommandKind = "set_lock_password"
	SetSystemUnlock      CommandKind = "set_system_unlock"
	LockApp              CommandKind = "lock_app"
	UnlockApp            CommandKind = "unlock_app"
	UnlockAppSystem      CommandKind = "unlock_app_system"
	MarkRead             CommandKind = "mark_read"
	SaveDraft            CommandKind = "save_draft"
	ShareFiles           CommandKind = "share_files"
	SetWebUploadApproval CommandKind = "set_web_upload_approval"
	SetDeviceShare       CommandKind = "set_device_share"
	RemoveShare          CommandKind = "remove_share"
	RefreshShares        CommandKind = "refresh_shares"
	GetShared            CommandKind = "get_shared"
	RefreshPrinters      CommandKind = "refresh_printers"
	SharePrinter         CommandKind = "share_printer"
	UnsharePrinter       CommandKind = "unshare_printer"
	AuthenticatePrinter  CommandKind = "authenticate_printer"
	SetPrinterPinned     CommandKind = "set_printer_pinned"
	PrintFile            CommandKind = "print_file"
	ClearError           CommandKind = "clear_error"
	ResetUserData        CommandKind = "reset_user_data"
	StartVoiceCall       CommandKind = "start_voice_call"
	AcceptVoiceCall      CommandKind = "accept_voice_call"
	RejectVoiceCall      CommandKind = "reject_voice_call"
	EndVoiceCall         CommandKind = "end_voice_call"
	SetVoiceMuted        CommandKind = "set_voice_muted"
	SetVoiceSpeakerMuted CommandKind = "set_voice_speaker_muted"

	SetCallMicrophone       CommandKind = "set_call_microphone"
	SetCallSpeaker          CommandKind = "set_call_speaker"
	SetCallMicrophoneVolume CommandKind = "set_call_microphone_volume"
	SetCallSpeakerVolume    CommandKind = "set_call_speaker_volume"
)

const (
	RecallMessage CommandKind = "recall_message"
	DeleteMessage CommandKind = "delete_message"
	RememberEmoji CommandKind = "remember_emoji"
)

type Command struct {
	LocalSend      LocalSendRequest
	ShareDownloads []ShareDownload `json:"-"`
	// OnResult reports local command completion. Callbacks must not block or mutate window state.
	OnResult       func(error) `json:"-"`
	Transport      string
	Compatibility  CompatibilityConfig
	DNSDiscovery   DNSDiscoveryConfig
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
	RemoveAvatar   bool
	Signature      string
	ProfileDetails *ProfileDetails
	Presence       Presence
	Request        uint64
	PrinterID      string
	PrintJobID     string
	PrintOptions   PrintOptions
	CallID         string
	ExpectedDevice *string // guard a failed device switch against a newer selection
}

// Backend snapshots are detached copies. Submit queues work, never blocks the UI.
type Backend interface {
	Snapshot() State
	Submit(Command)
	Updates() <-chan struct{}
}

// VoiceAudioBackend is implemented by backends that can carry live encoded
// call audio. The UI uses a type assertion so test backends remain minimal.
type VoiceAudioBackend interface {
	VoiceAudio(callID string) <-chan []byte
	SendVoiceAudio(callID string, encoded []byte) error
}

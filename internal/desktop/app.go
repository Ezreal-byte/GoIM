// Package desktop implements GoIM's native Viva desktop shell.
package desktop

import (
	"errors"
	"github.com/vivaui/viva/gfx"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/buildinfo"
	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	sectionMessages   = "messages"
	sectionContacts   = "contacts"
	sectionTransfers  = "transfers"
	sectionShares     = "shares"
	sectionPrint      = "print"
	sectionSettings   = "settings"
	titlebarHeight    = float32(42)
	railWidth         = float32(68)
	sidebarWidth      = float32(300)
	minimumWindowSize = float32(1000)

	sidebarConversationHeight = float32(64)
	sidebarContactHeight      = float32(54)
	sidebarSelfHeight         = float32(52)
	sidebarGroupHeight        = float32(52)
)

type palette struct {
	rail          ui.Color
	sidebar       ui.Color
	chat          ui.Color
	header        ui.Color
	border        ui.Color
	hover         ui.Color
	selected      ui.Color
	selectedText  ui.Color
	icon          ui.Color
	secondaryText ui.Color
	mutedAvatar   ui.Color
	avatarText    ui.Color
	incoming      ui.Color
	own           ui.Color
	bubbleBorder  ui.Color
	fileCard      ui.Color
	fileBorder    ui.Color
	invite        ui.Color
	success       ui.Color
	danger        ui.Color
}

type contactGroupView struct {
	Name   string
	Total  int
	Online int
	Peers  []domain.Peer
}

type composeState struct {
	open          bool
	mode          string
	groupID       string
	name          ui.Field
	search        ui.Field
	selected      map[string]bool
	pending       bool
	submittedName string
	submittedIDs  []string
	knownGroups   map[string]bool
	err           string
}

type confirmation struct {
	open   bool
	title  string
	body   string
	action func()
}

type conversationDragState struct {
	id              string
	card            conversationCardData
	selected        bool
	startX          float32
	startY          float32
	currentX        float32
	currentY        float32
	dragging        bool
	preview         conversationDragPreview
	previewOpenFail bool
}

type conversationCardData struct {
	username   string
	title      string
	avatarName string
	avatarPath string
	preview    string
	updatedAt  time.Time
	online     bool
	pinned     bool
	muted      bool
	unread     int
}

type conversationDragPreview interface {
	MoveTo(gfx.Point)
	Close()
}

type profileState struct {
	open      bool
	nickname  ui.Field
	signature ui.Field
	details   [5]ui.Field
	gender    int
	presence  domain.Presence
	avatar    string
	// avatarPreview is the square upload and avatarPreviewRound is its cached
	// circular presentation. avatar is the source path until the backend
	// replaces it with its normalized avatar asset.
	avatarPreview      gfx.Bitmap
	avatarPreviewRound gfx.Bitmap
	crop               profileAvatarCropState
	err                string
}

type profileAvatarCropState struct {
	pending bool
	open    bool
	source  string
	state   components.AvatarEditorState
	layout  components.AvatarEditorLayout
	err     string
}

type mentionCandidate struct {
	peerID string
	name   string
	online bool
	avatar string
}

type mentionState struct {
	open       bool
	start      int
	query      string
	selected   int
	candidates []mentionCandidate
}

type avatarPaint struct {
	bounds ui.Rect
	name   string
	image  gfx.Bitmap
	status components.AvatarStatus
}

type imageStamp struct {
	modified int64
	size     int64
}

type appIDs struct {
	page   gfx.Scope
	nav    gfx.Scope
	side   gfx.Scope
	chat   gfx.Scope
	dialog gfx.Scope
	menu   gfx.Scope

	navMessages   gfx.ID
	navContacts   gfx.ID
	navTransfers  gfx.ID
	navShares     gfx.ID
	navPrint      gfx.ID
	navLock       gfx.ID
	navTheme      gfx.ID
	navSettings   gfx.ID
	profileAvatar gfx.ID
	detachedPin   gfx.ID

	sidebarCreate   gfx.ID
	sidebarConnect  gfx.ID
	sidebarSearch   gfx.ID
	sidebarRefresh  gfx.ID
	filterAll       gfx.ID
	filterOnline    gfx.ID
	filterHidden    gfx.ID
	hiddenGroupHead gfx.ID
	emptyConnect    gfx.ID
	emptyCompose    gfx.ID

	chatScreenshot                                                         gfx.ID
	chatSend                                                               gfx.ID
	chatImage                                                              gfx.ID
	chatFile, chatFolder, chatCurrentContact                               gfx.ID
	chatEmoji                                                              gfx.ID
	chatEditorField                                                        gfx.ID
	mainPin                                                                gfx.ID
	chatHeaderFile                                                         gfx.ID
	chatHeaderSearch                                                       gfx.ID
	chatHeaderMembers                                                      gfx.ID
	chatHeaderMore                                                         gfx.ID
	chatHeaderCall                                                         gfx.ID
	chatSearchField                                                        gfx.ID
	chatSearchClose                                                        gfx.ID
	chatEmptyConnect                                                       gfx.ID
	chatEmptyCompose                                                       gfx.ID
	chatMenuDismiss                                                        gfx.ID
	chatMenuPin                                                            gfx.ID
	chatMenuMute                                                           gfx.ID
	chatMenuRead                                                           gfx.ID
	chatMenuLeave                                                          gfx.ID
	chatNoticeDismiss                                                      gfx.ID
	chatMenuScope                                                          gfx.ID
	shellMenuDismiss                                                       gfx.ID
	shellMenuPin                                                           gfx.ID
	shellMenuMute                                                          gfx.ID
	shellMenuRead                                                          gfx.ID
	shellMenuDelete                                                        gfx.ID
	shellMenuTalk                                                          gfx.ID
	shellMenuOrganize                                                      gfx.ID
	shellMenuHideContact                                                   gfx.ID
	shellMenuDeleteContact                                                 gfx.ID
	shellMenuRemark                                                        gfx.ID
	shellMenuProfile                                                       gfx.ID
	shellMenuNewGroup                                                      gfx.ID
	shellMenuNewContactGroup                                               gfx.ID
	shellMenuRenameContactGroup                                            gfx.ID
	shellMenuDeleteContactGroup                                            gfx.ID
	shellMenuConnect                                                       gfx.ID
	shellMenuExpand                                                        gfx.ID
	shellMenuCollapse                                                      gfx.ID
	shellMenuScope                                                         gfx.ID
	sidebarSurface                                                         gfx.ID
	selfRow                                                                gfx.ID
	mentionScope                                                           gfx.ID
	mentionDismiss                                                         gfx.ID
	emojiScope                                                             gfx.ID
	messageMenuCopy, messageMenuSave, messageMenuRecall, messageMenuDelete gfx.ID
	emojiDismiss                                                           gfx.ID

	connectScope         gfx.ID
	titleLAN             gfx.ID
	titleGitHub          gfx.ID
	toastScope           gfx.Scope
	connectDismiss       gfx.ID
	connectClose         gfx.ID
	connectField         gfx.ID
	contactTalk          gfx.ID
	contactCall          gfx.ID
	contactRefreshAvatar gfx.ID
	contactMemberScope   gfx.Scope
	chatHeaderAvatar     gfx.ID
	connectCancel        gfx.ID
	connectSubmit        gfx.ID

	composeScope   gfx.ID
	composeDismiss gfx.ID
	composeClose   gfx.ID
	composeName    gfx.ID
	composeSearch  gfx.ID
	composeCreate  gfx.ID
	composeCancel  gfx.ID

	profileScope       gfx.ID
	profileDismiss     gfx.ID
	profileClose       gfx.ID
	profileField       gfx.ID
	profileSignature   gfx.ID
	profileDetails     [5]gfx.ID
	profileGender      gfx.Scope
	profilePresence    gfx.Scope
	profileChooseImage gfx.ID
	profileRemoveImage gfx.ID
	profileSave        gfx.ID
	profileCancel      gfx.ID
	profileCropScope   gfx.ID
	profileCropDismiss gfx.ID
	profileCropClose   gfx.ID
	profileCropCanvas  gfx.ID
	profileCropZoom    gfx.ID
	profileCropOut     gfx.ID
	profileCropIn      gfx.ID
	profileCropCancel  gfx.ID
	profileCropConfirm gfx.ID

	organizeScope   gfx.ID
	organizeDismiss gfx.ID
	organizeClose   gfx.ID
	organizeField   gfx.ID
	organizeSave    gfx.ID
	organizeCancel  gfx.ID

	memberClose          gfx.ID
	memberAvatar         gfx.ID
	memberSearch         gfx.ID
	memberInvite         gfx.ID
	memberStartRename    gfx.ID
	memberRename         gfx.ID
	memberSaveRename     gfx.ID
	memberCancelRename   gfx.ID
	memberStartNickname  gfx.ID
	memberNickname       gfx.ID
	memberSaveNickname   gfx.ID
	memberCancelNickname gfx.ID
	memberLeave          gfx.ID
	memberDissolve       gfx.ID
	memberMenuDismiss    gfx.ID
	memberMenuScope      gfx.ID
	memberProfileScope   gfx.ID
	memberProfileDismiss gfx.ID
	memberProfileClose   gfx.ID
	memberProfileTalk    gfx.ID
	memberMuteToggle     gfx.ID
	memberPinToggle      gfx.ID
	groupAvatarScope     gfx.ID
	groupAvatarDismiss   gfx.ID
	groupAvatarClose     gfx.ID
	groupAvatarCanvas    gfx.ID
	groupAvatarZoom      gfx.ID
	groupAvatarOut       gfx.ID
	groupAvatarIn        gfx.ID
	groupAvatarRemove    gfx.ID
	groupAvatarCancel    gfx.ID
	groupAvatarConfirm   gfx.ID

	fileScope       gfx.ID
	fileDismiss     gfx.ID
	fileClose       gfx.ID
	fileSearch      gfx.ID
	fileFilterAll   gfx.ID
	fileFilterImage gfx.ID
	fileFilterFile  gfx.ID
	fileBack        gfx.ID
	fileOpen        gfx.ID
	fileReveal      gfx.ID
	fileAccept      gfx.ID
	fileReject      gfx.ID
	fileCancel      gfx.ID
	fileRetry       gfx.ID
	fileSave        gfx.ID
	fileRefresh     gfx.ID
	fileBatchCancel gfx.ID

	confirmScope   gfx.ID
	confirmDismiss gfx.ID
	confirmClose   gfx.ID
	confirmCancel  gfx.ID
	confirmRun     gfx.ID

	voiceScope   gfx.Scope
	voiceMute    gfx.ID
	voiceSpeaker gfx.ID
	voiceAccept  gfx.ID
	voiceReject  gfx.ID
	voiceHangup  gfx.ID
}

// UI is the native Viva desktop application. It reads detached backend
// snapshots and owns only presentation and editor state.
type UI struct {
	backend                       domain.Backend
	state                         domain.State
	voiceAudio                    *voiceAudioRuntime
	voiceFocusedFor               string
	voiceWindow                   bool
	voiceBoundID                  string
	voiceInvitation               bool
	voiceConcealed                bool
	inlineVoiceID                 string
	voiceFallbackPage             *ui.Page
	voicePicker                   callDevicePicker
	unlockForVoice                func()
	theme                         ui.Theme
	palette                       palette
	tree                          gfx.Tree
	panels                        Panels
	invalidator                   app.Invalidator
	done                          chan struct{}
	closeOnce                     sync.Once
	lanEndpoint                   string
	lanAnchor                     gfx.Rect
	toasts                        [3]shellNotice
	toastSlot                     int
	noticeRevision                uint64
	updater                       *updateController
	update                        updateView
	updateIDs                     updateControls
	updateNotes                   updateNotesView
	updateOpen, updateResultShown bool
	updateAnnounced               string
	screenshotActive              func() bool
	restartArguments              []string

	titlePage   *ui.Page
	railPage    *ui.Page
	sidebarPage *ui.Page
	chatPage    *ui.Page
	dialogPage  *ui.Page
	drawerPage  *ui.Page
	mentionPage *ui.Page
	emojiPage   *ui.Page
	previewPage *ui.Page
	surface     *chatSurface
	hover       components.HoverTooltip

	ids appIDs

	section            string
	selected           string
	pendingPeer        string
	pendingGroup       string
	sidebarFilter      string
	hiddenCollapsed    bool
	collapsed          map[string]bool
	sidebarSearch      ui.Field
	connectOpen        bool
	connectPending     bool
	connectionSequence uint64
	connectField       ui.Field
	connectRoute       connectionRoute
	contactPeer        string
	contactDrag        ui.DragState
	contactListBounds  ui.Rect
	contactScrollTime  time.Duration
	acceptContactDrop  func(gfx.ID, gfx.ID) bool
	moveDroppedContact func(gfx.ID, gfx.ID)
	contactGroup       string

	contactRefreshSequence  uint64
	contactRefreshRequested bool
	contactRefreshToast     shellNotice

	chatEditor           *text.Editor
	chatEditorField      ui.Field
	requestScreenshot    func()
	wakeViewer           func()
	forwardViewer        func(string)
	presentViewer        func()
	pinViewer            func(bool) bool
	saveViewer           func(string)
	viewerCopying        atomic.Bool
	writeClipboardPNG    func(string, []byte) error
	fullscreenViewer     func(bool) bool
	dismissViewer        func()
	presentDetached      func(string) bool
	presentHistory       func(string) bool
	presentMemberChat    func(string)
	presentArchiveTarget func(string)
	locateMessageID      string
	locateMessageBox     ui.Box
	highlightMessageID   string
	activePrintID        string
	activeVoiceRecord    domain.Message
	openDragPreview      func(app.DragPreviewConfig) (conversationDragPreview, error)
	chatInput            app.TextInput
	chatLayout           components.TextareaLayout
	pendingTexts         map[string]pendingChatText

	drafts          map[string]string
	draftSaved      map[string]string
	moreOpen        bool
	dragging        bool
	chatScrollToEnd bool
	timeline        chatTimeline
	emojiOpen       bool
	emojiButton     ui.Box

	mention       mentionState
	mentionEditor ui.Box
	composerBox   ui.Box
	chatFind      chatFindState

	compose        composeState
	memberSearch   ui.Field
	renameOpen     bool
	renameGroupID  string
	renameField    ui.Field
	nicknameOpen   bool
	nicknameGroup  string
	nicknameField  ui.Field
	nicknameErr    string
	memberMenu     string
	memberProfile  string
	fileSearch     ui.Field
	fileFilter     string
	drawer         string
	activeFileID   string
	fileReturn     string
	confirm        confirmation
	shellMenu      shellMenuState
	organize       organizeState
	lockUI         lockUIState
	focusSearch    bool
	focusedID      gfx.ID
	titleLeading   float32
	titleTrailing  float32
	mainPinned     bool
	lastBody       ui.Rect
	lastChat       ui.Rect
	lastDialogBody ui.Rect
	profile        profileState
	groupAvatar    profileAvatarCropState
	avatarGroup    string
	filePickerOpen bool
	nativeWindow   windowGeometry

	clipboardImage    func() ([]byte, error)
	clipboardCacheDir func() (string, error)

	windowVisible    atomic.Bool
	windowActive     bool
	windowFocusKnown bool
	readMarkID       string
	readMarkCount    int

	conversations    []domain.Conversation
	detached         map[string]bool
	conversationDrag conversationDragState
	groupChats       []domain.Conversation
	contacts         []contactGroupView
	hiddenContacts   contactGroupView
	invitations      []domain.Invitation
	outgoingInvites  []domain.Invitation
	ownershipInvites []domain.Invitation
	messages         []domain.Message
	files            []domain.File
	directByPeer     map[string]string
	groupNicknames   map[string]string
	transfersFor     map[string][]domain.Transfer
	fileLocalPath    map[string]string
	fileAvailable    map[string]bool
	mediaFiles       map[string]mediaFileRequest
	mediaFileGen     uint64

	peerRowIDs          map[string]gfx.ID
	convRowIDs          map[string]gfx.ID
	groupHeadIDs        map[string]gfx.ID
	inviteAcceptIDs     map[string]gfx.ID
	inviteRejectIDs     map[string]gfx.ID
	fileRowIDs          map[string]gfx.ID
	fileButtonIDs       map[string]gfx.ID
	fileAcceptIDs       map[string]gfx.ID
	fileRejectIDs       map[string]gfx.ID
	fileCancelIDs       map[string]gfx.ID
	fileRetryIDs        map[string]gfx.ID
	fileSaveIDs         map[string]gfx.ID
	groupSelectIDs      map[string]gfx.ID
	groupSelectRows     map[string]gfx.ID
	groupRemoveIDs      map[string]gfx.ID
	memberMenuIDs       map[string]gfx.ID
	memberInfoIDs       map[string]gfx.ID
	contactMemberIDs    map[string]gfx.ID
	memberMessageIDs    map[string]gfx.ID
	memberRemoveIDs     map[string]gfx.ID
	memberTransferIDs   map[string]gfx.ID
	composeGroupHeadIDs map[string]gfx.ID
	inviteWithdrawIDs   map[string]gfx.ID
	inviteReinviteIDs   map[string]gfx.ID
	mentionCandidateIDs map[string]gfx.ID
	emojiIDs            map[string]gfx.ID
	emojiRecentIDs      map[string]gfx.ID
	recentEmoji         []string
	emojiPanel          ui.Rect
	messageIDs          map[string]gfx.ID
	messageTextIDs      map[string]gfx.ID
	textSelection       messageTextSelection
	drawTextSelection   func(*ui.DrawContext)
	messageAvatarIDs    map[string]gfx.ID

	imageCache  map[string]gfx.Bitmap
	avatarCache map[string]gfx.Bitmap
	imageStamps map[string]imageStamp
	nextImageID uint64

	avatarImagePaths  map[string]bool
	detachedWindow    bool
	detachedConcealed bool
	detachedPinned    bool
	detachedBlank     bool

	media               *mediaWorker
	mediaImageGen       uint64
	mediaImageQueued    map[string]uint64
	mediaDesiredImages  map[string]bool
	mediaPendingBitmaps map[string]gfx.Bitmap
	iconMessage         gfx.Icon
	iconUsers           gfx.Icon
	iconTransfers       gfx.Icon
	iconFolder          gfx.Icon
	iconSettings        gfx.Icon
	iconMoon            gfx.Icon
	iconSun             gfx.Icon
	iconUserPlus        gfx.Icon
	iconPlus            gfx.Icon
	iconInbox           gfx.Icon
	iconChevronDown     gfx.Icon
	iconChevronRight    gfx.Icon
	iconPin             gfx.Icon
	iconBellOff         gfx.Icon
	iconPaperclip       gfx.Icon
	iconImage           gfx.Icon
	iconSmile           gfx.Icon
	iconSend            gfx.Icon
	iconFolderOpen      gfx.Icon
	iconDownload        gfx.Icon
	iconUpload          gfx.Icon
	iconCheck           gfx.Icon
	iconCheckCheck      gfx.Icon
	iconX               gfx.Icon
	iconMore            gfx.Icon
	iconSearch          gfx.Icon
	iconExternalLink    gfx.Icon
	iconRotate          gfx.Icon
	iconTrash           gfx.Icon
	iconUserMinus       gfx.Icon
	iconCrown           gfx.Icon
	iconLogOut          gfx.Icon
	iconPencil          gfx.Icon
	iconClock           gfx.Icon
	iconAlert           gfx.Icon
	iconWifi            gfx.Icon
	iconWifiOff         gfx.Icon
	iconAt              gfx.Icon
	iconMessageSquare   gfx.Icon
	iconCircleCheck     gfx.Icon
	iconPause           gfx.Icon
	iconHardDrive       gfx.Icon
	iconRefresh         gfx.Icon
	iconEye             gfx.Icon
	iconLock            gfx.Icon
	iconShield          gfx.Icon
	iconCalendar        gfx.Icon
	iconReply           gfx.Icon
	iconArchive         gfx.Icon
	iconLink            gfx.Icon
	iconCopy            gfx.Icon
	iconPhone           gfx.Icon
	iconPhoneOff        gfx.Icon
	iconMic             gfx.Icon
	iconMicOff          gfx.Icon
	iconVolume          gfx.Icon
	iconVolumeX         gfx.Icon
}

// New creates the shell from an already-running backend. It takes the first
// snapshot before the first paint and then refreshes only on OnWake.
func New(backend domain.Backend) *UI {
	return newUIShell(backend, true)
}

func newUIShell(backend domain.Backend, watch bool) *UI {
	u := &UI{
		backend:             backend,
		writeClipboardPNG:   setClipboardPNG,
		theme:               ui.LightTheme(),
		done:                make(chan struct{}),
		section:             sectionContacts,
		sidebarFilter:       "all",
		collapsed:           make(map[string]bool),
		chatEditor:          text.NewEditor(""),
		drafts:              make(map[string]string),
		draftSaved:          make(map[string]string),
		fileFilter:          "all",
		directByPeer:        make(map[string]string),
		groupNicknames:      make(map[string]string),
		transfersFor:        make(map[string][]domain.Transfer),
		fileLocalPath:       make(map[string]string),
		fileAvailable:       make(map[string]bool),
		mediaFiles:          make(map[string]mediaFileRequest),
		detached:            make(map[string]bool),
		peerRowIDs:          make(map[string]gfx.ID),
		convRowIDs:          make(map[string]gfx.ID),
		groupHeadIDs:        make(map[string]gfx.ID),
		inviteAcceptIDs:     make(map[string]gfx.ID),
		inviteRejectIDs:     make(map[string]gfx.ID),
		fileRowIDs:          make(map[string]gfx.ID),
		fileButtonIDs:       make(map[string]gfx.ID),
		fileAcceptIDs:       make(map[string]gfx.ID),
		fileRejectIDs:       make(map[string]gfx.ID),
		fileCancelIDs:       make(map[string]gfx.ID),
		fileRetryIDs:        make(map[string]gfx.ID),
		fileSaveIDs:         make(map[string]gfx.ID),
		groupSelectIDs:      make(map[string]gfx.ID),
		groupSelectRows:     make(map[string]gfx.ID),
		groupRemoveIDs:      make(map[string]gfx.ID),
		memberMenuIDs:       make(map[string]gfx.ID),
		memberInfoIDs:       make(map[string]gfx.ID),
		contactMemberIDs:    make(map[string]gfx.ID),
		memberMessageIDs:    make(map[string]gfx.ID),
		memberRemoveIDs:     make(map[string]gfx.ID),
		memberTransferIDs:   make(map[string]gfx.ID),
		composeGroupHeadIDs: make(map[string]gfx.ID),
		inviteWithdrawIDs:   make(map[string]gfx.ID),
		inviteReinviteIDs:   make(map[string]gfx.ID),
		mentionCandidateIDs: make(map[string]gfx.ID),
		emojiIDs:            make(map[string]gfx.ID),
		emojiRecentIDs:      make(map[string]gfx.ID),
		messageIDs:          make(map[string]gfx.ID),
		messageTextIDs:      make(map[string]gfx.ID),
		messageAvatarIDs:    make(map[string]gfx.ID),
		imageCache:          make(map[string]gfx.Bitmap),
		avatarCache:         make(map[string]gfx.Bitmap),
		imageStamps:         make(map[string]imageStamp),
		avatarImagePaths:    make(map[string]bool),
		mediaImageQueued:    make(map[string]uint64),
		mediaDesiredImages:  make(map[string]bool),
		mediaPendingBitmaps: make(map[string]gfx.Bitmap),
		compose: composeState{
			selected:    make(map[string]bool),
			knownGroups: make(map[string]bool),
		},
		iconMessage:       MessageCircle,
		iconUsers:         Users,
		iconTransfers:     ArrowRightLeft,
		iconFolder:        Folder,
		iconSettings:      Settings,
		iconMoon:          Moon,
		iconSun:           Sun,
		iconUserPlus:      UserPlus,
		iconPlus:          Plus,
		iconInbox:         Inbox,
		iconChevronDown:   ChevronDown,
		iconChevronRight:  ChevronRight,
		iconPin:           Pin,
		iconBellOff:       BellOff,
		iconPaperclip:     Paperclip,
		iconImage:         Image,
		iconSmile:         Smile,
		iconSend:          Send,
		iconFolderOpen:    FolderOpen,
		iconDownload:      Download,
		iconUpload:        Upload,
		iconCheck:         Check,
		iconCheckCheck:    CheckCheck,
		iconX:             X,
		iconMore:          MoreHorizontal,
		iconSearch:        Search,
		iconExternalLink:  ExternalLink,
		iconRotate:        RotateCcw,
		iconTrash:         Trash2,
		iconUserMinus:     UserMinus,
		iconCrown:         Crown,
		iconLogOut:        LogOut,
		iconPencil:        Pencil,
		iconClock:         Clock,
		iconAlert:         CircleAlert,
		iconWifi:          Wifi,
		iconWifiOff:       WifiOff,
		iconAt:            AtSign,
		iconMessageSquare: MessageSquare,
		iconCircleCheck:   CircleCheck,
		iconPause:         Pause,
		iconHardDrive:     HardDriveDownload,
		iconRefresh:       RefreshCw,
		iconEye:           Eye,
		iconLock:          Lock,
		iconShield:        Shield,
		iconCalendar:      Calendar,
		iconReply:         Reply,
		iconArchive:       Archive,
		iconLink:          Link,
		iconCopy:          Copy,
		iconPhone:         Phone,
		iconPhoneOff:      PhoneOff,
		iconMic:           Mic,
		iconMicOff:        MicOff,
		iconVolume:        Volume2,
		iconVolumeX:       VolumeX,
	}
	u.chatEditorField.Bind(u.chatEditor)
	u.acceptContactDrop, u.moveDroppedContact = u.canDropContact, u.dropContact
	u.drawTextSelection = u.paintMessageSelection
	u.chatFind.field.Bind(u.ensureChatSearchEditor())
	u.windowVisible.Store(true)
	u.palette = paletteFor(u.theme, u.state.Settings.ColorScheme)
	u.titleLeading, u.titleTrailing = app.TitlebarInsets()
	u.media = newMediaWorker(func() { u.invalidator.Invalidate() })
	u.voiceAudio = newVoiceAudioRuntime(backend, u.submit, u.invalidator.Invalidate)
	u.initSystemUnlock()
	u.initIDs()
	page := func(root string) *ui.Page {
		return ui.NewPage(ui.PageConfig{Root: root, Tree: &u.tree, Theme: &u.theme})
	}
	u.titlePage, u.railPage, u.sidebarPage = page("title"), page("rail"), page("sidebar")
	u.chatPage, u.drawerPage = page("chat"), page("drawer")
	u.dialogPage, u.mentionPage, u.emojiPage = page("dialog"), page("mention"), page("emoji")
	u.previewPage = page("conversation-preview")
	u.voiceFallbackPage = page("incoming-call")
	u.surface = u.newChatSurface(page("workspace"))
	u.panels.Init(&u.tree, &u.invalidator)
	u.panels.showIncoming = func() { u.setSection(sectionTransfers) }
	u.panels.settingsExtra.openProfile = u.openProfile
	u.panels.openUpdates = u.showUpdates
	u.panels.previewTransfer = u.openTransferMedia
	u.panels.setMediaSubmit(u.media.submitOne)
	u.panels.setToast(u.showToast)
	u.panels.printAvatar = u.paintPrintAvatar
	u.panels.settingsExtra.chooseBackground = u.requestBackgroundPicker
	u.panels.shareQRWorker.setNotify(u.invalidator.Invalidate)
	if backend != nil {
		u.refresh()
		u.section = restoreMainSection(u.state.Settings.LastSection)
		u.recentEmoji = append([]string(nil), u.state.Settings.RecentEmoji...)
		u.queueMediaJobs()
		u.refreshSectionCatalog(u.section)
		if watch {
			go u.watchUpdates()
		}
	}
	u.panels.updateLists(u.state)
	return u
}

// Config describes the one native window this shell owns.
func (u *UI) Config() app.Config {
	clear := colorToApp(u.palette.rail)
	return app.Config{
		Title:          "GoIM",
		Width:          1440,
		Height:         960,
		MinWidth:       1000,
		MinHeight:      700,
		WindowMode:     app.WindowCustomTitlebar,
		TitlebarHeight: titlebarHeight,
		ClearColor:     clear,
		WindowBorderColor: func() app.Color {
			return colorToApp(u.palette.rail)
		},
		Tree:           &u.tree,
		WindowControls: u.windowControls,
		Paint:          u.paint,
		TextInput:      u.textInput,
		Invalidator:    &u.invalidator,
		OnWake:         u.onWake,
		OnActivate:     u.windowActivated,
		OnDeactivate:   u.windowDeactivated,
		OnScroll:       u.onScroll,
		OnPointer:      u.onPointer,
		OnKey:          u.onKey,
		OnShortcut:     u.onShortcut,
		OnFileDrop:     u.onFileDrop,
		OnMaximize:     u.setWindowMaximized,
		OnFullscreen:   u.setWindowFullscreen,
		OnClosed:       u.Close,
	}
}

// Close stops the update watcher and remembers the current draft. It is
// intentionally safe to call both directly and from Config.OnClosed.
func (u *UI) Close() {
	u.closeOnce.Do(func() {
		u.panels.closeMemberProject()
		u.surface.viewer.video.Stop()
		u.voiceAudio.close()
		for _, page := range []*ui.Page{
			u.titlePage, u.railPage, u.sidebarPage, u.chatPage, u.drawerPage,
			u.dialogPage, u.mentionPage, u.emojiPage, u.surface.workspace, u.panels.page, u.voiceFallbackPage,
		} {
			page.Close()
		}
		u.media.close()
		u.saveCurrentDraft()
		close(u.done)
	})
}

func (u *UI) submit(command domain.Command) {
	if u.backend != nil {
		u.backend.Submit(command)
	}
}

func (u *UI) watchUpdates() {
	u.watchUpdatesWith(u.invalidator.Invalidate)
}

func (u *UI) watchUpdatesWith(wake func()) {
	if u.backend == nil {
		return
	}
	updates := u.backend.Updates()
	if updates == nil {
		return
	}
	for {
		select {
		case _, ok := <-updates:
			if !ok {
				return
			}
			wake()
		case <-u.done:
			return
		}
	}
}

func (u *UI) onWake() {
	u.refresh()
	if !u.detachedWindow && !u.voiceWindow {
		u.voiceAudio.sync(u.state.VoiceCall, u.state.Settings)
	}
	if notice := u.voiceAudio.takeNotice(); notice != "" {
		u.showToast(i18n.T("语音通话"), notice, components.ToastError)
	}
	u.collectSystemUnlock()
	u.updater.setAutomatic(!u.state.Settings.DisableAutoUpdateCheck)
	u.observeUpdate()
	u.applyMediaResults()
	u.panels.updateLists(u.state)
	u.queueMediaJobs()
}

// SetWindowVisible tells the shell whether its main window is currently shown
// and not minimized. Read state only advances while the selected chat is
// actually visible.
func (u *UI) SetWindowVisible(visible bool) {
	if u.windowVisible.Swap(visible) == visible {
		return
	}
	u.invalidator.Invalidate()
}

func (u *UI) windowActivated() {
	u.windowFocusKnown, u.windowActive = true, true
	u.invalidator.Invalidate()
}

func (u *UI) windowDeactivated() gfx.EventResult {
	u.windowFocusKnown, u.windowActive = true, false
	return gfx.EventResult{}
}

func (u *UI) foreground(window windowGeometry) bool {
	return (!u.windowFocusKnown || u.windowActive) && mainWindowForeground(window)
}

func (u *UI) refresh() {
	if u.backend == nil {
		return
	}
	oldError := u.state.Error
	u.state = u.backend.Snapshot()
	u.finishChatSubmissions()
	u.lanEndpoint = displayLANEndpoint(u.state)
	i18n.SetLanguage(u.state.Settings.Language)
	u.presentContactRefresh()
	u.presentConnectionFeedback()
	u.presentStateNotice()
	u.panels.OnWake(u.state)
	u.rebuildPresentation(oldError)
}

func (u *UI) presentStateNotice() {
	// The backend notice belongs to the primary GoIM shell. Detached chats share
	// the same backend and state revision, so allowing each shell to consume the
	// notice would display the same success message once per open conversation.
	if u.detachedWindow {
		return
	}
	notice := strings.TrimSpace(u.state.Notice)
	if notice == "" || u.noticeRevision == u.state.Revision {
		return
	}
	u.noticeRevision = u.state.Revision
	u.state.Notice = ""
	u.showToast(i18n.T(notice), "", components.ToastSuccess)
	u.submit(domain.Command{Kind: domain.ClearError})
}

func (u *UI) requestBackgroundPicker(canvas *gfx.Canvas) {
	canvas.AfterPaint(func() {
		paths, err := u.openFilePicker(app.OpenFileOptions{
			Title: i18n.T("选择聊天背景"),
			Filters: []app.FileFilter{{
				Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif",
			}},
		})
		switch {
		case err == nil && len(paths) > 0:
			u.submit(domain.Command{Kind: domain.SetSetting, Setting: "background", Value: paths[0]})
		case err != nil && !errors.Is(err, app.ErrCanceled) && !errors.Is(err, app.ErrNoDriverResponse):
			u.showToast(i18n.T("无法选择背景"), filePickerProblem(err), components.ToastError)
		}
	})
}

func (u *UI) requestFileAvailability(fileID, path string, previous bool, previousSet bool) {
	if fileID == "" || path == "" {
		return
	}
	generation := uint64(0)
	if queued, ok := u.mediaFiles[fileID]; ok && queued.path == path {
		if !previousSet {
			return
		}
		generation = queued.generation
	} else {
		u.mediaFileGen++
		generation = u.mediaFileGen
		u.mediaFiles[fileID] = mediaFileRequest{path: path, generation: generation}
	}
	u.media.submitOne(mediaJob{
		key:  mediaJobKey{kind: mediaJobFileAvailability, name: fileID},
		kind: mediaJobFileAvailability, path: path, fileID: fileID, generation: generation,
		previousAvailable: previous, previousAvailableSet: previousSet,
	})
}

func (u *UI) queueMediaJobs() {
	u.queueViewerOriginal()
	for fileID, path := range u.fileLocalPath {
		available, known := u.fileAvailable[fileID]
		u.requestFileAvailability(fileID, path, available, known)
	}
	for path := range u.mediaDesiredImages {
		u.queueImage(path)
	}
}

func (u *UI) applyMediaResults() {
	if u.media == nil {
		return
	}
	batch, ok := u.media.take()
	if !ok {
		return
	}
	u.applyMediaBatch(batch)
}

func (u *UI) applyMediaBatch(batch mediaResultBatch) {
	u.applyViewerOriginal(batch.viewerImage)
	availability := make([]mediaFileResult, 0, len(batch.files))
	for _, result := range batch.files {
		queued, ok := u.mediaFiles[result.fileID]
		if !ok || queued.generation != result.generation {
			continue
		}
		delete(u.mediaFiles, result.fileID)
		if queued.path != result.path || u.fileLocalPath[result.fileID] != result.path {
			continue
		}
		u.fileAvailable[result.fileID] = result.available
		availability = append(availability, result)
	}
	if len(availability) > 0 {
		u.panels.setLocalFileAvailability(availability)
	}

	for _, result := range batch.images {
		generation, queued := u.mediaImageQueued[result.path]
		if !queued || generation != result.generation {
			continue
		}
		delete(u.mediaImageQueued, result.path)
		if !u.mediaDesiredImages[result.path] {
			delete(u.mediaPendingBitmaps, result.path)
			continue
		}
		delete(u.mediaPendingBitmaps, result.path)
		u.imageStamps[result.path] = result.stamp
		if result.unchanged {
			if u.avatarImagePaths[result.path] {
				u.cacheAvatarBitmap(result.path)
			}
			u.syncProfileAvatarCrop(result.path)
			u.syncGroupAvatarCrop(result.path)
			if u.profile.open && u.profile.avatar == result.path {
				if _, ok := u.imageCache[result.path]; ok {
					u.profile.err = ""
				} else {
					u.profile.err = i18n.T("无法读取这张图片")
				}
			}
			continue
		}
		if !result.decoded {
			delete(u.imageCache, result.path)
			delete(u.avatarCache, result.path)
			u.rejectAvatarImage(result.path, result.err)
			if u.profile.open && u.profile.avatar == result.path {
				u.profile.err = i18n.T("无法读取这张图片")
			}
			continue
		}
		u.nextImageID++
		bitmap := result.bitmap
		bitmap.ID = u.nextImageID
		bitmap.Version = 1
		u.imageCache[result.path] = bitmap
		delete(u.avatarCache, result.path)
		if u.avatarImagePaths[result.path] {
			u.cacheAvatarBitmap(result.path)
		}
		u.syncProfileAvatarCrop(result.path)
		u.syncGroupAvatarCrop(result.path)
		if u.profile.open && u.profile.avatar == result.path {
			u.profile.err = ""
		}
	}

	if len(batch.actions) > 0 {
		u.panels.applyMediaActions(batch.actions)
	}
}

func (u *UI) markSelectedConversationRead() {
	if !u.windowVisible.Load() || u.section != sectionMessages || u.selected == "" ||
		u.pendingPeer != "" || u.pendingGroup != "" || u.chatFind.open || u.modalOpen() {
		return
	}
	if u.timeline.conversation == u.selected && !u.timeline.atEnd {
		return
	}
	conversation := u.state.Conversation(u.selected)
	if conversation.ID == "" || conversation.Unread <= 0 {
		u.readMarkID = ""
		u.readMarkCount = 0
		return
	}
	if u.readMarkID == conversation.ID && u.readMarkCount == conversation.Unread {
		return
	}
	if u.nativeWindow != nil && !u.foreground(u.nativeWindow) {
		return
	}
	u.readMarkID = conversation.ID
	u.readMarkCount = conversation.Unread
	u.submit(domain.Command{Kind: domain.MarkRead, ConversationID: conversation.ID})
}

func (u *UI) modalOpen() bool {
	return u.confirm.open || u.compose.open || u.connectOpen || u.updateOpen ||
		u.profile.open || u.groupAvatar.open || u.memberProfile != "" || u.organize.open ||
		u.shellMenu.open || u.panels.resetDataOpen || u.lockVisible() || u.voiceVisible()
}

func (u *UI) unreadTotal() int {
	total := 0
	for _, conversation := range u.state.Conversations {
		if !conversation.Hidden && conversation.Unread > 0 {
			total += conversation.Unread
		}
	}
	return total
}

func (u *UI) incompleteTransferCount() int {
	total := 0
	for _, transfer := range u.state.Transfers {
		if transferNeedsAttention(transfer) {
			total++
		}
	}
	return total
}

func compactCount(count int) string {
	if count > 99 {
		return "99+"
	}
	return intString(count)
}

func (u *UI) initIDs() {
	u.ids.page = u.tree.Feature("goim")
	u.ids.nav = u.tree.Sub(u.ids.page, u.tree.Name("navigation"))
	u.ids.side = u.tree.Sub(u.ids.page, u.tree.Name("sidebar"))
	u.ids.chat = u.tree.Sub(u.ids.page, u.tree.Name("chat"))
	u.ids.dialog = u.tree.Sub(u.ids.page, u.tree.Name("dialogs"))
	u.ids.menu = u.tree.Sub(u.ids.page, u.tree.Name("menus"))
	u.ids.titleGitHub = u.tree.ID(u.ids.page, "title-github")
	u.ids.titleLAN = u.tree.ID(u.ids.page, "lan-status")
	u.ids.toastScope = u.tree.Sub(u.ids.page, u.tree.Name("notices"))
	u.updateIDs.init(&u.tree, u.ids.page)

	u.ids.navMessages = u.tree.ID(u.ids.nav, "messages")
	u.ids.navContacts = u.tree.ID(u.ids.nav, "contacts")
	u.ids.navTransfers = u.tree.ID(u.ids.nav, "transfers")
	u.ids.navShares = u.tree.ID(u.ids.nav, "shares")
	u.ids.navPrint = u.tree.ID(u.ids.nav, "print")
	u.ids.navLock = u.tree.ID(u.ids.nav, "lock")
	u.ids.navTheme = u.tree.ID(u.ids.nav, "appearance")
	u.ids.navSettings = u.tree.ID(u.ids.nav, "settings")
	u.ids.profileAvatar = u.tree.ID(u.ids.nav, "profile-avatar")
	u.ids.detachedPin = u.tree.ID(u.ids.page, "detached-pin")

	u.ids.sidebarCreate = u.tree.ID(u.ids.side, "create")
	u.ids.sidebarConnect = u.tree.ID(u.ids.side, "connect")
	u.ids.sidebarSearch = u.tree.ID(u.ids.side, "search")
	u.ids.sidebarRefresh = u.tree.ID(u.ids.side, "refresh")
	u.ids.filterAll = u.tree.ID(u.ids.side, "filter-all")
	u.ids.filterOnline = u.tree.ID(u.ids.side, "filter-online")
	u.ids.filterHidden = u.tree.ID(u.ids.side, "filter-hidden")
	u.ids.hiddenGroupHead = u.tree.ID(u.ids.side, "hidden-contacts")
	u.ids.emptyConnect = u.tree.ID(u.ids.side, "empty-connect")
	u.ids.emptyCompose = u.tree.ID(u.ids.side, "empty-compose")
	u.ids.sidebarSurface = u.tree.ID(u.ids.side, "surface")
	u.ids.selfRow = u.tree.ID(u.ids.side, "self-row")
	u.ids.mentionScope = u.tree.ID(u.ids.chat, "mention-scope")
	u.ids.mentionDismiss = u.tree.ID(u.ids.chat, "mention-dismiss")
	u.ids.emojiScope = u.tree.ID(u.ids.chat, "emoji-scope")
	u.ids.messageMenuCopy = u.tree.ID(u.ids.menu, "message-copy")
	u.ids.messageMenuSave = u.tree.ID(u.ids.menu, "message-save")
	u.ids.messageMenuRecall = u.tree.ID(u.ids.menu, "message-recall")
	u.ids.messageMenuDelete = u.tree.ID(u.ids.menu, "message-delete")
	u.ids.emojiDismiss = u.tree.ID(u.ids.chat, "emoji-dismiss")

	u.ids.chatScreenshot = u.tree.ID(u.ids.chat, "screenshot")
	u.ids.chatSend = u.tree.ID(u.ids.chat, "send")
	u.ids.chatImage = u.tree.ID(u.ids.chat, "send-image")
	u.ids.chatFile = u.tree.ID(u.ids.chat, "send-file")
	u.ids.chatFolder = u.tree.ID(u.ids.chat, "send-folder")
	u.ids.chatCurrentContact = u.tree.ID(u.ids.chat, "current-contact")
	u.ids.chatEmoji = u.tree.ID(u.ids.chat, "emoji")
	u.ids.chatEditorField = u.tree.ID(u.ids.chat, "composer-editor")
	u.ids.mainPin = u.tree.ID(u.ids.page, "main-pin")
	u.ids.chatHeaderSearch = u.tree.ID(u.ids.chat, "header-search")
	u.ids.chatHeaderFile = u.tree.ID(u.ids.chat, "header-files")
	u.ids.chatHeaderMembers = u.tree.ID(u.ids.chat, "header-members")
	u.ids.chatHeaderMore = u.tree.ID(u.ids.chat, "header-more")
	u.ids.chatHeaderCall = u.tree.ID(u.ids.chat, "header-call")
	u.ids.chatSearchField = u.tree.ID(u.ids.chat, "find-field")
	u.ids.chatSearchClose = u.tree.ID(u.ids.chat, "find-close")
	u.ids.chatEmptyConnect = u.tree.ID(u.ids.chat, "empty-connect")
	u.ids.chatEmptyCompose = u.tree.ID(u.ids.chat, "empty-compose")
	u.ids.chatMenuDismiss = u.tree.ID(u.ids.menu, "chat-dismiss")
	u.ids.chatMenuPin = u.tree.ID(u.ids.menu, "chat-pin")
	u.ids.chatMenuMute = u.tree.ID(u.ids.menu, "chat-mute")
	u.ids.chatMenuRead = u.tree.ID(u.ids.menu, "chat-read")
	u.ids.chatMenuLeave = u.tree.ID(u.ids.menu, "chat-leave")
	u.ids.chatNoticeDismiss = u.tree.ID(u.ids.chat, "notice-dismiss")
	u.ids.chatMenuScope = u.tree.ID(u.ids.menu, "chat-menu-scope")
	u.ids.shellMenuDismiss = u.tree.ID(u.ids.menu, "shell-dismiss")
	u.ids.shellMenuPin = u.tree.ID(u.ids.menu, "shell-pin")
	u.ids.shellMenuMute = u.tree.ID(u.ids.menu, "shell-mute")
	u.ids.shellMenuRead = u.tree.ID(u.ids.menu, "shell-read")
	u.ids.shellMenuDelete = u.tree.ID(u.ids.menu, "shell-delete")
	u.ids.shellMenuTalk = u.tree.ID(u.ids.menu, "shell-talk")
	u.ids.shellMenuOrganize = u.tree.ID(u.ids.menu, "shell-organize")
	u.ids.shellMenuHideContact = u.tree.ID(u.ids.menu, "shell-hide-contact")
	u.ids.shellMenuDeleteContact = u.tree.ID(u.ids.menu, "shell-delete-contact")
	u.ids.shellMenuRemark = u.tree.ID(u.ids.menu, "shell-remark")
	u.ids.shellMenuProfile = u.tree.ID(u.ids.menu, "shell-profile")
	u.ids.shellMenuNewGroup = u.tree.ID(u.ids.menu, "shell-new-group")
	u.ids.shellMenuNewContactGroup = u.tree.ID(u.ids.menu, "shell-new-contact-group")
	u.ids.shellMenuRenameContactGroup = u.tree.ID(u.ids.menu, "shell-rename-contact-group")
	u.ids.shellMenuDeleteContactGroup = u.tree.ID(u.ids.menu, "shell-delete-contact-group")
	u.ids.shellMenuConnect = u.tree.ID(u.ids.menu, "shell-connect")
	u.ids.shellMenuExpand = u.tree.ID(u.ids.menu, "shell-expand")
	u.ids.shellMenuCollapse = u.tree.ID(u.ids.menu, "shell-collapse")
	u.ids.shellMenuScope = u.tree.ID(u.ids.menu, "shell-menu-scope")

	u.ids.connectScope = u.tree.ID(u.ids.dialog, "connect-scope")
	u.ids.connectDismiss = u.tree.ID(u.ids.dialog, "connect-dismiss")
	u.ids.connectClose = u.tree.ID(u.ids.dialog, "connect-close")
	u.ids.connectField = u.tree.ID(u.ids.dialog, "connect-address")
	contacts := u.tree.Feature("contact-details")
	u.ids.contactTalk = u.tree.ID(contacts, "primary-action")
	u.ids.contactCall = u.tree.ID(contacts, "call-action")
	u.ids.contactRefreshAvatar = u.tree.ID(contacts, "refresh-avatar")
	u.ids.contactMemberScope = u.tree.Sub(contacts, u.tree.Name("members"))
	u.ids.chatHeaderAvatar = u.tree.ID(u.ids.chat, "peer-avatar")
	u.ids.connectCancel = u.tree.ID(u.ids.dialog, "connect-cancel")
	u.ids.connectSubmit = u.tree.ID(u.ids.dialog, "connect-submit")

	u.ids.composeScope = u.tree.ID(u.ids.dialog, "compose-scope")
	u.ids.composeDismiss = u.tree.ID(u.ids.dialog, "compose-dismiss")
	u.ids.composeClose = u.tree.ID(u.ids.dialog, "compose-close")
	u.ids.composeName = u.tree.ID(u.ids.dialog, "compose-name")
	u.ids.composeSearch = u.tree.ID(u.ids.dialog, "compose-search")
	u.ids.composeCreate = u.tree.ID(u.ids.dialog, "compose-submit")
	u.ids.composeCancel = u.tree.ID(u.ids.dialog, "compose-cancel")

	u.ids.profileScope = u.tree.ID(u.ids.dialog, "profile-scope")
	u.ids.profileDismiss = u.tree.ID(u.ids.dialog, "profile-dismiss")
	u.ids.profileClose = u.tree.ID(u.ids.dialog, "profile-close")
	u.ids.profileField = u.tree.ID(u.ids.dialog, "profile-nickname")
	u.ids.profileSignature = u.tree.ID(u.ids.dialog, "profile-signature")
	for i, name := range profileDetailNames {
		u.ids.profileDetails[i] = u.tree.ID(u.ids.dialog, "profile-"+name)
	}
	u.ids.profileGender = u.tree.Sub(u.ids.dialog, u.tree.Name("profile-gender"))
	u.ids.profilePresence = u.tree.Sub(u.ids.dialog, u.tree.Name("profile-presence"))
	u.ids.profileChooseImage = u.tree.ID(u.ids.dialog, "profile-choose-image")
	u.ids.profileRemoveImage = u.tree.ID(u.ids.dialog, "profile-remove-image")
	u.ids.profileSave = u.tree.ID(u.ids.dialog, "profile-save")
	u.ids.profileCancel = u.tree.ID(u.ids.dialog, "profile-cancel")
	u.ids.profileCropScope = u.tree.ID(u.ids.dialog, "profile-avatar-crop-scope")
	u.ids.profileCropDismiss = u.tree.ID(u.ids.dialog, "profile-avatar-crop-dismiss")
	u.ids.profileCropClose = u.tree.ID(u.ids.dialog, "profile-avatar-crop-close")
	u.ids.profileCropCanvas = u.tree.ID(u.ids.dialog, "profile-avatar-crop-canvas")
	u.ids.profileCropZoom = u.tree.ID(u.ids.dialog, "profile-avatar-crop-zoom")
	u.ids.profileCropOut = u.tree.ID(u.ids.dialog, "profile-avatar-crop-zoom-out")
	u.ids.profileCropIn = u.tree.ID(u.ids.dialog, "profile-avatar-crop-zoom-in")
	u.ids.profileCropCancel = u.tree.ID(u.ids.dialog, "profile-avatar-crop-cancel")
	u.ids.profileCropConfirm = u.tree.ID(u.ids.dialog, "profile-avatar-crop-confirm")

	u.ids.organizeScope = u.tree.ID(u.ids.dialog, "organize-scope")
	u.ids.organizeDismiss = u.tree.ID(u.ids.dialog, "organize-dismiss")
	u.ids.organizeClose = u.tree.ID(u.ids.dialog, "organize-close")
	u.ids.organizeField = u.tree.ID(u.ids.dialog, "organize-field")
	u.ids.organizeSave = u.tree.ID(u.ids.dialog, "organize-save")
	u.ids.organizeCancel = u.tree.ID(u.ids.dialog, "organize-cancel")

	u.ids.memberClose = u.tree.ID(u.ids.dialog, "member-close")
	u.ids.memberAvatar = u.tree.ID(u.ids.dialog, "member-avatar")
	u.ids.memberSearch = u.tree.ID(u.ids.dialog, "member-search")
	u.ids.memberInvite = u.tree.ID(u.ids.dialog, "member-invite")
	u.ids.memberStartRename = u.tree.ID(u.ids.dialog, "member-start-rename")
	u.ids.memberRename = u.tree.ID(u.ids.dialog, "member-rename-field")
	u.ids.memberSaveRename = u.tree.ID(u.ids.dialog, "member-save-rename")
	u.ids.memberCancelRename = u.tree.ID(u.ids.dialog, "member-cancel-rename")
	u.ids.memberStartNickname = u.tree.ID(u.ids.dialog, "member-edit-nickname")
	u.ids.memberNickname = u.tree.ID(u.ids.dialog, "member-nickname-field")
	u.ids.memberSaveNickname = u.tree.ID(u.ids.dialog, "member-save-nickname")
	u.ids.memberCancelNickname = u.tree.ID(u.ids.dialog, "member-cancel-nickname")
	u.ids.memberLeave = u.tree.ID(u.ids.dialog, "member-leave")
	u.ids.memberDissolve = u.tree.ID(u.ids.dialog, "member-dissolve")
	u.ids.memberMenuDismiss = u.tree.ID(u.ids.dialog, "member-menu-dismiss")
	u.ids.memberMenuScope = u.tree.ID(u.ids.dialog, "member-menu-scope")
	u.ids.memberProfileScope = u.tree.ID(u.ids.dialog, "member-profile-scope")
	u.ids.memberProfileDismiss = u.tree.ID(u.ids.dialog, "member-profile-dismiss")
	u.ids.memberProfileClose = u.tree.ID(u.ids.dialog, "member-profile-close")
	u.ids.memberProfileTalk = u.tree.ID(u.ids.dialog, "member-profile-message")
	u.ids.memberMuteToggle = u.tree.ID(u.ids.dialog, "member-mute")
	u.ids.memberPinToggle = u.tree.ID(u.ids.dialog, "member-pin")
	u.ids.groupAvatarScope = u.tree.ID(u.ids.dialog, "group-avatar-crop-scope")
	u.ids.groupAvatarDismiss = u.tree.ID(u.ids.dialog, "group-avatar-crop-dismiss")
	u.ids.groupAvatarClose = u.tree.ID(u.ids.dialog, "group-avatar-crop-close")
	u.ids.groupAvatarCanvas = u.tree.ID(u.ids.dialog, "group-avatar-crop-canvas")
	u.ids.groupAvatarZoom = u.tree.ID(u.ids.dialog, "group-avatar-crop-zoom")
	u.ids.groupAvatarOut = u.tree.ID(u.ids.dialog, "group-avatar-crop-zoom-out")
	u.ids.groupAvatarIn = u.tree.ID(u.ids.dialog, "group-avatar-crop-zoom-in")
	u.ids.groupAvatarRemove = u.tree.ID(u.ids.dialog, "group-avatar-crop-remove")
	u.ids.groupAvatarCancel = u.tree.ID(u.ids.dialog, "group-avatar-crop-cancel")
	u.ids.groupAvatarConfirm = u.tree.ID(u.ids.dialog, "group-avatar-crop-confirm")

	u.ids.fileScope = u.tree.ID(u.ids.dialog, "file-scope")
	u.ids.fileDismiss = u.tree.ID(u.ids.dialog, "file-dismiss")
	u.ids.fileClose = u.tree.ID(u.ids.dialog, "file-close")
	u.ids.fileSearch = u.tree.ID(u.ids.dialog, "file-search")
	u.ids.fileFilterAll = u.tree.ID(u.ids.dialog, "file-filter-all")
	u.ids.fileFilterImage = u.tree.ID(u.ids.dialog, "file-filter-images")
	u.ids.fileFilterFile = u.tree.ID(u.ids.dialog, "file-filter-files")
	u.ids.fileBack = u.tree.ID(u.ids.dialog, "file-back")
	u.ids.fileOpen = u.tree.ID(u.ids.dialog, "file-open")
	u.ids.fileReveal = u.tree.ID(u.ids.dialog, "file-reveal")
	u.ids.fileAccept = u.tree.ID(u.ids.dialog, "file-accept")
	u.ids.fileReject = u.tree.ID(u.ids.dialog, "file-reject")
	u.ids.fileCancel = u.tree.ID(u.ids.dialog, "file-cancel")
	u.ids.fileRetry = u.tree.ID(u.ids.dialog, "file-retry")
	u.ids.fileSave = u.tree.ID(u.ids.dialog, "file-save")
	u.ids.fileRefresh = u.tree.ID(u.ids.dialog, "file-refresh")
	u.ids.fileBatchCancel = u.tree.ID(u.ids.dialog, "file-batch-cancel")

	u.ids.confirmScope = u.tree.ID(u.ids.dialog, "confirm-scope")
	u.ids.confirmDismiss = u.tree.ID(u.ids.dialog, "confirm-dismiss")
	u.ids.confirmClose = u.tree.ID(u.ids.dialog, "confirm-close")
	u.ids.confirmCancel = u.tree.ID(u.ids.dialog, "confirm-cancel")
	u.ids.confirmRun = u.tree.ID(u.ids.dialog, "confirm-run")

	u.ids.voiceScope = u.tree.Sub(u.ids.dialog, u.tree.Name("voice-call"))
	u.ids.voiceMute = u.tree.ID(u.ids.voiceScope, "mute")
	u.ids.voiceSpeaker = u.tree.ID(u.ids.voiceScope, "speaker")
	u.ids.voiceAccept = u.tree.ID(u.ids.voiceScope, "accept")
	u.ids.voiceReject = u.tree.ID(u.ids.voiceScope, "reject")
	u.ids.voiceHangup = u.tree.ID(u.ids.voiceScope, "hangup")

	for _, choice := range emojiChoices {
		u.emojiIDs[choice.Name] = u.tree.ID(u.ids.chat, "emoji:"+choice.Name)
		u.emojiRecentIDs[choice.Name] = u.tree.ID(u.ids.chat, "emoji-recent:"+choice.Name)
	}
}

func (u *UI) paint(canvas *gfx.Canvas, size ui.Size) {
	u.theme = themeForSettings(u.state.Settings)
	u.palette = paletteFor(u.theme, u.state.Settings.ColorScheme)
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.palette.rail)
	u.focusedID = canvas.Focused()
	u.emojiButton, u.mentionEditor, u.composerBox = ui.Box{}, ui.Box{}, ui.Box{}

	totalTitle := min(titlebarHeight, bounds.Height)
	titlebar := ui.Rect{Width: bounds.Width, Height: totalTitle}
	body := ui.Rect{Y: totalTitle, Width: bounds.Width, Height: max(bounds.Height-totalTitle, 0)}
	u.lastBody = body
	u.drawTitlebar(canvas, titlebar)
	if u.lockVisible() {
		u.drawLocked(canvas, body.Inset(gfx.Insets{Left: workspaceInset, Right: workspaceInset, Bottom: workspaceInset}))
		u.textSelection.clear()
		u.drawToasts(canvas, body)
		u.markSelectedConversationRead()
		return
	}

	u.panels.dialogViewport = bounds
	u.drawMainBody(canvas, body)
	u.drawConversationDragGhost(canvas, body)
	u.drawContactDragGhost(canvas, body)

	if u.chatFind.focus && u.chatFind.open {
		canvas.RequestFocus(u.ids.chatSearchField)
		u.chatFind.focus = false
	}
	if u.focusSearch {
		canvas.RequestFocus(u.ids.sidebarSearch)
		u.focusSearch = false
	}
	u.hover.Paint(canvas, u.theme, body, components.TooltipOptions{Layer: 55}, 0)
	u.drawLANHint(canvas, bounds)
	u.drawToasts(canvas, body)
	u.drawOverlays(canvas, bounds)
	u.drawChatImageViewer(canvas)
	if u.inlineVoiceID != "" && u.inlineVoiceID == u.state.VoiceCall.ID && incomingVoiceInvitation(u.state.VoiceCall) {
		width, height := u.voiceInvitationSize()
		card := ui.Rect{Width: min(float32(width), body.Width), Height: min(float32(height), body.Height)}
		card.X, card.Y = max(0, size.Width-card.Width-12), max(body.Y, size.Height-card.Height-12)
		u.drawVoiceInvitation(canvas, card, u.inlineVoiceID)
	}
	u.markSelectedConversationRead()
}

func (u *UI) isPanelSection() bool {
	return u.section == sectionSettings || u.section == sectionTransfers ||
		u.section == sectionShares || u.section == sectionPrint
}

func (u *UI) drawTitlebar(canvas *gfx.Canvas, bounds ui.Rect) {
	if bounds.Height <= 0 {
		return
	}
	canvas.Fill(bounds, u.palette.rail)
	var brand, status, pin ui.Box
	var githubClicked bool
	u.titlePage.Draw(canvas, bounds, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().
			PadSides(0, u.titleTrailing+14, 0, u.titleLeading+20).
			Items(ui.Center).Children(func() {
			brand = ui.Slot(f).Size(24, 24)
			ui.Row(f).Width(9).Height(1)
			ui.Text(f, "GoIM").FontSize(textSize(f, 16)).Weight(gfx.WeightSemibold)
			ui.Spacer(f)
			u.drawUpdateTitlebar(f, bounds.Width >= 650)
			status = u.drawLANStatus(f, bounds.Width >= 480)
			pinLabel := i18n.T("窗口置顶")
			if u.mainPinned {
				pinLabel = i18n.T("取消窗口置顶")
			}
			pin = u.iconButton(f, u.ids.mainPin, u.iconPin, pinLabel, 30)
			if u.mainPinned {
				pin = pin.Background(u.palette.selected)
			}
			if !u.state.Settings.HideTitleGitHub {
				githubClicked = u.iconButton(f, u.ids.titleGitHub, AboutGithub, i18n.T("GoIM 项目主页"), 30).Clicked()
			}
		})
	})
	paintGoIMMark(canvas, brand.Rect(), titleMark)
	u.lanAnchor = status.Rect()
	if githubClicked {
		canvas.AfterPaint(func() {
			if err := app.OpenURL(buildinfo.RepositoryURL); err != nil {
				u.showToast(i18n.T("无法打开链接"), err.Error(), components.ToastError)
			}
		})
	}
	if pin.Clicked() {
		u.toggleMainPinned()
	}
}

func restoreMainSection(section string) string {
	switch section {
	case sectionMessages, sectionContacts, sectionTransfers, sectionShares, sectionPrint, sectionSettings:
		return section
	default:
		return sectionContacts
	}
}

func (u *UI) toggleMainPinned() {
	pinner, ok := u.nativeWindow.(interface{ SetAlwaysOnTop(bool) error })
	if !ok {
		u.showToast(i18n.T("无法更改主窗口置顶"), i18n.T("当前系统不支持窗口置顶。"), components.ToastWarning)
		return
	}
	next := !u.mainPinned
	if err := pinner.SetAlwaysOnTop(next); err != nil {
		u.showToast(i18n.T("无法更改主窗口置顶"), err.Error(), components.ToastWarning)
		return
	}
	u.mainPinned = next
	u.invalidator.Invalidate()
}

func (u *UI) drawRail(canvas *gfx.Canvas, bounds ui.Rect) {
	canvas.Fill(bounds, u.palette.rail)
	var avatarSlot ui.Box
	var messages, contacts, transfers, shares, printShare, lock, themeToggle, settings ui.Box
	itemHeight, itemGap, railPadding, avatarSize, avatarGap := float32(58), float32(5), float32(14), float32(42), float32(10)
	if bounds.Height < 660 {
		itemHeight, itemGap, railPadding, avatarSize, avatarGap = 48, 2, 8, 36, 4
	}
	itemHeight = max(itemHeight, 37+u.theme.Metrics.ScaledText(11))
	unreadBadge, unreadHint := "", ""
	if unread := u.unreadTotal(); unread > 0 {
		unreadBadge = compactCount(unread)
		unreadHint = intString(unread) + i18n.T(" 条未读")
	}
	transferBadge, transferHint := "", ""
	if active := u.incompleteTransferCount(); active > 0 {
		transferBadge = compactCount(active)
		transferHint = intString(active) + i18n.T(" 个文件任务未完成")
	}
	selfName := u.selfDisplayName()
	selfAvatar := u.selfAvatarPath()
	u.railPage.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().PadY(railPadding).Items(ui.Center).Gap(itemGap).Children(func() {
			avatarSlot = ui.Stack(f).Size(avatarSize, avatarSize).HitWith(u.ids.profileAvatar, i18n.T("编辑个人资料"),
				gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("编辑昵称和头像")}).
				Children(func() {
					u.avatarStatusOn(f, selfName, selfAvatar, avatarSize, u.state.Self.Presence != domain.PresenceInvisible, u.palette.rail)
				})
			ui.Row(f).Height(avatarGap)
			messages = u.railItemBadged(f, u.ids.navMessages, u.iconMessage, i18n.T("消息"),
				u.section == sectionMessages, unreadBadge, unreadHint, itemHeight)
			contacts = u.railItem(f, u.ids.navContacts, u.iconUsers, i18n.T("联系人"), u.section == sectionContacts, itemHeight)
			transfers = u.railItemBadged(f, u.ids.navTransfers, u.iconTransfers, i18n.T("传输"),
				u.section == sectionTransfers, transferBadge, transferHint, itemHeight)
			shares = u.railItem(f, u.ids.navShares, FolderOutput, "文件共享", u.section == sectionShares, itemHeight)
			printShare = u.railItem(f, u.ids.navPrint, panelPrintIcon, "打印共享", u.section == sectionPrint, itemHeight)
			ui.Spacer(f)
			lock = u.railItem(f, u.ids.navLock, u.iconLock, i18n.T("锁定应用"), false, itemHeight)
			themeIcon, themeLabel := u.iconMoon, "切换到暗色外观"
			if u.isDark() {
				themeIcon, themeLabel = u.iconSun, "切换到明亮外观"
			}
			themeToggle = u.railItem(f, u.ids.navTheme, themeIcon, themeLabel, false, itemHeight)
			settings = u.railItem(f, u.ids.navSettings, u.iconSettings, i18n.T("设置"), u.section == sectionSettings, itemHeight)
		})
	})
	if messages.Clicked() {
		u.setSection(sectionMessages)
	}
	if contacts.Clicked() {
		u.setSection(sectionContacts)
	}
	if transfers.Clicked() {
		u.setSection(sectionTransfers)
	}
	if shares.Clicked() {
		u.setSection(sectionShares)
	}
	if printShare.Clicked() {
		u.setSection(sectionPrint)
	}
	if lock.Clicked() {
		u.requestLock()
	}
	if themeToggle.Clicked() {
		u.toggleTheme()
	}
	if settings.Clicked() {
		u.setSection(sectionSettings)
	}
	if avatarSlot.Clicked() {
		u.openProfile()
	}
}

func (u *UI) railItem(f ui.C, id gfx.ID, icon gfx.Icon, label string, active bool, height float32) ui.Box {
	return u.railItemBadged(f, id, icon, label, active, "", "", height)
}

func (u *UI) railItemBadged(f ui.C, id gfx.ID, icon gfx.Icon, label string,
	active bool, badge, badgeHint string, height float32) ui.Box {
	background := ui.Color{}
	foreground := u.palette.icon
	if active {
		background = u.palette.selected
		foreground = u.palette.selectedText
	}
	hover := u.palette.hover
	if active {
		hover = background
	}
	visual := i18n.T(label)
	if label == "文件共享" && i18n.Language() == i18n.English {
		visual = "Shares"
	}
	if label == "打印共享" && i18n.Language() == i18n.English {
		visual = "Print"
	}
	if label == "切换到暗色外观" || label == "切换到明亮外观" {
		visual = "外观"
		if i18n.Language() == i18n.English {
			visual = "Theme"
		}
	}
	accessible := i18n.T(label)
	if badge != "" && badgeHint != "" {
		accessible += "，" + badgeHint
	}
	box := ui.Column(f).Size(u.railExtent()-12, height).Radius(radiusControl).Items(ui.Center).Justify(ui.Center).Gap(4).
		Background(background).Hover(ui.Look{}.Background(hover)).
		HitWith(id, accessible, gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Hint: accessible}).
		Children(func() {
			badgeHeight := max(float32(16), u.theme.Metrics.ScaledText(9)+7)
			ui.Stack(f).Width(max(36, badgeHeight+20)).Height(max(24, badgeHeight)).Children(func() {
				ui.Stack(f).Size(21, 21).Absolute().Left(7).Top(2).Children(func() {
					ui.Icon(f, icon).Size(21, 21).Color(foreground)
				})
				if badge != "" {
					ui.Row(f).MinWidth(badgeHeight).Height(badgeHeight).PadX(4).Radius(badgeHeight / 2).Absolute().Right(0).Top(0).
						Background(u.theme.Primary).Items(ui.Center).Justify(ui.Center).
						Children(func() {
							ui.Text(f, badge).FontSize(textSize(f, 9)).Weight(gfx.WeightSemibold).
								Color(u.theme.PrimaryForeground).TextCenter()
						})
				}
			})
			ui.Text(f, visual).FontSize(textSize(f, 11)).Weight(gfx.WeightMedium).Color(foreground)
		})
	return box
}

func (u *UI) drawSidebar(canvas *gfx.Canvas, bounds ui.Rect) {
	canvas.Fill(bounds, u.palette.sidebar)
	var connect, create, refresh bool
	var list ui.Box
	u.sidebarPage.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().PadSides(10, 8, 4, 8).Children(func() {
			ui.TextFieldID(f, u.ids.sidebarSearch, &u.sidebarSearch, i18n.T("搜索"), u.searchPlaceholder()).
				Icon(u.iconSearch).FillWidth().Height(38)
			ui.Row(f).Height(6)
			if u.section == sectionContacts {
				u.drawContactFilters(f)
				ui.Row(f).Height(4)
			}
			list = ui.Scroll(f.Key(u.section)).Grow(1).Gap(4).
				HitWith(u.ids.sidebarSurface, i18n.T("侧栏列表"),
					gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
				Children(func() {
					if u.section == sectionContacts {
						u.drawContacts(f)
					} else {
						u.drawConversationList(f)
					}
				})
			ui.Divider(f).FillWidth()
			ui.Row(f).FillWidth().PadSides(max(8, u.theme.Metrics.ScaledText(8)), 0, 4, 0).Items(ui.Center).Gap(4).Children(func() {
				connect = u.iconButton(f, u.ids.sidebarConnect, u.iconUserPlus, i18n.T("手动连接"), 36).Clicked()
				create = u.iconButton(f, u.ids.sidebarCreate, u.iconPlus, i18n.T("创建群聊"), 36).Clicked()
				if u.section == sectionContacts {
					ui.Spacer(f)
					refresh = u.iconButton(f, u.ids.sidebarRefresh, u.iconRefresh, i18n.T("刷新联系人"), 36).Clicked()
				}
			})
		})
	})
	u.contactListBounds = list.Rect()
	u.scrollDraggedContact(canvas, list)
	if connect {
		u.openConnect()
	}
	if create {
		u.openCompose("create", "")
	}
	if refresh && !u.state.ContactRefresh.Pending {
		u.contactRefreshRequested = true
		u.submit(domain.Command{Kind: domain.RefreshContacts})
	}
	if event, ok := canvas.PointerOn(u.ids.sidebarSurface); ok &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openSidebarMenu(event.X, event.Y)
		canvas.Consume()
	}
	canvas.Fill(ui.Rect{X: bounds.X + bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, u.palette.border)
}

func (u *UI) searchPlaceholder() string {
	if u.section == sectionContacts {
		return i18n.T("搜索联系人")
	}
	return i18n.T("搜索会话")
}

func (u *UI) iconButton(f ui.C, id gfx.ID, icon gfx.Icon, label string, size float32) ui.Box {
	return u.sizedIconButton(f, id, icon, label, size, 18)
}

func (u *UI) sizedIconButton(f ui.C, id gfx.ID, icon gfx.Icon, label string, size, glyph float32) ui.Box {
	return u.sizedIconButtonWithHint(f, id, icon, label, label, size, glyph)
}

func (u *UI) sizedIconButtonWithHint(f ui.C, id gfx.ID, icon gfx.Icon, label, hint string, size, glyph float32) ui.Box {
	return ui.Row(f).Size(size, size).Radius(8).Items(ui.Center).Justify(ui.Center).
		Hover(ui.Look{}.Background(u.palette.hover)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint}).
		Children(func() {
			ui.Icon(f, icon).Size(glyph, glyph).Color(u.palette.icon)
		})
}

func (u *UI) drawContactFilters(f ui.C) {
	peerCount, allOnline := 0, 0
	if u.state.Self.ID != "" {
		peerCount, allOnline = 1, 1
	}
	for _, group := range u.contacts {
		peerCount += group.Total
		allOnline += group.Online
	}
	labels := []struct {
		id    gfx.ID
		label string
		value string
		count int
	}{
		{u.ids.filterAll, i18n.T("全部"), "all", peerCount},
		{u.ids.filterOnline, i18n.T("在线"), "online", allOnline},
		{u.ids.filterHidden, i18n.T("已隐藏"), "hidden", u.hiddenContacts.Total},
	}
	ui.Row(f).Height(32).Gap(8).Children(func() {
		for _, item := range labels {
			if item.value == "hidden" && item.count == 0 {
				continue
			}
			active := u.sidebarFilter == item.value
			background := u.palette.sidebar
			foreground := u.palette.secondaryText
			if active {
				background = u.palette.selected
				foreground = u.palette.selectedText
			}
			label := item.label + "  " + intString(item.count)
			box := ui.Row(f).Height(30).PadX(12).Radius(15).Items(ui.Center).Gap(4).
				Background(background).Hover(ui.Look{}.Background(u.palette.hover)).
				HitWith(item.id, item.label, gfx.HitOptions{Role: gfx.RoleButton, Selected: active}).
				Children(func() {
					ui.Text(f, label).FontSize(textSize(f, 12)).Weight(gfx.WeightMedium).Color(foreground)
				})
			if box.Clicked() {
				u.sidebarFilter = item.value
				u.reconcileContactSelection()
			}
		}
	})
}

func (u *UI) drawContacts(f ui.C) {
	query := strings.TrimSpace(u.sidebarSearch.Text())
	if u.sidebarFilter == "hidden" {
		u.drawHiddenContacts(f, query)
		return
	}
	u.selfContactRow(f)
	u.drawInvitations(f)
	u.drawGroupChats(f)
	otherPeers := 0
	visible := 0
	for _, group := range u.contacts {
		otherPeers += group.Total
		peers := u.filteredPeers(group.Peers, query)
		if len(peers) == 0 && (query != "" || group.Name == "未分组") && !u.contactDrag.Dragging() {
			continue
		}
		visible += len(peers)
		collapsed := u.collapsed[group.Name] && query == ""
		u.contactGroupHeader(f, group, collapsed)
		if collapsed {
			continue
		}
		for _, peer := range peers {
			u.contactRow(f, peer)
		}
	}
	if otherPeers == 0 {
		ui.Row(f).Height(8)
		u.emptyListMessage(f, i18n.T("还没有其他联系人"), i18n.T("联系人上线后会自动出现，也可手动连接。"))
		return
	}
	if visible == 0 && query != "" && !containsFold(u.selfDisplayName(), query) {
		u.emptyListMessage(f, i18n.T("没有匹配的联系人"), i18n.T("换一个昵称或分组名称试试。"))
		return
	}
}

func (u *UI) selfContactRow(f ui.C) {
	self := u.state.Self
	if self.ID == "" {
		return
	}
	name := u.selfDisplayName()
	query := strings.TrimSpace(u.sidebarSearch.Text())
	if query != "" && !containsFold(name, query) && !containsFold(i18n.T("我"), query) &&
		!containsFold(i18n.T("个人资料"), query) {
		return
	}
	selected := u.contactPeer == self.ID
	row := u.sidebarRow(f, selected).Height(u.textExtent(sidebarSelfHeight, 30)).PadSides(8, 7, 8, 7).
		HitWith(u.ids.selfRow, name+i18n.T("，个人资料"),
			gfx.HitOptions{Role: gfx.RoleButton, Selected: selected, Hint: i18n.T("编辑个人资料")}).
		Children(func() {
			u.avatarStatus(f, name, u.selfAvatarPath(), 34, true)
			ui.Column(f).Grow(1).Basis(0).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
					ui.Text(f, name).FontSize(textSize(f, 14)).Weight(gfx.WeightMedium).Ellipsis()
					ui.Stack(f).Size(6, 6).Radius(3).Background(u.palette.success)
				})
				ui.Text(f, i18n.T("个人资料 · 本机")).FontSize(textSize(f, 11)).Muted().Ellipsis()
			})
		})
	if event, ok := f.Canvas().PointerOn(u.ids.selfRow); ok &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openSelfMenu(event.X, event.Y)
		f.Canvas().Consume()
	}
	if row.Clicked() {
		u.showContact(self.ID)
	}
	ui.Row(f).Height(2)
}

func (u *UI) drawInvitations(f ui.C) {
	if len(u.invitations) == 0 {
		return
	}
	joinCount, transferCount := 0, 0
	for _, invitation := range u.invitations {
		if invitationKind(invitation) == "ownership" {
			transferCount++
		} else {
			joinCount++
		}
	}
	if joinCount == 0 && transferCount == 0 {
		return
	}
	ui.Row(f).Height(8)
	if joinCount > 0 {
		ui.Row(f).Height(24).Items(ui.Center).Children(func() {
			ui.Text(f, i18n.T("群聊邀请")).FontSize(textSize(f, 12)).Weight(gfx.WeightSemibold)
			ui.Row(f).Width(6)
			u.badgeText(f, intString(joinCount), false)
		})
		for _, invitation := range u.invitations {
			if invitationKind(invitation) != "ownership" {
				u.invitationCard(f, invitation, false)
			}
		}
	}
	if transferCount > 0 {
		if joinCount > 0 {
			ui.Row(f).Height(4)
		}
		ui.Row(f).Height(24).Items(ui.Center).Children(func() {
			ui.Text(f, i18n.T("群主转让")).FontSize(textSize(f, 12)).Weight(gfx.WeightSemibold)
			ui.Row(f).Width(6)
			u.badgeText(f, intString(transferCount), false)
		})
		for _, invitation := range u.invitations {
			if invitationKind(invitation) == "ownership" {
				u.invitationCard(f, invitation, true)
			}
		}
	}
	ui.Row(f).Height(12)
}

func (u *UI) invitationCard(f ui.C, invitation domain.Invitation, ownership bool) {
	ui.Column(f).FillWidth().Pad(11).Radius(radiusControl).Background(u.palette.invite).
		Border(1, u.palette.fileBorder).Gap(8).Children(func() {
		ui.Row(f).Items(ui.Center).Gap(9).Children(func() {
			u.initialsAvatar(f, invitation.GroupName, 30)
			ui.Column(f).Grow(1).Gap(2).Children(func() {
				name := invitation.GroupName
				if name == "" {
					name = i18n.T("群聊")
				}
				ui.Text(f, name).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).Ellipsis()
				from := invitation.FromName
				if from == "" {
					from = u.state.Peer(invitation.FromID).Name
				}
				description := from + i18n.T(" 邀请你加入")
				if ownership {
					description = from + i18n.T(" 提议将群主转让给你")
				}
				ui.Text(f, description).FontSize(textSize(f, 11)).Muted().Ellipsis()
				if !ownership {
					ui.Text(f, u.invitationMemberLine(invitation)).FontSize(textSize(f, 10.5)).Muted().MaxLines(2)
				}
			})
		})
		ui.Row(f).Height(28).Justify(ui.End).Gap(8).Children(func() {
			reject := u.inviteRejectIDs[invitation.ID]
			accept := u.inviteAcceptIDs[invitation.ID]
			rejectLabel := i18n.T("拒绝")
			acceptLabel := i18n.T("加入")
			if ownership {
				rejectLabel = i18n.T("拒绝转让")
				acceptLabel = i18n.T("接受转让")
			}
			if namedButton(f, reject, rejectLabel).XSmall().Outline().Clicked() {
				u.submit(domain.Command{Kind: domain.RejectInvite, InvitationID: invitation.ID})
			}
			if namedButton(f, accept, acceptLabel).XSmall().Clicked() {
				u.submit(domain.Command{Kind: domain.AcceptInvite, InvitationID: invitation.ID})
			}
		})
	})
}

func invitationKind(invitation domain.Invitation) string {
	if invitation.Kind == "" {
		return "join"
	}
	return invitation.Kind
}

func (u *UI) invitationMemberLine(invitation domain.Invitation) string {
	group := u.state.Group(invitation.GroupID)
	if group.ID == "" || len(group.Members) == 0 {
		return i18n.T("当前成员信息将在接受前同步")
	}
	names := make([]string, 0, len(group.Members))
	for _, peerID := range group.Members {
		name := u.state.Peer(peerID).Name
		if peerID == u.state.Self.ID {
			name = i18n.T("我")
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return i18n.T("当前成员信息将在接受前同步")
	}
	shown := names
	if len(shown) > 4 {
		shown = append(append([]string(nil), shown[:4]...), i18n.T("等 ")+intString(len(names))+i18n.T(" 人"))
	}
	return i18n.T("当前成员（") + intString(len(names)) + "）：" + strings.Join(shown, "、")
}

func (u *UI) contactGroupHeader(f ui.C, group contactGroupView, collapsed bool) {
	id := u.groupHeadIDs[group.Name]
	label := displayContactGroup(group.Name)
	hidden := u.sidebarFilter == "hidden"
	if hidden {
		id = u.ids.hiddenGroupHead
		label = i18n.T("隐藏的联系人")
	}
	icon := u.iconChevronDown
	if collapsed {
		icon = u.iconChevronRight
	}
	header := ui.Row(f).Height(u.textExtent(32, 10)).Items(ui.Center).Gap(6).Radius(radiusControl).
		Hover(ui.Look{}.Background(u.palette.hover)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: !collapsed}).
		Children(func() {
			ui.Icon(f, icon).Size(15, 15).Color(u.palette.secondaryText)
			ui.Text(f, label).FontSize(textSize(f, 13)).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Spacer(f)
			ui.Text(f, intString(group.Online)+"/"+intString(group.Total)).FontSize(textSize(f, 11)).Muted()
		})
	if !hidden {
		u.contactDropTarget(header, id)
	}
	if header.Clicked() {
		if hidden {
			u.hiddenCollapsed = !collapsed
		} else {
			u.collapsed[group.Name] = !collapsed
		}
	}
	if event, ok := f.Canvas().PointerOn(id); ok && !hidden && group.Name != "未分组" &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openShellMenu(shellMenuContactGroup, group.Name, event.X, event.Y)
		f.Canvas().Consume()
	}
}

func (u *UI) sidebarRow(f ui.C, selected bool) ui.Box {
	row := ui.Row(f).FillWidth().Radius(radiusCard).Gap(9)
	if selected {
		return row.Background(u.palette.selected).Hover(ui.Look{}.Background(u.palette.selected))
	}
	return row.Hover(ui.Look{}.Background(u.palette.hover))
}

func (u *UI) contactRow(f ui.C, peer domain.Peer) {
	id := u.peerRowIDs[peer.ID]
	selected := u.contactPeer == peer.ID
	row := u.sidebarRow(f, selected).Height(u.textExtent(sidebarContactHeight, 30)).PadSides(8, 7, 8, 7).
		HitWith(id, peer.DisplayName(), gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
		Children(func() {
			u.avatarStatus(f, peer.Name, peer.Avatar, 34, peer.VisibleOnline())
			ui.Column(f).Grow(1).Gap(2).Justify(ui.Center).Children(func() {
				u.peerNameLine(f, peer.DisplayName(), remarkUsername(peer), 14)
				subtitle := peerPresenceLabel(peer)
				if peer.Transport != domain.TransportGoIM {
					subtitle += " · " + contactClientLabel(peer)
				}
				if peer.Signature != "" {
					subtitle += " · " + peer.Signature
				} else if peer.VisibleOnline() && peer.Address != "" {
					subtitle += " · " + peer.Address
				}
				ui.Text(f, subtitle).FontSize(textSize(f, 11)).Muted().Ellipsis()
			})
		})
	if event, ok := f.Canvas().PointerOn(id); ok &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openContactMenu(peer, event.X, event.Y)
		f.Canvas().Consume()
	}
	if !peer.ContactHidden {
		row.DragSource(&u.contactDrag)
		u.contactDropTarget(row, id)
	}
	if row.Clicked() {
		u.showContact(peer.ID)
	}
	u.drawSidebarDivider(f)
}

func (u *UI) drawGroupChats(f ui.C) {
	if len(u.groupChats) == 0 {
		return
	}
	ui.Row(f).Height(6)
	ui.Row(f).Height(u.textExtent(32, 10)).Items(ui.Center).Gap(6).Children(func() {
		ui.Icon(f, u.iconChevronDown).Size(15, 15).Color(u.palette.secondaryText)
		ui.Text(f, i18n.T("我的群聊")).FontSize(textSize(f, 13)).Weight(gfx.WeightSemibold)
		ui.Spacer(f)
		ui.Text(f, intString(len(u.groupChats))).FontSize(textSize(f, 11)).Muted()
	})
	for _, conversation := range u.groupChats {
		if q := strings.TrimSpace(u.sidebarSearch.Text()); q == "" || containsFold(u.conversationTitle(conversation), q) {
			u.contactGroupRow(f, conversation)
		}
	}
	ui.Row(f).Height(8)
}

func (u *UI) drawConversationList(f ui.C) {
	u.drawInvitations(f)
	query := strings.TrimSpace(u.sidebarSearch.Text())
	shown := 0
	for _, conversation := range u.conversations {
		title := u.conversationTitle(conversation)
		if query != "" && !containsFold(title, query) && !containsFold(u.state.Peer(conversation.PeerID).Name, query) && !containsFold(conversation.Preview, query) {
			continue
		}
		shown++
		u.conversationRow(f, conversation)
	}
	if len(u.conversations) == 0 {
		u.emptySidebar(f, i18n.T("还没有会话"), i18n.T("发现联系人后开始聊天，或手动连接一个已知地址。"), true)
		return
	}
	if shown == 0 {
		u.emptyListMessage(f, i18n.T("没有匹配的会话"), i18n.T("换一个昵称或消息内容试试。"))
	}
}

func (u *UI) conversationRow(f ui.C, conversation domain.Conversation) {
	id := u.convRowIDs[conversation.ID]
	selected := conversation.ID == u.selected
	card := u.conversationCard(conversation)
	row := u.sidebarRow(f, selected).Height(u.textExtent(sidebarConversationHeight, 30)).PadSides(8, 7, 8, 7).
		HitWith(id, card.title, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
		Children(func() {
			u.conversationCardBody(f, card)
		})
	if event, ok := f.Canvas().PointerOn(id); ok &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openConversationMenu(conversation, event.X, event.Y)
		f.Canvas().Consume()
	}
	if row.Clicked() {
		if row.DoubleClicked() || u.detached[conversation.ID] {
			u.showDetachedConversation(conversation.ID)
			return
		}
		u.selectConversation(conversation.ID)
	}
	u.drawSidebarDivider(f)
}

func (u *UI) drawSidebarDivider(f ui.C) {
	ui.Row(f).FillWidth().Height(0.5).PadSides(0, 10, 0, 52).Children(func() {
		ui.Stack(f).Fill().Background(u.palette.border)
	})
}

func (u *UI) conversationCardBody(f ui.C, card conversationCardData) {
	u.avatarStatus(f, card.avatarName, card.avatarPath, 38, card.online)
	ui.Column(f).Grow(1).Basis(0).MinWidth(0).Gap(3).Justify(ui.Center).Children(func() {
		ui.Row(f).FillWidth().Items(ui.Center).Gap(5).Children(func() {
			ui.Column(f).Grow(1).Basis(0).MinWidth(0).Children(func() {
				u.peerNameLine(f, card.title, card.username, 14)
			})
			if card.pinned {
				ui.Icon(f, u.iconPin).Size(12, 12).Color(u.palette.secondaryText)
			}
			if card.muted {
				ui.Icon(f, u.iconBellOff).Size(12, 12).Color(u.palette.secondaryText)
			}
			if !card.updatedAt.IsZero() {
				ui.Text(f, shortTime(card.updatedAt)).FontSize(textSize(f, 10)).Muted()
			}
		})
		ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
			ui.Text(f, card.preview).FontSize(textSize(f, 11.5)).Muted().Ellipsis()
			ui.Spacer(f)
			if card.unread > 0 {
				u.badgeText(f, intString(card.unread), true)
			}
		})
	})
}

func (u *UI) emptySidebar(f ui.C, title, description string, actions bool) {
	ui.Column(f).Grow(1).PadSides(18, 10, 18, 10).Items(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Row(f).Size(46, 46).Radius(13).Background(u.palette.mutedAvatar).Items(ui.Center).Justify(ui.Center).
			Children(func() {
				ui.Icon(f, u.iconUsers).Size(22, 22).Color(u.palette.secondaryText)
			})
		ui.Paragraph(f, title, 3).FillWidth().FontSize(textSize(f, 13)).Weight(gfx.WeightSemibold).TextCenter()
		ui.Paragraph(f, description, 5).FillWidth().FontSize(textSize(f, 12)).Muted().TextCenter().LineHeight(textSize(f, 18))
		if actions {
			ui.Column(f).FillWidth().Gap(8).Children(func() {
				if namedButton(f, u.ids.emptyConnect, i18n.T("手动连接")).FillWidth().Small().FontSize(textSize(f, 12)).PadX(8).Outline().Clicked() {
					u.openConnect()
				}
				if namedButton(f, u.ids.emptyCompose, i18n.T("创建群聊")).FillWidth().Small().FontSize(textSize(f, 12)).PadX(8).Clicked() {
					u.openCompose("create", "")
				}
			})
		}
	})
}

func (u *UI) emptyListMessage(f ui.C, title, body string) {
	ui.Column(f).PadSides(24, 22, 18, 22).Gap(5).Children(func() {
		ui.Text(f, title).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).TextCenter()
		ui.Text(f, body).FontSize(textSize(f, 11)).Muted().TextCenter()
	})
}

func (u *UI) initialsAvatar(f ui.C, name string, size float32) {
	ui.Row(f).Size(size, size).Radius(size / 2).Background(u.palette.mutedAvatar).
		Items(ui.Center).Justify(ui.Center).
		Children(func() {
			u.avatarInitial(f, name, size)
		})
}

func (u *UI) avatarInitial(f ui.C, name string, size float32) {
	ui.Text(f, initialOf(name)).
		FontSize(min(u.theme.Metrics.ScaledText(max(11, size*0.36)), size*0.42)).
		Weight(gfx.WeightSemibold).
		Color(u.palette.avatarText).
		TextCenter()
}

func (u *UI) badgeText(f ui.C, label string, primary bool) {
	background := u.palette.selected
	foreground := u.palette.selectedText
	if primary {
		background = u.theme.Primary
		foreground = u.theme.PrimaryForeground
	}
	scale := u.theme.Metrics.ScaledText(1)
	width := max(21, float32(len([]rune(label)))*7*scale+12)
	ui.Stack(f).Width(width).Height(max(20, 10.5*scale+7)).Radius(radiusControl).Background(background).Children(func() {
		ui.Text(f, label).FontSize(textSize(f, 10.5)).Weight(gfx.WeightSemibold).Color(foreground).TextCenter()
	})
}

func (u *UI) conversationTitle(conversation domain.Conversation) string {
	if conversation.Kind == "direct" {
		for _, peer := range u.state.Peers {
			if peer.ID == conversation.PeerID && peer.DisplayName() != "" {
				return peer.DisplayName()
			}
		}
	}
	title := conversation.Title
	if title != "" {
		return title
	}
	if conversation.Kind == "group" {
		if group := u.state.Group(conversation.GroupID); group.Name != "" {
			return group.Name
		}
		return i18n.T("群聊")
	}
	return u.state.Peer(conversation.PeerID).Name
}

func (u *UI) conversationCard(conversation domain.Conversation) conversationCardData {
	title := u.conversationTitle(conversation)
	card := conversationCardData{
		title: title, avatarName: title, preview: conversation.Preview,
		updatedAt: conversation.UpdatedAt, pinned: conversation.Pinned,
		muted: conversation.Muted, unread: conversation.Unread,
	}
	if conversation.Kind == "group" {
		group := u.state.Group(conversation.GroupID)
		_, onlineCount := u.groupCounts(group)
		card.online = onlineCount > 0
		card.avatarPath = group.Avatar
	} else {
		peer := u.state.Peer(conversation.PeerID)
		card.online = peer.ID != "" && peer.Online
		card.avatarPath = peer.Avatar
		card.username = remarkUsername(peer)
		if peer.Name != "" {
			card.avatarName = peer.Name
		}
	}
	if card.preview == "" {
		card.preview = i18n.T("还没有消息")
	}
	return card
}

func (u *UI) rebuildPresentation(oldError string) {
	u.ensureStateIDs()
	u.theme = themeForSettings(u.state.Settings)
	u.palette = paletteFor(u.theme, u.state.Settings.ColorScheme)

	u.conversations = u.conversations[:0]
	for _, conversation := range u.state.Conversations {
		if !conversation.Hidden {
			u.conversations = append(u.conversations, conversation)
		}
	}
	sort.SliceStable(u.conversations, func(i, j int) bool {
		left, right := u.conversations[i], u.conversations[j]
		if left.Pinned != right.Pinned {
			return left.Pinned
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return u.conversationTitle(left) < u.conversationTitle(right)
	})

	u.groupChats = u.groupChats[:0]
	u.directByPeer = make(map[string]string)
	for _, conversation := range u.conversations {
		if conversation.Kind == "direct" && conversation.PeerID != "" {
			u.directByPeer[conversation.PeerID] = conversation.ID
		}
	}
	for _, conversation := range u.state.Conversations {
		if conversation.Kind == "group" {
			group := u.state.Group(conversation.GroupID)
			if !group.Left && !group.Dissolved {
				u.groupChats = append(u.groupChats, conversation)
			}
		}
	}

	u.rebuildContactGroups()

	u.invitations = append(u.invitations[:0], u.state.Invitations...)
	sort.SliceStable(u.invitations, func(i, j int) bool {
		return u.invitations[i].CreatedAt.After(u.invitations[j].CreatedAt)
	})
	filtered := u.invitations[:0]
	for _, invitation := range u.invitations {
		if invitation.Status != "" && invitation.Status != "pending" {
			continue
		}
		if invitation.FromID == u.state.Self.ID {
			continue
		}
		if invitation.ToID != "" && invitation.ToID != u.state.Self.ID {
			continue
		}
		filtered = append(filtered, invitation)
	}
	u.invitations = filtered

	u.groupNicknames = make(map[string]string, len(u.state.GroupNicknames))
	for _, nickname := range u.state.GroupNicknames {
		name := strings.TrimSpace(nickname.Name)
		if nickname.GroupID == "" || nickname.PeerID == "" || name == "" {
			continue
		}
		u.groupNicknames[groupInviteKey(nickname.GroupID, nickname.PeerID)] = name
	}

	u.outgoingInvites = u.outgoingInvites[:0]
	u.ownershipInvites = u.ownershipInvites[:0]
	for _, invitation := range u.state.Invitations {
		if invitation.GroupID == "" || invitation.FromID != u.state.Self.ID {
			continue
		}
		switch invitation.Status {
		case "", "pending", "rejected", "failed":
			if invitationKind(invitation) == "ownership" {
				u.ownershipInvites = append(u.ownershipInvites, invitation)
			} else {
				u.outgoingInvites = append(u.outgoingInvites, invitation)
			}
		}
	}
	sort.SliceStable(u.outgoingInvites, func(i, j int) bool {
		return u.outgoingInvites[i].CreatedAt.After(u.outgoingInvites[j].CreatedAt)
	})
	sort.SliceStable(u.ownershipInvites, func(i, j int) bool {
		return u.ownershipInvites[i].CreatedAt.After(u.ownershipInvites[j].CreatedAt)
	})

	u.transfersFor = make(map[string][]domain.Transfer)
	for _, transfer := range u.state.Transfers {
		if transfer.FileID != "" {
			u.transfersFor[transfer.FileID] = append(u.transfersFor[transfer.FileID], transfer)
		}
	}
	for fileID := range u.transfersFor {
		transfers := u.transfersFor[fileID]
		sort.SliceStable(transfers, func(i, j int) bool {
			left := u.state.Peer(transfers[i].PeerID).Name
			right := u.state.Peer(transfers[j].PeerID).Name
			if left != right {
				return left < right
			}
			return transfers[i].ID < transfers[j].ID
		})
	}
	oldPaths := u.fileLocalPath
	oldAvailable := u.fileAvailable
	u.fileLocalPath = localFilePaths(u.state)
	u.fileAvailable = make(map[string]bool, len(u.fileLocalPath))
	for id, path := range u.fileLocalPath {
		if oldPaths[id] != path {
			continue
		}
		if available, ok := oldAvailable[id]; ok {
			u.fileAvailable[id] = available
		}
	}

	oldSelected := u.selected
	u.reconcileCompose(oldError)
	u.selectAfterSnapshot(oldSelected)
	u.rebuildSelection()
	if u.chatFind.open {
		u.rebuildChatSearchMatches()
	}
	u.refreshImages()
	if u.activeFileID != "" {
		if u.state.File(u.activeFileID).ID == "" {
			u.activeFileID, u.drawer, u.fileReturn = "", "", ""
		}
	}
}

func (u *UI) initialsAvatarStatus(f ui.C, name string, size float32, online bool) {
	ui.Row(f).Size(size, size).Radius(size / 2).Background(u.palette.mutedAvatar).
		Items(ui.Center).Justify(ui.Center).
		Children(func() {
			u.avatarInitial(f, name, size)
			dot := max(8, size*0.22)
			color := u.palette.secondaryText
			if online {
				color = u.palette.success
			}
			ui.Stack(f).Size(dot, dot).Absolute().Right(0).Bottom(0).Radius(dot/2).
				Background(color).Border(2, u.palette.chat)
		})
}

func (u *UI) selectAfterSnapshot(oldSelected string) {
	target := u.selected
	if c := u.state.Conversation(u.pendingGroup); c.ID != "" && !c.Hidden {
		target, u.pendingGroup = c.ID, ""
	}
	if u.pendingPeer != "" {
		if convID, ok := u.directByPeer[u.pendingPeer]; ok {
			target = convID
			u.pendingPeer = ""
		}
	}
	found := false
	for _, conversation := range u.state.Conversations {
		if conversation.ID == target && !conversation.Hidden {
			found = true
			break
		}
	}
	if !found {
		target = ""
	}
	if target == "" && len(u.conversations) > 0 && !u.detachedBlank {
		target = u.conversations[0].ID
	}
	if target != oldSelected {
		if oldSelected != "" && u.chatEditor != nil && !u.state.Conversation(oldSelected).Hidden {
			u.persistDraft(oldSelected, u.chatEditor.String())
		}
		u.resetChatSearch()
		u.selected = target
		u.loadDraft(target)
		u.chatScrollToEnd = target != ""
		return
	}
	if u.selected == "" && u.chatEditor != nil && u.chatEditor.String() != "" {
		u.setChatEditorText("")
	}
}

func (u *UI) rebuildSelection() {
	u.messages = append(u.messages[:0], u.state.ChatMessages(u.selected)...)
	keepSelection := false
	for _, m := range u.messages {
		u.ensureID(u.messageIDs, u.ids.chat, "message:", m.ID)
		if m.Text != "" && !m.Recalled {
			u.ensureID(u.messageTextIDs, u.ids.chat, "message-text:", m.ID)
			if u.textSelection.id == u.messageTextIDs[m.ID] && u.textSelection.text == m.Text {
				keepSelection = true
			}
		}
		u.ensureID(u.messageAvatarIDs, u.ids.chat, "message-avatar:", m.ID)
	}
	if !keepSelection {
		u.textSelection.clear()
	}
	sort.SliceStable(u.messages, func(i, j int) bool {
		return u.messages[i].CreatedAt.Before(u.messages[j].CreatedAt)
	})
	u.files = u.files[:0]
	for _, file := range u.state.Files {
		if file.ConversationID == u.selected {
			u.files = append(u.files, file)
		}
	}
	sort.SliceStable(u.files, func(i, j int) bool {
		return u.files[i].CreatedAt.After(u.files[j].CreatedAt)
	})
}

func (u *UI) reconcileCompose(oldError string) {
	if !u.compose.open || !u.compose.pending {
		return
	}
	switch u.compose.mode {
	case "create":
		for _, group := range u.state.Groups {
			if u.compose.knownGroups[group.ID] || group.Name != u.compose.submittedName || group.Dissolved {
				continue
			}
			u.compose.pending = false
			u.compose.open = false
			u.setSection(sectionMessages)
			for _, conversation := range u.state.Conversations {
				if conversation.GroupID == group.ID {
					if u.selected != conversation.ID {
						u.persistDraft(u.selected, u.chatEditor.String())
						u.selected = conversation.ID
						u.loadDraft(conversation.ID)
					}
					break
				}
			}
			if u.state.Error != "" && u.state.Error != oldError {
				u.showToast(i18n.T("群聊已创建"), i18n.T("部分邀请发送失败：")+u.state.Error, components.ToastWarning)
				u.submit(domain.Command{Kind: domain.ClearError})
			}
			return
		}
	}
	if u.state.Error != "" && u.state.Error != oldError {
		u.compose.pending = false
		u.compose.err = u.state.Error
		return
	}
	switch u.compose.mode {
	case "invite":
		group := u.state.Group(u.compose.groupID)
		if group.ID == "" {
			u.compose.pending = false
			return
		}
		for _, id := range u.compose.submittedIDs {
			if containsString(group.Members, id) || containsString(group.Pending, id) {
				u.compose.pending = false
				u.compose.open = false
				return
			}
		}
	}
}

func (u *UI) loadDraft(conversationID string) {
	if u.chatEditor == nil {
		return
	}
	if conversationID == "" {
		u.setChatEditorText("")
		return
	}
	draft, ok := u.drafts[conversationID]
	if !ok {
		conversation := u.state.Conversation(conversationID)
		draft = conversation.Draft
		u.drafts[conversationID] = draft
		u.draftSaved[conversationID] = draft
	}
	u.setChatEditorText(draft)
}

func (u *UI) saveCurrentDraft() {
	if u.selected == "" || u.chatEditor == nil {
		return
	}
	u.persistDraft(u.selected, u.chatEditor.String())
}

func (u *UI) persistDraft(conversationID, value string) {
	if conversationID == "" {
		return
	}
	if saved, ok := u.draftSaved[conversationID]; ok && saved == value {
		return
	}
	u.drafts[conversationID] = value
	u.draftSaved[conversationID] = value
	u.submit(domain.Command{Kind: domain.SaveDraft, ConversationID: conversationID, Text: value})
}

func (u *UI) selectConversation(id string) {
	if id == "" {
		return
	}
	u.detachedBlank = false
	u.closeShellMenu()
	if id == u.selected {
		u.pendingPeer, u.pendingGroup = "", ""
		return
	}
	if !u.guardGroupRenameNavigation(func() { u.selectConversation(id) }) {
		return
	}
	u.pendingPeer, u.pendingGroup = "", ""
	if c := u.state.Conversation(id); c.Hidden && c.Kind == "group" {
		u.pendingGroup = id
		u.submit(domain.Command{Kind: domain.StartChat, ConversationID: id})
		return
	}
	u.saveCurrentDraft()
	u.closeChatPickers()
	u.resetChatSearch()
	u.selected = id
	u.loadDraft(id)
	u.rebuildSelection()
	u.moreOpen = false
	u.memberMenu = ""
	u.memberProfile = ""
	u.chatScrollToEnd = true
	u.closeDrawer()
}

func (u *UI) startChat(peerID string) {
	if convID, ok := u.directByPeer[peerID]; ok {
		conversation := u.state.Conversation(convID)
		if conversation.ID != "" && !conversation.Hidden {
			u.selectConversation(convID)
			return
		}
	}
	u.pendingPeer = peerID
	u.submit(domain.Command{Kind: domain.StartChat, PeerID: peerID})
}

func (u *UI) refreshSectionCatalog(section string) {
	switch section {
	case sectionPrint:
		u.submit(domain.Command{Kind: domain.RefreshPrinters})
	case sectionShares:
		if u.panels.shareFilter == panelSharesRemote {
			u.submit(domain.Command{Kind: domain.RefreshShares})
		}
	}
}

func (u *UI) setSection(section string) {
	if section == u.section {
		return
	}
	if u.compose.open {
		u.requestCloseComposeThen(func() { u.setSection(section) })
		return
	}
	if u.connectOpen || u.confirm.open || u.profile.open || u.groupAvatar.open ||
		u.memberProfile != "" || u.organize.open {
		return
	}
	if !u.guardGroupRenameNavigation(func() { u.setSection(section) }) {
		return
	}
	if !u.isPanelSection() {
		u.saveCurrentDraft()
	}
	u.panels.closeMemberProject()
	u.section = section
	u.textSelection.clear()
	u.state.Settings.LastSection = section
	u.submit(domain.Command{Kind: domain.SetSetting, Setting: "last_section", Value: section})
	u.invalidator.Invalidate()
	if section != sectionMessages {
		u.pendingPeer, u.pendingGroup = "", ""
	}
	if section == sectionSettings {
		u.panels.refreshNetwork = true
		u.invalidator.Invalidate()
	}
	u.refreshSectionCatalog(section)
	u.closeChatPickers()
	u.closeShellMenu()
	u.closeOrganize()
	u.resetChatSearch()
	u.closeProfile()
	u.moreOpen = false
	u.closeDrawer()
	u.compose.open = false
	u.connectOpen = false
	u.confirm.open = false
	u.memberMenu = ""
	u.memberProfile = ""
	u.resetGroupRename()
}

func (u *UI) toggleTheme() {
	next := "dark"
	if u.isDark() {
		next = "light"
	}
	u.state.Settings.Theme = next
	u.theme = themeForSettings(u.state.Settings)
	u.palette = paletteFor(u.theme, u.state.Settings.ColorScheme)
	u.submit(domain.Command{Kind: domain.SetSetting, Setting: "theme", Value: next})
}

func (u *UI) openConnect() {
	u.closeShellMenu()
	u.connectOpen = true
	u.connectPending = false
	u.connectRoute = routeAuto
	u.connectField.SetText("")
}

func (u *UI) openCompose(mode, groupID string) {
	u.closeShellMenu()
	if !u.guardGroupRenameNavigation(func() { u.openCompose(mode, groupID) }) {
		return
	}
	known := make(map[string]bool, len(u.state.Groups))
	for _, group := range u.state.Groups {
		known[group.ID] = true
	}
	u.compose = composeState{
		open:        true,
		mode:        mode,
		groupID:     groupID,
		selected:    make(map[string]bool),
		knownGroups: known,
	}
	u.compose.name.SetText("")
	u.compose.search.SetText("")
	u.closeChatPickers()
	u.moreOpen = false
	u.drawer = ""
	u.memberProfile = ""
}

func (u *UI) submitCompose() {
	if !u.compose.open || u.compose.pending {
		return
	}
	ids := u.selectedPeersForCompose()
	if u.compose.mode == "create" {
		if !u.composeReady(ids) {
			return
		}
		name := strings.TrimSpace(u.compose.name.Text())
		if name == "" {
			name = generatedGroupName(u, ids)
		}
		name = truncateRunes(name, 30)
		u.compose.submittedName = name
		u.compose.submittedIDs = append(u.compose.submittedIDs[:0], ids...)
		u.compose.pending = true
		u.compose.err = ""
		u.submit(domain.Command{Kind: domain.CreateGroup, Name: name, IDs: append([]string(nil), ids...)})
		return
	}
	if len(ids) == 0 {
		return
	}
	u.compose.submittedIDs = append(u.compose.submittedIDs[:0], ids...)
	u.compose.pending = true
	u.compose.err = ""
	u.submit(domain.Command{Kind: domain.InviteMembers, GroupID: u.compose.groupID, IDs: append([]string(nil), ids...)})
}

func (u *UI) selectedPeersForCompose() []string {
	ids := make([]string, 0, len(u.compose.selected))
	for _, group := range u.contacts {
		for _, peer := range group.Peers {
			if u.compose.selected[peer.ID] && peer.Online && peer.SupportsGroups() {
				ids = append(ids, peer.ID)
			}
		}
	}
	return ids
}

func (u *UI) composeReady(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	transport := u.state.Peer(ids[0]).Transport
	for _, id := range ids[1:] {
		if u.state.Peer(id).Transport != transport {
			return false
		}
	}
	return transport == domain.TransportNeiwangtong || len(ids) >= 2
}

func (u *UI) composeDirty() bool {
	if !u.compose.open || u.compose.pending {
		return false
	}
	if strings.TrimSpace(u.compose.name.Text()) != "" || strings.TrimSpace(u.compose.search.Text()) != "" {
		return true
	}
	return len(u.compose.selected) > 0
}

func (u *UI) requestCloseCompose() {
	u.requestCloseComposeThen(nil)
}

func (u *UI) requestCloseComposeThen(after func()) {
	if u.compose.pending {
		return
	}
	if !u.composeDirty() {
		u.compose.open = false
		if after != nil {
			after()
		}
		return
	}
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("放弃未保存的内容？"),
		body:  i18n.T("群聊选择或群名尚未提交。"),
		action: func() {
			u.compose.open = false
			if after != nil {
				after()
			}
		},
	}
}

func (u *UI) filteredPeers(peers []domain.Peer, query string) []domain.Peer {
	if query == "" && u.sidebarFilter != "online" {
		return peers
	}
	out := make([]domain.Peer, 0, len(peers))
	for _, peer := range peers {
		if u.sidebarFilter == "online" && !peer.VisibleOnline() {
			continue
		}
		if query != "" && !containsFold(peer.Name, query) && !containsFold(peer.Remark, query) && !containsFold(peer.ContactGroup, query) {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func (u *UI) drawOverlays(canvas *gfx.Canvas, viewport ui.Rect) {
	if u.moreOpen && !u.isPanelSection() {
		u.drawChatMenu(canvas)
	}
	if !u.isPanelSection() {
		u.drawMentionPicker(canvas, viewport)
		u.drawEmojiPicker(canvas, viewport)
	}
	u.drawShellMenu(canvas, viewport)
	if u.memberProfile != "" && !u.isPanelSection() {
		u.drawMemberProfile(canvas, viewport)
	}
	if u.profile.open && !u.profile.crop.open {
		u.drawProfileDialog(canvas, viewport)
	}
	if u.profile.crop.open {
		u.drawProfileAvatarEditor(canvas, viewport)
	}
	if u.groupAvatar.open {
		u.drawGroupAvatarEditor(canvas, viewport)
	}
	if u.organize.open {
		u.drawOrganizeDialog(canvas, viewport)
	}
	if u.connectOpen {
		u.drawConnectDialog(canvas, viewport)
	}
	if u.compose.open {
		u.drawComposeDialog(canvas, viewport)
	}
	if u.confirm.open {
		u.drawConfirmation(canvas, viewport)
	}
	if u.updateOpen {
		u.drawUpdateDialog(canvas, viewport)
	}
	u.drawVoiceCall(canvas, viewport)
}

func (u *UI) textInput(context app.TextInputContext) *app.TextInput {
	if u.detachedWindow && u.lockVisible() {
		return nil
	}
	if u.shellMenu.open {
		return nil
	}
	if u.modalOpen() {
		return u.dialogPage.TextInput(context)
	}
	if u.isPanelSection() {
		return u.panels.TextInput(context)
	}
	if u.drawer != "" {
		if input := u.drawerPage.TextInput(context); input != nil {
			return input
		}
	}
	if input := u.sidebarPage.TextInput(context); input != nil {
		return input
	}
	input := u.chatPage.TextInput(context)
	if input != nil {
		if input.Editor == u.chatEditor {
			return u.chatTextInput()
		}
		if input := u.chatSearchTextInput(context); input != nil {
			return input
		}
	}
	return input
}

func (u *UI) onScroll(event gfx.ScrollEvent) gfx.EventResult {
	if crop := u.activeAvatarCrop(); crop != nil {
		state, result := crop.layout.Scroll(event, crop.state)
		crop.state = state
		if result.Handled {
			return result
		}
	}
	if !u.lockVisible() && u.emojiOpen && pointInside(event.X, event.Y, u.emojiPanel) {
		result := u.emojiPage.Wheel(event)
		result.Handled = true
		return result
	}
	if u.lockVisible() {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := u.chatOverlayWheel(event); handled {
		return result
	}
	if u.shellMenu.open {
		u.closeShellMenu()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if u.modalOpen() {
		result := u.dialogPage.Wheel(event)
		result.Handled = true
		return result
	}
	if (u.drawer != "") && pointInside(event.X, event.Y, u.lastChat) {
		return u.drawerPage.Wheel(event)
	}
	if u.isPanelSection() {
		return u.panels.Wheel(event)
	}
	if event.X < u.lastChat.X {
		return u.sidebarPage.Wheel(event)
	}
	return u.chatPage.Wheel(event)
}

func (u *UI) onPointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind == gfx.PointerDown && !u.shellMenu.open && u.textSelection.id != event.Target {
		u.textSelection.clear()
	}
	if u.detachedWindow && u.lockVisible() {
		return gfx.EventResult{Handled: true}
	}
	if u.voiceVisible() {
		return gfx.EventResult{}
	}
	if result, handled := u.dragConversationPointer(event); handled {
		return result
	}
	if u.section == sectionContacts && !u.detachedWindow && !u.modalOpen() {
		if event.Kind == gfx.PointerUp && u.contactDrag.Dragging() && !pointInside(event.X, event.Y, u.contactListBounds) {
			u.contactDrag.Cancel()
		}
		if result := u.sidebarPage.Pointer(event); result.Handled {
			return result
		}
	}
	if crop := u.activeAvatarCrop(); crop != nil {
		state, result := crop.layout.Pointer(event, crop.state)
		crop.state = state
		if result.Handled {
			return result
		}
	}
	if u.isPanelSection() {
		if result := u.panels.Pointer(event); result.Handled {
			return result
		}
	}
	if u.emojiOpen && !u.lockVisible() {
		return u.emojiPage.Pointer(event)
	}
	return gfx.EventResult{}
}

func (u *UI) onKey(event gfx.KeyEvent) gfx.EventResult {
	if u.lockVisible() {
		// The only registered hits while locked belong to the lock screen.
		// Let Viva deliver text keys, focus traversal and Enter to those hits.
		return u.lockKey(event)
	}
	if u.voiceVisible() {
		return gfx.EventResult{}
	}
	if result, handled := u.chatOverlayKey(event); handled {
		return result
	}
	if u.contactDrag.Dragging() && event.Key == gfx.KeyEscape {
		return u.sidebarPage.Key(event)
	}
	if u.mention.open && u.fieldFocused(&u.chatEditorField) && !event.Release &&
		event.Modifiers == 0 && u.chatEditor != nil {
		if _, composing := u.chatEditor.Composition(); !composing {
			switch event.Key {
			case gfx.KeyUp:
				u.moveMention(-1)
				return gfx.EventResult{Handled: true, Repaint: true}
			case gfx.KeyDown:
				u.moveMention(1)
				return gfx.EventResult{Handled: true, Repaint: true}
			case gfx.KeyEnter:
				if u.insertMention() {
					return gfx.EventResult{Handled: true, Repaint: true}
				}
			}
		}
	}
	if event.Key == gfx.KeyEscape && !event.Release {
		switch {
		case u.updateOpen:
			return gfx.EventResult{}
		case u.confirm.open:
			u.confirm.open = false
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.shellMenu.open:
			u.closeShellMenu()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.profile.crop.open:
			u.closeProfileAvatarEditor()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.groupAvatar.open:
			u.closeGroupAvatarEditor()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.organize.open:
			u.closeOrganize()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.profile.open:
			u.closeProfile()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.emojiOpen:
			u.emojiOpen = false
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.mention.open:
			u.closeMention()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.compose.open:
			u.requestCloseCompose()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.connectOpen:
			// The dialog and the connection selector register their own Escape
			// targets. Viva sends Escape to the innermost one first.
			return gfx.EventResult{}
		case u.memberProfile != "":
			u.memberProfile = ""
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.memberMenu != "":
			u.memberMenu = ""
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.activeFileID != "" && u.fileReturn == "list":
			u.activeFileID = ""
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.drawer != "":
			u.requestCloseDrawer()
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.moreOpen:
			u.moreOpen = false
			return gfx.EventResult{Handled: true, Repaint: true}
		case u.chatFind.open:
			u.closeChatSearch()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if u.shellMenu.open {
		return gfx.EventResult{Handled: true}
	}
	if crop := u.activeAvatarCrop(); crop != nil {
		state, result := crop.layout.Key(event, crop.state)
		crop.state = state
		if result.Handled {
			return result
		}
	}
	if u.modalOpen() {
		return u.dialogPage.Key(event)
	}
	if u.isPanelSection() {
		return u.panels.Key(event)
	}
	if u.drawer != "" {
		if result := u.drawerPage.Key(event); result.Handled {
			return result
		}
	}
	return u.chatPage.Key(event)
}

func (u *UI) onShortcut(event gfx.KeyEvent) gfx.EventResult {
	if u.lockVisible() || (u.section == sectionSettings && u.panels.settingsTab == panelSettingsSecurity &&
		u.panels.settingsExtra.isPasswordField(event.Target)) {
		// Viva offers plain printable keys here before native text input.
		// Skip application shortcuts without consuming the editor's keys.
		return gfx.EventResult{}
	}
	if result, handled := u.chatOverlayShortcut(event); handled {
		return result
	}
	if u.modalOpen() {
		return gfx.EventResult{}
	}
	if !event.Release && (event.Modifiers == gfx.ModifierControl || event.Modifiers == gfx.ModifierMeta) &&
		strings.EqualFold(event.Text, "c") && event.Target == u.textSelection.id {
		if value := u.textSelection.selected(); value != "" {
			return gfx.EventResult{Handled: true, ClipboardText: value}
		}
	}
	if u.isPanelSection() {
		if result := u.panels.Shortcut(event); result.Handled {
			return result
		}
	}
	control := event.Modifiers&gfx.ModifierControl != 0 || event.Modifiers&gfx.ModifierMeta != 0
	if event.Key == gfx.KeyEnter && control && u.fieldFocused(&u.chatEditorField) && u.selected != "" {
		if u.chatEditor == nil {
			return gfx.EventResult{}
		}
		if _, composing := u.chatEditor.Composition(); composing {
			return gfx.EventResult{Handled: true}
		}
		if u.state.Settings.SendKey == "Ctrl+Enter" {
			u.sendChatText()
			return gfx.EventResult{Handled: true}
		}
		u.chatEditor.NewLine()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if u.shortcutTriggered(shortcutActionOpenSettings, "Ctrl+,", event) {
		u.setSection(sectionSettings)
		return gfx.EventResult{Handled: true}
	}
	if u.shortcutTriggered(shortcutActionSearchInConversation, "Ctrl+F", event) {
		if !u.isPanelSection() && u.selected != "" {
			u.openChatSearch()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if u.shortcutTriggered(shortcutActionQuickFind, "Ctrl+K", event) {
		if u.isPanelSection() {
			u.setSection(sectionMessages)
		}
		u.chatFind.focus = false
		u.focusSearch = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if u.shortcutTriggered(shortcutActionCreateGroup, "Ctrl+Shift+N", event) {
		if u.isPanelSection() {
			u.setSection(sectionMessages)
		}
		u.openCompose("create", "")
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (u *UI) fieldFocused(field *ui.Field) bool {
	return field != nil && ui.Fields(field)(app.TextInputContext{ID: u.focusedID}) != nil
}

func (u *UI) onFileDrop(event gfx.FileDropEvent) gfx.EventResult {
	if u.lockVisible() {
		u.dragging = false
		return gfx.EventResult{}
	}
	switch event.Phase {
	case gfx.FileDropOver:
		if u.chatDropReady() && pointInside(event.X, event.Y, u.lastChat) {
			u.dragging = true
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	case gfx.FileDropLeave:
		if u.dragging {
			u.dragging = false
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	case gfx.FileDropDrop:
		u.dragging = false
		if u.chatDropReady() && len(event.Paths) > 0 && pointInside(event.X, event.Y, u.lastChat) {
			u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: u.selected, Paths: append([]string(nil), event.Paths...)})
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

func (u *UI) chatDropReady() bool {
	return u.selected != "" && !u.isPanelSection() && u.drawer == "" &&
		!u.modalOpen() && !u.shellMenu.open &&
		u.chatReadOnlyReason(u.state.Conversation(u.selected)) == ""
}

func (u *UI) ensureStateIDs() {
	u.ensurePeerID(u.state.Self.ID)
	u.ensureID(u.groupHeadIDs, u.ids.side, "contact-group:", "未分组")
	for _, name := range u.state.ContactGroups {
		u.ensureID(u.groupHeadIDs, u.ids.side, "contact-group:", name)
	}
	for _, peer := range u.state.Peers {
		u.ensurePeerID(peer.ID)
	}
	for _, conversation := range u.state.Conversations {
		u.ensureID(u.convRowIDs, u.ids.side, "conversation-row:", conversation.ID)
	}
	for _, group := range u.state.Groups {
		for _, id := range group.Members {
			u.ensurePeerID(id)
		}
		for _, id := range group.Pending {
			u.ensurePeerID(id)
			u.ensureGroupInviteIDs(group.ID, id)
		}
	}
	for _, invitation := range u.state.Invitations {
		u.ensurePeerID(invitation.FromID)
		u.ensureID(u.inviteAcceptIDs, u.ids.side, "invite-accept:", invitation.ID)
		u.ensureID(u.inviteRejectIDs, u.ids.side, "invite-reject:", invitation.ID)
		if invitation.GroupID != "" && invitation.ToID != "" {
			u.ensurePeerID(invitation.ToID)
			u.ensureGroupInviteIDs(invitation.GroupID, invitation.ToID)
		}
	}
	for _, peer := range u.state.Peers {
		name := strings.TrimSpace(peer.ContactGroup)
		if name == "" {
			name = "未分组"
		}
		u.ensureID(u.groupHeadIDs, u.ids.side, "contact-group:", name)
		u.ensureID(u.composeGroupHeadIDs, u.ids.dialog, "compose-group:", name)
	}
	for _, message := range u.state.Messages {
		if message.FileID != "" {
			u.ensureID(u.fileButtonIDs, u.ids.chat, "message-file:", message.FileID)
		}
	}
	for _, file := range u.state.Files {
		u.ensureID(u.fileRowIDs, u.ids.chat, "file-row:", file.ID)
		u.ensureID(u.fileButtonIDs, u.ids.chat, "file-detail:", file.ID)
	}
	for _, transfer := range u.state.Transfers {
		if transfer.ID == "" {
			continue
		}
		u.ensureID(u.fileAcceptIDs, u.ids.dialog, "transfer-accept:", transfer.ID)
		u.ensureID(u.fileRejectIDs, u.ids.dialog, "transfer-reject:", transfer.ID)
		u.ensureID(u.fileCancelIDs, u.ids.dialog, "transfer-cancel:", transfer.ID)
		u.ensureID(u.fileRetryIDs, u.ids.dialog, "transfer-retry:", transfer.ID)
		u.ensureID(u.fileSaveIDs, u.ids.dialog, "transfer-save:", transfer.ID)
	}
}

func (u *UI) ensurePeerID(id string) {
	if id == "" {
		return
	}
	u.ensureID(u.peerRowIDs, u.ids.side, "peer-row:", id)
	u.ensureID(u.groupSelectIDs, u.ids.dialog, "peer-select:", id)
	u.ensureID(u.groupSelectRows, u.ids.dialog, "peer-select-row:", id)
	u.ensureID(u.groupRemoveIDs, u.ids.dialog, "peer-remove:", id)
	u.ensureID(u.memberMenuIDs, u.ids.dialog, "member-menu:", id)
	u.ensureID(u.memberInfoIDs, u.ids.dialog, "member-info:", id)
	u.ensureID(u.contactMemberIDs, u.ids.contactMemberScope, "contact-member:", id)
	u.ensureID(u.memberMessageIDs, u.ids.dialog, "member-message:", id)
	u.ensureID(u.memberRemoveIDs, u.ids.dialog, "member-remove:", id)
	u.ensureID(u.memberTransferIDs, u.ids.dialog, "member-transfer:", id)
	u.ensureID(u.mentionCandidateIDs, u.ids.chat, "mention:", id)
}

func (u *UI) ensureGroupInviteIDs(groupID, peerID string) {
	key := groupInviteKey(groupID, peerID)
	u.ensureID(u.inviteWithdrawIDs, u.ids.dialog, "invite-withdraw:", key)
	u.ensureID(u.inviteReinviteIDs, u.ids.dialog, "invite-reinvite:", key)
}

func (u *UI) ensureID(cache map[string]gfx.ID, scope gfx.Scope, prefix, key string) gfx.ID {
	if key == "" {
		key = "empty"
	}
	if id, ok := cache[key]; ok {
		return id
	}
	id := u.tree.ID(scope, prefix+key)
	cache[key] = id
	return id
}

func (u *UI) refreshImages() {
	u.mediaDesiredImages = make(map[string]bool)
	u.avatarImagePaths = make(map[string]bool)
	for _, peer := range u.state.Peers {
		u.markAvatarImage(peer.Avatar)
	}
	u.markAvatarImage(u.state.Self.Avatar)
	u.markAvatarImage(u.state.Settings.Avatar)
	if u.profile.crop.open || u.profile.crop.pending {
		u.markImage(u.profile.crop.source)
	} else if u.profile.open {
		u.markAvatarImage(u.profile.avatar)
	}
	if u.groupAvatar.open || u.groupAvatar.pending {
		u.markImage(u.groupAvatar.source)
	}
	for _, group := range u.state.Groups {
		u.markAvatarImage(group.Avatar)
	}
	u.markImage(u.state.Settings.Background)
	for _, message := range u.messages {
		if message.Kind != "image" && message.Kind != "file" {
			continue
		}
		file := u.state.File(message.FileID)
		if fileIsImage(file) {
			u.markImage(u.fileLocalPath[file.ID])
		}
	}
	for _, file := range u.files {
		if fileIsImage(file) {
			u.markImage(u.fileLocalPath[file.ID])
		}
	}
	if viewer := &u.surface.viewer; viewer.open {
		u.markImage(u.viewerPath())
	}
	for path := range u.imageCache {
		if !u.mediaDesiredImages[path] {
			delete(u.imageCache, path)
			delete(u.avatarCache, path)
			delete(u.imageStamps, path)
		}
	}
}

func (u *UI) markImage(path string) {
	if path == "" {
		return
	}
	u.mediaDesiredImages[path] = true
}

func (u *UI) markAvatarImage(path string) {
	if path == "" {
		return
	}
	u.mediaDesiredImages[path] = true
	u.avatarImagePaths[path] = true
	if _, ok := u.imageCache[path]; ok {
		u.cacheAvatarBitmap(path)
	}
}

func (u *UI) ensureImage(path string) {
	u.markImage(path)
	u.queueImage(path)
}

func (u *UI) queueImage(path string) {
	if path == "" {
		return
	}
	previous, previousSet := u.imageStamps[path]
	validatingAvatar := u.profile.crop.pending && u.profile.crop.source == path ||
		u.groupAvatar.pending && u.groupAvatar.source == path
	generation, queued := u.mediaImageQueued[path]
	if queued {
		// The old stat can return unchanged without waking the UI. Keep one
		// coalesced probe at the same generation for the newer wake, but do
		// not duplicate the first decode of an image we haven't loaded yet.
		if !previousSet || validatingAvatar {
			return
		}
	} else {
		u.mediaImageGen++
		generation = u.mediaImageGen
		u.mediaImageQueued[path] = generation
	}
	limit := 512
	if (u.profile.crop.open || u.profile.crop.pending) && u.profile.crop.source == path ||
		(u.groupAvatar.open || u.groupAvatar.pending) && u.groupAvatar.source == path {
		// Keep enough source pixels for an 8x crop to remain sharp on a
		// high-DPI display while bounding worker-thread memory.
		limit = 4096
		// A previously cached thumbnail has different decode limits and may be
		// stale. Validate the selected file before entering the editor.
		if validatingAvatar {
			previousSet = false
		}
	} else if u.avatarImagePaths[path] && path != u.state.Settings.Background {
		limit = 192
	} else if path == u.state.Settings.Background {
		limit = 1024
	}
	u.media.submitOne(mediaJob{
		key: mediaJobKey{kind: mediaJobImage, name: path}, kind: mediaJobImage,
		path: path, generation: generation, imageLimit: limit,
		imagePrevious: previous, imagePreviousSet: previousSet,
	})
}

func (u *UI) avatarBitmap(path string) gfx.Bitmap {
	if path == "" {
		return gfx.Bitmap{}
	}
	if bitmap, ok := u.avatarCache[path]; ok && bitmapReady(bitmap) {
		return bitmap
	}
	if bitmap, ok := u.imageCache[path]; ok {
		if bitmap.Width == bitmap.Height && bitmap.Stride == bitmap.Width*4 {
			return bitmap
		}
	}
	if u.profile.open {
		if _, queued := u.mediaImageQueued[path]; queued {
			return u.pendingBitmap(path)
		}
	}
	return gfx.Bitmap{}
}

func (u *UI) selfDisplayName() string {
	name := strings.TrimSpace(u.state.Settings.Nickname)
	if name == "" {
		name = strings.TrimSpace(u.state.Self.Name)
	}
	if name == "" {
		name = i18n.T("我")
	}
	return name
}

func (u *UI) selfAvatarPath() string {
	if avatar := strings.TrimSpace(u.state.Self.Avatar); avatar != "" {
		return avatar
	}
	return strings.TrimSpace(u.state.Settings.Avatar)
}

func (u *UI) cacheAvatarBitmap(path string) {
	if cached, ok := u.avatarCache[path]; ok && bitmapReady(cached) {
		return
	}
	full, ok := u.imageCache[path]
	if !ok {
		return
	}
	square, ok := squareBitmap(full)
	if !ok {
		return
	}
	round, ok := circularBitmap(square)
	if !ok {
		return
	}
	u.nextImageID++
	round.ID = u.nextImageID
	round.Version = 1
	u.avatarCache[path] = round
}

func squareBitmap(bitmap gfx.Bitmap) (gfx.Bitmap, bool) {
	if bitmap.Width <= 0 || bitmap.Height <= 0 || bitmap.Stride < bitmap.Width*4 ||
		len(bitmap.Pix) < bitmap.Stride*bitmap.Height {
		return gfx.Bitmap{}, false
	}
	side := min(bitmap.Width, bitmap.Height)
	if bitmap.Width == bitmap.Height && bitmap.Stride == side*4 {
		return bitmap, true
	}
	offsetX := (bitmap.Width - side) / 2
	offsetY := (bitmap.Height - side) / 2
	stride := side * 4
	pixels := make([]byte, stride*side)
	for y := 0; y < side; y++ {
		start := (offsetY+y)*bitmap.Stride + offsetX*4
		copy(pixels[y*stride:(y+1)*stride], bitmap.Pix[start:start+stride])
	}
	return gfx.Bitmap{
		Pix: pixels, Width: side, Height: side, Stride: stride,
		ID: bitmap.ID, Version: bitmap.Version,
	}, true
}

// cropScaledBitmap materializes the source region a preview showed into one
// square BGRA bitmap. It uses the same area-filtered resize as image decoding,
// so both preview and saved avatar retain detail from high-resolution sources.
// Sampling happens only after confirmation, never in Paint.
func cropScaledBitmap(bitmap gfx.Bitmap, crop gfx.Rect, size int) (gfx.Bitmap, bool) {
	if size <= 0 || bitmap.Width <= 0 || bitmap.Height <= 0 || bitmap.Stride < bitmap.Width*4 ||
		len(bitmap.Pix) < bitmap.Stride*bitmap.Height || crop.Width <= 0 || crop.Height <= 0 {
		return gfx.Bitmap{}, false
	}
	left := max(float64(0), min(float64(bitmap.Width), float64(crop.X)))
	top := max(float64(0), min(float64(bitmap.Height), float64(crop.Y)))
	right := max(left, min(float64(bitmap.Width), float64(crop.X+crop.Width)))
	bottom := max(top, min(float64(bitmap.Height), float64(crop.Y+crop.Height)))
	if left >= right || top >= bottom {
		return gfx.Bitmap{}, false
	}
	stride := size * 4
	pixels := make([]byte, stride*size)
	for y := 0; y < size; y++ {
		y0 := top + float64(y)*(bottom-top)/float64(size)
		y1 := top + float64(y+1)*(bottom-top)/float64(size)
		for x := 0; x < size; x++ {
			x0 := left + float64(x)*(right-left)/float64(size)
			x1 := left + float64(x+1)*(right-left)/float64(size)
			var sums [4]float64
			var weight float64
			for sourceY := int(y0); float64(sourceY) < y1; sourceY++ {
				rowWeight := min(float64(sourceY+1), y1) - max(float64(sourceY), y0)
				for sourceX := int(x0); float64(sourceX) < x1; sourceX++ {
					pixelWeight := rowWeight * (min(float64(sourceX+1), x1) - max(float64(sourceX), x0))
					source := sourceY*bitmap.Stride + sourceX*4
					for channel := 0; channel < 4; channel++ {
						sums[channel] += pixelWeight * float64(bitmap.Pix[source+channel])
					}
					weight += pixelWeight
				}
			}
			target := y*stride + x*4
			if weight == 0 {
				continue
			}
			for channel := 0; channel < 4; channel++ {
				pixels[target+channel] = uint8(sums[channel]/weight + .5)
			}
		}
	}
	return gfx.Bitmap{Pix: pixels, Width: size, Height: size, Stride: stride}, true
}

// circularBitmap applies a one-pixel-antialiased circular alpha mask without
// touching the source. The image is already a square center crop, so painting
// it whole yields a round avatar with one draw instead of per-row strips.
func circularBitmap(bitmap gfx.Bitmap) (gfx.Bitmap, bool) {
	side := bitmap.Width
	if side <= 0 || bitmap.Height != side || bitmap.Stride != side*4 ||
		len(bitmap.Pix) < bitmap.Stride*side {
		return gfx.Bitmap{}, false
	}
	pixels := make([]byte, len(bitmap.Pix))
	copy(pixels, bitmap.Pix)
	center := float64(side) / 2
	radius := center - 0.5
	for y := 0; y < side; y++ {
		dy := float64(y) + 0.5 - center
		for x := 0; x < side; x++ {
			dx := float64(x) + 0.5 - center
			distance := math.Hypot(dx, dy)
			coverage := radius + 1 - distance
			if coverage >= 1 {
				continue
			}
			if coverage <= 0 {
				offset := y*bitmap.Stride + x*4
				pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3] = 0, 0, 0, 0
				continue
			}
			offset := y*bitmap.Stride + x*4
			for channel := 0; channel < 4; channel++ {
				pixels[offset+channel] = uint8(float64(pixels[offset+channel])*coverage + 0.5)
			}
		}
	}
	return gfx.Bitmap{
		Pix: pixels, Width: side, Height: side, Stride: bitmap.Stride,
		ID: bitmap.ID, Version: bitmap.Version,
	}, true
}

func (u *UI) fileBitmap(path string) gfx.Bitmap {
	if path == "" {
		return gfx.Bitmap{}
	}
	return u.imageCache[path]
}

func bitmapReady(bitmap gfx.Bitmap) bool {
	if bitmap.Width <= 0 || bitmap.Height <= 0 || bitmap.Stride < bitmap.Width*4 ||
		len(bitmap.Pix) < bitmap.Stride*bitmap.Height {
		return false
	}
	return bitmap.Width != 1 || bitmap.Height != 1 || bitmap.Stride != 4 ||
		len(bitmap.Pix) != 4 || bitmap.Version != 0
}

func (u *UI) pendingBitmap(path string) gfx.Bitmap {
	if bitmap, ok := u.mediaPendingBitmaps[path]; ok {
		return bitmap
	}
	u.nextImageID++
	bitmap := gfx.Bitmap{
		Pix: make([]byte, 4), Width: 1, Height: 1, Stride: 4,
		ID: u.nextImageID,
	}
	u.mediaPendingBitmaps[path] = bitmap
	return bitmap
}

func (u *UI) isDark() bool { return u.theme.Background.Red < 0.5 }

func themeForName(name string) ui.Theme {
	if strings.EqualFold(name, "dark") {
		theme := ui.DarkTheme()
		theme.Background = ui.RGB(33, 39, 46)
		theme.Card = theme.Background
		theme.CardElevated = ui.RGB(43, 51, 60)
		theme.Popover = ui.RGB(40, 47, 56)
		theme.Muted = theme.CardElevated
		theme.Secondary = theme.CardElevated
		theme.Hover = ui.RGB(48, 57, 67)
		theme.Accent = theme.Hover
		theme.MutedForeground = ui.RGB(166, 175, 187)
		return theme
	}
	return ui.LightTheme()
}

func themeForSettings(settings domain.Settings) ui.Theme {
	theme := themeForName(settings.Theme)
	if pair, ok := schemeColors[settings.ColorScheme]; ok {
		colors := pair.light
		if settings.Theme == "dark" {
			colors = pair.dark
		}
		colors.apply(&theme)
	}
	theme.Metrics.RadiusSurface = radiusCard
	return theme.WithRadius(radiusControl).
		WithTextScale(float32(domain.NormalizeInterfaceFontSize(settings.InterfaceFontSize)) / domain.InterfaceFontSizeDefault)
}

func paletteFor(theme ui.Theme, scheme string) palette {
	if _, ok := schemeColors[scheme]; ok {
		return colorSchemePalette(theme)
	}
	dark := theme.Background.Red < 0.5
	if dark {
		return palette{
			rail:          ui.RGB(24, 29, 36),
			sidebar:       theme.Background,
			chat:          ui.RGB(40, 47, 56),
			header:        theme.Background,
			border:        ui.RGBA(255, 255, 255, 0.09),
			hover:         theme.Hover,
			selected:      ui.RGB(44, 66, 98),
			selectedText:  ui.RGB(152, 195, 255),
			icon:          ui.RGB(205, 210, 220),
			secondaryText: theme.MutedForeground,
			mutedAvatar:   theme.CardElevated,
			avatarText:    ui.RGB(225, 231, 241),
			incoming:      ui.RGB(53, 62, 73),
			own:           ui.RGB(35, 56, 82),
			bubbleBorder:  ui.RGBA(255, 255, 255, 0.06),
			fileCard:      ui.RGB(42, 50, 63),
			fileBorder:    ui.RGBA(172, 196, 230, 0.15),
			invite:        theme.Popover,
			success:       ui.RGB(55, 197, 118),
			danger:        ui.RGB(255, 112, 116),
		}
	}
	return palette{
		rail:          ui.RGB(227, 233, 241),
		sidebar:       ui.RGB(240, 243, 248),
		chat:          ui.RGB(251, 252, 254),
		header:        ui.RGB(251, 252, 254),
		border:        ui.RGB(231, 235, 241),
		hover:         ui.RGB(231, 236, 244),
		selected:      ui.RGB(215, 230, 253),
		selectedText:  ui.RGB(31, 80, 191),
		icon:          ui.RGB(78, 86, 100),
		secondaryText: ui.RGB(96, 107, 124),
		mutedAvatar:   ui.RGB(233, 238, 246),
		avatarText:    ui.RGB(60, 72, 92),
		incoming:      ui.RGB(237, 241, 247),
		own:           ui.RGB(229, 239, 255),
		bubbleBorder:  ui.RGBA(30, 64, 120, 0.05),
		fileCard:      ui.RGB(242, 247, 255),
		fileBorder:    ui.RGB(208, 224, 250),
		invite:        ui.RGB(247, 249, 253),
		success:       ui.RGB(34, 176, 94),
		danger:        ui.RGB(220, 48, 54),
	}
}

func (u *UI) windowControls() app.WindowControlsStyle {
	return windowControlsFor(u.palette)
}

func windowControlsFor(colors palette) app.WindowControlsStyle {
	return app.WindowControlsStyle{
		Foreground:      colors.icon,
		Hover:           colors.hover,
		Pressed:         colors.selected,
		Close:           ui.RGB(218, 54, 51),
		CloseForeground: ui.RGB(255, 255, 255),
		Minimize:        i18n.T("最小化"),
		Maximize:        i18n.T("最大化"),
		Restore:         i18n.T("还原"),
		CloseName:       i18n.T("关闭"),
	}
}

func colorToApp(color ui.Color) app.Color {
	return app.Color{Red: float64(color.Red), Green: float64(color.Green), Blue: float64(color.Blue), Alpha: float64(color.Alpha)}
}

func pointInside(x, y float32, bounds ui.Rect) bool {
	return x >= bounds.X && x < bounds.X+bounds.Width && y >= bounds.Y && y < bounds.Y+bounds.Height
}

const (
	maxImageFileBytes    = int64(32 << 20)
	maxImageDecodePixels = int64(16_000_000)
	maxImageDimension    = 8192
)

func decodeBitmap(path string, limit int) (gfx.Bitmap, bool) {
	bitmap, err := decodeBitmapChecked(path, limit)
	return bitmap, err == nil
}

func decodeBitmapChecked(path string, limit int) (gfx.Bitmap, error) {
	file, err := os.Open(path)
	if err != nil {
		return gfx.Bitmap{}, errors.New("无法读取图片，请检查文件是否存在或有读取权限。")
	}
	defer file.Close()
	info, err := file.Stat()
	maxBytes := maxImageFileBytes
	if limit <= 192 {
		maxBytes = 8 << 20
	}
	if err != nil || !info.Mode().IsRegular() {
		return gfx.Bitmap{}, errors.New("无法读取图片，请检查文件是否存在或有读取权限。")
	}
	if info.Size() > maxBytes {
		return gfx.Bitmap{}, errors.New("图片文件太大，请选择不超过 32 MB 的图片。")
	}
	config, format, err := image.DecodeConfig(io.LimitReader(file, maxBytes+1))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return gfx.Bitmap{}, errors.New("不支持的图片格式，请选择 PNG、JPEG 或 GIF 图片。")
		}
		return gfx.Bitmap{}, errors.New("图片已损坏或无法解码，请选择其他图片。")
	}
	maxPixels := maxImageDecodePixels
	if format == "gif" || (format == "png" && limit <= 192) {
		maxPixels = min(maxPixels, 8_000_000)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageDimension || config.Height > maxImageDimension ||
		int64(config.Width)*int64(config.Height) > maxPixels {
		return gfx.Bitmap{}, errors.New("图片尺寸太大：最长边不超过 8192 像素，总像素不超过 1600 万（GIF 为 800 万）。")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return gfx.Bitmap{}, errors.New("无法读取图片，请检查文件是否存在或有读取权限。")
	}
	source, _, err := image.Decode(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return gfx.Bitmap{}, errors.New("图片已损坏或无法解码，请选择其他图片。")
	}
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return gfx.Bitmap{}, errors.New("图片已损坏或无法解码，请选择其他图片。")
	}
	width, height := bounds.Dx(), bounds.Dy()
	if width > limit || height > limit {
		scale := min(float64(limit)/float64(width), float64(limit)/float64(height))
		width = max(1, int(float64(width)*scale))
		height = max(1, int(float64(height)*scale))
	}
	if width == bounds.Dx() && height == bounds.Dy() {
		rgba, ok := source.(*image.RGBA)
		if !ok || rgba.Stride != width*4 || len(rgba.Pix) != width*height*4 {
			rgba = image.NewRGBA(image.Rect(0, 0, width, height))
			draw.Draw(rgba, rgba.Bounds(), source, bounds.Min, draw.Src)
		}
		for i := 0; i < len(rgba.Pix); i += 4 {
			rgba.Pix[i], rgba.Pix[i+2] = rgba.Pix[i+2], rgba.Pix[i]
		}
		return gfx.Bitmap{Pix: rgba.Pix, Width: width, Height: height, Stride: rgba.Stride}, nil
	}
	pixels := resamplePremultipliedBGRA(source, width, height)
	return gfx.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4}, nil
}

// resamplePremultipliedBGRA shrinks an image with a box filter. Each output
// pixel averages every source pixel it covers, including fractional coverage at
// the edges. Sampling in premultiplied color keeps transparent edges from
// acquiring dark fringes when the image later reaches the GPU.
func resamplePremultipliedBGRA(source image.Image, width, height int) []byte {
	bounds := source.Bounds()
	pixels := make([]byte, width*height*4)
	sourceWidth, sourceHeight := float64(bounds.Dx()), float64(bounds.Dy())
	for y := 0; y < height; y++ {
		y0 := float64(y) * sourceHeight / float64(height)
		y1 := float64(y+1) * sourceHeight / float64(height)
		for x := 0; x < width; x++ {
			x0 := float64(x) * sourceWidth / float64(width)
			x1 := float64(x+1) * sourceWidth / float64(width)
			var sums [4]float64
			var weight float64
			for sourceY := int(y0); float64(sourceY) < y1; sourceY++ {
				rowWeight := min(float64(sourceY+1), y1) - max(float64(sourceY), y0)
				for sourceX := int(x0); float64(sourceX) < x1; sourceX++ {
					pixelWeight := rowWeight * (min(float64(sourceX+1), x1) - max(float64(sourceX), x0))
					red, green, blue, alpha := source.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY).RGBA()
					sums[0] += pixelWeight * float64(blue)
					sums[1] += pixelWeight * float64(green)
					sums[2] += pixelWeight * float64(red)
					sums[3] += pixelWeight * float64(alpha)
					weight += pixelWeight
				}
			}
			if weight == 0 {
				continue
			}
			offset := (y*width + x) * 4
			for channel := range sums {
				pixels[offset+channel] = uint8(sums[channel]/weight/257 + .5)
			}
		}
	}
	return pixels
}

func initialOf(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return "?"
	}
	last := gfx.GraphemeStart(name, len(name)-1)
	if last <= 0 {
		return name
	}
	previous := gfx.GraphemeStart(name, last-1)
	return name[previous:]
}

func shortTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("15:04")
}

func intString(value int) string {
	switch {
	case value == 0:
		return "0"
	case value < 0:
		return "-" + intString(-value)
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func containsFold(value, query string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(query))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func generatedGroupName(u *UI, ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, u.state.Peer(id).Name)
	}
	switch len(names) {
	case 0:
		return i18n.T("新群聊")
	case 1:
		return truncateRunes(names[0]+"的群聊", 30)
	case 2:
		return truncateRunes(names[0]+"、"+names[1], 30)
	default:
		return truncateRunes(names[0]+"、"+names[1]+"等"+intString(len(names))+"人", 30)
	}
}

func fileIsImage(file domain.File) bool {
	if strings.HasPrefix(strings.ToLower(file.MIME), "image/") {
		return true
	}
	name := strings.ToLower(file.Name)
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func displayContactGroup(name string) string {
	if name == "未分组" {
		return i18n.T(name)
	}
	return name
}

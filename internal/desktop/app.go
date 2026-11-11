// Package desktop implements GoIM's native Viva desktop shell.
package desktop

import (
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
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	sectionMessages   = "messages"
	sectionContacts   = "contacts"
	sectionTransfers  = "transfers"
	sectionShares     = "shares"
	sectionSettings   = "settings"
	titlebarHeight    = float32(48)
	railWidth         = float32(68)
	sidebarWidth      = float32(300)
	minimumWindowSize = float32(1000)
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
	name          flex.Field
	search        flex.Field
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

type profileState struct {
	open      bool
	nickname  flex.Field
	signature flex.Field
	details   [5]flex.Field
	gender    int
	presence  domain.Presence
	avatar    string
	err       string
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
	image  ui.Bitmap
	status components.AvatarStatus
}

type imageStamp struct {
	modified int64
	size     int64
}

type appIDs struct {
	page   ui.Scope
	nav    ui.Scope
	side   ui.Scope
	chat   ui.Scope
	dialog ui.Scope
	menu   ui.Scope

	navMessages   ui.ID
	navContacts   ui.ID
	navTransfers  ui.ID
	navShares     ui.ID
	navLock       ui.ID
	navTheme      ui.ID
	navSettings   ui.ID
	profileAvatar ui.ID

	sidebarCreate  ui.ID
	sidebarConnect ui.ID
	sidebarSearch  ui.ID
	filterAll      ui.ID
	filterOnline   ui.ID
	emptyConnect   ui.ID
	emptyCompose   ui.ID

	chatMessages                                                           ui.ID
	chatEditor                                                             ui.ID
	chatScreenshot                                                         ui.ID
	chatSend                                                               ui.ID
	chatImage                                                              ui.ID
	chatFile                                                               ui.ID
	chatEmoji                                                              ui.ID
	chatHeaderFile                                                         ui.ID
	chatHeaderSearch                                                       ui.ID
	chatHeaderMembers                                                      ui.ID
	chatHeaderMore                                                         ui.ID
	chatSearchField                                                        ui.ID
	chatSearchClose                                                        ui.ID
	chatEmptyConnect                                                       ui.ID
	chatEmptyCompose                                                       ui.ID
	chatMenuDismiss                                                        ui.ID
	chatMenuPin                                                            ui.ID
	chatMenuMute                                                           ui.ID
	chatMenuRead                                                           ui.ID
	chatMenuLeave                                                          ui.ID
	chatNoticeDismiss                                                      ui.ID
	chatMenuScope                                                          ui.ID
	shellMenuDismiss                                                       ui.ID
	shellMenuPin                                                           ui.ID
	shellMenuMute                                                          ui.ID
	shellMenuRead                                                          ui.ID
	shellMenuDelete                                                        ui.ID
	shellMenuTalk                                                          ui.ID
	shellMenuOrganize                                                      ui.ID
	shellMenuProfile                                                       ui.ID
	shellMenuNewGroup                                                      ui.ID
	shellMenuNewContactGroup                                               ui.ID
	shellMenuRenameContactGroup                                            ui.ID
	shellMenuDeleteContactGroup                                            ui.ID
	shellMenuConnect                                                       ui.ID
	shellMenuExpand                                                        ui.ID
	shellMenuCollapse                                                      ui.ID
	shellMenuScope                                                         ui.ID
	sidebarSurface                                                         ui.ID
	selfRow                                                                ui.ID
	sidebarContactList                                                     ui.ID
	sidebarChatList                                                        ui.ID
	mentionScope                                                           ui.ID
	mentionDismiss                                                         ui.ID
	emojiScope                                                             ui.ID
	emojiScrollbar                                                         ui.ID
	emojiScroll                                                            ui.ID
	messageMenuCopy, messageMenuSave, messageMenuRecall, messageMenuDelete ui.ID
	emojiDismiss                                                           ui.ID

	connectScope       ui.ID
	connectDismiss     ui.ID
	connectClose       ui.ID
	connectField       ui.ID
	connectProtocol    ui.Scope
	contactDetails     ui.ID
	contactTalk        ui.ID
	contactMemberScope ui.Scope
	chatHeaderAvatar   ui.ID
	connectCancel      ui.ID
	connectSubmit      ui.ID

	composeScope   ui.ID
	composeDismiss ui.ID
	composeClose   ui.ID
	composeName    ui.ID
	composeSearch  ui.ID
	composeCreate  ui.ID
	composeCancel  ui.ID
	composeScroll  ui.ID
	composePicked  ui.ID

	profileScope       ui.ID
	profileDismiss     ui.ID
	profileClose       ui.ID
	profileField       ui.ID
	profileSignature   ui.ID
	profileDetails     [5]ui.ID
	profileGender      ui.Scope
	profilePresence    ui.Scope
	profileChooseImage ui.ID
	profileSave        ui.ID
	profileCancel      ui.ID

	organizeScope   ui.ID
	organizeDismiss ui.ID
	organizeClose   ui.ID
	organizeField   ui.ID
	organizeSave    ui.ID
	organizeCancel  ui.ID

	memberScope          ui.ID
	memberListScroll     ui.ID
	memberDismiss        ui.ID
	memberClose          ui.ID
	memberSearch         ui.ID
	memberInvite         ui.ID
	memberStartRename    ui.ID
	memberRename         ui.ID
	memberSaveRename     ui.ID
	memberCancelRename   ui.ID
	memberStartNickname  ui.ID
	memberNickname       ui.ID
	memberSaveNickname   ui.ID
	memberCancelNickname ui.ID
	memberLeave          ui.ID
	memberDissolve       ui.ID
	memberMenuDismiss    ui.ID
	memberMenuScope      ui.ID
	memberProfileScope   ui.ID
	memberProfileDismiss ui.ID
	memberProfileClose   ui.ID
	memberProfileTalk    ui.ID
	memberProfileScroll  ui.ID
	memberMuteToggle     ui.ID
	memberPinToggle      ui.ID

	fileScope        ui.ID
	fileListScroll   ui.ID
	fileDetailScroll ui.ID
	fileDismiss      ui.ID
	fileClose        ui.ID
	fileSearch       ui.ID
	fileFilterAll    ui.ID
	fileFilterImage  ui.ID
	fileFilterFile   ui.ID
	fileBack         ui.ID
	fileOpen         ui.ID
	fileReveal       ui.ID
	fileAccept       ui.ID
	fileReject       ui.ID
	fileCancel       ui.ID
	fileRetry        ui.ID
	fileSave         ui.ID
	fileRefresh      ui.ID
	fileBatchCancel  ui.ID

	confirmScope   ui.ID
	confirmDismiss ui.ID
	confirmClose   ui.ID
	confirmCancel  ui.ID
	confirmRun     ui.ID
}

// UI is the native Viva desktop application. It reads detached backend
// snapshots and owns only presentation and editor state.
type UI struct {
	backend     domain.Backend
	state       domain.State
	localNotice string
	theme       ui.Theme
	palette     palette
	tree        ui.Tree
	panels      Panels
	panelInput  app.TextInputProvider
	invalidator app.Invalidator
	done        chan struct{}
	closeOnce   sync.Once

	titleView   flex.View
	railView    flex.View
	sidebarView flex.View
	chatView    flex.View
	dialogView  flex.View
	drawerView  flex.View
	mentionView flex.View
	emojiView   flex.View
	hover       components.HoverTooltip

	ids appIDs

	section         string
	selected        string
	pendingPeer     string
	pendingGroup    string
	sidebarFilter   string
	collapsed       map[string]bool
	sidebarSearch   flex.Field
	connectOpen     bool
	connectField    flex.Field
	connectProtocol flex.SelectState
	connectRoute    int
	contactPeer     string
	contactGroup    string

	chatEditor        *text.Editor
	requestScreenshot func()
	presentViewer     func()
	pinViewer         func(bool) bool
	saveViewer        func(string)
	fullscreenViewer  func(bool) bool
	dismissViewer     func()
	chatInput         app.TextInput
	chatLayout        components.TextareaLayout
	drafts            map[string]string
	draftSaved        map[string]string
	moreOpen          bool
	dragging          bool
	chatScrollToEnd   bool
	emojiOpen         bool
	emojiButton       flex.Box

	mention       mentionState
	mentionEditor flex.Box
	composerBox   flex.Box
	chatFind      chatFindState

	compose        composeState
	memberSearch   flex.Field
	renameOpen     bool
	renameGroupID  string
	renameField    flex.Field
	nicknameOpen   bool
	nicknameGroup  string
	nicknameField  flex.Field
	nicknameErr    string
	memberMenu     string
	memberProfile  string
	fileSearch     flex.Field
	fileFilter     string
	drawer         string
	activeFileID   string
	fileReturn     string
	confirm        confirmation
	shellMenu      shellMenuState
	organize       organizeState
	lockUI         lockUIState
	focusSearch    bool
	focusedID      ui.ID
	titleLeading   float32
	titleTrailing  float32
	lastBody       ui.Rect
	lastChat       ui.Rect
	lastDialogBody ui.Rect
	profile        profileState
	filePickerOpen bool
	nativeWindow   windowGeometry

	clipboardImage    func() ([]byte, error)
	clipboardCacheDir func() (string, error)

	windowVisible atomic.Bool
	readMarkID    string
	readMarkCount int

	conversations    []domain.Conversation
	groupChats       []domain.Conversation
	contacts         []contactGroupView
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

	peerRowIDs          map[string]ui.ID
	convRowIDs          map[string]ui.ID
	groupHeadIDs        map[string]ui.ID
	inviteAcceptIDs     map[string]ui.ID
	inviteRejectIDs     map[string]ui.ID
	fileRowIDs          map[string]ui.ID
	fileButtonIDs       map[string]ui.ID
	fileAcceptIDs       map[string]ui.ID
	fileRejectIDs       map[string]ui.ID
	fileCancelIDs       map[string]ui.ID
	fileRetryIDs        map[string]ui.ID
	fileSaveIDs         map[string]ui.ID
	groupSelectIDs      map[string]ui.ID
	groupSelectRows     map[string]ui.ID
	groupRemoveIDs      map[string]ui.ID
	memberMenuIDs       map[string]ui.ID
	memberInfoIDs       map[string]ui.ID
	contactMemberIDs    map[string]ui.ID
	memberMessageIDs    map[string]ui.ID
	memberRemoveIDs     map[string]ui.ID
	memberTransferIDs   map[string]ui.ID
	composeGroupHeadIDs map[string]ui.ID
	inviteWithdrawIDs   map[string]ui.ID
	inviteReinviteIDs   map[string]ui.ID
	mentionCandidateIDs map[string]ui.ID
	emojiIDs            map[string]ui.ID
	emojiRecentIDs      map[string]ui.ID
	recentEmoji         []string
	emojiScrollDrag     bool
	emojiScrollGrab     float32
	emojiPanel          ui.Rect
	messageIDs          map[string]ui.ID
	messageAvatarIDs    map[string]ui.ID

	imageCache  map[string]ui.Bitmap
	avatarCache map[string]ui.Bitmap
	imageStamps map[string]imageStamp
	nextImageID uint64

	avatarImagePaths map[string]bool

	media               *mediaWorker
	mediaImageGen       uint64
	mediaImageQueued    map[string]uint64
	mediaDesiredImages  map[string]bool
	mediaPendingBitmaps map[string]ui.Bitmap

	iconMessage       ui.Icon
	iconUsers         ui.Icon
	iconTransfers     ui.Icon
	iconFolder        ui.Icon
	iconSettings      ui.Icon
	iconMoon          ui.Icon
	iconSun           ui.Icon
	iconUserPlus      ui.Icon
	iconPlus          ui.Icon
	iconInbox         ui.Icon
	iconChevronDown   ui.Icon
	iconChevronRight  ui.Icon
	iconPin           ui.Icon
	iconBellOff       ui.Icon
	iconPaperclip     ui.Icon
	iconImage         ui.Icon
	iconSmile         ui.Icon
	iconSend          ui.Icon
	iconFile          ui.Icon
	iconFileText      ui.Icon
	iconFolderOpen    ui.Icon
	iconDownload      ui.Icon
	iconUpload        ui.Icon
	iconCheck         ui.Icon
	iconCheckCheck    ui.Icon
	iconX             ui.Icon
	iconMore          ui.Icon
	iconSearch        ui.Icon
	iconExternalLink  ui.Icon
	iconRotate        ui.Icon
	iconTrash         ui.Icon
	iconUserMinus     ui.Icon
	iconCrown         ui.Icon
	iconLogOut        ui.Icon
	iconPencil        ui.Icon
	iconClock         ui.Icon
	iconAlert         ui.Icon
	iconWifi          ui.Icon
	iconWifiOff       ui.Icon
	iconAt            ui.Icon
	iconFileArchive   ui.Icon
	iconMessageSquare ui.Icon
	iconCircleCheck   ui.Icon
	iconPlay          ui.Icon
	iconPause         ui.Icon
	iconHardDrive     ui.Icon
	iconRefresh       ui.Icon
	iconEye           ui.Icon
	iconLock          ui.Icon
	iconShield        ui.Icon
	iconCalendar      ui.Icon
	iconReply         ui.Icon
	iconArchive       ui.Icon
	iconLink          ui.Icon
	iconCopy          ui.Icon
}

// New creates the shell from an already-running backend. It takes the first
// snapshot before the first paint and then refreshes only on OnWake.
func New(backend domain.Backend) *UI {
	u := &UI{
		backend:             backend,
		theme:               ui.LightTheme(),
		done:                make(chan struct{}),
		section:             sectionMessages,
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
		peerRowIDs:          make(map[string]ui.ID),
		convRowIDs:          make(map[string]ui.ID),
		groupHeadIDs:        make(map[string]ui.ID),
		inviteAcceptIDs:     make(map[string]ui.ID),
		inviteRejectIDs:     make(map[string]ui.ID),
		fileRowIDs:          make(map[string]ui.ID),
		fileButtonIDs:       make(map[string]ui.ID),
		fileAcceptIDs:       make(map[string]ui.ID),
		fileRejectIDs:       make(map[string]ui.ID),
		fileCancelIDs:       make(map[string]ui.ID),
		fileRetryIDs:        make(map[string]ui.ID),
		fileSaveIDs:         make(map[string]ui.ID),
		groupSelectIDs:      make(map[string]ui.ID),
		groupSelectRows:     make(map[string]ui.ID),
		groupRemoveIDs:      make(map[string]ui.ID),
		memberMenuIDs:       make(map[string]ui.ID),
		memberInfoIDs:       make(map[string]ui.ID),
		contactMemberIDs:    make(map[string]ui.ID),
		memberMessageIDs:    make(map[string]ui.ID),
		memberRemoveIDs:     make(map[string]ui.ID),
		memberTransferIDs:   make(map[string]ui.ID),
		composeGroupHeadIDs: make(map[string]ui.ID),
		inviteWithdrawIDs:   make(map[string]ui.ID),
		inviteReinviteIDs:   make(map[string]ui.ID),
		mentionCandidateIDs: make(map[string]ui.ID),
		emojiIDs:            make(map[string]ui.ID),
		emojiRecentIDs:      make(map[string]ui.ID),
		messageIDs:          make(map[string]ui.ID),
		messageAvatarIDs:    make(map[string]ui.ID),
		imageCache:          make(map[string]ui.Bitmap),
		avatarCache:         make(map[string]ui.Bitmap),
		imageStamps:         make(map[string]imageStamp),
		avatarImagePaths:    make(map[string]bool),
		mediaImageQueued:    make(map[string]uint64),
		mediaDesiredImages:  make(map[string]bool),
		mediaPendingBitmaps: make(map[string]ui.Bitmap),
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
		iconFile:          File,
		iconFileText:      FileText,
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
		iconFileArchive:   FileArchive,
		iconMessageSquare: MessageSquare,
		iconCircleCheck:   CircleCheck,
		iconPlay:          Play,
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
	}
	u.windowVisible.Store(true)
	u.palette = paletteFor(u.theme)
	u.titleLeading, u.titleTrailing = app.TitlebarInsets()
	u.media = newMediaWorker(func() { u.invalidator.Invalidate() })
	u.initIDs()
	u.panels.Init(&u.tree)
	u.panels.settingsExtra.openProfile = u.openProfile
	u.panels.setMediaSubmit(u.media.submitOne)
	u.panels.shareQRWorker.setNotify(u.invalidator.Invalidate)
	u.panelInput = u.panels.TextInput()
	if backend != nil {
		u.refresh()
		u.recentEmoji = append([]string(nil), u.state.Settings.RecentEmoji...)
		u.queueMediaJobs()
		go u.watchUpdates()
	}
	return u
}

// Config describes the one native window this shell owns.
func (u *UI) Config() app.Config {
	clear := colorToApp(u.palette.sidebar)
	return app.Config{
		Title:          "GoIM",
		Width:          1440,
		Height:         960,
		MinWidth:       1000,
		MinHeight:      700,
		WindowMode:     app.WindowCustomTitlebar,
		TitlebarHeight: titlebarHeight,
		ClearColor:     clear,
		Tree:           &u.tree,
		WindowControls: u.windowControls,
		Paint:          u.paint,
		TextInput:      u.textInput,
		Invalidator:    &u.invalidator,
		OnWake:         u.onWake,
		OnScroll:       u.onScroll,
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
			u.invalidator.Invalidate()
		case <-u.done:
			return
		}
	}
}

func (u *UI) onWake() {
	u.refresh()
	u.applyMediaResults()
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

func (u *UI) refresh() {
	if u.backend == nil {
		return
	}
	oldError := u.state.Error
	u.state = u.backend.Snapshot()
	i18n.SetLanguage(u.state.Settings.Language)
	u.panels.OnWake(u.state)
	u.rebuildPresentation(oldError)
	u.markSelectedConversationRead()
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
		if u.profile.open && u.profile.avatar == result.path {
			u.profile.err = ""
		}
	}

	if len(batch.actions) > 0 {
		u.panels.applyMediaActions(batch.actions)
	}
}

func (u *UI) markSelectedConversationRead() {
	if !u.windowVisible.Load() || (u.nativeWindow != nil && !mainWindowForeground()) || u.section != sectionMessages || u.selected == "" ||
		u.pendingPeer != "" || u.pendingGroup != "" || u.chatFind.open || u.modalOpen() {
		u.readMarkID = ""
		u.readMarkCount = 0
		return
	}
	if state := u.chatView.ScrollState(u.ids.chatMessages); state != nil && !state.AtEnd() {
		u.readMarkID = ""
		u.readMarkCount = 0
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
	u.readMarkID = conversation.ID
	u.readMarkCount = conversation.Unread
	u.submit(domain.Command{Kind: domain.MarkRead, ConversationID: conversation.ID})
}

func (u *UI) modalOpen() bool {
	return u.confirm.open || u.compose.open || u.connectOpen ||
		u.profile.open || u.memberProfile != "" || u.organize.open ||
		u.shellMenu.open || u.lockVisible()
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

	u.ids.navMessages = u.tree.ID(u.ids.nav, "messages")
	u.ids.navContacts = u.tree.ID(u.ids.nav, "contacts")
	u.ids.navTransfers = u.tree.ID(u.ids.nav, "transfers")
	u.ids.navShares = u.tree.ID(u.ids.nav, "shares")
	u.ids.navLock = u.tree.ID(u.ids.nav, "lock")
	u.ids.navTheme = u.tree.ID(u.ids.nav, "appearance")
	u.ids.navSettings = u.tree.ID(u.ids.nav, "settings")
	u.ids.profileAvatar = u.tree.ID(u.ids.nav, "profile-avatar")

	u.ids.sidebarCreate = u.tree.ID(u.ids.side, "create")
	u.ids.sidebarConnect = u.tree.ID(u.ids.side, "connect")
	u.ids.sidebarSearch = u.tree.ID(u.ids.side, "search")
	u.ids.filterAll = u.tree.ID(u.ids.side, "filter-all")
	u.ids.filterOnline = u.tree.ID(u.ids.side, "filter-online")
	u.ids.emptyConnect = u.tree.ID(u.ids.side, "empty-connect")
	u.ids.emptyCompose = u.tree.ID(u.ids.side, "empty-compose")
	u.ids.sidebarSurface = u.tree.ID(u.ids.side, "surface")
	u.ids.sidebarContactList = u.tree.ID(u.ids.side, "contact-list")
	u.ids.sidebarChatList = u.tree.ID(u.ids.side, "chat-list")
	u.ids.selfRow = u.tree.ID(u.ids.side, "self-row")
	u.ids.mentionScope = u.tree.ID(u.ids.chat, "mention-scope")
	u.ids.mentionDismiss = u.tree.ID(u.ids.chat, "mention-dismiss")
	u.ids.emojiScope = u.tree.ID(u.ids.chat, "emoji-scope")
	u.ids.emojiScroll = u.tree.ID(u.ids.chat, "emoji-scroll")
	u.ids.emojiScrollbar = u.tree.ID(u.ids.chat, "emoji-scrollbar")
	u.ids.messageMenuCopy = u.tree.ID(u.ids.menu, "message-copy")
	u.ids.messageMenuSave = u.tree.ID(u.ids.menu, "message-save")
	u.ids.messageMenuRecall = u.tree.ID(u.ids.menu, "message-recall")
	u.ids.messageMenuDelete = u.tree.ID(u.ids.menu, "message-delete")
	u.ids.emojiDismiss = u.tree.ID(u.ids.chat, "emoji-dismiss")

	u.ids.chatMessages = u.tree.ID(u.ids.chat, "messages")
	u.ids.chatEditor = u.tree.ID(u.ids.chat, "editor")
	u.ids.chatScreenshot = u.tree.ID(u.ids.chat, "screenshot")
	u.ids.chatSend = u.tree.ID(u.ids.chat, "send")
	u.ids.chatImage = u.tree.ID(u.ids.chat, "send-image")
	u.ids.chatFile = u.tree.ID(u.ids.chat, "send-file")
	u.ids.chatEmoji = u.tree.ID(u.ids.chat, "emoji")
	u.ids.chatHeaderSearch = u.tree.ID(u.ids.chat, "header-search")
	u.ids.chatHeaderFile = u.tree.ID(u.ids.chat, "header-files")
	u.ids.chatHeaderMembers = u.tree.ID(u.ids.chat, "header-members")
	u.ids.chatHeaderMore = u.tree.ID(u.ids.chat, "header-more")
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
	u.ids.connectProtocol = u.tree.Sub(u.ids.dialog, u.tree.Name("connect-protocol"))
	contacts := u.tree.Feature("contact-details")
	u.ids.contactDetails = u.tree.ID(contacts, "scroll")
	u.ids.contactTalk = u.tree.ID(contacts, "primary-action")
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
	u.ids.composeScroll = u.tree.ID(u.ids.dialog, "compose-list-scroll")
	u.ids.composePicked = u.tree.ID(u.ids.dialog, "compose-selected-scroll")

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
	u.ids.profileSave = u.tree.ID(u.ids.dialog, "profile-save")
	u.ids.profileCancel = u.tree.ID(u.ids.dialog, "profile-cancel")

	u.ids.organizeScope = u.tree.ID(u.ids.dialog, "organize-scope")
	u.ids.organizeDismiss = u.tree.ID(u.ids.dialog, "organize-dismiss")
	u.ids.organizeClose = u.tree.ID(u.ids.dialog, "organize-close")
	u.ids.organizeField = u.tree.ID(u.ids.dialog, "organize-field")
	u.ids.organizeSave = u.tree.ID(u.ids.dialog, "organize-save")
	u.ids.organizeCancel = u.tree.ID(u.ids.dialog, "organize-cancel")

	u.ids.memberScope = u.tree.ID(u.ids.dialog, "member-scope")
	u.ids.memberListScroll = u.tree.ID(u.ids.dialog, "member-list-scroll")
	u.ids.memberDismiss = u.tree.ID(u.ids.dialog, "member-dismiss")
	u.ids.memberClose = u.tree.ID(u.ids.dialog, "member-close")
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
	u.ids.memberProfileScroll = u.tree.ID(u.ids.dialog, "member-profile-scroll")
	u.ids.memberMuteToggle = u.tree.ID(u.ids.dialog, "member-mute")
	u.ids.memberPinToggle = u.tree.ID(u.ids.dialog, "member-pin")

	u.ids.fileScope = u.tree.ID(u.ids.dialog, "file-scope")
	u.ids.fileListScroll = u.tree.ID(u.ids.dialog, "file-list-scroll")
	u.ids.fileDetailScroll = u.tree.ID(u.ids.dialog, "file-detail-scroll")
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

	for _, choice := range emojiChoices {
		u.emojiIDs[choice.Name] = u.tree.ID(u.ids.chat, "emoji:"+choice.Name)
		u.emojiRecentIDs[choice.Name] = u.tree.ID(u.ids.chat, "emoji-recent:"+choice.Name)
	}
}

func (u *UI) paint(canvas *ui.Canvas, size ui.Size) {
	u.theme = themeForName(u.state.Settings.Theme)
	u.palette = paletteFor(u.theme)
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.palette.sidebar)
	u.focusedID = canvas.Focused()

	totalTitle := min(titlebarHeight, bounds.Height)
	titlebar := ui.Rect{Width: bounds.Width, Height: totalTitle}
	body := ui.Rect{Y: totalTitle, Width: bounds.Width, Height: max(bounds.Height-totalTitle, 0)}
	u.lastBody = body
	u.drawTitlebar(canvas, titlebar)
	if u.lockVisible() {
		u.drawLocked(canvas, body)
		return
	}

	u.drawMainBody(canvas, body)

	if u.chatFind.focus && u.chatFind.open {
		canvas.RequestFocus(u.ids.chatSearchField)
		u.chatFind.focus = false
	}
	if u.focusSearch {
		canvas.RequestFocus(u.ids.sidebarSearch)
		u.focusSearch = false
	}
	u.hover.Paint(canvas, u.theme, body, components.TooltipOptions{Layer: 55}, 0)
	u.drawOverlays(canvas, bounds)
	u.drawChatImageViewer(canvas)
}

func (u *UI) isPanelSection() bool {
	return u.section == sectionSettings || u.section == sectionTransfers || u.section == sectionShares
}

func (u *UI) drawTitlebar(canvas *ui.Canvas, bounds ui.Rect) {
	if bounds.Height <= 0 {
		return
	}
	canvas.Fill(bounds, u.palette.rail)
	var brand flex.Box
	u.titleView.Draw(canvas, u.theme, bounds, func(f *flex.Frame) {
		f.Row().Fill().MovesWindow().
			PadSides(0, u.titleTrailing+14, 0, u.titleLeading+20).
			Items(flex.Center).Children(func() {
			brand = f.Slot().Size(24, 24)
			f.Row().Width(9).Height(1)
			f.Text("GoIM").FontSize(16).Weight(ui.WeightSemibold)
			f.Spacer()
			if u.state.ListenAddress != "" && bounds.Width > 900 {
				f.Row().Items(flex.Center).Gap(7).Children(func() {
					f.Stack().Size(6, 6).Radius(3).Background(u.palette.success)
					f.Text(u.state.ListenAddress).FontSize(11).Muted()
				})
			}
		})
	})
	paintGoIMMark(canvas, brand.Rect(), titleMark)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, u.palette.border)
}

func (u *UI) drawRail(canvas *ui.Canvas, bounds ui.Rect) {
	canvas.Fill(bounds, u.palette.rail)
	var avatarSlot flex.Box
	var messages, contacts, transfers, shares, lock, themeToggle, settings flex.Box
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
	u.railView.Draw(canvas, u.theme, bounds, func(f *flex.Frame) {
		f.Column().Fill().PadY(14).Items(flex.Center).Gap(5).Children(func() {
			avatarSlot = f.Stack().Size(42, 42).HitWith(u.ids.profileAvatar, i18n.T("编辑个人资料"),
				ui.HitOptions{Role: ui.RoleButton, Hint: i18n.T("编辑昵称和头像")}).
				Children(func() {
					u.avatarStatusOn(f, selfName, selfAvatar, 42, u.state.Self.Presence != domain.PresenceInvisible, u.palette.rail)
				})
			f.Row().Height(10)
			messages = u.railItemBadged(f, u.ids.navMessages, u.iconMessage, i18n.T("消息"),
				u.section == sectionMessages, unreadBadge, unreadHint)
			contacts = u.railItem(f, u.ids.navContacts, u.iconUsers, i18n.T("联系人"), u.section == sectionContacts)
			transfers = u.railItemBadged(f, u.ids.navTransfers, u.iconTransfers, i18n.T("传输"),
				u.section == sectionTransfers, transferBadge, transferHint)
			shares = u.railItem(f, u.ids.navShares, FolderOutput, "文件共享", u.section == sectionShares)
			f.Spacer()
			lock = u.railItem(f, u.ids.navLock, u.iconLock, i18n.T("锁定应用"), false)
			themeIcon, themeLabel := u.iconMoon, "切换到暗色外观"
			if u.isDark() {
				themeIcon, themeLabel = u.iconSun, "切换到明亮外观"
			}
			themeToggle = u.railItem(f, u.ids.navTheme, themeIcon, themeLabel, false)
			settings = u.railItem(f, u.ids.navSettings, u.iconSettings, i18n.T("设置"), u.section == sectionSettings)
		})
	})
	if messages.Clicked() {
		u.setSection(sectionMessages)
		u.markSelectedConversationRead()
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
	canvas.Fill(ui.Rect{X: bounds.X + bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, u.palette.border)
}

func (u *UI) railItem(f *flex.Frame, id ui.ID, icon ui.Icon, label string, active bool) flex.Box {
	return u.railItemBadged(f, id, icon, label, active, "", "")
}

func (u *UI) railItemBadged(f *flex.Frame, id ui.ID, icon ui.Icon, label string,
	active bool, badge, badgeHint string) flex.Box {
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
	box := f.Column().Size(56, 58).Radius(10).Items(flex.Center).Justify(flex.Center).Gap(4).
		Background(background).Hover(flex.Look{}.Background(hover)).
		HitWith(id, accessible, ui.HitOptions{Role: ui.RoleButton, Selected: active, Hint: accessible}).
		Children(func() {
			f.Stack().Width(36).Height(24).Children(func() {
				f.Stack().Size(21, 21).Absolute().Left(7).Top(2).Children(func() {
					f.Icon(icon).Size(21, 21).Color(foreground)
				})
				if badge != "" {
					f.Row().MinWidth(16).Height(16).PadX(4).Radius(8).Absolute().Right(0).Top(0).
						Background(u.theme.Primary).Items(flex.Center).Justify(flex.Center).
						Children(func() {
							f.Text(badge).FontSize(9).Weight(ui.WeightSemibold).
								Color(u.theme.PrimaryForeground).TextCenter()
						})
				}
			})
			f.Text(visual).FontSize(11).Weight(ui.WeightMedium).Color(foreground)
		})
	return box
}

func (u *UI) drawSidebar(canvas *ui.Canvas, bounds ui.Rect) {
	canvas.Fill(bounds, u.palette.sidebar)
	var connect, create flex.Box
	u.sidebarView.Draw(canvas, u.theme, bounds, func(f *flex.Frame) {
		f.Column().Fill().PadSides(24, 16, 14, 16).Children(func() {
			f.Row().Height(38).Items(flex.Center).Gap(8).Children(func() {
				f.Field(&u.sidebarSearch, u.ids.sidebarSearch, i18n.T("搜索"), u.searchPlaceholder()).
					Height(38).Grow(1)
				connect = u.iconButton(f, u.ids.sidebarConnect, u.iconUserPlus, i18n.T("手动连接"), 32)
				create = u.iconButton(f, u.ids.sidebarCreate, u.iconPlus, i18n.T("创建群聊"), 32)
			})
			f.Row().Height(10)
			if u.section == sectionContacts {
				u.drawContactFilters(f)
				f.Row().Height(10)
			}
			listID := u.ids.sidebarChatList
			if u.section == sectionContacts {
				listID = u.ids.sidebarContactList
			}
			f.ScrollArea(listID).Grow(1).Gap(4).
				HitWith(u.ids.sidebarSurface, i18n.T("侧栏列表"),
					ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true}).
				Children(func() {
					if u.section == sectionContacts {
						u.drawContacts(f)
					} else {
						u.drawConversationList(f)
					}
				})
			u.drawSidebarFooter(f)
		})
	})
	if connect.Clicked() {
		u.openConnect()
	}
	if create.Clicked() {
		u.openCompose("create", "")
	}
	if event, ok := canvas.PointerOn(u.ids.sidebarSurface); ok &&
		event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
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

func (u *UI) iconButton(f *flex.Frame, id ui.ID, icon ui.Icon, label string, size float32) flex.Box {
	return u.sizedIconButton(f, id, icon, label, size, 18)
}

func (u *UI) sizedIconButton(f *flex.Frame, id ui.ID, icon ui.Icon, label string, size, glyph float32) flex.Box {
	return f.Row().Size(size, size).Radius(8).Items(flex.Center).Justify(flex.Center).
		Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Hint: label}).
		Children(func() {
			f.Icon(icon).Size(glyph, glyph).Color(u.palette.icon)
		})
}

func (u *UI) drawContactFilters(f *flex.Frame) {
	peerCount, allOnline := 0, 0
	if u.state.Self.ID != "" {
		peerCount, allOnline = 1, 1
	}
	for _, peer := range u.state.Peers {
		if peer.ID == u.state.Self.ID || u.state.Settings.HideOfflineContacts && !peer.Online {
			continue
		}
		peerCount++
		if peer.Online {
			allOnline++
		}
	}
	labels := []struct {
		id    ui.ID
		label string
		value string
		count int
	}{
		{u.ids.filterAll, i18n.T("全部"), "all", peerCount},
		{u.ids.filterOnline, i18n.T("在线"), "online", allOnline},
	}
	f.Row().Height(32).Gap(8).Children(func() {
		for _, item := range labels {
			active := u.sidebarFilter == item.value
			background := u.palette.sidebar
			foreground := u.palette.secondaryText
			if active {
				background = u.palette.selected
				foreground = u.palette.selectedText
			}
			label := item.label + "  " + intString(item.count)
			box := f.Row().Height(30).PadX(12).Radius(15).Items(flex.Center).Gap(4).
				Background(background).Hover(flex.Look{}.Background(u.palette.hover)).
				HitWith(item.id, item.label, ui.HitOptions{Role: ui.RoleButton, Selected: active}).
				Children(func() {
					f.Text(label).FontSize(12).Weight(ui.WeightMedium).Color(foreground)
				})
			if box.Clicked() {
				u.sidebarFilter = item.value
			}
		}
	})
}

func (u *UI) drawSidebarFooter(f *flex.Frame) {
	f.Row().Height(30).Items(flex.Center).Children(func() {
		f.Spacer()
		f.Row().Items(flex.Center).Gap(5).Children(func() {
			f.Stack().Size(7, 7).Radius(4).Background(u.palette.success)
			label := i18n.T("局域网在线")
			if u.state.ListenAddress == "" {
				label = i18n.T("等待网络")
			}
			color := u.palette.secondaryText
			if u.state.ListenAddress == "" {
				color = u.palette.danger
			}
			f.Text(label).FontSize(10.5).Color(color)
		})
	})
}

func (u *UI) drawContacts(f *flex.Frame) {
	u.selfContactRow(f)
	u.drawInvitations(f)
	u.drawGroupChats(f)
	query := strings.TrimSpace(u.sidebarSearch.Text())
	otherPeers := 0
	for _, peer := range u.state.Peers {
		if peer.ID == u.state.Self.ID {
			continue
		}
		if u.state.Settings.HideOfflineContacts && !peer.Online {
			continue
		}
		otherPeers++
	}
	visible := 0
	for _, group := range u.contacts {
		peers := u.filteredPeers(group.Peers, query)
		if len(peers) == 0 && (query != "" || group.Name == "未分组") {
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
		f.Row().Height(8)
		u.emptyListMessage(f, i18n.T("还没有其他联系人"), i18n.T("联系人上线后会自动出现，也可手动连接。"))
		return
	}
	if visible == 0 && query != "" && !containsFold(u.selfDisplayName(), query) {
		u.emptyListMessage(f, i18n.T("没有匹配的联系人"), i18n.T("换一个昵称或分组名称试试。"))
		return
	}
}

func (u *UI) selfContactRow(f *flex.Frame) {
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
	row := f.Row().FillWidth().Height(58).PadSides(8, 9, 8, 9).Radius(9).Gap(10).
		Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(u.ids.selfRow, name+i18n.T("，个人资料"),
			ui.HitOptions{Role: ui.RoleButton, Hint: i18n.T("编辑个人资料")}).
		Children(func() {
			u.avatarStatus(f, name, u.selfAvatarPath(), 38, true)
			f.Column().Grow(1).Gap(3).Justify(flex.Center).Children(func() {
				f.Row().Items(flex.Center).Gap(6).Children(func() {
					f.Text(name).FontSize(14).Weight(ui.WeightMedium).Ellipsis()
					f.Stack().Size(6, 6).Radius(3).Background(u.palette.success)
				})
				f.Text(i18n.T("个人资料 · 本机")).FontSize(11).Muted().Ellipsis()
			})
		})
	if event, ok := f.Canvas().PointerOn(u.ids.selfRow); ok &&
		event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
		u.openSelfMenu(event.X, event.Y)
		f.Canvas().Consume()
	}
	if row.Clicked() {
		u.showContact(self.ID)
	}
	f.Row().Height(2)
}

func (u *UI) drawInvitations(f *flex.Frame) {
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
	f.Row().Height(8)
	if joinCount > 0 {
		f.Row().Height(24).Items(flex.Center).Children(func() {
			f.Text(i18n.T("群聊邀请")).FontSize(12).Weight(ui.WeightSemibold)
			f.Row().Width(6)
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
			f.Row().Height(4)
		}
		f.Row().Height(24).Items(flex.Center).Children(func() {
			f.Text(i18n.T("群主转让")).FontSize(12).Weight(ui.WeightSemibold)
			f.Row().Width(6)
			u.badgeText(f, intString(transferCount), false)
		})
		for _, invitation := range u.invitations {
			if invitationKind(invitation) == "ownership" {
				u.invitationCard(f, invitation, true)
			}
		}
	}
	f.Row().Height(12)
}

func (u *UI) invitationCard(f *flex.Frame, invitation domain.Invitation, ownership bool) {
	f.Column().FillWidth().Pad(11).Radius(10).Background(u.palette.invite).
		Border(1, u.palette.fileBorder).Gap(8).Children(func() {
		f.Row().Items(flex.Center).Gap(9).Children(func() {
			u.initialsAvatar(f, invitation.GroupName, 30)
			f.Column().Grow(1).Gap(2).Children(func() {
				name := invitation.GroupName
				if name == "" {
					name = i18n.T("群聊")
				}
				f.Text(name).FontSize(13).Weight(ui.WeightMedium).Ellipsis()
				from := invitation.FromName
				if from == "" {
					from = u.state.Peer(invitation.FromID).Name
				}
				description := from + i18n.T(" 邀请你加入")
				if ownership {
					description = from + i18n.T(" 提议将群主转让给你")
				}
				f.Text(description).FontSize(11).Muted().Ellipsis()
				if !ownership {
					f.Text(u.invitationMemberLine(invitation)).FontSize(10.5).Muted().MaxLines(2)
				}
			})
		})
		f.Row().Height(28).Justify(flex.End).Gap(8).Children(func() {
			reject := u.inviteRejectIDs[invitation.ID]
			accept := u.inviteAcceptIDs[invitation.ID]
			rejectLabel := i18n.T("拒绝")
			acceptLabel := i18n.T("加入")
			if ownership {
				rejectLabel = i18n.T("拒绝转让")
				acceptLabel = i18n.T("接受转让")
			}
			if f.Button(reject, rejectLabel).XSmall().Outline().Clicked() {
				u.submit(domain.Command{Kind: domain.RejectInvite, InvitationID: invitation.ID})
			}
			if f.Button(accept, acceptLabel).XSmall().Clicked() {
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

func (u *UI) contactGroupHeader(f *flex.Frame, group contactGroupView, collapsed bool) {
	id := u.groupHeadIDs[group.Name]
	icon := u.iconChevronDown
	if collapsed {
		icon = u.iconChevronRight
	}
	header := f.Row().Height(32).Items(flex.Center).Gap(6).Radius(7).
		Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(id, group.Name, ui.HitOptions{Role: ui.RoleButton, Expanded: !collapsed}).
		Children(func() {
			f.Icon(icon).Size(15, 15).Color(u.palette.secondaryText)
			f.Text(displayContactGroup(group.Name)).FontSize(13).Weight(ui.WeightSemibold).Ellipsis()
			f.Spacer()
			f.Text(intString(group.Online) + "/" + intString(group.Total)).FontSize(11).Muted()
		})
	if header.Clicked() {
		u.collapsed[group.Name] = !collapsed
	}
	if event, ok := f.Canvas().PointerOn(id); ok && group.Name != "未分组" &&
		event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
		u.openShellMenu(shellMenuContactGroup, group.Name, event.X, event.Y)
		f.Canvas().Consume()
	}
}

func (u *UI) contactRow(f *flex.Frame, peer domain.Peer) {
	id := u.peerRowIDs[peer.ID]
	selected := u.contactPeer == peer.ID
	background := ui.Color{}
	if selected {
		background = u.palette.selected
	}
	row := f.Row().FillWidth().Height(62).PadSides(8, 9, 8, 9).Radius(9).Gap(10).
		Background(background).Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(id, peer.Name, ui.HitOptions{Role: ui.RoleButton, Selected: selected}).
		Children(func() {
			u.avatarStatus(f, peer.Name, peer.Avatar, 38, peer.VisibleOnline())
			f.Column().Grow(1).Gap(3).Justify(flex.Center).Children(func() {
				f.Row().Items(flex.Center).Gap(6).Children(func() {
					f.Text(peer.Name).FontSize(14).Weight(ui.WeightMedium).Ellipsis()
				})
				subtitle := peerPresenceLabel(peer)
				if peer.Transport != domain.TransportGoIM {
					subtitle += " · " + contactClientLabel(peer)
				}
				if peer.Signature != "" {
					subtitle += " · " + peer.Signature
				} else if peer.VisibleOnline() && peer.Address != "" {
					subtitle += " · " + peer.Address
				}
				f.Text(subtitle).FontSize(11).Muted().Ellipsis()
			})
		})
	if event, ok := f.Canvas().PointerOn(id); ok &&
		event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
		u.openContactMenu(peer, event.X, event.Y)
		f.Canvas().Consume()
	}
	if row.Clicked() {
		u.showContact(peer.ID)
	}
}

func (u *UI) drawGroupChats(f *flex.Frame) {
	if len(u.groupChats) == 0 {
		return
	}
	f.Row().Height(6)
	f.Row().Height(32).Items(flex.Center).Gap(6).Children(func() {
		f.Icon(u.iconChevronDown).Size(15, 15).Color(u.palette.secondaryText)
		f.Text(i18n.T("我的群聊")).FontSize(13).Weight(ui.WeightSemibold)
		f.Spacer()
		f.Text(intString(len(u.groupChats))).FontSize(11).Muted()
	})
	for _, conversation := range u.groupChats {
		if q := strings.TrimSpace(u.sidebarSearch.Text()); q == "" || containsFold(u.conversationTitle(conversation), q) {
			u.contactGroupRow(f, conversation)
		}
	}
	f.Row().Height(8)
}

func (u *UI) drawConversationList(f *flex.Frame) {
	u.drawInvitations(f)
	query := strings.TrimSpace(u.sidebarSearch.Text())
	shown := 0
	for _, conversation := range u.conversations {
		title := u.conversationTitle(conversation)
		if query != "" && !containsFold(title, query) && !containsFold(conversation.Preview, query) {
			continue
		}
		shown++
		u.conversationRow(f, conversation, 74)
	}
	if len(u.conversations) == 0 {
		u.emptySidebar(f, i18n.T("还没有会话"), i18n.T("发现联系人后开始聊天，或手动连接一个已知地址。"), true)
		return
	}
	if shown == 0 {
		u.emptyListMessage(f, i18n.T("没有匹配的会话"), i18n.T("换一个昵称或消息内容试试。"))
	}
}

func (u *UI) conversationRow(f *flex.Frame, conversation domain.Conversation, height float32) {
	id := u.convRowIDs[conversation.ID]
	selected := conversation.ID == u.selected
	background := ui.Color{}
	if selected {
		background = u.palette.selected
	}
	title := u.conversationTitle(conversation)
	row := f.Row().FillWidth().Height(height).PadSides(9, 9, 9, 9).Radius(9).Gap(10).
		Background(background).Hover(flex.Look{}.Background(u.palette.hover)).
		HitWith(id, title, ui.HitOptions{Role: ui.RoleButton, Selected: selected}).
		Children(func() {
			online := false
			avatar := ""
			avatarName := title
			if conversation.Kind == "group" {
				_, onlineCount := u.groupCounts(u.state.Group(conversation.GroupID))
				online = onlineCount > 0
			} else {
				peer := u.state.Peer(conversation.PeerID)
				online = peer.ID != "" && peer.Online
				avatar = peer.Avatar
				if peer.Name != "" {
					avatarName = peer.Name
				}
			}
			u.avatarStatus(f, avatarName, avatar, 42, online)
			f.Column().Grow(1).Gap(4).Justify(flex.Center).Children(func() {
				f.Row().Items(flex.Center).Gap(5).Children(func() {
					f.Text(title).FontSize(14).Weight(ui.WeightMedium).Ellipsis()
					if conversation.Pinned {
						f.Icon(u.iconPin).Size(12, 12).Color(u.palette.secondaryText)
					}
					if conversation.Muted {
						f.Icon(u.iconBellOff).Size(12, 12).Color(u.palette.secondaryText)
					}
					f.Spacer()
					if !conversation.UpdatedAt.IsZero() {
						f.Text(shortTime(conversation.UpdatedAt)).FontSize(10).Muted()
					}
				})
				f.Row().Items(flex.Center).Gap(6).Children(func() {
					preview := conversation.Preview
					if preview == "" {
						preview = i18n.T("还没有消息")
					}
					f.Text(preview).FontSize(11.5).Muted().Ellipsis()
					f.Spacer()
					if conversation.Unread > 0 {
						u.badgeText(f, intString(conversation.Unread), true)
					}
				})
			})
		})
	if event, ok := f.Canvas().PointerOn(id); ok &&
		event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
		u.openConversationMenu(conversation, event.X, event.Y)
		f.Canvas().Consume()
	}
	if row.Clicked() {
		u.selectConversation(conversation.ID)
	}
}

func (u *UI) emptySidebar(f *flex.Frame, title, description string, actions bool) {
	f.Column().Grow(1).PadSides(18, 10, 18, 10).Items(flex.Center).Justify(flex.Center).Gap(8).Children(func() {
		f.Row().Size(46, 46).Radius(13).Background(u.palette.mutedAvatar).Items(flex.Center).Justify(flex.Center).
			Children(func() {
				f.Icon(u.iconUsers).Size(22, 22).Color(u.palette.secondaryText)
			})
		f.Text(title).FontSize(15).Weight(ui.WeightSemibold).TextCenter()
		f.Paragraph(description, 3).FontSize(12).Muted().TextCenter().LineHeight(18)
		if actions {
			f.Row().Gap(8).Justify(flex.Center).Children(func() {
				if f.Button(u.ids.emptyConnect, i18n.T("手动连接")).Small().Outline().Clicked() {
					u.openConnect()
				}
				if f.Button(u.ids.emptyCompose, i18n.T("创建群聊")).Small().Clicked() {
					u.openCompose("create", "")
				}
			})
		}
	})
}

func (u *UI) emptyListMessage(f *flex.Frame, title, body string) {
	f.Column().PadSides(24, 22, 18, 22).Gap(5).Children(func() {
		f.Text(title).FontSize(13).Weight(ui.WeightMedium).TextCenter()
		f.Text(body).FontSize(11).Muted().TextCenter()
	})
}

func (u *UI) initialsAvatar(f *flex.Frame, name string, size float32) {
	initial := initialOf(name)
	f.Row().Size(size, size).Radius(size / 2).Background(u.palette.mutedAvatar).
		Items(flex.Center).Justify(flex.Center).
		Children(func() {
			f.Text(initial).FontSize(max(11, size*0.36)).Weight(ui.WeightSemibold).
				Color(u.palette.avatarText).TextCenter()
		})
}

func (u *UI) badgeText(f *flex.Frame, label string, primary bool) {
	background := u.palette.selected
	foreground := u.palette.selectedText
	if primary {
		background = u.theme.Primary
		foreground = u.theme.PrimaryForeground
	}
	width := max(21, float32(len([]rune(label)))*7+12)
	f.Stack().Width(width).Height(20).Radius(10).Background(background).Children(func() {
		f.Text(label).FontSize(10.5).Weight(ui.WeightSemibold).Color(foreground).TextCenter()
	})
}

func (u *UI) conversationTitle(conversation domain.Conversation) string {
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

func (u *UI) rebuildPresentation(oldError string) {
	u.ensureStateIDs()
	u.theme = themeForName(u.state.Settings.Theme)
	u.palette = paletteFor(u.theme)

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

	groups := make(map[string][]domain.Peer)
	for _, name := range u.state.ContactGroups {
		groups[name] = nil
	}
	for _, peer := range u.state.Peers {
		if peer.ID == u.state.Self.ID || u.state.Settings.HideOfflineContacts && !peer.Online {
			continue
		}
		name := strings.TrimSpace(peer.ContactGroup)
		if name == "" {
			name = "未分组"
		}
		groups[name] = append(groups[name], peer)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if (names[i] == "未分组") != (names[j] == "未分组") {
			return names[j] == "未分组"
		}
		return names[i] < names[j]
	})
	u.contacts = u.contacts[:0]
	for _, name := range names {
		peers := groups[name]
		sort.SliceStable(peers, func(i, j int) bool {
			if peers[i].Online != peers[j].Online {
				return peers[i].Online
			}
			return peers[i].Name < peers[j].Name
		})
		online := 0
		for _, peer := range peers {
			if peer.Online {
				online++
			}
		}
		u.contacts = append(u.contacts, contactGroupView{
			Name: name, Total: len(peers), Online: online, Peers: peers,
		})
	}

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

func (u *UI) initialsAvatarStatus(f *flex.Frame, name string, size float32, online bool) {
	f.Row().Size(size, size).Radius(size / 2).Background(u.palette.mutedAvatar).
		Items(flex.Center).Justify(flex.Center).
		Children(func() {
			f.Text(initialOf(name)).FontSize(max(11, size*0.36)).Weight(ui.WeightSemibold).
				Color(u.palette.avatarText).TextCenter()
			dot := max(8, size*0.22)
			color := u.palette.secondaryText
			if online {
				color = u.palette.success
			}
			f.Stack().Size(dot, dot).Absolute().Right(0).Bottom(0).Radius(dot/2).
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
	if target == "" && len(u.conversations) > 0 {
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
	for _, m := range u.messages {
		u.ensureID(u.messageIDs, u.ids.chat, "message:", m.ID)
		u.ensureID(u.messageAvatarIDs, u.ids.chat, "message-avatar:", m.ID)
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
				u.localNotice = i18n.T("群聊已创建；部分邀请发送失败：") + u.state.Error
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
	u.closeShellMenu()
	if id == u.selected {
		u.pendingPeer, u.pendingGroup = "", ""
		u.markSelectedConversationRead()
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
	u.markSelectedConversationRead()
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

func (u *UI) setSection(section string) {
	if section == u.section {
		return
	}
	if u.compose.open {
		u.requestCloseComposeThen(func() { u.setSection(section) })
		return
	}
	if u.connectOpen || u.confirm.open || u.profile.open || u.memberProfile != "" || u.organize.open {
		return
	}
	if !u.guardGroupRenameNavigation(func() { u.setSection(section) }) {
		return
	}
	if !u.isPanelSection() {
		u.saveCurrentDraft()
	}
	u.section = section
	if section != sectionMessages {
		u.pendingPeer, u.pendingGroup = "", ""
	}
	if section == sectionSettings {
		u.panels.refreshNetworks()
	}
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
	u.theme = themeForName(next)
	u.palette = paletteFor(u.theme)
	u.submit(domain.Command{Kind: domain.SetSetting, Setting: "theme", Value: next})
}

func (u *UI) openConnect() {
	u.closeShellMenu()
	u.connectOpen = true
	u.connectRoute, u.connectProtocol = 0, flex.SelectState{}
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
	u.syncFilesWindow()
}

func (u *UI) submitCompose() {
	if !u.compose.open || u.compose.pending {
		return
	}
	ids := u.selectedPeersForCompose()
	if u.compose.mode == "create" {
		if len(ids) < 2 {
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
			if u.compose.selected[peer.ID] && peer.Online {
				ids = append(ids, peer.ID)
			}
		}
	}
	return ids
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
	out := make([]domain.Peer, 0, len(peers))
	for _, peer := range peers {
		if u.sidebarFilter == "online" && !peer.Online {
			continue
		}
		if query != "" && !containsFold(peer.Name, query) && !containsFold(peer.ContactGroup, query) {
			continue
		}
		out = append(out, peer)
	}
	return out
}

func (u *UI) drawOverlays(canvas *ui.Canvas, viewport ui.Rect) {
	if u.moreOpen && !u.isPanelSection() {
		u.drawChatMenu(canvas)
	}
	if !u.isPanelSection() {
		u.drawMentionPicker(canvas, viewport)
		u.drawEmojiPicker(canvas, viewport)
	}
	if u.drawer != "" && !u.isPanelSection() {
		u.drawSideDrawer(canvas)
	}
	u.drawShellMenu(canvas, viewport)
	if u.memberProfile != "" && !u.isPanelSection() {
		u.drawMemberProfile(canvas, viewport)
	}
	if u.profile.open {
		u.drawProfileDialog(canvas, viewport)
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
}

func (u *UI) textInput(context app.TextInputContext) *app.TextInput {
	if u.lockVisible() {
		return u.lockTextInput(context)
	}
	if u.shellMenu.open {
		return nil
	}
	if context.ID == u.ids.chatEditor {
		return u.chatTextInput()
	}
	if input := u.chatSearchTextInput(context); input != nil {
		return input
	}
	if input := flex.Fields(&u.sidebarSearch, &u.connectField, &u.compose.name, &u.compose.search,
		&u.profile.nickname, &u.profile.signature, &u.organize.field, &u.memberSearch, &u.renameField,
		&u.nicknameField, &u.fileSearch)(context); input != nil {
		return input
	}
	if u.profile.open {
		details := &u.profile.details
		if input := flex.Fields(&details[0], &details[1], &details[2], &details[3], &details[4])(context); input != nil {
			return input
		}
	}
	if u.panelInput != nil {
		return u.panelInput(context)
	}
	return nil
}

func (u *UI) onScroll(event ui.ScrollEvent) ui.EventResult {
	if !u.lockVisible() && u.emojiOpen && pointInside(event.X, event.Y, u.emojiPanel) {
		result := u.emojiView.Wheel(event)
		result.Handled = true
		return result
	}
	if u.lockVisible() {
		return ui.EventResult{Handled: true}
	}
	if result, handled := u.chatOverlayWheel(event); handled {
		return result
	}
	if u.shellMenu.open {
		u.closeShellMenu()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if u.modalOpen() {
		if result := u.dialogView.Wheel(event); result.Handled {
			return result
		}
	}
	if u.drawer != "" && pointInside(event.X, event.Y, u.lastChat) {
		return u.drawerView.Wheel(event)
	}
	if u.isPanelSection() {
		return u.panels.Wheel(event)
	}
	if event.X < u.lastChat.X {
		return u.sidebarView.Wheel(event)
	}
	result := u.chatView.Wheel(event)
	if result.Handled {
		u.markSelectedConversationRead()
	}
	return result
}

func (u *UI) onKey(event ui.KeyEvent) ui.EventResult {
	if u.lockVisible() {
		// The only registered hits while locked belong to the lock screen.
		// Let Viva deliver text keys, focus traversal and Enter to those hits.
		return u.lockKey(event)
	}
	if result, handled := u.chatOverlayKey(event); handled {
		return result
	}
	if u.mention.open && u.focusedID == u.ids.chatEditor && !event.Release &&
		event.Modifiers == 0 && u.chatEditor != nil {
		if _, composing := u.chatEditor.Composition(); !composing {
			switch event.Key {
			case ui.KeyUp:
				u.moveMention(-1)
				return ui.EventResult{Handled: true, Repaint: true}
			case ui.KeyDown:
				u.moveMention(1)
				return ui.EventResult{Handled: true, Repaint: true}
			case ui.KeyEnter:
				if u.insertMention() {
					return ui.EventResult{Handled: true, Repaint: true}
				}
			}
		}
	}
	if event.Key == ui.KeyEscape {
		switch {
		case u.confirm.open:
			u.confirm.open = false
			return ui.EventResult{Handled: true}
		case u.shellMenu.open:
			u.closeShellMenu()
			return ui.EventResult{Handled: true, Repaint: true}
		case u.organize.open:
			u.closeOrganize()
			return ui.EventResult{Handled: true, Repaint: true}
		case u.profile.open:
			u.closeProfile()
			return ui.EventResult{Handled: true, Repaint: true}
		case u.emojiOpen:
			u.emojiOpen = false
			return ui.EventResult{Handled: true, Repaint: true}
		case u.mention.open:
			u.closeMention()
			return ui.EventResult{Handled: true, Repaint: true}
		case u.compose.open:
			u.requestCloseCompose()
			return ui.EventResult{Handled: true}
		case u.connectOpen:
			u.connectOpen = false
			return ui.EventResult{Handled: true}
		case u.memberProfile != "":
			u.memberProfile = ""
			return ui.EventResult{Handled: true}
		case u.memberMenu != "":
			u.memberMenu = ""
			return ui.EventResult{Handled: true}
		case u.activeFileID != "" && u.fileReturn == "list":
			u.activeFileID = ""
			return ui.EventResult{Handled: true}
		case u.drawer != "":
			u.requestCloseDrawer()
			return ui.EventResult{Handled: true}
		case u.moreOpen:
			u.moreOpen = false
			return ui.EventResult{Handled: true}
		case u.chatFind.open:
			u.closeChatSearch()
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if u.shellMenu.open {
		return ui.EventResult{Handled: true}
	}
	if u.isPanelSection() {
		return u.panels.Key(event)
	}
	return ui.EventResult{}
}

func (u *UI) onShortcut(event ui.KeyEvent) ui.EventResult {
	if u.lockVisible() || (u.section == sectionSettings && u.panels.settingsTab == panelSettingsSecurity &&
		u.panels.settingsExtra.isPasswordField(event.Target)) {
		// Viva offers plain printable keys here before native text input.
		// Skip application shortcuts without consuming the editor's keys.
		return ui.EventResult{}
	}
	if result, handled := u.chatOverlayShortcut(event); handled {
		return result
	}
	if u.modalOpen() {
		return ui.EventResult{}
	}
	if u.isPanelSection() {
		if result := u.panels.Shortcut(event); result.Handled {
			return result
		}
	}
	control := event.Modifiers&ui.ModifierControl != 0 || event.Modifiers&ui.ModifierMeta != 0
	if event.Key == ui.KeyEnter && control && u.focusedID == u.ids.chatEditor && u.selected != "" {
		if u.chatEditor == nil {
			return ui.EventResult{}
		}
		if _, composing := u.chatEditor.Composition(); composing {
			return ui.EventResult{Handled: true}
		}
		if u.state.Settings.SendKey == "Ctrl+Enter" {
			u.sendChatText()
			return ui.EventResult{Handled: true}
		}
		u.chatEditor.NewLine()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if u.shortcutTriggered(shortcutActionOpenSettings, "Ctrl+,", event) {
		u.setSection(sectionSettings)
		return ui.EventResult{Handled: true}
	}
	if u.shortcutTriggered(shortcutActionSearchInConversation, "Ctrl+F", event) {
		if !u.isPanelSection() && u.selected != "" {
			u.openChatSearch()
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if u.shortcutTriggered(shortcutActionQuickFind, "Ctrl+K", event) {
		if u.isPanelSection() {
			u.setSection(sectionMessages)
		}
		u.chatFind.focus = false
		u.focusSearch = true
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if u.shortcutTriggered(shortcutActionCreateGroup, "Ctrl+Shift+N", event) {
		if u.isPanelSection() {
			u.setSection(sectionMessages)
		}
		u.openCompose("create", "")
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (u *UI) onFileDrop(event ui.FileDropEvent) ui.EventResult {
	if u.lockVisible() {
		u.dragging = false
		return ui.EventResult{}
	}
	switch event.Phase {
	case ui.FileDropOver:
		if u.chatDropReady() && pointInside(event.X, event.Y, u.lastChat) {
			u.dragging = true
			return ui.EventResult{Handled: true, Repaint: true}
		}
	case ui.FileDropLeave:
		if u.dragging {
			u.dragging = false
			return ui.EventResult{Handled: true, Repaint: true}
		}
	case ui.FileDropDrop:
		u.dragging = false
		if u.chatDropReady() && len(event.Paths) > 0 && pointInside(event.X, event.Y, u.lastChat) {
			u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: u.selected, Paths: append([]string(nil), event.Paths...)})
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	return ui.EventResult{}
}

func (u *UI) chatDropReady() bool {
	return u.selected != "" && !u.isPanelSection() && u.drawer == "" &&
		!u.modalOpen() && !u.shellMenu.open &&
		u.chatReadOnlyReason(u.state.Conversation(u.selected)) == ""
}

func (u *UI) ensureStateIDs() {
	u.ensurePeerID(u.state.Self.ID)
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

func (u *UI) ensureID(cache map[string]ui.ID, scope ui.Scope, prefix, key string) ui.ID {
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
	if u.profile.open {
		u.markAvatarImage(u.profile.avatar)
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
	if viewer := &u.chatSurface().viewer; viewer.open {
		u.markImage(u.fileLocalPath[viewer.fileID])
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
	generation, queued := u.mediaImageQueued[path]
	if queued {
		// The old stat can return unchanged without waking the UI. Keep one
		// coalesced probe at the same generation for the newer wake, but do
		// not duplicate the first decode of an image we haven't loaded yet.
		if !previousSet {
			return
		}
	} else {
		u.mediaImageGen++
		generation = u.mediaImageGen
		u.mediaImageQueued[path] = generation
	}
	limit := 512
	if u.avatarImagePaths[path] && path != u.state.Settings.Background {
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

func (u *UI) avatarBitmap(path string) ui.Bitmap {
	if path == "" {
		return ui.Bitmap{}
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
	return ui.Bitmap{}
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

func (u *UI) avatarUploadBitmap(path string) (ui.Bitmap, bool) {
	full, ok := u.imageCache[path]
	if !ok {
		return ui.Bitmap{}, false
	}
	return squareBitmap(full)
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

func squareBitmap(bitmap ui.Bitmap) (ui.Bitmap, bool) {
	if bitmap.Width <= 0 || bitmap.Height <= 0 || bitmap.Stride < bitmap.Width*4 ||
		len(bitmap.Pix) < bitmap.Stride*bitmap.Height {
		return ui.Bitmap{}, false
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
	return ui.Bitmap{
		Pix: pixels, Width: side, Height: side, Stride: stride,
		ID: bitmap.ID, Version: bitmap.Version,
	}, true
}

// circularBitmap applies a one-pixel-antialiased circular alpha mask without
// touching the source. The image is already a square center crop, so painting
// it whole yields a round avatar with one draw instead of per-row strips.
func circularBitmap(bitmap ui.Bitmap) (ui.Bitmap, bool) {
	side := bitmap.Width
	if side <= 0 || bitmap.Height != side || bitmap.Stride != side*4 ||
		len(bitmap.Pix) < bitmap.Stride*side {
		return ui.Bitmap{}, false
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
	return ui.Bitmap{
		Pix: pixels, Width: side, Height: side, Stride: bitmap.Stride,
		ID: bitmap.ID, Version: bitmap.Version,
	}, true
}

func (u *UI) fileBitmap(path string) ui.Bitmap {
	if path == "" {
		return ui.Bitmap{}
	}
	return u.imageCache[path]
}

func bitmapReady(bitmap ui.Bitmap) bool {
	if bitmap.Width <= 0 || bitmap.Height <= 0 || bitmap.Stride < bitmap.Width*4 ||
		len(bitmap.Pix) < bitmap.Stride*bitmap.Height {
		return false
	}
	return bitmap.Width != 1 || bitmap.Height != 1 || bitmap.Stride != 4 ||
		len(bitmap.Pix) != 4 || bitmap.Version != 0
}

func (u *UI) pendingBitmap(path string) ui.Bitmap {
	if bitmap, ok := u.mediaPendingBitmaps[path]; ok {
		return bitmap
	}
	u.nextImageID++
	bitmap := ui.Bitmap{
		Pix: make([]byte, 4), Width: 1, Height: 1, Stride: 4,
		ID: u.nextImageID,
	}
	u.mediaPendingBitmaps[path] = bitmap
	return bitmap
}

func (u *UI) isDark() bool { return u.theme.Background.Red < 0.5 }

func themeForName(name string) ui.Theme {
	if strings.EqualFold(name, "dark") {
		return ui.DarkTheme()
	}
	return ui.LightTheme()
}

func paletteFor(theme ui.Theme) palette {
	dark := theme.Background.Red < 0.5
	if dark {
		return palette{
			rail:          ui.RGB(15, 17, 21),
			sidebar:       ui.RGB(18, 20, 25),
			chat:          ui.RGB(11, 13, 17),
			header:        ui.RGB(14, 16, 20),
			border:        ui.RGBA(255, 255, 255, 0.09),
			hover:         ui.RGB(27, 30, 37),
			selected:      ui.RGB(25, 47, 88),
			selectedText:  ui.RGB(152, 195, 255),
			icon:          ui.RGB(205, 210, 220),
			secondaryText: ui.RGB(145, 151, 164),
			mutedAvatar:   ui.RGB(39, 44, 54),
			avatarText:    ui.RGB(225, 231, 241),
			incoming:      ui.RGB(32, 35, 42),
			own:           ui.RGB(23, 49, 95),
			bubbleBorder:  ui.RGBA(255, 255, 255, 0.06),
			fileCard:      ui.RGB(19, 31, 55),
			fileBorder:    ui.RGBA(98, 155, 255, 0.20),
			invite:        ui.RGB(27, 30, 38),
			success:       ui.RGB(55, 197, 118),
			danger:        ui.RGB(255, 112, 116),
		}
	}
	return palette{
		rail:          ui.RGB(253, 253, 255),
		sidebar:       ui.RGB(251, 252, 254),
		chat:          ui.RGB(249, 251, 254),
		header:        ui.RGB(251, 252, 254),
		border:        ui.RGB(231, 235, 241),
		hover:         ui.RGB(246, 248, 252),
		selected:      ui.RGB(229, 239, 255),
		selectedText:  ui.RGB(31, 80, 191),
		icon:          ui.RGB(78, 86, 100),
		secondaryText: ui.RGB(117, 125, 139),
		mutedAvatar:   ui.RGB(233, 238, 246),
		avatarText:    ui.RGB(60, 72, 92),
		incoming:      ui.RGB(244, 247, 251),
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

func decodeBitmap(path string, limit int) (ui.Bitmap, bool) {
	file, err := os.Open(path)
	if err != nil {
		return ui.Bitmap{}, false
	}
	defer file.Close()
	info, err := file.Stat()
	maxBytes := maxImageFileBytes
	if limit <= 192 {
		maxBytes = 8 << 20
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBytes {
		return ui.Bitmap{}, false
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		return ui.Bitmap{}, false
	}
	maxPixels := maxImageDecodePixels
	if format == "gif" || (format == "png" && limit <= 192) {
		maxPixels = min(maxPixels, 8_000_000)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageDimension || config.Height > maxImageDimension ||
		int64(config.Width)*int64(config.Height) > maxPixels {
		return ui.Bitmap{}, false
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ui.Bitmap{}, false
	}
	source, _, err := image.Decode(file)
	if err != nil {
		return ui.Bitmap{}, false
	}
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return ui.Bitmap{}, false
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
		return ui.Bitmap{Pix: rgba.Pix, Width: width, Height: height, Stride: rgba.Stride}, true
	}
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		sourceY := bounds.Min.Y + y*bounds.Dy()/height
		for x := 0; x < width; x++ {
			sourceX := bounds.Min.X + x*bounds.Dx()/width
			red, green, blue, alpha := source.At(sourceX, sourceY).RGBA()
			offset := (y*width + x) * 4
			pixels[offset+0] = uint8(blue >> 8)
			pixels[offset+1] = uint8(green >> 8)
			pixels[offset+2] = uint8(red >> 8)
			pixels[offset+3] = uint8(alpha >> 8)
		}
	}
	return ui.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4}, true
}

func initialOf(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return "?"
	}
	last := ui.GraphemeStart(name, len(name)-1)
	if last <= 0 {
		return name
	}
	previous := ui.GraphemeStart(name, last-1)
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

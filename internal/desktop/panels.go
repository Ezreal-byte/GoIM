package desktop

import (
	"fmt"
	"github.com/vivaui/viva/gfx"
	"goim/internal/i18n"
	"net"

	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	panelModuleSettings  = "settings"
	panelModuleTransfers = "transfers"
	panelModuleShares    = "shares"
	panelModulePrint     = "print"

	panelSettingsWidth = 300
)

type panelSettingsTab uint8

const (
	panelSettingsGeneral panelSettingsTab = iota
	panelSettingsShortcuts
	panelSettingsAbout
	panelSettingsProfile
	panelSettingsNotifications
	panelSettingsVoice
	panelSettingsNetwork
	panelSettingsTransfers
	panelSettingsSecurity
	panelSettingsCount
)

type transferFilter uint8

const (
	panelTransferAll transferFilter = iota
	panelTransferIncoming
	panelTransferPending
	panelTransferFinished
	panelTransferReceived
)

type shareFilter uint8

const (
	panelSharesRemote shareFilter = iota
	panelSharesMine
)

var transferFilters = [...]transferFilter{
	panelTransferAll,
	panelTransferReceived,
	panelTransferIncoming,
	panelTransferPending,
	panelTransferFinished,
}
var shareFilters = [...]shareFilter{panelSharesRemote, panelSharesMine}

var (
	panelSettingsLabels = [...]string{"常规", "快捷键", "关于", "个人资料", "通知", "通话", "网络", "文件传输", "安全"}
	panelSettingsOrder  = [...]panelSettingsTab{panelSettingsProfile, panelSettingsGeneral, panelSettingsNotifications,
		panelSettingsVoice, panelSettingsNetwork, panelSettingsTransfers, panelSettingsShortcuts, panelSettingsSecurity, panelSettingsAbout}
)

type panelIDs struct {
	settingsFeature    gfx.Scope
	settingsCategories gfx.Scope

	settingsNotification  gfx.ID
	settingsSound         gfx.ID
	settingsAutoAccept    gfx.ID
	settingsCloseToTray   gfx.ID
	settingsShowGitHub    gfx.ID
	settingsSearch        gfx.ID
	settingsLaunchAtLogin gfx.ID
	settingsChooseDir     gfx.ID
	settingsOpenDir       gfx.ID
	settingsRefreshNet    gfx.ID
	settingsRefreshVoice  gfx.ID
	settingsDismissLocal  gfx.ID
	settingsClearError    gfx.ID

	networkDefer  gfx.ID
	networkCancel gfx.ID

	shortcutRecord        gfx.Scope
	shortcutSave          gfx.Scope
	shortcutCancel        gfx.Scope
	shortcutReset         gfx.ID
	shortcutConfirmReset  gfx.ID
	shortcutCancelReset   gfx.ID
	shortcutDialog        gfx.ID
	shortcutDialogScope   gfx.Scope
	shortcutDialogDismiss gfx.ID
	shortcutDialogClose   gfx.ID
	shortcutDialogReset   gfx.Scope
	shortcutDialogClear   gfx.Scope
	shortcutDialogSave    gfx.Scope
	shortcutDialogCancel  gfx.Scope

	aboutCopyVersion     gfx.ID
	aboutLicense         gfx.ID
	aboutBack            gfx.ID
	aboutCopyLicense     gfx.ID
	aboutDiagnostics     gfx.ID
	aboutCopyDiagnostics gfx.ID
	aboutResetData       gfx.ID
	aboutResetConfirm    gfx.ID
	aboutResetCancel     gfx.ID
	aboutResetScope      gfx.ID
	aboutResetClose      gfx.ID
	aboutResetDismiss    gfx.ID
	aboutAutoUpdate      gfx.ID
	aboutCards           gfx.Scope

	transferActions     gfx.Scope
	transferDirectory   gfx.ID
	transferSearch      gfx.ID
	shareSearch         gfx.ID
	shareLocation       gfx.ID
	transferMenuScope   gfx.ID
	transferMenuDismiss gfx.ID
	transferMenuCopy    gfx.ID
	transfersDismiss    gfx.ID
	transfersClearError gfx.ID

	sharesFeature                        gfx.Scope
	sharesRefresh                        gfx.ID
	sharesAdd                            gfx.ID
	sharePickFiles, sharePickFolder      gfx.ID
	deviceShareMenu, shareMenuEnableLink gfx.ID
	shareToastScope, shareToastDismiss   gfx.ID
	deviceShareApproval                  gfx.ID
	deviceShareStart                     gfx.ID
	deviceShareCopy                      gfx.ID
	deviceShareQR                        gfx.ID
	deviceShareInbox                     gfx.ID
	deviceShareStop                      gfx.ID
	shareCopyLink, shareDownload         gfx.Scope
	shareMore                            gfx.Scope
	shareRow                             gfx.Scope

	shareMenuScope                               gfx.ID
	shareMenuDismiss                             gfx.ID
	shareMenuOpen                                gfx.ID
	shareMenuFolder                              gfx.ID
	shareMenuGet                                 gfx.ID
	shareMenuVisit                               gfx.ID
	shareMenuCopy                                gfx.ID
	shareMenuQR                                  gfx.ID
	shareMenuPassword                            gfx.ID
	shareMenuClearPass                           gfx.ID
	shareMenuReplaceFile, shareMenuReplaceFolder gfx.ID
	shareMenuUnshare                             gfx.ID

	shareDialogScope    gfx.ID
	shareDialogDismiss  gfx.ID
	shareDialogClose    gfx.ID
	shareDialogPassword gfx.ID
	shareDialogCancel   gfx.ID
	shareDialogSubmit   gfx.ID

	sharePasswordScope   gfx.ID
	sharePasswordDismiss gfx.ID
	sharePasswordClose   gfx.ID
	sharePasswordField   gfx.ID
	sharePasswordCancel  gfx.ID
	sharePasswordSubmit  gfx.ID

	shareQRScope         gfx.ID
	shareQRDismiss       gfx.ID
	shareQRClose         gfx.ID
	shareQRDismissButton gfx.ID
	shareQRCopy          gfx.ID
}

type panelNetworkOption struct {
	value string
	label string
}

func (option panelNetworkOption) title() string { return option.label }

type panelNetworkChange struct {
	value  string
	label  string
	active int
}

type panelLocalFile struct {
	path      string
	available bool
	known     bool
}

type panelLocalAction struct {
	id     uint64
	path   string
	reveal bool
}

type panelTransferEntry struct {
	icon                                              *gfx.Icon
	transfer                                          domain.Transfer
	ids                                               transferActionIDs
	name, peer, source, status, meta, err, path, size string
	directory                                         bool
	progress, percent                                 string
	fraction                                          float32
	updated                                           time.Time

	accept  bool
	saveAs  bool
	reject  bool
	cancel  bool
	retry   bool
	open    bool
	preview bool
	reveal  bool
}

type panelShareEntry struct {
	icon                                          *gfx.Icon
	online                                        bool
	file                                          domain.File
	rowID, copyID, downloadID, moreID             gfx.ID
	title, label, kind, date, access, compactMeta string
	size                                          string
	sender                                        string
	meta                                          string
	state                                         string
	own                                           bool
	path                                          string
	available                                     bool
}

type panelTableColumns struct {
	name, peer, source, size, status, access, date, actions float32
}

type panelPendingKind uint8

const (
	panelPendingValue panelPendingKind = iota
	panelPendingBool
	panelPendingShortcuts
	panelPendingResetShortcuts
)

type panelPendingSetting struct {
	kind     panelPendingKind
	setting  string
	value    string
	boolean  bool
	shortcut []domain.Shortcut
	revision uint64
}

// Panels owns the settings, transfers, and file-sharing surfaces. Init must be
// called with the application's tree before Draw.
type Panels struct {
	firewall       firewallPanel
	memberProject  *memberProjectView
	dialogViewport ui.Rect
	tree           *gfx.Tree
	page           *ui.Page
	theme          ui.Theme
	ids            panelIDs

	module string

	search         settingsSearch
	settingsTab    panelSettingsTab
	settingsScroll [panelSettingsCount]float32
	settingsExtra  panelSettingsExtra
	openUpdates    func(bool)
	update         updateView
	settingsWidth  float32
	settingsNarrow bool
	scrollTab      panelSettingsTab
	scrollTabSet   bool
	aboutLicense   bool
	resetShortcuts bool
	shortcutEdit   int
	shortcutKeys   string
	shortcutError  string

	transferSort       panelTableSort
	shareSort          panelTableSort
	transferFilter     transferFilter
	transferMenu       string
	transferAnchor     ui.Box
	transferMenuEntry  *panelTransferEntry
	previewTransfer    func(domain.File)
	transferPickerOpen bool
	transferSearch     ui.Field
	transferRows       []panelTransferEntry
	shareFilter        shareFilter
	shareAnchor        ui.Box
	shareChecked       map[string]bool
	shareBatch         []domain.ShareDownload
	shareUnlock        []domain.File
	showIncoming       func()
	shareSelected      string
	sharePickerOpen    bool
	shareSearch        ui.Field
	shareToast         shareToastState
	lastShareFeedback  uint64

	networks         []panelNetworkOption
	networkChoices   []panelNetworkOption
	networkFor       string
	networkLanguage  string
	refreshNetwork   bool
	networkReadError string
	listNetworks     func() ([]net.Interface, error)
	networkChange    panelNetworkChange

	localFiles           map[string]panelLocalFile
	mediaSubmit          func(mediaJob)
	toast                func(title, description string, tone components.ToastType)
	invalidate           func()
	fontFamilies         func() []string
	setInterfaceFont     func(string) bool
	diagnosticDataDir    func() string
	diagnosticPickerOpen bool
	resetDataOpen        bool

	localActions    map[uint64]panelLocalAction
	nextLocalAction uint64

	transferIncoming      []panelTransferEntry
	transferPending       []panelTransferEntry
	transferFinished      []panelTransferEntry
	transferReceived      []panelTransferEntry
	transferLabels        [5]string
	transferIncomingLabel string
	sharesAll             []panelShareEntry
	sharesMine            []panelShareEntry
	sharesRemote          []panelShareEntry
	shareLabels           [2]string

	shareQRWorker shareQRWorker
	shareMenu     shareMenuState
	shareDialog   shareSendDialogState
	sharePassword sharePasswordDialogState
	shareQR       shareQRDialogState
	shareQRCache  map[shareQRKey]shareQRResult

	print        printPanel
	printAvatar  func(f ui.C, peerID, name, path string, size float32, online bool)
	presentPrint func(shareID string) bool

	listRevision uint64
	listLanguage string
	listDirty    bool

	pending    *panelPendingSetting
	saveFailed bool
	localError string
	build      panelBuildInfo
}

func (p *Panels) Init(tree *gfx.Tree, wake *app.Invalidator) {
	if tree == nil {
		return
	}
	if p.tree == tree {
		return
	}
	p.tree = tree
	p.shareSort = panelTableSort{column: "分享时间", descending: true}
	p.page = ui.NewPage(ui.PageConfig{Root: "panels", Tree: tree, Theme: &p.theme, Invalidator: wake})
	p.invalidate = wake.Invalidate
	p.shortcutEdit = -1
	p.localActions = make(map[uint64]panelLocalAction)
	p.shareQRCache = make(map[shareQRKey]shareQRResult)
	p.fontFamilies = app.FontFamilies
	p.setInterfaceFont = app.SetInterfaceFontFamily

	settings := tree.Feature("settings-panels")
	p.ids.settingsFeature = settings
	p.ids.settingsCategories = tree.Sub(settings, tree.Name("categories"))
	p.settingsExtra.init(tree, settings)

	p.ids.settingsNotification = tree.ID(settings, "desktop-notifications")
	p.ids.settingsSound = tree.ID(settings, "sound")
	p.ids.settingsAutoAccept = tree.ID(settings, "auto-accept")
	p.ids.settingsShowGitHub = tree.ID(settings, "show-title-github")
	p.ids.settingsSearch = tree.ID(settings, "search")
	p.ids.settingsCloseToTray = tree.ID(settings, "close-to-tray")
	p.ids.settingsLaunchAtLogin = tree.ID(settings, "launch-at-login")
	p.ids.settingsChooseDir = tree.ID(settings, "choose-download-directory")
	p.ids.settingsOpenDir = tree.ID(settings, "open-download-directory")
	p.ids.settingsRefreshNet = tree.ID(settings, "refresh-networks")
	p.ids.settingsRefreshVoice = tree.ID(settings, "refresh-voice-devices")
	p.ids.settingsDismissLocal = tree.ID(settings, "dismiss-local-error")
	p.ids.settingsClearError = tree.ID(settings, "clear-error")
	p.ids.networkDefer = tree.ID(settings, "network-defer")
	p.ids.networkCancel = tree.ID(settings, "network-cancel")

	p.ids.shortcutRecord = tree.Sub(settings, tree.Name("shortcut-record"))
	p.ids.shortcutSave = tree.Sub(settings, tree.Name("shortcut-save"))
	p.ids.shortcutCancel = tree.Sub(settings, tree.Name("shortcut-cancel"))
	p.ids.shortcutReset = tree.ID(settings, "reset-shortcuts")
	p.ids.shortcutConfirmReset = tree.ID(settings, "confirm-reset-shortcuts")
	p.ids.shortcutCancelReset = tree.ID(settings, "cancel-reset-shortcuts")
	shortcutDialog := tree.Sub(settings, tree.Name("shortcut-dialog"))
	p.ids.shortcutDialogScope = shortcutDialog
	p.ids.shortcutDialog = tree.ID(shortcutDialog, "scope")
	p.ids.shortcutDialogDismiss = tree.ID(shortcutDialog, "dismiss")
	p.ids.shortcutDialogClose = tree.ID(shortcutDialog, "close")
	p.ids.shortcutDialogReset = tree.Sub(shortcutDialog, tree.Name("reset"))
	p.ids.shortcutDialogClear = tree.Sub(shortcutDialog, tree.Name("clear"))
	p.ids.shortcutDialogSave = tree.Sub(shortcutDialog, tree.Name("save"))
	p.ids.shortcutDialogCancel = tree.Sub(shortcutDialog, tree.Name("cancel"))
	p.ids.shortcutSave = p.ids.shortcutDialogSave
	p.ids.shortcutCancel = p.ids.shortcutDialogCancel

	p.ids.aboutCopyVersion = tree.ID(settings, "copy-version")
	p.ids.aboutLicense = tree.ID(settings, "show-license")
	p.ids.aboutBack = tree.ID(settings, "about-back")
	p.ids.aboutCopyLicense = tree.ID(settings, "copy-license")
	p.ids.aboutDiagnostics = tree.ID(settings, "export-diagnostics")
	p.ids.aboutCopyDiagnostics = tree.ID(settings, "copy-diagnostics")
	p.ids.aboutResetData = tree.ID(settings, "reset-data")
	p.ids.aboutResetConfirm = tree.ID(settings, "reset-data-confirm")
	p.ids.aboutResetCancel = tree.ID(settings, "reset-data-cancel")
	p.ids.aboutResetScope = tree.ID(settings, "reset-data-dialog")
	p.ids.aboutResetClose = tree.ID(settings, "reset-data-close")
	p.ids.aboutResetDismiss = tree.ID(settings, "reset-data-dismiss")
	p.ids.aboutAutoUpdate = tree.ID(settings, "auto-check-updates")
	p.ids.aboutCards = tree.Sub(settings, tree.Name("about-card"))

	transfers := tree.Feature("transfers-panel")
	p.ids.transferActions = tree.Sub(transfers, tree.Name("task"))
	p.ids.transferDirectory = tree.ID(transfers, "receive-directory")
	p.ids.transferSearch = tree.ID(transfers, "search")
	p.ids.transferMenuScope = tree.ID(transfers, "action-menu")
	p.ids.transferMenuDismiss = tree.ID(transfers, "action-menu-dismiss")
	p.ids.transferMenuCopy = tree.ID(transfers, "copy-file-name")
	p.ids.transfersDismiss = tree.ID(transfers, "dismiss-local-error")
	p.ids.transfersClearError = tree.ID(transfers, "clear-error")

	shares := tree.Feature("shares-panel")
	p.ids.sharesFeature = shares
	p.ids.shareSearch = tree.ID(shares, "search")
	p.ids.shareLocation = tree.ID(shares, "folder-location")
	p.ids.sharesRefresh = tree.ID(shares, "refresh")
	p.ids.sharesAdd = tree.ID(shares, "add")
	p.ids.sharePickFiles = tree.ID(shares, "pick-files")
	p.ids.sharePickFolder = tree.ID(shares, "pick-folder")
	p.ids.deviceShareMenu = tree.ID(shares, "device-menu")
	p.ids.shareMenuEnableLink = tree.ID(shares, "share-menu-enable-link")
	p.ids.shareToastScope = tree.ID(shares, "toast")
	p.ids.shareToastDismiss = tree.ID(shares, "toast-dismiss")
	p.ids.deviceShareApproval = tree.ID(shares, "device-upload-approval")
	p.ids.deviceShareStart = tree.ID(shares, "device-start")
	p.ids.deviceShareCopy = tree.ID(shares, "device-copy")
	p.ids.deviceShareQR = tree.ID(shares, "device-qr")
	p.ids.deviceShareInbox = tree.ID(shares, "device-inbox")
	p.ids.deviceShareStop = tree.ID(shares, "device-stop")
	p.ids.shareCopyLink = tree.Sub(shares, tree.Name("copy-link"))
	p.ids.shareDownload = tree.Sub(shares, tree.Name("download"))
	p.ids.shareMore = tree.Sub(shares, tree.Name("more"))
	p.ids.shareRow = tree.Sub(shares, tree.Name("share-row"))

	p.ids.shareMenuScope = tree.ID(shares, "share-menu-scope")
	p.ids.shareMenuDismiss = tree.ID(shares, "share-menu-dismiss")
	p.ids.shareMenuOpen = tree.ID(shares, "share-menu-open")
	p.ids.shareMenuFolder = tree.ID(shares, "share-menu-folder")
	p.ids.shareMenuGet = tree.ID(shares, "share-menu-get")
	p.ids.shareMenuVisit = tree.ID(shares, "share-menu-visit")
	p.ids.shareMenuCopy = tree.ID(shares, "share-menu-copy")
	p.ids.shareMenuQR = tree.ID(shares, "share-menu-qr")
	p.ids.shareMenuPassword = tree.ID(shares, "share-menu-password")
	p.ids.shareMenuClearPass = tree.ID(shares, "share-menu-clear-password")
	p.ids.shareMenuReplaceFile = tree.ID(shares, "replace-file")
	p.ids.shareMenuReplaceFolder = tree.ID(shares, "replace-folder")
	p.ids.shareMenuUnshare = tree.ID(shares, "share-menu-unshare")

	p.ids.shareDialogScope = tree.ID(shares, "share-dialog-scope")
	p.ids.shareDialogDismiss = tree.ID(shares, "share-dialog-dismiss")
	p.ids.shareDialogClose = tree.ID(shares, "share-dialog-close")
	p.ids.shareDialogPassword = tree.ID(shares, "share-dialog-password")
	p.ids.shareDialogCancel = tree.ID(shares, "share-dialog-cancel")
	p.ids.shareDialogSubmit = tree.ID(shares, "share-dialog-submit")

	p.ids.sharePasswordScope = tree.ID(shares, "share-password-scope")
	p.ids.sharePasswordDismiss = tree.ID(shares, "share-password-dismiss")
	p.ids.sharePasswordClose = tree.ID(shares, "share-password-close")
	p.ids.sharePasswordField = tree.ID(shares, "share-password-field")
	p.ids.sharePasswordCancel = tree.ID(shares, "share-password-cancel")
	p.ids.sharePasswordSubmit = tree.ID(shares, "share-password-submit")

	p.ids.shareQRScope = tree.ID(shares, "share-qr-scope")
	p.ids.shareQRDismiss = tree.ID(shares, "share-qr-dismiss")
	p.ids.shareQRClose = tree.ID(shares, "share-qr-close")
	p.ids.shareQRDismissButton = tree.ID(shares, "share-qr-dismiss-button")
	p.ids.shareQRCopy = tree.ID(shares, "share-qr-copy")
	p.print.init(tree)

	p.build = panelReadBuildInfo()
	p.refreshNetworks()
}

// Draw renders one whole panel inside bounds. The shell owns the outer rail and
// passes state it obtained on OnWake; this method never blocks and never starts
// background work.
func (p *Panels) Draw(canvas *gfx.Canvas, theme ui.Theme, bounds ui.Rect, module string, state domain.State, submit func(domain.Command)) {
	if canvas == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	previousModule := p.module
	if previousModule != "" && previousModule != module {
		p.scrollTabSet = false
		p.cancelShortcutEdit()
		p.resetShortcuts = false
		p.aboutLicense = false
		p.resetDataOpen = false
		p.networkChange = panelNetworkChange{}
		p.localError = ""
		p.transferMenu = ""
		p.closeShareInteractions()
		p.shareToast = shareToastState{}
		p.settingsExtra.resetSecrets()
		p.print.resetInteractions()
	}
	p.module = module
	p.search.anchor = ui.Box{}
	p.shareAnchor = ui.Box{}
	p.transferAnchor = ui.Box{}
	p.transferMenuEntry = nil

	canvas.Fill(bounds, theme.Background)
	p.theme = theme
	if module == panelModuleSettings {
		p.theme = theme.WithControlHeight(panelSettingsControlH)
	}
	var settingsScroll ui.Box
	p.page.Draw(canvas, bounds, func(f ui.C) {
		// Clip the panel's content, not the canvas hosting its floating dialogs.
		// Viva paints popups outside this subtree after its clip has been restored.
		ui.Stack(f).Fill().Clip().Children(func() {
			switch module {
			case panelModuleSettings:
				settingsScroll = p.drawSettings(f, theme, state, submit)
			case panelModuleTransfers:
				p.drawTransfers(f, theme, bounds, state, submit)
			case panelModuleShares:
				p.drawShares(f, theme, bounds, state, submit)
			case panelModulePrint:
				p.drawPrint(f, theme, bounds, state, submit)
			}
		})
	})
	canvas.Clip(bounds)
	if module == panelModuleShares {
		p.drawShareMenu(canvas, theme, bounds, state.DeviceShare, submit)
		p.drawShareToast(canvas, theme, bounds, state.ShareFeedback)
	} else if module == panelModuleTransfers {
		p.drawTransferMenu(canvas, theme, bounds, submit)
	}
	if settingsScroll.Valid() {
		if p.search.anchor.Valid() {
			off := settingsScroll.ScrollState().Offset + p.search.anchor.Rect().Y - settingsScroll.Rect().Y
			settingsScroll.ScrollState().ScrollTo(max(0, off))
			canvas.RequestFrame()
		}
		p.search.target = ""
		p.settingsScroll[p.settingsTab] = settingsScroll.ScrollState().Offset
	}
	canvas.Unclip()
}

// Panel content has a smaller layout region than the window. Every modal uses
// the shell's full viewport so its scrim also blocks the rail and title bar.
func (p *Panels) dialog(f ui.C, spec ui.DialogSpec) ui.Box {
	spec.Layer = 70
	dialog := ui.Dialog(f, spec)
	if p.dialogViewport.Width > 0 && p.dialogViewport.Height > 0 {
		dialog = dialog.Within(p.dialogViewport)
	}
	return dialog
}

// OnWake resolves local paths from the detached snapshot. Filesystem probes
// run on the shared media worker; Draw only reads the cached result.
func (p *Panels) OnWake(state domain.State) {
	if p.refreshNetwork || p.module == panelModuleSettings && p.networkLanguage != i18n.Language() {
		p.refreshNetworks()
	}
	p.applyShareBrowserEvents()
	p.observePending(state)
	p.settingsExtra.observePassword(state)
	previous := p.localFiles
	paths := localFilePaths(state)
	p.localFiles = make(map[string]panelLocalFile, len(paths))
	for fileID, path := range paths {
		entry := panelLocalFile{path: path}
		if old, ok := previous[fileID]; ok && old.path == path {
			entry.available = old.available
			entry.known = old.known
		}
		p.localFiles[fileID] = entry
	}
}

func (p *Panels) setMediaSubmit(submit func(mediaJob)) {
	p.mediaSubmit = submit
	p.shareQRWorker.setNotify(func() {
		if p.mediaSubmit == nil {
			return
		}
		p.mediaSubmit(mediaJob{
			key:  mediaJobKey{kind: mediaJobFileAvailability, name: "share-browser-wake"},
			kind: mediaJobFileAvailability, path: "", fileID: "share-browser-wake",
		})
	})
}

func (p *Panels) setToast(toast func(title, description string, tone components.ToastType)) {
	p.toast = toast
}

func (p *Panels) setLocalFileAvailability(entries []mediaFileResult) {
	for _, result := range entries {
		entry, ok := p.localFiles[result.fileID]
		if !ok || entry.path != result.path {
			continue
		}
		if entry.known && entry.available == result.available {
			continue
		}
		entry.available = result.available
		entry.known = true
		p.localFiles[result.fileID] = entry
		p.listDirty = true
	}
}

func (p *Panels) applyMediaActions(actions []mediaActionResult) {
	for _, result := range actions {
		action, ok := p.localActions[result.id]
		if !ok {
			continue
		}
		delete(p.localActions, result.id)
		if action.path != result.path || action.reveal != result.reveal {
			continue
		}
		if result.canceled {
			continue
		}
		if !result.available {
			message := i18n.T("本地文件不可用")
			if result.err != "" {
				message += "：" + result.err
			}
			p.reportLocalActionError(message)
			continue
		}
		if result.err != "" {
			p.reportLocalActionError(result.err)
		} else {
			p.localError = ""
		}
	}
}

func (p *Panels) reportLocalActionError(message string) {
	if p.module == panelModuleShares {
		p.setShareToast("无法打开本地项目", message, components.ToastError)
	} else {
		p.localError = message
	}
}

// TextInput returns a function instead of nil so the shell can combine every
// panel field provider safely.
func (p *Panels) TextInput(context app.TextInputContext) *app.TextInput {
	return p.page.TextInput(context)
}

// Key handles keys the shell routes while a panel is visible. Call it from
// Config.OnKey and Config.OnShortcut (Shortcut is an alias for convenience).
// It consumes only shortcut recording and Escape on its own detail pages.
func (p *Panels) Key(event gfx.KeyEvent) gfx.EventResult {
	if !event.Release && event.Key == gfx.KeyEscape && p.memberProject != nil && p.shareMenu.kind == shareMenuClosed && !p.shareModalOpen() {
		p.backMemberProject()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Release {
		return gfx.EventResult{}
	}
	if p.module == panelModuleSettings {
		if event.Target == p.ids.settingsSearch && event.Key == gfx.KeyEscape && p.search.field.Text() != "" {
			p.search.field.SetText("")
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if p.settingsExtra.interfaceFont.open {
			if result, handled := p.settingsExtra.interfaceFont.key(event, p.confirmInterfaceFont); handled {
				return result
			}
		}
		if p.shortcutEdit >= 0 {
			if event.Key == gfx.KeyEscape {
				p.cancelShortcutEdit()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			if event.Key.IsModifier() {
				return gfx.EventResult{Handled: true}
			}
			inDialog := p.tree != nil && p.tree.Within(event.Target.Scope(), p.ids.shortcutDialogScope)
			if event.Key == gfx.KeyTab && event.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) == 0 ||
				inDialog && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) &&
					event.Modifiers&(gfx.ModifierControl|gfx.ModifierAlt|gfx.ModifierMeta) == 0 {
				return gfx.EventResult{}
			}
			keys, ok := panelShortcutFromKey(event)
			if !ok {
				p.shortcutError = i18n.T("此按键无法录制，请换一组")
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			p.shortcutKeys = keys
			p.shortcutError = ""
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if event.Key == gfx.KeyEscape {
			switch {
			case p.aboutLicense:
				p.aboutLicense = false
				return gfx.EventResult{Handled: true, Repaint: true}
			case p.resetShortcuts:
				p.resetShortcuts = false
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
	}
	if p.module == panelModulePrint && p.print.shareOpen && event.Key == gfx.KeyEscape {
		p.print.closeShareDialog()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return p.page.Key(event)
}

// Shortcut is for Config.OnShortcut; its events carry Ctrl/Alt/Meta chords.
func (p *Panels) Shortcut(event gfx.KeyEvent) gfx.EventResult {
	return p.Key(event)
}

// Pointer lets Viva update hover and custom pointer state for the panel page.
// This is required by stateful controls such as scrollbar drag targets and
// dialogs whose native hit regions extend beyond ordinary Clicked handling.
func (p *Panels) Pointer(event gfx.PointerEvent) gfx.EventResult {
	return p.page.Pointer(event)
}

// Wheel routes the wheel to an open settings dropdown first, then to the panel
// scroll area under the pointer.
func (p *Panels) Wheel(event gfx.ScrollEvent) gfx.EventResult {
	if p.module == panelModuleSettings && p.settingsExtra.interfaceFont.open {
		return p.page.Wheel(event)
	}
	if p.module == panelModuleTransfers && p.transferMenu != "" {
		p.transferMenu = ""
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return p.page.Wheel(event)
}

func (p *Panels) observePending(state domain.State) {
	if p.pending == nil {
		return
	}
	if p.pending.matches(state.Settings) {
		p.pending = nil
		p.saveFailed = false
		return
	}
	if state.Revision != p.pending.revision && state.Error != "" {
		p.pending = nil
		p.saveFailed = true
	}
}

func (p *Panels) submitString(submit func(domain.Command), state domain.State, setting, value string) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: setting, Value: value})
	p.pending = &panelPendingSetting{kind: panelPendingValue, setting: setting, value: value, revision: state.Revision}
	p.saveFailed = false
}

func (p *Panels) submitBool(submit func(domain.Command), state domain.State, setting string, value bool) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: setting, Bool: value})
	p.pending = &panelPendingSetting{kind: panelPendingBool, setting: setting, boolean: value, revision: state.Revision}
	p.saveFailed = false
}

func (p *Panels) submitShortcuts(submit func(domain.Command), state domain.State, shortcuts []domain.Shortcut) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: "shortcuts", Value: panelEncodeShortcuts(shortcuts)})
	p.pending = &panelPendingSetting{
		kind: panelPendingShortcuts, setting: "shortcuts",
		shortcut: append([]domain.Shortcut(nil), shortcuts...), revision: state.Revision,
	}
	p.saveFailed = false
}

func (p *Panels) submitResetShortcuts(submit func(domain.Command), state domain.State) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: "reset_shortcuts"})
	p.pending = &panelPendingSetting{kind: panelPendingResetShortcuts, setting: "reset_shortcuts", revision: state.Revision}
	p.saveFailed = false
}

// updateLists runs after the snapshot and any media results have been applied.
func (p *Panels) updateLists(state domain.State) {
	language := i18n.Language()
	if !p.listDirty && p.listRevision == state.Revision && p.listLanguage == language {
		return
	}
	p.rebuildTransfers(state)
	p.rebuildShares(state)
	p.print.rebuild(state)
	p.listRevision, p.listLanguage, p.listDirty = state.Revision, language, false
}

func (p *Panels) rebuildTransfers(state domain.State) {
	p.transferRows = p.transferRows[:0]
	fileIndex := make(map[string]int)
	if len(state.Transfers) > 0 {
		for i := range state.Files {
			fileIndex[state.Files[i].ID] = i
		}
	}
	for _, transfer := range state.Transfers {
		var file domain.File
		if i, ok := fileIndex[transfer.FileID]; ok {
			file = state.Files[i]
		}
		entry := panelBuildTransfer(state, transfer, file)
		scope := p.tree.Sub(p.ids.transferActions, p.tree.Name(transfer.ID))
		entry.ids = newTransferActionIDs(p.tree, scope, entry)
		p.transferRows = append(p.transferRows, entry)
	}
	p.sortTransfers()
	p.transferIncomingLabel = fmt.Sprintf(i18n.T("待接收 %d"), len(p.transferIncoming))
	p.transferLabels = [5]string{
		fmt.Sprintf(i18n.T("全部 %d"), len(p.transferRows)),
		fmt.Sprintf(i18n.T("待接收 %d"), len(p.transferIncoming)),
		fmt.Sprintf(i18n.T("传输中 %d"), len(p.transferPending)),
		fmt.Sprintf(i18n.T("已结束 %d"), len(p.transferFinished)),
		fmt.Sprintf(i18n.T("传入 %d"), len(p.transferReceived)),
	}
}

func panelBuildTransfer(state domain.State, transfer domain.Transfer, file domain.File) panelTransferEntry {
	peer := state.Peer(transfer.PeerID)
	name := peer.Name
	if transfer.WebSender != "" {
		name = transfer.WebSender
	}
	if strings.TrimSpace(name) == "" {
		name = i18n.T("未知联系人")
	}
	entry := panelTransferEntry{
		transfer: transfer,
		name:     transfer.Name,
		peer:     name,
		status:   panelTransferStatus(transfer.Status, transfer.Direction),
		fraction: panelTransferFraction(transfer),
		updated:  transfer.UpdatedAt,
		err:      transfer.Error,
	}
	if strings.TrimSpace(entry.name) == "" {
		entry.name = panelShareName(file)
	}
	entry.source = i18n.T("聊天")
	if transfer.FromShare || transfer.Direction == "receive" && file.Shared {
		entry.source = i18n.T("文件分享")
	}
	if transfer.PrintJobID != "" {
		entry.source = i18n.T("打印共享")
	}
	if transfer.WebSender != "" {
		entry.source = i18n.T("网页接收")
	}
	entry.path = transfer.Path
	if transfer.ShareFolder {
		file.Folder = true
	}
	file.Name = entry.name
	if entry.path == "" && transfer.Direction == "send" {
		entry.path = file.SourcePath
	}
	entry.directory = file.Folder
	entry.preview = !file.Folder && !file.ShareFolder && entry.path != "" &&
		(transfer.Direction == "send" || transfer.Status == "completed") && (fileIsImage(file) || file.Video())
	entry.icon = fileTypeIcon(file)
	direction := i18n.T("发送给 ") + name
	if transfer.Direction == "receive" {
		direction = fmt.Sprintf(i18n.T("%s 发来"), name)
	}
	size := panelFormatBytes(transfer.Size)
	entry.size = size
	entry.meta = direction + " · " + size
	if panelTransferIsFinished(transfer.Status) && !transfer.UpdatedAt.IsZero() {
		entry.meta += " · " + transfer.UpdatedAt.Local().Format("2006-01-02 15:04")
	}
	if transfer.Size > 0 && (transfer.Status == "transferring" || transfer.Status == "verifying" || transfer.Status == "waiting" && transfer.Done > 0) {
		entry.meta = direction
		entry.percent = fmt.Sprintf("%d%%", int(entry.fraction*100))
		done := transfer.Done
		if transfer.Status == "verifying" {
			done = transfer.Size
		}
		entry.progress = panelFormatBytes(done) + " / " + size
		if transfer.Status == "transferring" && transfer.Speed > 0 {
			entry.progress += " · " + panelFormatBytes(int64(transfer.Speed)) + "/s"
			if remaining := transfer.Size - done; remaining > 0 {
				entry.progress += " · " + panelETA(remaining, transfer.Speed)
			}
		}
	}
	if transfer.Status == "offered" && transfer.Direction == "send" {
		entry.meta = direction
		entry.status = size + " · " + entry.status
	}

	switch transfer.Status {
	case "offered":
		if transfer.Direction == "receive" {
			entry.accept, entry.saveAs, entry.reject = true, transfer.WebSender == "", true
		} else {
			entry.cancel = true
		}
	case "transferring", "verifying", "waiting", "retry_requested", "requesting", "queued":
		entry.cancel = true
	case "failed", "cancelled", "rejected":
		entry.retry = transfer.WebSender == ""
		entry.saveAs = transfer.CanRelocateFailedReceive()
		if transfer.Direction == "send" && transfer.FromShare {
			entry.retry = !transfer.Revoked && file.Shared && !file.ShareProtected
		}
	case "completed":
		entry.open, entry.reveal = entry.path != "", entry.path != ""
	}
	return entry
}

func panelTransferIsFinished(status string) bool {
	switch status {
	case "completed", "rejected", "cancelled", "failed":
		return true
	default:
		return false
	}
}

func panelTransferStatus(status, direction string) string {
	switch status {
	case "queued":
		return i18n.T("等待下载")
	case "requesting":
		return i18n.T("正在请求文件")
	case "offered":
		if direction == "receive" {
			return i18n.T("等待接收")
		}
		return i18n.T("等待对方接收")
	case "transferring":
		return i18n.T("传输中")
	case "verifying":
		return i18n.T("校验中")
	case "completed":
		return i18n.T("已完成")
	case "rejected":
		return i18n.T("已拒绝")
	case "waiting":
		return i18n.T("等待连接")
	case "retry_requested":
		return i18n.T("等待对方确认重试")
	case "cancelled":
		return i18n.T("已取消")
	case "failed":
		return i18n.T("失败")
	case "":
		return i18n.T("状态未知")
	default:
		return status
	}
}

func panelTransferFraction(transfer domain.Transfer) float32 {
	switch transfer.Status {
	case "completed", "verifying":
		return 1
	case "transferring", "waiting":
		if transfer.Size <= 0 {
			return 0
		}
		return min(max(float32(float64(transfer.Done)/float64(transfer.Size)), 0), 1)
	default:
		return 0
	}
}

func panelETA(remaining int64, speed float64) string {
	if remaining <= 0 || speed <= 0 {
		return i18n.T("预计时间计算中")
	}
	seconds := float64(remaining) / speed
	if seconds < 1 {
		return i18n.T("预计不到 1 秒")
	}
	duration := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	if duration < time.Minute {
		return fmt.Sprintf(i18n.T("预计 %d 秒"), int(duration/time.Second))
	}
	if duration < time.Hour {
		minutes := int(duration / time.Minute)
		secs := int((duration % time.Minute) / time.Second)
		if secs == 0 {
			return fmt.Sprintf(i18n.T("预计 %d 分钟"), minutes)
		}
		return fmt.Sprintf(i18n.T("预计 %d 分 %d 秒"), minutes, secs)
	}
	return fmt.Sprintf(i18n.T("预计 %d 小时"), int(duration/time.Hour))
}

func (p *Panels) rebuildShares(state domain.State) {
	p.sharesAll = p.sharesAll[:0]
	p.sharesMine = p.sharesMine[:0]
	p.sharesRemote = p.sharesRemote[:0]
	for _, file := range state.Files {
		if !file.Shared {
			continue
		}
		own := file.SenderID != "" && file.SenderID == state.Self.ID
		peer := state.Peer(file.SenderID)
		sender := peer.Name
		if own {
			sender = i18n.T("我")
		} else if strings.TrimSpace(sender) == "" {
			sender = i18n.T("未知联系人")
		}
		local := p.localFiles[file.ID]
		entry := panelShareEntry{
			file: file, sender: sender, own: own, online: peer.Online,
			path: local.path, available: local.available,
		}
		entry.title = panelShareName(file)
		entry.kind = i18n.T("文件")
		entry.label = i18n.T("共享文件 ") + entry.title
		entry.meta = panelFormatBytes(file.Size)
		if file.ShareFolder {
			entry.kind = i18n.T("文件夹")
			entry.label = i18n.T("共享文件夹 ") + entry.title
			entry.meta = fmt.Sprintf(i18n.T("%d 个项目"), file.ShareEntryCount) + " · " + entry.meta
		}
		if !own {
			if file.ShareProtected {
				entry.meta = sender
			} else {
				entry.meta += " · " + sender
			}
		}
		if !file.CreatedAt.IsZero() {
			entry.date = file.CreatedAt.Local().Format("2006-01-02 15:04")
		} else {
			entry.date = "—"
		}
		entry.compactMeta = entry.meta
		if entry.date != "—" {
			entry.compactMeta += " · " + entry.date
		}
		switch {
		case own && file.ShareUnavailable:
			entry.access = i18n.T("分享不可用")
		case file.ShareProtected:
			entry.access = i18n.T("密码访问")
		case file.ShareURL != "":
			entry.access = i18n.T("公开访问")
		default:
			entry.access = i18n.T("成员访问")
		}
		entry.icon = fileTypeIcon(file)
		entry.rowID = p.tree.ID(p.ids.shareRow, file.ID)
		entry.copyID = p.tree.ID(p.ids.shareCopyLink, file.ID)
		if entry.memberWebProject() {
			entry.downloadID = p.tree.ID(p.ids.shareDownload, file.ID)
		}
		entry.moreID = p.tree.ID(p.ids.shareMore, file.ID)
		switch {
		case own && file.ShareUnavailable:
			entry.state = i18n.T("分享不可用")
		case file.ShareFolder:
			entry.state = i18n.T("通过链接浏览")
		case entry.available:
			entry.state = i18n.T("本地文件可用")
		case own:
			entry.state = i18n.T("源文件当前不可用")
		case entry.path != "":
			entry.state = i18n.T("本地文件已不可用")
		default:
			entry.state = i18n.T("尚未获取")
		}
		p.sharesAll = append(p.sharesAll, entry)
		if own {
			p.sharesMine = append(p.sharesMine, entry)
		} else {
			p.sharesRemote = append(p.sharesRemote, entry)
		}
	}
	p.sortShares()
	p.shareLabels = [2]string{
		panelSharesRemote: fmt.Sprintf(i18n.T("成员共享 %d"), len(p.sharesRemote)),
		panelSharesMine:   fmt.Sprintf(i18n.T("我的分享 %d"), len(p.sharesMine)),
	}
}

func (p *Panels) refreshNetworks() {
	p.refreshNetwork = false
	p.networks = p.networks[:0]
	p.networkChoices = p.networkChoices[:0]
	p.networkReadError = ""
	p.networkLanguage = i18n.Language()
	list := p.listNetworks
	if list == nil {
		list = net.Interfaces
	}
	interfaces, err := list()
	if err != nil {
		p.networkReadError = err.Error()
		p.networkFor = ""
		return
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		status := i18n.T("未连接")
		if iface.Flags&net.FlagUp != 0 {
			status = i18n.T("已连接")
		}
		label := iface.Name + " · " + status
		if iface.Flags&net.FlagPointToPoint != 0 {
			label += i18n.T(" · 点对点")
		}
		p.networks = append(p.networks, panelNetworkOption{value: iface.Name, label: label})
	}
	sort.SliceStable(p.networks, func(i, j int) bool {
		return p.networks[i].label < p.networks[j].label
	})
	p.networkFor = ""
}

func (p *Panels) networkName(value string) string {
	if value == "" || value == "auto" {
		return i18n.T("自动选择")
	}
	for _, option := range p.networks {
		if option.value == value {
			return option.label
		}
	}
	return value
}

func panelActiveTransferCount(state domain.State) int {
	count := 0
	for _, transfer := range state.Transfers {
		if transfer.Status == "transferring" || transfer.Status == "verifying" {
			count++
		}
	}
	return count
}

// The core stores a network change as PendingNetwork while a transfer is
// active. Until the backend supports an atomic cancel-and-switch command, the
// panel offers only the deferred change or cancellation, never a false
// "immediate" success.
func (p *Panels) chooseNetwork(submit func(domain.Command), state domain.State, value, label string) {
	active := panelActiveTransferCount(state)
	if active > 0 {
		p.networkChange = panelNetworkChange{value: value, label: label, active: active}
		return
	}
	p.networkChange = panelNetworkChange{}
	p.submitString(submit, state, "network", value)
}

func (p *Panels) networkChoicesFor(current string) ([]panelNetworkOption, panelNetworkOption) {
	if current == "" {
		current = "auto"
	}
	if p.networkFor != current || len(p.networkChoices) == 0 {
		p.networkChoices = p.networkChoices[:0]
		p.networkChoices = append(p.networkChoices, panelNetworkOption{value: "auto", label: i18n.T("自动选择")})
		p.networkChoices = append(p.networkChoices, p.networks...)
		if !slices.ContainsFunc(p.networkChoices, func(option panelNetworkOption) bool {
			return option.value == current
		}) {
			p.networkChoices = append(p.networkChoices, panelNetworkOption{
				value: current, label: i18n.T("当前配置 · ") + current,
			})
		}
		p.networkFor = current
	}
	for _, option := range p.networkChoices {
		if option.value == current {
			return p.networkChoices, option
		}
	}
	return p.networkChoices, p.networkChoices[0]
}

func (p *Panels) cancelShortcutEdit() {
	p.shortcutEdit = -1
	p.shortcutKeys = ""
	p.shortcutError = ""
}

func panelShortcutFromKey(event gfx.KeyEvent) (string, bool) {
	if event.Release || event.Key.IsModifier() {
		return "", false
	}
	name := ""
	switch event.Key {
	case gfx.KeyEnter:
		name = "Enter"
	case gfx.KeySpace:
		name = "Space"
	case gfx.KeyTab:
		name = "Tab"
	case gfx.KeyBackspace:
		name = "Backspace"
	case gfx.KeyDelete:
		name = "Delete"
	case gfx.KeyHome:
		name = "Home"
	case gfx.KeyEnd:
		name = "End"
	case gfx.KeyPageUp:
		name = "PageUp"
	case gfx.KeyPageDown:
		name = "PageDown"
	case gfx.KeyInsert:
		name = "Insert"
	case gfx.KeyLeft, gfx.KeyRight, gfx.KeyUp, gfx.KeyDown:
		name = map[gfx.Key]string{
			gfx.KeyLeft: "Left", gfx.KeyRight: "Right", gfx.KeyUp: "Up", gfx.KeyDown: "Down",
		}[event.Key]
	case gfx.KeyF1, gfx.KeyF2, gfx.KeyF3, gfx.KeyF4, gfx.KeyF5, gfx.KeyF6,
		gfx.KeyF7, gfx.KeyF8, gfx.KeyF9, gfx.KeyF10, gfx.KeyF11, gfx.KeyF12:
		name = fmt.Sprintf("F%d", int(event.Key-gfx.KeyF1)+1)
	default:
		name = strings.TrimSpace(event.Text)
		if name != "" {
			name = strings.ToUpper(name)
		}
	}
	if name == "" || name == "Escape" {
		return "", false
	}
	var parts []string
	if event.Modifiers&gfx.ModifierControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if event.Modifiers&gfx.ModifierAlt != 0 {
		parts = append(parts, "Alt")
	}
	if event.Modifiers&gfx.ModifierShift != 0 {
		parts = append(parts, "Shift")
	}
	if event.Modifiers&gfx.ModifierMeta != 0 {
		parts = append(parts, "Meta")
	}
	parts = append(parts, name)
	return strings.Join(parts, "+"), true
}

func panelShortcutValid(keys string) bool {
	if strings.TrimSpace(keys) == "" {
		return true
	}
	return strings.Contains(keys, "Ctrl+") || strings.Contains(keys, "Alt+") ||
		strings.Contains(keys, "Meta+") || strings.Contains(keys, "F") && strings.HasPrefix(keys, "F")
}

func panelShortcutConflict(shortcuts []domain.Shortcut, index int, keys string) string {
	keys = strings.ToLower(strings.TrimSpace(keys))
	if keys == "" {
		return ""
	}
	for at, shortcut := range shortcuts {
		if at == index {
			continue
		}
		if strings.EqualFold(shortcut.Scope, shortcuts[index].Scope) &&
			strings.ToLower(shortcuts[at].Keys) == keys {
			return i18n.T("与“") + shortcut.Action + i18n.T("”冲突")
		}
	}
	return ""
}

func panelEncodeShortcuts(shortcuts []domain.Shortcut) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for index, shortcut := range shortcuts {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"Action":`)
		builder.WriteString(panelJSONString(shortcut.Action))
		builder.WriteString(`,"Keys":`)
		builder.WriteString(panelJSONString(shortcut.Keys))
		builder.WriteString(`,"Scope":`)
		builder.WriteString(panelJSONString(shortcut.Scope))
		builder.WriteByte('}')
	}
	builder.WriteByte(']')
	return builder.String()
}

func panelJSONString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, char := range value {
		switch char {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`\"`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			builder.WriteRune(char)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func (pending *panelPendingSetting) matches(settings domain.Settings) bool {
	switch pending.kind {
	case panelPendingValue:
		switch pending.setting {
		case "language":
			return settings.Language == pending.value
		case "color_scheme":
			return settings.ColorScheme == pending.value
		case "theme":
			return settings.Theme == pending.value
		case "download_dir":
			return settings.DownloadDir == pending.value
		case "duplicate_policy":
			return settings.DuplicatePolicy == pending.value
		case "network":
			return settings.Network == pending.value || settings.PendingNetwork == pending.value
		case "send_key":
			return settings.SendKey == pending.value
		case "voice_microphone":
			return settings.VoiceMicrophone == pending.value
		case "voice_speaker":
			return settings.VoiceSpeaker == pending.value
		case "voice_microphone_volume":
			return strconv.Itoa(settings.VoiceMicrophoneVolume) == pending.value
		case "voice_speaker_volume":
			return strconv.Itoa(settings.VoiceSpeakerVolume) == pending.value
		case "background_transparency":
			return strconv.Itoa(settings.BackgroundTransparency) == pending.value
		case "interface_font_size":
			return strconv.Itoa(domain.NormalizeInterfaceFontSize(settings.InterfaceFontSize)) == pending.value
		}
	case panelPendingBool:
		switch pending.setting {
		case "desktop_notifications":
			return settings.DesktopNotifications == pending.boolean
		case "sound":
			return settings.Sound == pending.boolean
		case "auto_accept":
			return settings.AutoAccept == pending.boolean
		case "show_title_github":
			return !settings.HideTitleGitHub == pending.boolean
		case "close_to_tray":
			return settings.CloseToTray == pending.boolean
		case "hide_notification_text":
			return settings.HideNotificationText == pending.boolean
		case "hide_offline_contacts":
			return settings.HideOfflineContacts == pending.boolean
		case "disable_auto_discovery":
			return settings.DisableAutoDiscovery == pending.boolean
		case "auto_receive_videos":
			return !settings.ManualVideoReceive == pending.boolean
		case "auto_accept_images":
			return settings.AutoAcceptImages == pending.boolean
		case "lock_on_startup":
			return settings.LockOnStartup == pending.boolean
		}
	case panelPendingShortcuts:
		return slices.Equal(settings.Shortcuts, pending.shortcut)
	case panelPendingResetShortcuts:
		return slices.Equal(settings.Shortcuts, domain.DefaultSettings(settings.DownloadDir).Shortcuts)
	}
	return false
}

func panelCopy(canvas *gfx.Canvas, p *Panels, text string) {
	canvas.AfterPaint(func() {
		if err := app.SetClipboardText(text); err != nil {
			p.localError = i18n.T("复制失败：") + err.Error()
			return
		}
		p.localError = ""
		if p.toast != nil {
			p.toast(i18n.T("已复制"), "", components.ToastSuccess)
		}
	})
}

func panelOpenLocal(_ *gfx.Canvas, p *Panels, path string, reveal bool) {
	p.requestLocalAction(path, reveal, false)
}

func (p *Panels) requestLocalAction(path string, reveal, directory bool) {
	p.queueLocalAction(mediaJob{path: path, reveal: reveal, directory: directory})
}

func (p *Panels) queueLocalAction(job mediaJob) {
	if job.path == "" {
		return
	}
	p.nextLocalAction++
	action := panelLocalAction{id: p.nextLocalAction, path: job.path, reveal: job.reveal}
	if p.localActions == nil {
		p.localActions = make(map[uint64]panelLocalAction)
	}
	p.localActions[action.id] = action
	if p.mediaSubmit == nil {
		delete(p.localActions, action.id)
		p.reportLocalActionError(i18n.T("本地文件检查服务不可用"))
		return
	}
	job.key = mediaJobKey{kind: mediaJobLocalAction, id: action.id}
	job.kind, job.actionID = mediaJobLocalAction, action.id
	p.mediaSubmit(job)
}

func panelFormatBytes(size int64) string {
	if size < 0 {
		return i18n.T("大小未知")
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	value := float64(size)
	unit := ""
	for _, candidate := range units {
		value /= 1024
		unit = candidate
		if value < 1024 {
			break
		}
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f %s", value, unit)
	}
	return fmt.Sprintf("%.2f %s", value, unit)
}

func panelDark(theme ui.Theme) bool {
	return theme.Background.Red < 0.5
}

func panelAccentText(theme ui.Theme) ui.Color {
	if panelDark(theme) {
		return theme.Chart[0]
	}
	return theme.Primary
}

func panelAccentSurface(theme ui.Theme) ui.Color {
	if panelDark(theme) {
		return theme.Background.Mix(theme.Chart[1], 0.20)
	}
	return theme.Background.Mix(theme.Primary, 0.08)
}

func panelTint(color ui.Color, alpha float32, backdrop ui.Color) ui.Color {
	return color.WithAlpha(alpha).Over(backdrop)
}

func panelStatusColor(theme ui.Theme, status string) ui.Color {
	switch status {
	case "completed", "已完成", "本地文件可用":
		return theme.Success
	case "failed", "rejected", "失败", "已拒绝", "源文件当前不可用", "本地文件已不可用":
		return theme.Danger
	case "transferring", "verifying", "传输中", "校验中":
		return panelAccentText(theme)
	default:
		return theme.MutedForeground
	}
}

func panelPageHeader(f ui.C, theme ui.Theme, title, description string, trailing func()) {
	ui.Row(f).FillWidth().Items(ui.Center).Gap(20).Children(func() {
		ui.Column(f).Grow(1).Gap(3).Children(func() {
			ui.Text(f, title).FontSize(textSize(f, 25)).Weight(gfx.WeightSemibold)
			if description != "" {
				ui.Paragraph(f, description, 2).Muted().FontSize(theme.Metrics.FontSizeS + 1)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}

func panelSection(f ui.C, title, description string, body func()) {
	ui.Column(f).FillWidth().Gap(2).PadSides(0, 0, 2, 0).Children(func() {
		ui.Row(f).FillWidth().PadY(8).Children(func() {
			ui.Text(f, title).FontSize(textSize(f, 16)).Weight(gfx.WeightSemibold)
		})
		if description != "" {
			ui.Paragraph(f, description, 2).Muted().FontSize(textSize(f, 12))
		}
		body()
	})
}

func panelSettingRow(f ui.C, theme ui.Theme, title, description string, control func()) {
	ui.Row(f).FillWidth().Gap(24).PadY(13).Items(ui.Center).Children(func() {
		ui.Column(f).Grow(1).Gap(3).Children(func() {
			ui.Text(f, title).FontSize(theme.Metrics.FontSizeM + 1).Weight(gfx.WeightMedium)
			if description != "" {
				ui.Paragraph(f, description, 3).Muted().FontSize(theme.Metrics.FontSizeS)
			}
		})
		if control != nil {
			control()
		}
	})
	ui.Divider(f).FillWidth()
}

func panelControlColumn(f ui.C, width float32, body func()) {
	ui.Column(f).Width(width).Shrink(0).Items(ui.End).Justify(ui.Center).Children(body)
}

func panelPill(f ui.C, theme ui.Theme, text string, tone ui.Color) {
	background := panelTint(tone, 0.10, theme.Background)
	ui.Row(f).PadSides(4, 9, 4, 9).Radius(gfx.RadiusFull).Background(background).
		Border(1, panelTint(tone, 0.22, theme.Background)).Children(func() {
		ui.Text(f, text).FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(tone)
	})
}

func panelBanner(f ui.C, theme ui.Theme, text string, danger bool, dismiss func()) {
	if text == "" {
		return
	}
	tone := theme.Primary
	if danger {
		tone = theme.Danger
	}
	ui.Row(f).FillWidth().PadSides(9, 12, 9, 12).Gap(12).Items(ui.Center).
		Background(panelTint(tone, 0.08, theme.Background)).
		Border(1, panelTint(tone, 0.24, theme.Background)).Radius(8).Children(func() {
		ui.Paragraph(f, text, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1).Color(tone)
		if dismiss != nil {
			if namedButton(f, gfx.ID{}, i18n.T("关闭")).Small().Ghost().Clicked() {
				dismiss()
			}
		}
	})
}

func panelScrollFooter(f ui.C, theme ui.Theme, left, right string) {
	ui.Divider(f).FillWidth()
	ui.Row(f).FillWidth().PadY(10).Items(ui.Center).Children(func() {
		ui.Text(f, left).Muted().FontSize(theme.Metrics.FontSizeS).Grow(1)
		if right != "" {
			ui.Text(f, right).Muted().FontSize(theme.Metrics.FontSizeS)
		}
	})
}

func (p *Panels) panelFeedback(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command), dismissLocal, clearError gfx.ID) {
	if p.localError != "" {
		id := dismissLocal
		if id.IsZero() {
			id = clearError
		}
		panelBannerWithAction(f, theme, p.localError, true, id, func() {
			p.localError = ""
		})
	}
	if state.Error != "" {
		panelBannerWithAction(f, theme, i18n.T(state.Error), true, clearError, func() {
			submit(domain.Command{Kind: domain.ClearError})
		})
	}
}

func panelBannerWithAction(f ui.C, theme ui.Theme, text string, danger bool, id gfx.ID, dismiss func()) {
	tone := theme.Primary
	if danger {
		tone = theme.Danger
	}
	ui.Row(f).FillWidth().PadSides(9, 12, 9, 12).Gap(12).Items(ui.Center).
		Background(panelTint(tone, 0.08, theme.Background)).
		Border(1, panelTint(tone, 0.24, theme.Background)).Radius(8).Children(func() {
		ui.Paragraph(f, text, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1).Color(tone)
		if dismiss != nil {
			if namedButton(f, id, i18n.T("关闭")).Small().Ghost().Clicked() {
				dismiss()
			}
		}
	})
}

func panelEmpty(f ui.C, theme ui.Theme, title, description string) {
	ui.Column(f).FillWidth().PadY(64).Gap(6).Items(ui.Center).Children(func() {
		ui.Text(f, title).FontSize(textSize(f, 16)).Weight(gfx.WeightMedium)
		ui.Paragraph(f, description, 2).Muted().FontSize(theme.Metrics.FontSizeS + 1).MaxWidth(420).TextCenter()
	})
}

func panelSectionGap(f ui.C) {
	ui.Spacer(f).Height(18)
}

func panelPathText(f ui.C, theme ui.Theme, path string) {
	ui.Row(f).FillWidth().Height(theme.Metrics.ControlHeight).PadX(10).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
		Clip().Items(ui.Center).Children(func() {
		ui.Text(f, path).FontSize(theme.Metrics.FontSizeS + 1).Ellipsis().Grow(1)
	})
}

var (
	panelIconGear = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 3.5), gfx.CubicTo(16.7, 3.5, 20.5, 7.3, 20.5, 12),
		gfx.CubicTo(20.5, 16.7, 16.7, 20.5, 12, 20.5),
		gfx.CubicTo(7.3, 20.5, 3.5, 16.7, 3.5, 12), gfx.CubicTo(3.5, 7.3, 7.3, 3.5, 12, 3.5),
		gfx.MoveTo(12, 8.2), gfx.CubicTo(14.1, 8.2, 15.8, 9.9, 15.8, 12),
		gfx.CubicTo(15.8, 14.1, 14.1, 15.8, 12, 15.8),
		gfx.CubicTo(9.9, 15.8, 8.2, 14.1, 8.2, 12), gfx.CubicTo(8.2, 9.9, 9.9, 8.2, 12, 8.2),
		gfx.MoveTo(12, 1.5), gfx.LineTo(12, 3.5), gfx.MoveTo(12, 20.5), gfx.LineTo(12, 22.5),
		gfx.MoveTo(1.5, 12), gfx.LineTo(3.5, 12), gfx.MoveTo(20.5, 12), gfx.LineTo(22.5, 12),
		gfx.MoveTo(4.6, 4.6), gfx.LineTo(6, 6), gfx.MoveTo(18, 18), gfx.LineTo(19.4, 19.4),
		gfx.MoveTo(19.4, 4.6), gfx.LineTo(18, 6), gfx.MoveTo(6, 18), gfx.LineTo(4.6, 19.4),
	}}
	panelIconKeyboard = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(6, 6), gfx.LineTo(18, 6), gfx.QuadTo(20, 6, 20, 8), gfx.LineTo(20, 16),
		gfx.QuadTo(20, 18, 18, 18), gfx.LineTo(6, 18), gfx.QuadTo(4, 18, 4, 16),
		gfx.LineTo(4, 8), gfx.QuadTo(4, 6, 6, 6), gfx.ClosePath(),
		gfx.MoveTo(7, 10), gfx.LineTo(17, 10), gfx.MoveTo(7, 14), gfx.LineTo(13, 14),
	}}
	panelIconInfo = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 3.5), gfx.CubicTo(16.7, 3.5, 20.5, 7.3, 20.5, 12),
		gfx.CubicTo(20.5, 16.7, 16.7, 20.5, 12, 20.5),
		gfx.CubicTo(7.3, 20.5, 3.5, 16.7, 3.5, 12), gfx.CubicTo(3.5, 7.3, 7.3, 3.5, 12, 3.5),
		gfx.MoveTo(12, 10.5), gfx.LineTo(12, 17), gfx.MoveTo(12, 7.2), gfx.LineTo(12, 7.6),
	}}
	panelIconFolder = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(3.5, 7), gfx.QuadTo(3.5, 5, 5.5, 5), gfx.LineTo(9.2, 5),
		gfx.LineTo(11, 7), gfx.LineTo(18.5, 7), gfx.QuadTo(20.5, 7, 20.5, 9),
		gfx.LineTo(20.5, 17), gfx.QuadTo(20.5, 19, 18.5, 19), gfx.LineTo(5.5, 19),
		gfx.QuadTo(3.5, 19, 3.5, 17), gfx.ClosePath(),
	}}
	panelIconDownload = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 4), gfx.LineTo(12, 15), gfx.MoveTo(7.5, 10.5), gfx.LineTo(12, 15),
		gfx.LineTo(16.5, 10.5), gfx.MoveTo(4.5, 18.5), gfx.LineTo(19.5, 18.5),
	}}
	panelIconUpload = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 15), gfx.LineTo(12, 4), gfx.MoveTo(7.5, 8.5), gfx.LineTo(12, 4),
		gfx.LineTo(16.5, 8.5), gfx.MoveTo(4.5, 18.5), gfx.LineTo(19.5, 18.5),
	}}
	panelIconRefresh = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(19.5, 8), gfx.CubicTo(17.8, 5.2, 14.7, 3.5, 11.3, 4.1),
		gfx.CubicTo(7.2, 4.8, 4, 8, 3.5, 12), gfx.MoveTo(19.5, 4.5), gfx.LineTo(19.5, 8),
		gfx.LineTo(16, 8), gfx.MoveTo(4.5, 16), gfx.CubicTo(6.2, 18.8, 9.3, 20.5, 12.7, 19.9),
		gfx.CubicTo(16.8, 19.2, 20, 16, 20.5, 12), gfx.MoveTo(4.5, 19.5),
		gfx.LineTo(4.5, 16), gfx.LineTo(8, 16),
	}}
	panelIconCopy = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(8, 8), gfx.LineTo(18, 8), gfx.QuadTo(20, 8, 20, 10), gfx.LineTo(20, 20),
		gfx.QuadTo(20, 22, 18, 22), gfx.LineTo(8, 22), gfx.QuadTo(6, 22, 6, 20),
		gfx.LineTo(6, 10), gfx.QuadTo(6, 8, 8, 8),
		gfx.MoveTo(4, 16), gfx.QuadTo(2, 16, 2, 14), gfx.LineTo(2, 4), gfx.QuadTo(2, 2, 4, 2),
		gfx.LineTo(14, 2), gfx.QuadTo(16, 2, 16, 4), gfx.MoveTo(16, 4), gfx.LineTo(16, 6),
	}}
	panelIconBack = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(19, 12), gfx.LineTo(5, 12), gfx.MoveTo(11, 6), gfx.LineTo(5, 12), gfx.LineTo(11, 18),
	}}
	panelIconFile = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(6, 3.5), gfx.LineTo(14, 3.5), gfx.LineTo(19, 8.5), gfx.LineTo(19, 20.5),
		gfx.LineTo(6, 20.5), gfx.ClosePath(), gfx.MoveTo(14, 3.5), gfx.LineTo(14, 8.5),
		gfx.LineTo(19, 8.5),
	}}
	panelIconCheck = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(5, 12.5), gfx.LineTo(9.5, 17), gfx.LineTo(19, 7),
	}}
)

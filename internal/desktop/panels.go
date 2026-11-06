package desktop

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	panelModuleSettings  = "settings"
	panelModuleTransfers = "transfers"
	panelModuleShares    = "shares"

	panelSettingsWidth = 300
)

type panelSettingsTab uint8

const (
	panelSettingsGeneral panelSettingsTab = iota
	panelSettingsShortcuts
	panelSettingsAbout
)

const (
	panelTransferPending = iota
	panelTransferFinished
)

const (
	panelSharesAll = iota
	panelSharesMine
	panelSharesRemote
)

var (
	panelSettingsLabels  = []string{"常规", "快捷键", "关于"}
	panelDuplicateLabels = []string{"自动重命名", "每次询问"}
	panelDuplicateValues = []string{"rename", "ask"}
)

type panelIDs struct {
	settingsFeature    ui.Scope
	settingsCategories ui.Scope
	themeChoice        ui.Scope
	duplicateChoice    ui.Scope
	networkChoice      ui.Scope
	sendKeyChoice      ui.Scope

	settingsNotification ui.ID
	settingsSound        ui.ID
	settingsAutoAccept   ui.ID
	settingsCloseToTray  ui.ID
	settingsChooseDir    ui.ID
	settingsOpenDir      ui.ID
	settingsDismissLocal ui.ID
	settingsClearError   ui.ID

	settingsGeneralScroll   ui.ID
	settingsShortcutsScroll ui.ID
	settingsAboutScroll     ui.ID

	shortcutRecord       ui.Scope
	shortcutSave         ui.Scope
	shortcutCancel       ui.Scope
	shortcutReset        ui.ID
	shortcutConfirmReset ui.ID
	shortcutCancelReset  ui.ID

	aboutCopyVersion ui.ID
	aboutLicense     ui.ID
	aboutBack        ui.ID
	aboutCopyLicense ui.ID

	transfersFeature    ui.Scope
	transferScroll      ui.ID
	transferFilters     ui.Scope
	transferAccept      ui.Scope
	transferSaveAs      ui.Scope
	transferReject      ui.Scope
	transferCancel      ui.Scope
	transferRetry       ui.Scope
	transferOpen        ui.Scope
	transferReveal      ui.Scope
	transfersDismiss    ui.ID
	transfersClearError ui.ID

	sharesFeature    ui.Scope
	sharesScroll     ui.ID
	sharesFilters    ui.Scope
	sharesRefresh    ui.ID
	sharesAdd        ui.ID
	shareGet         ui.Scope
	shareRemove      ui.Scope
	shareOpen        ui.Scope
	shareReveal      ui.Scope
	sharesDismiss    ui.ID
	sharesClearError ui.ID
}

type panelNetworkOption struct {
	value string
	label string
}

type panelTransferEntry struct {
	transfer domain.Transfer
	peer     string
	status   string
	meta     string
	err      string
	path     string
	fraction float32
	updated  time.Time

	accept   bool
	saveAs   bool
	reject   bool
	cancel   bool
	retry    bool
	open     bool
	reveal   bool
	sharable bool
}

type panelShareEntry struct {
	file   domain.File
	sender string
	meta   string
	state  string
	own    bool
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

type panelBuildInfo struct {
	goVersion   string
	vivaVersion string
	revision    string
	buildTime   string
}

// Panels owns the settings, transfers, and file-sharing surfaces. Init must be
// called with the application's tree before Draw; Draw can also initialize from
// the canvas tree for standalone tests.
type Panels struct {
	tree        *ui.Tree
	initialized bool
	view        flex.View
	ids         panelIDs

	module string

	settingsTab     panelSettingsTab
	settingsScroll  [3]float32
	scrollTab       panelSettingsTab
	scrollTabSet    bool
	aboutLicense    bool
	resetShortcuts  bool
	shortcutEdit    int
	shortcutKeys    string
	shortcutError   string
	duplicateSelect flex.SelectState
	networkSelect   flex.SelectState

	transferFilter int
	shareFilter    int

	networks         []panelNetworkOption
	networkChoices   []panelNetworkOption
	networkLabels    []string
	networkValues    []string
	networkFor       string
	networkReadError string

	transferPending  []panelTransferEntry
	transferFinished []panelTransferEntry
	transferLabels   []string
	sharesAll        []panelShareEntry
	sharesMine       []panelShareEntry
	sharesRemote     []panelShareEntry
	shareLabels      []string

	cacheReady    bool
	cacheRevision uint64

	pending     *panelPendingSetting
	saveFailed  bool
	localError  string
	localNotice string
	build       panelBuildInfo
}

func (p *Panels) Init(tree *ui.Tree) {
	if tree == nil {
		return
	}
	if p.initialized && p.tree == tree {
		return
	}
	p.tree = tree
	p.initialized = true
	p.shortcutEdit = -1

	settings := tree.Feature("settings-panels")
	p.ids.settingsFeature = settings
	p.ids.settingsCategories = tree.Sub(settings, tree.Name("categories"))
	p.ids.themeChoice = tree.Sub(settings, tree.Name("theme-choice"))
	p.ids.duplicateChoice = tree.Sub(settings, tree.Name("duplicate-choice"))
	p.ids.networkChoice = tree.Sub(settings, tree.Name("network-choice"))
	p.ids.sendKeyChoice = tree.Sub(settings, tree.Name("send-key-choice"))

	p.ids.settingsNotification = tree.ID(settings, "desktop-notifications")
	p.ids.settingsSound = tree.ID(settings, "sound")
	p.ids.settingsAutoAccept = tree.ID(settings, "auto-accept")
	p.ids.settingsCloseToTray = tree.ID(settings, "close-to-tray")
	p.ids.settingsChooseDir = tree.ID(settings, "choose-download-directory")
	p.ids.settingsOpenDir = tree.ID(settings, "open-download-directory")
	p.ids.settingsDismissLocal = tree.ID(settings, "dismiss-local-error")
	p.ids.settingsClearError = tree.ID(settings, "clear-error")

	p.ids.settingsGeneralScroll = tree.ID(settings, "general-scroll")
	p.ids.settingsShortcutsScroll = tree.ID(settings, "shortcuts-scroll")
	p.ids.settingsAboutScroll = tree.ID(settings, "about-scroll")

	p.ids.shortcutRecord = tree.Sub(settings, tree.Name("shortcut-record"))
	p.ids.shortcutSave = tree.Sub(settings, tree.Name("shortcut-save"))
	p.ids.shortcutCancel = tree.Sub(settings, tree.Name("shortcut-cancel"))
	p.ids.shortcutReset = tree.ID(settings, "reset-shortcuts")
	p.ids.shortcutConfirmReset = tree.ID(settings, "confirm-reset-shortcuts")
	p.ids.shortcutCancelReset = tree.ID(settings, "cancel-reset-shortcuts")

	p.ids.aboutCopyVersion = tree.ID(settings, "copy-version")
	p.ids.aboutLicense = tree.ID(settings, "show-license")
	p.ids.aboutBack = tree.ID(settings, "about-back")
	p.ids.aboutCopyLicense = tree.ID(settings, "copy-license")

	transfers := tree.Feature("transfers-panel")
	p.ids.transfersFeature = transfers
	p.ids.transferScroll = tree.ID(transfers, "scroll")
	p.ids.transferFilters = tree.Sub(transfers, tree.Name("filters"))
	p.ids.transferAccept = tree.Sub(transfers, tree.Name("accept"))
	p.ids.transferSaveAs = tree.Sub(transfers, tree.Name("save-as"))
	p.ids.transferReject = tree.Sub(transfers, tree.Name("reject"))
	p.ids.transferCancel = tree.Sub(transfers, tree.Name("cancel"))
	p.ids.transferRetry = tree.Sub(transfers, tree.Name("retry"))
	p.ids.transferOpen = tree.Sub(transfers, tree.Name("open"))
	p.ids.transferReveal = tree.Sub(transfers, tree.Name("reveal"))
	p.ids.transfersDismiss = tree.ID(transfers, "dismiss-local-error")
	p.ids.transfersClearError = tree.ID(transfers, "clear-error")

	shares := tree.Feature("shares-panel")
	p.ids.sharesFeature = shares
	p.ids.sharesScroll = tree.ID(shares, "scroll")
	p.ids.sharesFilters = tree.Sub(shares, tree.Name("filters"))
	p.ids.sharesRefresh = tree.ID(shares, "refresh")
	p.ids.sharesAdd = tree.ID(shares, "add")
	p.ids.shareGet = tree.Sub(shares, tree.Name("get"))
	p.ids.shareRemove = tree.Sub(shares, tree.Name("remove"))
	p.ids.shareOpen = tree.Sub(shares, tree.Name("open"))
	p.ids.shareReveal = tree.Sub(shares, tree.Name("reveal"))
	p.ids.sharesDismiss = tree.ID(shares, "dismiss-local-error")
	p.ids.sharesClearError = tree.ID(shares, "clear-error")

	p.build = panelReadBuildInfo()
	p.refreshNetworks()
}

// Draw renders one whole panel inside bounds. The shell owns the outer rail and
// passes state it obtained on OnWake; this method never blocks and never starts
// background work.
func (p *Panels) Draw(canvas *ui.Canvas, theme ui.Theme, bounds ui.Rect, module string, state domain.State, submit func(domain.Command)) {
	if canvas == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	if !p.initialized || p.tree == nil {
		p.Init(canvas.Tree())
	}
	if p.module != "" && p.module != module {
		p.duplicateSelect.Open = false
		p.networkSelect.Open = false
		p.cancelShortcutEdit()
		p.resetShortcuts = false
		p.aboutLicense = false
		p.localError = ""
		p.localNotice = ""
	}
	p.module = module
	p.observePending(state)
	p.prepare(state)

	canvas.Clip(bounds)
	canvas.Fill(bounds, theme.Background)
	p.view.Draw(canvas, theme, bounds, func(f *flex.Frame) {
		switch module {
		case panelModuleSettings:
			p.drawSettings(f, theme, state, submit)
		case panelModuleTransfers:
			p.drawTransfers(f, theme, state, submit)
		case panelModuleShares:
			p.drawShares(f, theme, state, submit)
		}
	})
	p.captureScroll()
	canvas.Unclip()
}

// TextInput returns a function instead of nil so the shell can combine every
// panel field provider safely. These panels use buttons, switches, and selects;
// they own no native text editor.
func (p *Panels) TextInput() app.TextInputProvider {
	return func(app.TextInputContext) *app.TextInput { return nil }
}

// Key handles keys the shell routes while a panel is visible. Call it from
// Config.OnKey and Config.OnShortcut (Shortcut is an alias for convenience).
// It consumes only shortcut recording and Escape on its own detail pages.
func (p *Panels) Key(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	if p.module == panelModuleSettings {
		if p.settingsTab == panelSettingsGeneral {
			if event.Key == ui.KeyEscape && (p.duplicateSelect.Open || p.networkSelect.Open) {
				p.duplicateSelect.Open = false
				p.networkSelect.Open = false
				return ui.EventResult{Handled: true, Repaint: true}
			}
			if result := p.duplicateSelect.Key(event); result.Handled {
				return result
			}
			if result := p.networkSelect.Key(event); result.Handled {
				return result
			}
		}
		if p.shortcutEdit >= 0 {
			if event.Key == ui.KeyEscape {
				p.cancelShortcutEdit()
				return ui.EventResult{Handled: true, Repaint: true}
			}
			keys, ok := panelShortcutFromKey(event)
			if !ok {
				p.shortcutError = "此按键无法录制，请换一组"
				return ui.EventResult{Handled: true, Repaint: true}
			}
			p.shortcutKeys = keys
			p.shortcutError = ""
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if event.Key == ui.KeyEscape {
			switch {
			case p.aboutLicense:
				p.aboutLicense = false
				return ui.EventResult{Handled: true, Repaint: true}
			case p.resetShortcuts:
				p.resetShortcuts = false
				return ui.EventResult{Handled: true, Repaint: true}
			}
		}
	}
	return ui.EventResult{}
}

// Shortcut is for Config.OnShortcut; its events carry Ctrl/Alt/Meta chords.
func (p *Panels) Shortcut(event ui.KeyEvent) ui.EventResult {
	return p.Key(event)
}

// Wheel routes the wheel to an open settings dropdown first, then to the panel
// scroll area under the pointer.
func (p *Panels) Wheel(event ui.ScrollEvent) ui.EventResult {
	if p.module == panelModuleSettings && p.settingsTab == panelSettingsGeneral {
		if p.duplicateSelect.Open {
			return p.duplicateSelect.Wheel(event)
		}
		if p.networkSelect.Open {
			return p.networkSelect.Wheel(event)
		}
	}
	return p.view.Wheel(event)
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
	p.localNotice = ""
}

func (p *Panels) submitBool(submit func(domain.Command), state domain.State, setting string, value bool) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: setting, Bool: value})
	p.pending = &panelPendingSetting{kind: panelPendingBool, setting: setting, boolean: value, revision: state.Revision}
	p.saveFailed = false
	p.localNotice = ""
}

func (p *Panels) submitShortcuts(submit func(domain.Command), state domain.State, shortcuts []domain.Shortcut) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: "shortcuts", Value: panelEncodeShortcuts(shortcuts)})
	p.pending = &panelPendingSetting{
		kind: panelPendingShortcuts, setting: "shortcuts",
		shortcut: append([]domain.Shortcut(nil), shortcuts...), revision: state.Revision,
	}
	p.saveFailed = false
	p.localNotice = ""
}

func (p *Panels) submitResetShortcuts(submit func(domain.Command), state domain.State) {
	submit(domain.Command{Kind: domain.SetSetting, Setting: "reset_shortcuts"})
	p.pending = &panelPendingSetting{kind: panelPendingResetShortcuts, setting: "reset_shortcuts", revision: state.Revision}
	p.saveFailed = false
}

func (p *Panels) prepare(state domain.State) {
	if p.cacheReady && p.cacheRevision == state.Revision {
		return
	}
	p.rebuildTransfers(state)
	p.rebuildShares(state)
	p.cacheRevision = state.Revision
	p.cacheReady = true
}

func (p *Panels) captureScroll() {
	if p.module != panelModuleSettings {
		return
	}
	id := p.settingsScrollID(p.settingsTab)
	if id.IsZero() {
		return
	}
	state := p.view.ScrollState(id)
	if state.Viewport > 0 {
		p.settingsScroll[p.settingsTab] = state.Offset
	}
}

func (p *Panels) settingsScrollID(tab panelSettingsTab) ui.ID {
	switch tab {
	case panelSettingsGeneral:
		return p.ids.settingsGeneralScroll
	case panelSettingsShortcuts:
		return p.ids.settingsShortcutsScroll
	case panelSettingsAbout:
		return p.ids.settingsAboutScroll
	default:
		return ui.ID{}
	}
}

func (p *Panels) rebuildTransfers(state domain.State) {
	p.transferPending = p.transferPending[:0]
	p.transferFinished = p.transferFinished[:0]
	for _, transfer := range state.Transfers {
		entry := panelBuildTransfer(state, transfer)
		if panelTransferIsFinished(transfer.Status) {
			p.transferFinished = append(p.transferFinished, entry)
		} else {
			p.transferPending = append(p.transferPending, entry)
		}
	}
	sort.SliceStable(p.transferPending, func(i, j int) bool {
		return p.transferPending[i].updated.After(p.transferPending[j].updated)
	})
	sort.SliceStable(p.transferFinished, func(i, j int) bool {
		return p.transferFinished[i].updated.After(p.transferFinished[j].updated)
	})
	p.transferLabels = []string{
		fmt.Sprintf("进行中 %d", len(p.transferPending)),
		fmt.Sprintf("已完成 %d", len(p.transferFinished)),
	}
}

func panelBuildTransfer(state domain.State, transfer domain.Transfer) panelTransferEntry {
	peer := state.Peer(transfer.PeerID)
	name := peer.Name
	if strings.TrimSpace(name) == "" {
		name = "未知联系人"
	}
	entry := panelTransferEntry{
		transfer: transfer,
		peer:     name,
		status:   panelTransferStatus(transfer.Status, transfer.Direction),
		fraction: panelTransferFraction(transfer),
		updated:  transfer.UpdatedAt,
		err:      transfer.Error,
	}
	if transfer.Path != "" {
		entry.path = transfer.Path
	}
	direction := "发送给 " + name
	if transfer.Direction == "receive" {
		direction = "来自 " + name
	}
	meta := []string{direction, panelFormatBytes(transfer.Size)}
	if transfer.Done > 0 && transfer.Done < transfer.Size {
		meta = append(meta, panelFormatBytes(transfer.Done))
	}
	if transfer.Speed > 0 && !panelTransferIsFinished(transfer.Status) {
		meta = append(meta, panelFormatBytes(int64(transfer.Speed))+"/s")
		if remaining := transfer.Size - transfer.Done; remaining > 0 {
			meta = append(meta, panelETA(remaining, transfer.Speed))
		}
	}
	if !transfer.UpdatedAt.IsZero() {
		meta = append(meta, transfer.UpdatedAt.Format("2006-01-02 15:04"))
	}
	entry.meta = strings.Join(meta, " · ")

	switch transfer.Status {
	case "offered":
		if transfer.Direction == "receive" {
			entry.accept, entry.saveAs, entry.reject = true, true, true
		} else {
			entry.cancel = true
		}
	case "transferring", "verifying", "waiting":
		entry.cancel = true
	case "failed", "cancelled", "rejected":
		entry.retry = true
	case "completed":
		entry.open, entry.reveal = transfer.Path != "", transfer.Path != ""
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
	case "offered":
		if direction == "receive" {
			return "等待接收"
		}
		return "等待确认"
	case "transferring":
		return "传输中"
	case "verifying":
		return "校验中"
	case "completed":
		return "已完成"
	case "rejected":
		return "已拒绝"
	case "waiting":
		return "等待连接"
	case "cancelled":
		return "已取消"
	case "failed":
		return "失败"
	case "":
		return "状态未知"
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
		return "预计时间计算中"
	}
	seconds := float64(remaining) / speed
	if seconds < 1 {
		return "预计不到 1 秒"
	}
	duration := time.Duration(seconds * float64(time.Second)).Round(time.Second)
	if duration < time.Minute {
		return fmt.Sprintf("预计 %d 秒", int(duration/time.Second))
	}
	if duration < time.Hour {
		minutes := int(duration / time.Minute)
		secs := int((duration % time.Minute) / time.Second)
		if secs == 0 {
			return fmt.Sprintf("预计 %d 分钟", minutes)
		}
		return fmt.Sprintf("预计 %d 分 %d 秒", minutes, secs)
	}
	return fmt.Sprintf("预计 %d 小时", int(duration/time.Hour))
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
		sender := state.Peer(file.SenderID).Name
		if own {
			sender = "我"
		} else if strings.TrimSpace(sender) == "" {
			sender = "未知联系人"
		}
		entry := panelShareEntry{file: file, sender: sender, own: own}
		eventTime := file.CreatedAt
		meta := []string{panelFormatBytes(file.Size), sender}
		if !eventTime.IsZero() {
			meta = append(meta, eventTime.Format("2006-01-02 15:04"))
		}
		entry.meta = strings.Join(meta, " · ")
		switch {
		case file.SourcePath != "":
			entry.state = "本地文件可用"
		case own:
			entry.state = "源文件当前不可用"
		default:
			entry.state = "尚未获取"
		}
		p.sharesAll = append(p.sharesAll, entry)
		if own {
			p.sharesMine = append(p.sharesMine, entry)
		} else {
			p.sharesRemote = append(p.sharesRemote, entry)
		}
	}
	panelSortShares(p.sharesAll)
	panelSortShares(p.sharesMine)
	panelSortShares(p.sharesRemote)
	p.shareLabels = []string{
		fmt.Sprintf("全部 %d", len(p.sharesAll)),
		fmt.Sprintf("我分享的 %d", len(p.sharesMine)),
		fmt.Sprintf("他人分享 %d", len(p.sharesRemote)),
	}
}

func panelSortShares(entries []panelShareEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].file.CreatedAt.After(entries[j].file.CreatedAt)
	})
}

func (p *Panels) refreshNetworks() {
	p.networks = p.networks[:0]
	p.networkReadError = ""
	interfaces, err := net.Interfaces()
	if err != nil {
		p.networkReadError = err.Error()
		p.networkFor = ""
		return
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		status := "未连接"
		if iface.Flags&net.FlagUp != 0 {
			status = "已连接"
		}
		label := iface.Name + " · " + status
		if iface.Flags&net.FlagPointToPoint != 0 {
			label += " · 点对点"
		}
		p.networks = append(p.networks, panelNetworkOption{value: iface.Name, label: label})
	}
	sort.SliceStable(p.networks, func(i, j int) bool {
		return p.networks[i].label < p.networks[j].label
	})
	p.networkFor = ""
}

func (p *Panels) networkChoicesFor(current string) ([]string, []string, int) {
	normalized := current
	if normalized == "" || normalized == "auto" {
		normalized = ""
	}
	if p.networkFor != normalized || len(p.networkChoices) == 0 {
		p.networkChoices = p.networkChoices[:0]
		p.networkChoices = append(p.networkChoices, panelNetworkOption{value: "auto", label: "自动选择"})
		p.networkChoices = append(p.networkChoices, p.networks...)
		if normalized != "" && !slices.ContainsFunc(p.networkChoices, func(option panelNetworkOption) bool {
			return option.value == normalized
		}) {
			p.networkChoices = append(p.networkChoices, panelNetworkOption{
				value: normalized, label: "当前配置 · " + normalized,
			})
		}
		p.networkLabels = p.networkLabels[:0]
		p.networkValues = p.networkValues[:0]
		for _, option := range p.networkChoices {
			p.networkLabels = append(p.networkLabels, option.label)
			p.networkValues = append(p.networkValues, option.value)
		}
		p.networkFor = normalized
	}
	selected := 0
	for index, value := range p.networkValues {
		if value == current || current == "" && value == "auto" || current == "auto" && value == "auto" {
			selected = index
			break
		}
	}
	return p.networkLabels, p.networkValues, selected
}

func (p *Panels) cancelShortcutEdit() {
	p.shortcutEdit = -1
	p.shortcutKeys = ""
	p.shortcutError = ""
}

func panelShortcutFromKey(event ui.KeyEvent) (string, bool) {
	if event.Release || event.Key.IsModifier() {
		return "", false
	}
	name := ""
	switch event.Key {
	case ui.KeyEnter:
		name = "Enter"
	case ui.KeySpace:
		name = "Space"
	case ui.KeyTab:
		name = "Tab"
	case ui.KeyBackspace:
		name = "Backspace"
	case ui.KeyDelete:
		name = "Delete"
	case ui.KeyHome:
		name = "Home"
	case ui.KeyEnd:
		name = "End"
	case ui.KeyPageUp:
		name = "PageUp"
	case ui.KeyPageDown:
		name = "PageDown"
	case ui.KeyInsert:
		name = "Insert"
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
		name = map[ui.Key]string{
			ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
		}[event.Key]
	case ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6,
		ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12:
		name = fmt.Sprintf("F%d", int(event.Key-ui.KeyF1)+1)
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
	if event.Modifiers&ui.ModifierControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if event.Modifiers&ui.ModifierAlt != 0 {
		parts = append(parts, "Alt")
	}
	if event.Modifiers&ui.ModifierShift != 0 {
		parts = append(parts, "Shift")
	}
	if event.Modifiers&ui.ModifierMeta != 0 {
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
			return "与“" + shortcut.Action + "”冲突"
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
		case "theme":
			return settings.Theme == pending.value
		case "download_dir":
			return settings.DownloadDir == pending.value
		case "duplicate_policy":
			return settings.DuplicatePolicy == pending.value
		case "network":
			return settings.Network == pending.value
		case "send_key":
			return settings.SendKey == pending.value
		}
	case panelPendingBool:
		switch pending.setting {
		case "desktop_notifications":
			return settings.DesktopNotifications == pending.boolean
		case "sound":
			return settings.Sound == pending.boolean
		case "auto_accept":
			return settings.AutoAccept == pending.boolean
		case "close_to_tray":
			return settings.CloseToTray == pending.boolean
		}
	case panelPendingShortcuts:
		return slices.Equal(settings.Shortcuts, pending.shortcut)
	case panelPendingResetShortcuts:
		return slices.Equal(settings.Shortcuts, domain.DefaultSettings(settings.DownloadDir).Shortcuts)
	}
	return false
}

func panelReadBuildInfo() panelBuildInfo {
	info := panelBuildInfo{goVersion: runtime.Version(), vivaVersion: "未提供"}
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if build.GoVersion != "" {
		info.goVersion = build.GoVersion
	}
	for _, dependency := range build.Deps {
		if dependency.Path != "github.com/vivaui/viva" {
			continue
		}
		switch {
		case dependency.Replace != nil && dependency.Replace.Version != "":
			info.vivaVersion = dependency.Replace.Version
		case dependency.Replace != nil:
			info.vivaVersion = "本地替换"
		case dependency.Version != "":
			info.vivaVersion = dependency.Version
		}
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.revision = setting.Value
		case "vcs.time":
			info.buildTime = setting.Value
		}
	}
	if len(info.revision) > 12 {
		info.revision = info.revision[:12]
	}
	return info
}

func panelCopy(canvas *ui.Canvas, p *Panels, text string) {
	canvas.AfterPaint(func() {
		if err := app.SetClipboardText(text); err != nil {
			p.localError = "复制失败：" + err.Error()
			return
		}
		p.localError = ""
		p.localNotice = "已复制"
	})
}

func panelOpenLocal(canvas *ui.Canvas, p *Panels, path string, reveal bool) {
	canvas.AfterPaint(func() {
		var err error
		if reveal {
			err = app.RevealPath(path)
		} else {
			err = app.OpenPath(path)
		}
		switch {
		case errors.Is(err, app.ErrCanceled):
			return
		case err != nil:
			p.localError = err.Error()
		default:
			p.localError = ""
		}
	})
}

func panelFormatBytes(size int64) string {
	if size < 0 {
		return "大小未知"
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
	case "failed", "rejected", "失败", "已拒绝", "源文件当前不可用":
		return theme.Danger
	case "transferring", "verifying", "传输中", "校验中":
		return panelAccentText(theme)
	default:
		return theme.MutedForeground
	}
}

func panelPageHeader(f *flex.Frame, theme ui.Theme, title, description string, trailing func()) {
	f.Row().FillWidth().Items(flex.Center).Gap(20).Children(func() {
		f.Column().Grow(1).Gap(3).Children(func() {
			f.Text(title).FontSize(25).Weight(ui.WeightSemibold)
			if description != "" {
				f.Paragraph(description, 2).Muted().FontSize(theme.Metrics.FontSizeS + 1)
			}
		})
		if trailing != nil {
			trailing()
		}
	})
}

func panelSection(f *flex.Frame, title, description string, body func()) {
	f.Column().FillWidth().Gap(2).PadSides(0, 0, 2, 0).Children(func() {
		f.Row().FillWidth().PadY(8).Children(func() {
			f.Text(title).FontSize(16).Weight(ui.WeightSemibold)
		})
		if description != "" {
			f.Paragraph(description, 2).Muted().FontSize(12)
		}
		body()
	})
}

func panelSettingRow(f *flex.Frame, theme ui.Theme, title, description string, control func()) {
	f.Row().FillWidth().Gap(24).PadY(13).Items(flex.Center).Children(func() {
		f.Column().Grow(1).Gap(3).Children(func() {
			f.Text(title).FontSize(theme.Metrics.FontSizeM + 1).Weight(ui.WeightMedium)
			if description != "" {
				f.Paragraph(description, 3).Muted().FontSize(theme.Metrics.FontSizeS)
			}
		})
		if control != nil {
			control()
		}
	})
	f.Divider().FillWidth()
}

func panelControlColumn(f *flex.Frame, width float32, body func()) {
	f.Column().Width(width).Shrink(0).Items(flex.End).Justify(flex.Center).Children(body)
}

func panelPill(f *flex.Frame, theme ui.Theme, text string, tone ui.Color) {
	background := panelTint(tone, 0.10, theme.Background)
	f.Row().PadSides(4, 9, 4, 9).Radius(ui.RadiusFull).Background(background).
		Border(1, panelTint(tone, 0.22, theme.Background)).Children(func() {
		f.Text(text).FontSize(theme.Metrics.FontSizeS).Weight(ui.WeightMedium).Color(tone)
	})
}

func panelBanner(f *flex.Frame, theme ui.Theme, text string, danger bool, dismiss func()) {
	if text == "" {
		return
	}
	tone := theme.Primary
	if danger {
		tone = theme.Danger
	}
	f.Row().FillWidth().PadSides(9, 12, 9, 12).Gap(12).Items(flex.Center).
		Background(panelTint(tone, 0.08, theme.Background)).
		Border(1, panelTint(tone, 0.24, theme.Background)).Radius(8).Children(func() {
		f.Paragraph(text, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1).Color(tone)
		if dismiss != nil {
			if f.Button(ui.ID{}, "关闭").Small().Ghost().Clicked() {
				dismiss()
			}
		}
	})
}

func panelScrollFooter(f *flex.Frame, theme ui.Theme, left, right string) {
	f.Divider().FillWidth()
	f.Row().FillWidth().PadY(10).Items(flex.Center).Children(func() {
		f.Text(left).Muted().FontSize(theme.Metrics.FontSizeS).Grow(1)
		if right != "" {
			f.Text(right).Muted().FontSize(theme.Metrics.FontSizeS)
		}
	})
}

func (p *Panels) panelFeedback(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command), dismissLocal, clearError ui.ID) {
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
		panelBannerWithAction(f, theme, state.Error, true, clearError, func() {
			submit(domain.Command{Kind: domain.ClearError})
		})
	}
	if state.Notice != "" {
		panelBanner(f, theme, state.Notice, false, nil)
	}
	if p.localNotice != "" {
		panelBanner(f, theme, p.localNotice, false, nil)
	}
}

func panelBannerWithAction(f *flex.Frame, theme ui.Theme, text string, danger bool, id ui.ID, dismiss func()) {
	tone := theme.Primary
	if danger {
		tone = theme.Danger
	}
	f.Row().FillWidth().PadSides(9, 12, 9, 12).Gap(12).Items(flex.Center).
		Background(panelTint(tone, 0.08, theme.Background)).
		Border(1, panelTint(tone, 0.24, theme.Background)).Radius(8).Children(func() {
		f.Paragraph(text, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1).Color(tone)
		if dismiss != nil {
			if f.Button(id, "关闭").Small().Ghost().Clicked() {
				dismiss()
			}
		}
	})
}

func panelEmpty(f *flex.Frame, theme ui.Theme, title, description string) {
	f.Column().FillWidth().PadY(64).Gap(6).Items(flex.Center).Children(func() {
		f.Text(title).FontSize(16).Weight(ui.WeightMedium)
		f.Paragraph(description, 2).Muted().FontSize(theme.Metrics.FontSizeS + 1).MaxWidth(420).TextCenter()
	})
}

func panelSectionGap(f *flex.Frame) {
	f.Spacer().Height(18)
}

func panelPathText(f *flex.Frame, theme ui.Theme, path string) {
	f.Row().FillWidth().Height(theme.Metrics.ControlHeight).PadX(10).
		Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
		Clip().Items(flex.Center).Children(func() {
		f.Text(path).FontSize(theme.Metrics.FontSizeS + 1).Ellipsis().Grow(1)
	})
}

var (
	panelIconGear = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(12, 3.5), ui.CubicTo(16.7, 3.5, 20.5, 7.3, 20.5, 12),
		ui.CubicTo(20.5, 16.7, 16.7, 20.5, 12, 20.5),
		ui.CubicTo(7.3, 20.5, 3.5, 16.7, 3.5, 12), ui.CubicTo(3.5, 7.3, 7.3, 3.5, 12, 3.5),
		ui.MoveTo(12, 8.2), ui.CubicTo(14.1, 8.2, 15.8, 9.9, 15.8, 12),
		ui.CubicTo(15.8, 14.1, 14.1, 15.8, 12, 15.8),
		ui.CubicTo(9.9, 15.8, 8.2, 14.1, 8.2, 12), ui.CubicTo(8.2, 9.9, 9.9, 8.2, 12, 8.2),
		ui.MoveTo(12, 1.5), ui.LineTo(12, 3.5), ui.MoveTo(12, 20.5), ui.LineTo(12, 22.5),
		ui.MoveTo(1.5, 12), ui.LineTo(3.5, 12), ui.MoveTo(20.5, 12), ui.LineTo(22.5, 12),
		ui.MoveTo(4.6, 4.6), ui.LineTo(6, 6), ui.MoveTo(18, 18), ui.LineTo(19.4, 19.4),
		ui.MoveTo(19.4, 4.6), ui.LineTo(18, 6), ui.MoveTo(6, 18), ui.LineTo(4.6, 19.4),
	}}
	panelIconKeyboard = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(6, 6), ui.LineTo(18, 6), ui.QuadTo(20, 6, 20, 8), ui.LineTo(20, 16),
		ui.QuadTo(20, 18, 18, 18), ui.LineTo(6, 18), ui.QuadTo(4, 18, 4, 16),
		ui.LineTo(4, 8), ui.QuadTo(4, 6, 6, 6), ui.ClosePath(),
		ui.MoveTo(7, 10), ui.LineTo(17, 10), ui.MoveTo(7, 14), ui.LineTo(13, 14),
	}}
	panelIconInfo = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(12, 3.5), ui.CubicTo(16.7, 3.5, 20.5, 7.3, 20.5, 12),
		ui.CubicTo(20.5, 16.7, 16.7, 20.5, 12, 20.5),
		ui.CubicTo(7.3, 20.5, 3.5, 16.7, 3.5, 12), ui.CubicTo(3.5, 7.3, 7.3, 3.5, 12, 3.5),
		ui.MoveTo(12, 10.5), ui.LineTo(12, 17), ui.MoveTo(12, 7.2), ui.LineTo(12, 7.6),
	}}
	panelIconFolder = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(3.5, 7), ui.QuadTo(3.5, 5, 5.5, 5), ui.LineTo(9.2, 5),
		ui.LineTo(11, 7), ui.LineTo(18.5, 7), ui.QuadTo(20.5, 7, 20.5, 9),
		ui.LineTo(20.5, 17), ui.QuadTo(20.5, 19, 18.5, 19), ui.LineTo(5.5, 19),
		ui.QuadTo(3.5, 19, 3.5, 17), ui.ClosePath(),
	}}
	panelIconDownload = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(12, 4), ui.LineTo(12, 15), ui.MoveTo(7.5, 10.5), ui.LineTo(12, 15),
		ui.LineTo(16.5, 10.5), ui.MoveTo(4.5, 18.5), ui.LineTo(19.5, 18.5),
	}}
	panelIconUpload = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(12, 15), ui.LineTo(12, 4), ui.MoveTo(7.5, 8.5), ui.LineTo(12, 4),
		ui.LineTo(16.5, 8.5), ui.MoveTo(4.5, 18.5), ui.LineTo(19.5, 18.5),
	}}
	panelIconRefresh = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(19.5, 8), ui.CubicTo(17.8, 5.2, 14.7, 3.5, 11.3, 4.1),
		ui.CubicTo(7.2, 4.8, 4, 8, 3.5, 12), ui.MoveTo(19.5, 4.5), ui.LineTo(19.5, 8),
		ui.LineTo(16, 8), ui.MoveTo(4.5, 16), ui.CubicTo(6.2, 18.8, 9.3, 20.5, 12.7, 19.9),
		ui.CubicTo(16.8, 19.2, 20, 16, 20.5, 12), ui.MoveTo(4.5, 19.5),
		ui.LineTo(4.5, 16), ui.LineTo(8, 16),
	}}
	panelIconCopy = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(8, 8), ui.LineTo(18, 8), ui.QuadTo(20, 8, 20, 10), ui.LineTo(20, 20),
		ui.QuadTo(20, 22, 18, 22), ui.LineTo(8, 22), ui.QuadTo(6, 22, 6, 20),
		ui.LineTo(6, 10), ui.QuadTo(6, 8, 8, 8),
		ui.MoveTo(4, 16), ui.QuadTo(2, 16, 2, 14), ui.LineTo(2, 4), ui.QuadTo(2, 2, 4, 2),
		ui.LineTo(14, 2), ui.QuadTo(16, 2, 16, 4), ui.MoveTo(16, 4), ui.LineTo(16, 6),
	}}
	panelIconBack = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(19, 12), ui.LineTo(5, 12), ui.MoveTo(11, 6), ui.LineTo(5, 12), ui.LineTo(11, 18),
	}}
	panelIconFile = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(6, 3.5), ui.LineTo(14, 3.5), ui.LineTo(19, 8.5), ui.LineTo(19, 20.5),
		ui.LineTo(6, 20.5), ui.ClosePath(), ui.MoveTo(14, 3.5), ui.LineTo(14, 8.5),
		ui.LineTo(19, 8.5),
	}}
	panelIconCheck = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
		ui.MoveTo(5, 12.5), ui.LineTo(9.5, 17), ui.LineTo(19, 7),
	}}
)

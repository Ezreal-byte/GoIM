package desktop

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"

	"goim/internal/buildinfo"
	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/selfupdate"
)

type desktopRuntime struct {
	messageSound        *ringtoneSession
	geometry            windowLayoutStore
	ui                  *UI
	backend             domain.Backend
	application         *app.Application
	post                func(func())
	window              *app.Window
	mainCanvas          gfx.Canvas
	conversationWindows map[string]*detachedConversationWindow
	printWindows        map[string]*printWindowEntry
	historyWindows      map[string]*historyWindowEntry
	historyPending      string
	tray                *app.StatusItem
	seen                map[string]bool
	heardTransfers      map[string]bool
	badge               int
	trayRevision        uint64
	trayPrivate         bool
	traySummary         string
	trayHover           bool
	trayPanelHover      bool
	trayPanel           *unreadTrayPanel
	trayReminderPaused  bool
	trayLanguage        string
	global              string
	globalRejected      string
	stopGlobal          func()
	screenshotGlobal    string
	screenshotRejected  string
	stopScreenshot      func()
	notificationError   bool
	trayNormal          app.StatusIcon
	trayFlash           app.StatusIcon
	flashTimer          *time.Timer
	flashDue            time.Time
	flashUntil          time.Time
	flashOn             bool
	screenshotRunning   atomic.Bool
	screenshotContext   context.Context
	viewerWindow        *app.Window
	viewerWake          app.Invalidator
	viewerPending       bool
	viewerSavePath      string
	viewerSaving        bool
	windowRevision      uint64
	detachedRevision    uint64
	voiceWindow         *app.Window
	voiceView           *UI
	voiceDismissedID    string
	voicePopupFailed    string
	systemInvitation    *systemCallInvitation
}

type detachedConversationWindow struct {
	id              string
	ui              *UI
	window          *app.Window
	restoreOnUnlock bool
}

// Run owns the native window and optional tray. The same lifecycle is exercised
// by the vivahead runner without opening a real window or using the OS tray.
func Run(backend domain.Backend, restartArguments []string) error {
	defer registerEmojiFallback()()
	shell := newUIShell(backend, false)
	defer shell.Close()
	screenshotContext, stopCaptures := context.WithCancel(context.Background())
	defer stopCaptures()
	icon, statusIcon := windowIcon(), trayIcon()
	r := &desktopRuntime{
		ui: shell, backend: backend, seen: make(map[string]bool), heardTransfers: make(map[string]bool), badge: -1,
		trayNormal: statusIcon, trayFlash: attentionIcon(statusIcon),
		conversationWindows: make(map[string]*detachedConversationWindow),
		printWindows:        make(map[string]*printWindowEntry),
		screenshotContext:   screenshotContext,
	}
	shell.panels.setAutostartProvider(
		func() (bool, error) { return app.AutostartEnabled(selfupdate.AppID) },
		func(enabled bool) error {
			return app.SetAutostart(app.Autostart{
				ID: selfupdate.AppID, Name: "GoIM",
			}, enabled)
		},
	)
	shell.panels.loadAutostart()
	if dataDir, ok := backend.(interface{ DataDir() string }); ok {
		r.geometry = loadWindowGeometry(dataDir.DataDir())
		shell.panels.diagnosticDataDir = dataDir.DataDir
	}
	shell.updater = newUpdateController(
		buildinfo.Current().Version,
		buildinfo.UpdatePublicKey,
		!shell.state.Settings.DisableAutoUpdateCheck,
		shell.invalidator.Invalidate,
	)
	shell.screenshotActive = r.screenshotRunning.Load
	shell.restartArguments = restartArguments
	defer shell.updater.stop()
	for _, t := range shell.state.Transfers {
		if t.Direction == "receive" && (t.Status == "offered" || t.Status == "completed") {
			r.heardTransfers[t.ID] = true
		}
	}
	for _, m := range shell.state.Messages {
		r.seen[m.ID] = true
	}
	defer func() {
		if r.flashTimer != nil {
			r.flashTimer.Stop()
		}
		if r.stopGlobal != nil {
			r.stopGlobal()
		}
		if r.stopScreenshot != nil {
			r.stopScreenshot()
		}
		if r.tray != nil {
			r.tray.Remove()
		}
	}()
	return app.RunApplication(func(application *app.Application) error {
		r.application = application
		r.post = application.Post
		if headlessApplication() {
			// Viva's headless host executes Application.Post synchronously.
			// Its driver serializes Invalidator.Post with simulated inputs.
			r.post = func(fn func()) { shell.invalidator.Post(fn) }
		}
		shell.openDragPreview = func(config app.DragPreviewConfig) (conversationDragPreview, error) {
			return application.OpenDragPreview(config)
		}
		supported := app.SetInterfaceFontFamily(strings.TrimSpace(shell.state.Settings.InterfaceFont))
		shell.panels.settingsExtra.interfaceFont.supportKnown = true
		shell.panels.settingsExtra.interfaceFont.supported = supported
		shell.requestScreenshot = r.launchScreenshot
		shell.wakeViewer = r.viewerWake.Invalidate
		shell.presentViewer = func() {
			r.viewerPending = true
			shell.invalidator.Invalidate()
		}
		shell.presentDetached = r.openDetachedConversation
		shell.presentHistory = r.requestHistoryWindow
		shell.panels.presentPrint = r.openPrintWindow
		shell.dismissViewer = r.closeViewer
		shell.pinViewer = r.pinViewer
		shell.saveViewer = r.requestSaveViewer
		shell.fullscreenViewer = r.fullscreenViewer
		config := shell.Config()
		config.Icon = icon
		config.Class = "GoIM.Desktop"
		config.OnMinimize = func(minimized bool) { shell.SetWindowVisible(!minimized && (r.window == nil || !r.window.Hidden())) }
		closed := config.OnClosed
		config.OnClosed = func() {
			r.cancelSystemInvitation()
			if r.messageSound != nil {
				r.messageSound.stop()
			}
			if r.voiceWindow != nil {
				r.voiceWindow.Close()
			}
			r.closeViewer()
			r.closeUnreadTrayPanel()
			r.closeDetachedConversations()
			r.closePrintWindows()
			r.closeHistoryWindows()
			if closed != nil {
				closed()
			}
		}
		wake := config.OnWake
		config.OnWake = func() {
			if r.window != nil {
				shell.SetWindowVisible(!r.window.Hidden() && !r.window.Minimized())
			}
			if wake != nil {
				wake()
			}
			if r.trayPanel != nil {
				r.trayPanel.wake.Invalidate()
			}
			if shell.update.quit || shell.state.DataResetPending {
				application.Quit()
				return
			}
			r.observe()
			r.tickAttention(time.Now())
			r.syncViewer()
		}
		config.OnCloseRequest = func(allow func()) {
			if shell.state.Settings.CloseToTray && r.tray != nil {
				shell.SetWindowVisible(false)
				r.window.Hide()
				return
			}
			if hasActiveTransfers(shell.state.Transfers) && !shell.state.Locked {
				shell.confirm = confirmation{open: true, title: i18n.T("退出 GoIM？"), body: i18n.T("进行中的文件传输将中断，重新连接后可继续。"), action: application.Quit}
				shell.invalidator.Invalidate()
				return
			}
			allow()
		}
		window, err := application.OpenWindow(config, &r.mainCanvas)
		if err != nil {
			return err
		}
		r.window = window
		shell.attachNativeWindow(window)
		shell.unlockForVoice = func() { window.Show(); shell.SetWindowVisible(true) }
		// Background work can finish between constructing the UI and binding its
		// invalidator to a native window. Wake once after binding to collect it.
		shell.invalidator.Invalidate()
		show := func() { r.stopAttention(); window.Show(); shell.SetWindowVisible(true) }
		trayConfig := app.StatusItemConfig{Tooltip: i18n.T("GoIM · 局域网即时通讯"), Icon: statusIcon, OnClick: func(click app.StatusClick) {
			if click.Button == gfx.PointerButtonPrimary && r.openUnreadTrayFromClick(click) {
				return
			}
			show()
		}, Menu: func() []app.MenuItem {
			items := append(r.voiceTrayItems(), r.unreadTrayItems()...)
			return append(items, app.MenuItem{Title: i18n.T("打开 GoIM"), OnSelect: show}, app.MenuItem{Separator: true}, app.MenuItem{Title: i18n.T("退出 GoIM"), OnSelect: func() {
				if hasActiveTransfers(shell.state.Transfers) && !shell.state.Locked {
					show()
					shell.confirm = confirmation{open: true, title: i18n.T("退出 GoIM？"), body: i18n.T("进行中的文件传输将中断，重新连接后可继续。"), action: application.Quit}
					shell.invalidator.Invalidate()
				} else {
					application.Quit()
				}
			}})
		}}
		tray, err := application.AddStatusItem(trayConfig)
		if runtime.GOOS == "linux" && errors.Is(err, app.ErrNoStatusArea) {
			trayConfig.LinuxTrayBackend = app.LinuxTrayXEmbed
			tray, err = application.AddStatusItem(trayConfig)
		}
		if err == nil {
			r.tray = tray
		} else if shell.state.Settings.CloseToTray {
			backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("系统托盘暂不可用，关闭主窗口将退出应用：") + err.Error()})
		}
		r.observe()
		var updateQueued atomic.Bool
		go shell.watchUpdatesWith(func() {
			if updateQueued.Swap(true) {
				return
			}
			r.post(func() {
				updateQueued.Store(false)
				select {
				case <-shell.done:
					return
				default:
				}
				shell.refresh()
				shell.voiceAudio.sync(shell.state.VoiceCall, shell.state.Settings)
				r.observe()
				r.tickAttention(time.Now())
				shell.invalidator.Invalidate()
			})
		})
		return nil
	})
}

func (r *desktopRuntime) observe() {
	if r.window == nil {
		return
	}
	s := r.ui.state
	locked := r.ui.lockVisible()
	if r.historyPending != "" {
		id := r.historyPending
		r.historyPending = ""
		r.openHistoryWindow(id)
	}
	for _, entry := range r.historyWindows {
		entry.conceal(locked)
	}
	if locked && r.ui.conversationDrag.preview != nil {
		r.ui.conversationDrag.preview.Close()
		r.ui.conversationDrag = conversationDragState{}
	}
	for _, detached := range r.conversationWindows {
		detached.conceal(locked)
	}
	for _, printWindow := range r.printWindows {
		printWindow.conceal(locked)
	}
	if s.Revision != r.windowRevision {
		r.windowRevision = s.Revision
		for _, entry := range r.historyWindows {
			entry.view.wake.Invalidate()
		}
		for _, detached := range r.conversationWindows {
			detached.ui.invalidator.Invalidate()
		}
		for _, printWindow := range r.printWindows {
			printWindow.view.wake.Invalidate()
		}
	}
	r.applyGlobal(s)
	r.syncVoiceWindow(s, locked)
	attention := false
	unread := 0
	for _, c := range s.Conversations {
		unread += c.Unread
	}
	if unread == 0 {
		r.stopAttention()
	}
	if unread != r.badge {
		r.badge = unread
		text := ""
		if unread > 0 {
			text = strconv.Itoa(unread)
		}
		r.application.SetDockBadge(text)
	}
	mainForeground := r.ui.foreground(r.window)
	for _, m := range s.Messages {
		if r.seen[m.ID] {
			continue
		}
		r.seen[m.ID] = true
		if m.SenderID == s.Self.ID || m.Kind == "system" {
			continue
		}
		conv := s.Conversation(m.ConversationID)
		if conv.Muted || conv.Hidden {
			continue
		}
		if !s.Locked && !r.window.Hidden() && !r.window.Minimized() && mainForeground &&
			r.ui.section == sectionMessages && r.ui.selected == m.ConversationID && !r.ui.modalOpen() {
			continue
		}
		if detached := r.conversationWindows[m.ConversationID]; detached != nil &&
			detached.ui.foreground(detached.window) &&
			!detached.window.Hidden() && !detached.window.Minimized() {
			continue
		}
		attention = true
		name := s.Peer(m.SenderID).Name
		if m.Nickname != "" {
			name = m.Nickname
		}
		body := m.Text
		if m.Kind == "file" {
			body = i18n.T("[文件] ") + body
		}
		if m.Kind == "print" {
			body = i18n.T("[打印] ") + body
		}
		if m.Kind == "image" {
			body = i18n.T("[图片]")
		}
		if len([]rune(body)) > 100 {
			body = string([]rune(body)[:100]) + "…"
		}
		if conv.Kind == "group" {
			name = conv.Title + " · " + name
		}
		if s.Locked || s.Settings.HideNotificationText {
			name, body = "GoIM", i18n.T("收到一条新消息")
		}
		if s.Settings.DesktopNotifications {
			if err := app.SendNotification(app.Notification{Title: name, Body: body}); err != nil && !r.notificationError {
				r.notificationError = true
				r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("系统通知暂不可用：") + err.Error()})
			}
		}
	}
	resumeReminder := attention && unread > 0 && r.trayReminderPaused
	if resumeReminder {
		r.trayReminderPaused = false
	}
	r.syncUnreadTray(unread, resumeReminder)
	transferAttention := false
	var done, total int64
	for _, t := range s.Transfers {
		if r.transferNeedsChime(&s, t) {
			transferAttention = true
		}
		if t.Status == "transferring" || t.Status == "verifying" {
			done += t.Done
			total += t.Size
		}
	}
	if total > 0 {
		r.application.SetProgress(float32(done) / float32(total))
	} else {
		r.application.SetProgress(-1)
	}
	if (attention || transferAttention) && s.Settings.Sound && !s.VoiceCall.Live() {
		r.playMessageChime(s.Settings)
	}
	if attention && unread > 0 {
		r.startAttention(time.Now())
	}
}

func parseGlobal(keys string) (app.GlobalShortcut, error) {
	var s app.GlobalShortcut
	parts := strings.Split(keys, "+")
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			s.Modifiers |= gfx.ModifierControl
		case "alt", "option":
			s.Modifiers |= gfx.ModifierAlt
		case "shift":
			s.Modifiers |= gfx.ModifierShift
		case "cmd", "command", "meta", "win", "super":
			s.Modifiers |= gfx.ModifierMeta
		default:
			if s.Key != "" {
				return s, errors.New(i18n.T("无效的快捷键组合"))
			}
			s.Key = strings.TrimSpace(part)
		}
	}
	return s, nil
}
func (r *desktopRuntime) applyGlobal(s domain.State) {
	desired := ""
	screenshot := ""
	for _, key := range s.Settings.Shortcuts {
		switch key.Action {
		case "显示或隐藏窗口":
			desired = key.Keys
		case domain.ShortcutScreenshot:
			screenshot = key.Keys
		}
	}
	r.applyGlobalShortcut("显示或隐藏窗口", desired, &r.global, &r.globalRejected, &r.stopGlobal, func() {
		if r.window.Hidden() || r.window.Minimized() {
			r.stopAttention()
			r.window.Show()
			r.ui.SetWindowVisible(true)
		} else {
			r.window.Hide()
			r.ui.SetWindowVisible(false)
		}
	})
	r.applyGlobalShortcut(domain.ShortcutScreenshot, screenshot,
		&r.screenshotGlobal, &r.screenshotRejected, &r.stopScreenshot, func() { r.launchScreenshot() })
}

func (r *desktopRuntime) applyGlobalShortcut(action, desired string, current, rejected *string,
	stopCurrent *func(), callback func()) {
	if strings.EqualFold(desired, *current) {
		*rejected = ""
		return
	}
	if desired != "" && strings.EqualFold(desired, *rejected) {
		return
	}
	if desired == "" {
		*rejected = ""
		if *stopCurrent != nil {
			(*stopCurrent)()
			*stopCurrent = nil
		}
		*current = ""
		return
	}
	shortcut, err := parseGlobal(desired)
	var stop func()
	if err == nil {
		stop, err = app.RegisterGlobalShortcut(shortcut, callback)
	}
	if err != nil {
		*rejected = desired
		detail := i18n.T(action) + " · " + desired
		if *current != "" {
			detail += " · " + i18n.T("原快捷键仍可用")
		}
		r.ui.showToast(i18n.T("无法注册全局快捷键"), detail, components.ToastWarning)
		return
	}
	if *stopCurrent != nil {
		(*stopCurrent)()
	}
	*stopCurrent = stop
	*current = desired
	*rejected = ""
}

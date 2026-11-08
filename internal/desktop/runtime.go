package desktop

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"image/png"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
)

//go:embed assets/icon.png
var applicationIcon []byte

type desktopRuntime struct {
	ui                *UI
	backend           domain.Backend
	application       *app.Application
	window            *app.Window
	tray              *app.StatusItem
	seen              map[string]bool
	badge             int
	global            string
	stopGlobal        func()
	screenshotGlobal  string
	stopScreenshot    func()
	notificationError bool
}

func decodeApplicationIcon() (app.WindowIcon, error) {
	picture, err := png.Decode(bytes.NewReader(applicationIcon))
	if err != nil {
		return app.WindowIcon{}, err
	}
	bounds := picture.Bounds()
	pixels := make([]byte, bounds.Dx()*bounds.Dy()*4)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := picture.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			i := (y*bounds.Dx() + x) * 4
			pixels[i] = uint8(r >> 8)
			pixels[i+1] = uint8(g >> 8)
			pixels[i+2] = uint8(b >> 8)
			pixels[i+3] = uint8(a >> 8)
		}
	}
	return app.WindowIcon{Size: bounds.Dx(), RGBA: pixels}, nil
}

// Run owns the native window and optional tray. The same lifecycle is exercised
// by the vivahead runner without opening a real window or using the OS tray.
func Run(backend domain.Backend) error {
	shell := New(backend)
	defer shell.Close()
	icon, err := decodeApplicationIcon()
	if err != nil {
		return err
	}
	r := &desktopRuntime{ui: shell, backend: backend, seen: make(map[string]bool), badge: -1}
	for _, m := range shell.state.Messages {
		r.seen[m.ID] = true
	}
	defer func() {
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
		config := shell.Config()
		config.Icon = icon
		config.Class = "GoIM.Desktop"
		config.OnMinimize = func(minimized bool) { shell.SetWindowVisible(!minimized && (r.window == nil || !r.window.Hidden())) }
		wake := config.OnWake
		config.OnWake = func() {
			if r.window != nil {
				shell.SetWindowVisible(!r.window.Hidden() && !r.window.Minimized())
			}
			if wake != nil {
				wake()
			}
			r.observe()
		}
		config.OnCloseRequest = func(allow func()) {
			if shell.state.Settings.CloseToTray && r.tray != nil {
				shell.SetWindowVisible(false)
				r.window.Hide()
				return
			}
			active := false
			for _, t := range shell.state.Transfers {
				if t.Status == "transferring" || t.Status == "verifying" {
					active = true
				}
			}
			if active && !shell.state.Locked {
				shell.confirm = confirmation{open: true, title: "退出 GoIM？", body: "进行中的文件传输将中断，重新连接后可继续。", action: application.Quit}
				shell.invalidator.Invalidate()
				return
			}
			allow()
		}
		var canvas ui.Canvas
		window, err := application.OpenWindow(config, &canvas)
		if err != nil {
			return err
		}
		r.window = window
		shell.attachNativeWindow(window)
		// Background work can finish between constructing the UI and binding its
		// invalidator to a native window. Wake once after binding to collect it.
		shell.invalidator.Invalidate()
		show := func() { window.Show(); shell.SetWindowVisible(true) }
		tray, err := application.AddStatusItem(app.StatusItemConfig{Tooltip: "GoIM · 局域网即时通讯", Icon: icon.StatusIcon(32), OnClick: func(app.StatusClick) { show() }, Menu: func() []app.MenuItem {
			return []app.MenuItem{{Title: "打开 GoIM", OnSelect: show}, {Separator: true}, {Title: "退出 GoIM", OnSelect: func() {
				active := false
				for _, t := range shell.state.Transfers {
					if t.Status == "transferring" || t.Status == "verifying" {
						active = true
					}
				}
				if active && !shell.state.Locked {
					show()
					shell.confirm = confirmation{open: true, title: "退出 GoIM？", body: "进行中的文件传输将中断，重新连接后可继续。", action: application.Quit}
					shell.invalidator.Invalidate()
				} else {
					application.Quit()
				}
			}}}
		}})
		if err == nil {
			r.tray = tray
		} else if shell.state.Settings.CloseToTray {
			backend.Submit(domain.Command{Kind: domain.ReportError, Text: "系统托盘暂不可用，关闭主窗口将退出应用：" + err.Error()})
		}
		r.observe()
		return nil
	})
}

func (r *desktopRuntime) observe() {
	s := r.ui.state
	r.applyGlobal(s)
	unread := 0
	for _, c := range s.Conversations {
		unread += c.Unread
	}
	if unread != r.badge {
		r.badge = unread
		text := ""
		if unread > 0 {
			text = strconv.Itoa(unread)
		}
		r.application.SetDockBadge(text)
		if r.tray != nil {
			r.tray.SetTooltip(fmt.Sprintf("GoIM · %d 条未读消息", unread))
		}
	}
	var done, total int64
	for _, t := range s.Transfers {
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
	for _, m := range s.Messages {
		if r.seen[m.ID] {
			continue
		}
		r.seen[m.ID] = true
		if m.SenderID == s.Self.ID {
			continue
		}
		conv := s.Conversation(m.ConversationID)
		if conv.Muted {
			continue
		}
		if !s.Locked && !r.window.Hidden() && !r.window.Minimized() && !r.ui.isPanelSection() && r.ui.selected == m.ConversationID {
			continue
		}
		name := s.Peer(m.SenderID).Name
		if m.Nickname != "" {
			name = m.Nickname
		}
		body := m.Text
		if m.Kind == "file" {
			body = "[文件] " + body
		}
		if m.Kind == "image" {
			body = "[图片]"
		}
		if len([]rune(body)) > 100 {
			body = string([]rune(body)[:100]) + "…"
		}
		if conv.Kind == "group" {
			name = conv.Title + " · " + name
		}
		if s.Locked || s.Settings.HideNotificationText {
			name, body = "GoIM", "收到一条新消息"
		}
		if s.Settings.DesktopNotifications {
			if err := app.SendNotification(app.Notification{Title: name, Body: body}); err != nil && !r.notificationError {
				r.notificationError = true
				r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: "系统通知暂不可用：" + err.Error()})
			}
		}
		if s.Settings.Sound {
			_ = app.PlaySystemSound(app.SoundInformation)
		}
	}
}

func parseGlobal(keys string) (app.GlobalShortcut, error) {
	var s app.GlobalShortcut
	parts := strings.Split(keys, "+")
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			s.Modifiers |= ui.ModifierControl
		case "alt", "option":
			s.Modifiers |= ui.ModifierAlt
		case "shift":
			s.Modifiers |= ui.ModifierShift
		case "cmd", "command", "meta", "win", "super":
			s.Modifiers |= ui.ModifierMeta
		default:
			if s.Key != "" {
				return s, fmt.Errorf("无效的快捷键组合")
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
	r.applyGlobalShortcut("显示或隐藏窗口", desired, &r.global, &r.stopGlobal, func() {
		if r.window.Hidden() || r.window.Minimized() {
			r.window.Show()
			r.ui.SetWindowVisible(true)
		} else {
			r.window.Hide()
			r.ui.SetWindowVisible(false)
		}
	})
	r.applyGlobalShortcut(domain.ShortcutScreenshot, screenshot,
		&r.screenshotGlobal, &r.stopScreenshot, func() { r.launchScreenshot() })
}

func (r *desktopRuntime) applyGlobalShortcut(action, desired string, current *string,
	stopCurrent *func(), callback func()) {
	if strings.EqualFold(desired, *current) {
		return
	}
	if desired == "" {
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
		keys := append([]domain.Shortcut(nil), r.ui.state.Settings.Shortcuts...)
		for i := range keys {
			if keys[i].Action == action {
				keys[i].Keys = *current
			}
		}
		data, _ := json.Marshal(keys)
		r.backend.Submit(domain.Command{Kind: domain.SetSetting, Setting: "shortcuts", Value: string(data)})
		r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: action + "快捷键不可用，已保留之前的设置：" + err.Error()})
		return
	}
	if *stopCurrent != nil {
		(*stopCurrent)()
	}
	*stopCurrent = stop
	*current = desired
}

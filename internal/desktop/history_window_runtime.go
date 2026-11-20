package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type historyWindowEntry struct {
	view                       *historyWindowView
	window                     *app.Window
	restoreOnUnlock, concealed bool
}

func (r *desktopRuntime) requestHistoryWindow(id string) bool {
	if r.ui.lockVisible() {
		return false
	}
	r.historyPending = id
	r.ui.invalidator.Invalidate()
	return true
}

func (r *desktopRuntime) openHistoryWindow(id string) bool {
	if r.application == nil || r.backend == nil || r.ui.lockVisible() {
		return false
	}
	if r.historyWindows == nil {
		r.historyWindows = make(map[string]*historyWindowEntry)
	}
	if entry := r.historyWindows[id]; entry != nil {
		entry.view.focus = true
		entry.window.Show()
		entry.view.wake.Invalidate()
		return true
	}
	v := newHistoryWindowView(r.backend, id)
	v.avatar = r.ui.paintPrintAvatar
	v.locate = r.locateHistoryMessage
	v.close = func() {
		if entry := r.historyWindows[id]; entry != nil {
			entry.window.Close()
		}
	}
	config := v.Config()
	config.Icon = windowIcon()
	config.Class = "GoIM.ChatHistory"
	var window *app.Window
	config.OnClosed = func() { delete(r.historyWindows, id); v.Close() }
	config.OnWake = func() {
		v.refresh()
		if window != nil {
			_ = window.SetTitle(v.title)
		}
	}
	r.geometry.remember("history", &config)
	var canvas gfx.Canvas
	opened, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		v.Close()
		r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法打开聊天记录窗口：") + err.Error()})
		return false
	}
	window = opened
	r.historyWindows[id] = &historyWindowEntry{view: v, window: opened}
	opened.Show()
	return true
}

func (r *desktopRuntime) locateHistoryMessage(conversationID, messageID string) {
	if r.ui.lockVisible() {
		return
	}
	target := r.ui
	if entry := r.conversationWindows[conversationID]; entry != nil {
		target = entry.ui
		entry.window.Show()
	} else if r.window != nil {
		r.window.Show()
		target.SetWindowVisible(true)
	}
	target.locateChatMessage(conversationID, messageID)
}

func (u *UI) locateChatMessage(conversationID, messageID string) {
	if u.lockVisible() {
		return
	}
	u.selectConversation(conversationID)
	u.closeDrawer()
	u.closeChatSearch()
	u.chatFind.restorePending = false
	u.chatScrollToEnd = false
	u.locateMessageID = messageID
	u.highlightMessageID = messageID
	u.invalidator.Invalidate()
}

func (entry *historyWindowEntry) conceal(locked bool) {
	if entry.concealed == locked {
		return
	}
	entry.concealed = locked
	if locked {
		entry.restoreOnUnlock = !entry.window.Hidden()
		entry.window.Hide()
	} else if entry.restoreOnUnlock {
		entry.restoreOnUnlock = false
		entry.view.refresh()
		entry.window.Show()
	}
}

func (r *desktopRuntime) closeHistoryWindows() {
	windows := make([]*historyWindowEntry, 0, len(r.historyWindows))
	for _, entry := range r.historyWindows {
		windows = append(windows, entry)
	}
	for _, entry := range windows {
		entry.window.Close()
	}
}

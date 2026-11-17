package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (r *desktopRuntime) openDetachedConversation(conversationID string) bool {
	if r.application == nil || r.backend == nil || r.conversationWindows == nil {
		return false
	}
	if existing := r.conversationWindows[conversationID]; existing != nil {
		existing.window.Show()
		return true
	}

	detached := newDetachedConversationView(r.backend, conversationID)
	config := detached.DetachedConfig()
	config.Icon = windowIcon()
	config.Class = "GoIM.Conversation"
	var window *app.Window
	baseClosed := config.OnClosed
	config.OnClosed = func() {
		if entry := r.conversationWindows[conversationID]; entry != nil && entry.window == window {
			delete(r.conversationWindows, conversationID)
			r.ui.restoreDetachedConversation(conversationID)
		}
		if baseClosed != nil {
			baseClosed()
		}
	}

	var canvas gfx.Canvas
	opened, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		detached.Close()
		r.backend.Submit(domain.Command{
			Kind: domain.ReportError,
			Text: i18n.T("无法打开独立会话窗口：") + err.Error(),
		})
		return false
	}
	window = opened
	detached.attachNativeWindow(window)
	r.conversationWindows[conversationID] = &detachedConversationWindow{
		id: conversationID, ui: detached, window: opened,
	}
	opened.Show()
	return true
}

func (r *desktopRuntime) closeDetachedConversations() {
	if len(r.conversationWindows) == 0 {
		return
	}
	windows := make([]*detachedConversationWindow, 0, len(r.conversationWindows))
	for _, entry := range r.conversationWindows {
		windows = append(windows, entry)
	}
	for _, entry := range windows {
		entry.window.Close()
	}
}

package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (r *desktopRuntime) openDetachedConversation(conversationID string) bool {
	if r.application == nil || r.backend == nil || r.conversationWindows == nil || r.ui.lockVisible() {
		return false
	}
	if existing := r.conversationWindows[conversationID]; existing != nil {
		existing.ui.focusChat = true
		existing.ui.invalidator.Invalidate()
		existing.window.Show()
		return true
	}

	detached := newDetachedConversationView(r.backend, conversationID)
	detached.forwardViewer = func(fileID string) { r.ui.openConversationMedia(fileID, conversationID) }
	detached.presentHistory = r.requestHistoryWindow
	detached.presentMemberChat = r.showPeerTarget
	detached.presentArchiveTarget = r.showPeerTarget
	config := detached.DetachedConfig()
	config.Icon = windowIcon()
	config.Class = "GoIM.Conversation"
	var window *app.Window
	baseWake := config.OnWake
	config.OnWake = func() {
		baseWake()
		if entry := r.conversationWindows[conversationID]; entry != nil && detached.state.Locked {
			entry.conceal(true)
		}
	}
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

	r.geometry.remember("conversation", &config)
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

func (r *desktopRuntime) showPeerTarget(peerID string) {
	if r.ui.lockVisible() {
		return
	}
	if peerID != "" {
		if entry := r.conversationWindows[r.ui.directByPeer[peerID]]; entry != nil {
			entry.ui.focusChat = true
			entry.ui.invalidator.Invalidate()
			entry.window.Show()
			return
		}
	}
	r.window.Show()
	r.ui.openArchiveTarget(peerID)
	r.ui.invalidator.Invalidate()
}

func (entry *detachedConversationWindow) conceal(locked bool) {
	if entry.ui.detachedConcealed == locked {
		return
	}
	if locked {
		entry.restoreOnUnlock = !entry.window.Hidden()
		entry.window.Hide()
	} else {
		entry.ui.refresh()
		if entry.ui.state.Locked {
			return
		}
	}
	entry.ui.concealConversation(locked)
	_ = entry.window.SetTitle(entry.ui.detachedTitle())
	if !locked && entry.restoreOnUnlock {
		entry.restoreOnUnlock = false
		entry.window.Show()
	}
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

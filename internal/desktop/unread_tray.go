package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"goim/internal/domain"
	"goim/internal/i18n"
)

func (r *desktopRuntime) unreadTrayItems() []app.MenuItem {
	u := r.ui
	if u.lockVisible() || u.state.Settings.HideNotificationText {
		return nil
	}
	var chats []domain.Conversation
	groups := make(map[string]string)
	for _, c := range u.state.Conversations {
		if !trayConversationVisible(c) {
			continue
		}
		chats = append(chats, c)
		if c.Kind == "group" {
			groups[c.ID] = ""
		}
	}
	if len(chats) == 0 {
		return nil
	}
	remaining := len(groups)
	for i := len(u.state.Messages) - 1; i >= 0 && remaining > 0; i-- {
		m := u.state.Messages[i]
		if _, wanted := groups[m.ConversationID]; !wanted || groups[m.ConversationID] != "" || m.Deleted || m.Recalled || m.Kind == "system" || m.SenderID == u.state.Self.ID {
			continue
		}
		sender := u.state.Peer(m.SenderID)
		name := sender.Remark
		if name == "" {
			name = m.Nickname
		}
		if name == "" {
			name = sender.Name
		}
		if name != "" {
			groups[m.ConversationID] = name
			remaining--
		}
	}
	slices.SortStableFunc(chats, func(a, b domain.Conversation) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	items := make([]app.MenuItem, 0, len(chats)+1)
	for _, c := range chats {
		title := u.conversationTitle(c)
		if sender := groups[c.ID]; sender != "" {
			title += " · " + sender
		}
		title = strings.Join(strings.Fields(title), " ")
		items = append(items, app.MenuItem{ID: "unread-" + c.ID, Title: fmt.Sprintf(i18n.T("%s（%d 条未读）"), title, c.Unread), OnSelect: func() { r.openUnreadChat(c.ID) }})
	}
	return append(items, app.MenuItem{Separator: true})
}

func trayConversationVisible(c domain.Conversation) bool {
	return c.ID != "" && c.Unread > 0 && !c.Muted && !c.Hidden
}

func (r *desktopRuntime) firstUnreadChat() string {
	u := r.ui
	if u.lockVisible() || u.state.Settings.HideNotificationText {
		return ""
	}
	var latest domain.Conversation
	for _, c := range u.state.Conversations {
		if trayConversationVisible(c) && (latest.ID == "" || c.UpdatedAt.After(latest.UpdatedAt)) {
			latest = c
		}
	}
	return latest.ID
}

func (r *desktopRuntime) openUnreadChat(id string) {
	u := r.ui
	if u.lockVisible() || u.state.Settings.HideNotificationText || !trayConversationVisible(u.state.Conversation(id)) {
		return
	}
	r.stopAttention()
	if detached := r.conversationWindows[id]; detached != nil {
		detached.window.Show()
		detached.ui.SetWindowVisible(true)
		return
	}
	if r.window != nil {
		r.window.Show()
	}
	u.SetWindowVisible(true)
	if u.modalOpen() || !r.mainCanvas.TrappedFocusScope().IsZero() {
		return
	}
	u.setSection(sectionMessages)
	if u.section == sectionMessages {
		u.selectConversation(id)
	}
	u.invalidator.Invalidate()
}

func (r *desktopRuntime) syncUnreadTray(unread int) {
	if r.tray == nil {
		return
	}
	u := r.ui
	private := u.lockVisible() || u.state.Settings.HideNotificationText
	lang := i18n.Language()
	if r.trayLanguage == lang && r.trayRevision == u.state.Revision && r.trayPrivate == private {
		return
	}
	r.trayRevision, r.trayPrivate, r.trayLanguage = u.state.Revision, private, lang
	summary := i18n.T("GoIM · 局域网即时通讯")
	if unread > 0 {
		summary = fmt.Sprintf(i18n.T("GoIM · %d 条未读消息"), unread)
	}
	var items []app.MenuItem
	if unread > 0 && !private {
		items = r.unreadTrayItems()
	}
	for i := 0; i < min(3, len(items)); i++ {
		if !items[i].Separator {
			summary += "\n" + items[i].Title
		}
	}
	if summary != r.traySummary {
		r.tray.SetTooltip(summary)
		r.traySummary = summary
	}
	hover := len(items) > 0
	if hover != r.trayHover && r.tray.SetMenuOnHover(hover) {
		r.trayHover = hover
	}
	r.tray.RefreshMenu()
}

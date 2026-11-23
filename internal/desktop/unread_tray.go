package desktop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type unreadTrayChat struct {
	id, title, sender, avatar string
	count, label              string
}

func (u *UI) unreadTrayChats() []unreadTrayChat {
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
	entries := make([]unreadTrayChat, 0, len(chats))
	for _, c := range chats {
		avatar := u.state.Peer(c.PeerID).Avatar
		if c.Kind == "group" {
			avatar = u.state.Group(c.GroupID).Avatar
		}
		title := strings.Join(strings.Fields(u.conversationTitle(c)), " ")
		sender := strings.Join(strings.Fields(groups[c.ID]), " ")
		menuTitle := title
		if sender != "" {
			menuTitle += " · " + sender
		}
		entries = append(entries, unreadTrayChat{id: c.ID, title: title, sender: sender, avatar: avatar,
			count: intString(c.Unread), label: fmt.Sprintf(i18n.T("%s（%d 条未读）"), menuTitle, c.Unread)})
	}
	return entries
}

func (r *desktopRuntime) unreadTrayItems() []app.MenuItem {
	entries := r.ui.unreadTrayChats()
	if len(entries) == 0 {
		return nil
	}
	items := make([]app.MenuItem, 0, len(entries)+2)
	if !r.trayPanelHover && !r.trayReminderPaused {
		items = append(items, app.MenuItem{ID: "goim-unread-panel", Title: i18n.T("查看未读消息"), OnSelect: func() {
			r.openUnreadTrayPanel(app.StatusAnchor{}, app.WindowPanel)
		}})
	}
	for _, c := range entries {
		items = append(items, app.MenuItem{ID: "unread-" + c.id, Title: c.label, OnSelect: func() { r.openUnreadChat(c.id) }})
	}
	return append(items, app.MenuItem{Separator: true})
}

func (r *desktopRuntime) openUnreadTrayFromClick(click app.StatusClick) bool {
	if click.PanelDismissed {
		return true
	}
	// Activation gives mouse and keyboard users the same list as hovering.
	return r.openUnreadTrayPanel(click.Anchor, app.WindowPanel) != nil
}

func trayConversationVisible(c domain.Conversation) bool {
	return c.ID != "" && c.Unread > 0 && !c.Muted && !c.Hidden
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

func (r *desktopRuntime) dismissUnreadTrayReminder() {
	r.trayReminderPaused = true
	r.stopAttention()
	if r.messageSound != nil {
		r.messageSound.stop()
		r.messageSound = nil
	}
	r.closeUnreadTrayPanel()
	r.syncUnreadTray(r.badge, true)
}

func (r *desktopRuntime) syncUnreadTray(unread int, force bool) {
	if r.tray == nil {
		return
	}
	u := r.ui
	private := u.lockVisible() || u.state.Settings.HideNotificationText
	lang := i18n.Language()
	if !force && r.trayLanguage == lang && r.trayRevision == u.state.Revision && r.trayPrivate == private {
		return
	}
	r.trayRevision, r.trayPrivate, r.trayLanguage = u.state.Revision, private, lang
	summary := i18n.T("GoIM · 局域网即时通讯")
	var entries []unreadTrayChat
	if unread > 0 && !r.trayReminderPaused {
		summary = fmt.Sprintf(i18n.T("GoIM · %d 条未读消息"), unread)
		entries = u.unreadTrayChats()
	}
	hover := len(entries) > 0
	if hover != r.trayHover {
		var panel func(app.StatusAnchor) *app.Window
		if hover {
			panel = r.showUnreadTrayPanel
		}
		panelSupported := r.tray.SetHoverPanel(panel)
		r.trayPanelHover = hover && panelSupported
		if panelSupported || r.tray.SetMenuOnHover(hover) {
			r.trayHover = hover
		}
	}
	if !r.trayPanelHover {
		for _, entry := range entries[:min(3, len(entries))] {
			summary += "\n" + entry.label
		}
	}
	if summary != r.traySummary {
		r.tray.SetTooltip(summary)
		r.traySummary = summary
	}
	r.updateUnreadTrayPanel(entries)
	r.tray.RefreshMenu()
}

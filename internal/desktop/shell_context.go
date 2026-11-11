package desktop

import (
	"goim/internal/i18n"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	shellMenuConversation = "conversation"
	shellMenuContact      = "contact"
	shellMenuSelf         = "self"
	shellMenuSidebar      = "sidebar"
	shellMenuContactGroup = "contact-group"
	shellMenuMessage      = "message"
	shellMenuAvatar       = "avatar"
)

type shellMenuState struct {
	open   bool
	kind   string
	target string
	x      float32
	y      float32
}

type organizeState struct {
	open    bool
	peerID  string
	field   flex.Field
	errText string
	mode    domain.CommandKind
	group   string
}

func (u *UI) closeShellMenu() {
	u.shellMenu = shellMenuState{}
}

func (u *UI) openShellMenu(kind, target string, x, y float32) {
	u.closeChatPickers()
	u.moreOpen = false
	u.memberMenu = ""
	u.shellMenu = shellMenuState{open: true, kind: kind, target: target, x: x, y: y}
}

func (u *UI) openConversationMenu(conversation domain.Conversation, x, y float32) {
	if conversation.ID == "" {
		return
	}
	u.openShellMenu(shellMenuConversation, conversation.ID, x, y)
}

func (u *UI) openContactMenu(peer domain.Peer, x, y float32) {
	if peer.ID == "" {
		return
	}
	if peer.ID == u.state.Self.ID {
		u.openSelfMenu(x, y)
		return
	}
	u.openShellMenu(shellMenuContact, peer.ID, x, y)
}

func (u *UI) openSelfMenu(x, y float32) {
	if u.state.Self.ID == "" {
		return
	}
	u.openShellMenu(shellMenuSelf, u.state.Self.ID, x, y)
}

func (u *UI) openSidebarMenu(x, y float32) {
	u.openShellMenu(shellMenuSidebar, "", x, y)
}

func (u *UI) drawShellMenu(canvas *ui.Canvas, viewport ui.Rect) {
	if !u.shellMenu.open {
		return
	}
	switch u.shellMenu.kind {
	case shellMenuConversation:
		u.drawConversationContextMenu(canvas, viewport)
	case shellMenuContact:
		u.drawContactContextMenu(canvas, viewport)
	case shellMenuSelf:
		u.drawSelfContextMenu(canvas, viewport)
	case shellMenuSidebar:
		u.drawSidebarContextMenu(canvas, viewport)
	case shellMenuContactGroup:
		u.drawContactGroupContextMenu(canvas, viewport)
	case shellMenuMessage:
		u.drawMessageContextMenu(canvas, viewport)
	case shellMenuAvatar:
		u.drawAvatarContextMenu(canvas, viewport)
	default:
		u.closeShellMenu()
	}
}

func (u *UI) beginShellMenu(canvas *ui.Canvas, viewport ui.Rect, items int, separators int) components.ContextMenu {
	height := float32(12 + items*32 + separators*9)
	return components.BeginContextMenu(canvas, u.theme,
		ui.Rect{X: u.shellMenu.x, Y: u.shellMenu.y, Width: 220, Height: height},
		components.ContextMenuOptions{
			Layer: 67, Scope: u.ids.shellMenuScope, DismissID: u.ids.shellMenuDismiss,
			Viewport: viewport,
		})
}

func (u *UI) drawConversationContextMenu(canvas *ui.Canvas, viewport ui.Rect) {
	conversation := u.state.Conversation(u.shellMenu.target)
	if conversation.ID == "" {
		u.closeShellMenu()
		return
	}
	menu := u.beginShellMenu(canvas, viewport, 4, 1)
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuPin, Label: i18n.T("置顶聊天"), Kind: components.ContextMenuCheckbox,
		Checked: conversation.Pinned, Icon: u.iconPin,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuMute, Label: i18n.T("消息免打扰"), Kind: components.ContextMenuCheckbox,
		Checked: conversation.Muted, Icon: u.iconBellOff,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuRead, Label: i18n.T("标为已读"), Icon: u.iconCheckCheck,
		Disabled: conversation.Unread == 0,
	})
	menu.AddSeparator()
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuDelete, Label: i18n.T("删除会话"), Variant: components.ContextMenuItemDestructive,
		Icon: u.iconTrash,
	})
	menu.End()

	switch {
	case canvas.Clicked(u.ids.shellMenuPin):
		u.closeShellMenu()
		u.submit(domain.Command{Kind: domain.TogglePin, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.shellMenuMute):
		u.closeShellMenu()
		u.submit(domain.Command{Kind: domain.ToggleMute, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.shellMenuRead):
		u.closeShellMenu()
		u.submit(domain.Command{Kind: domain.MarkRead, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.shellMenuDelete):
		u.closeShellMenu()
		u.requestDeleteConversation(conversation)
	case canvas.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) drawContactContextMenu(canvas *ui.Canvas, viewport ui.Rect) {
	peer := u.state.Peer(u.shellMenu.target)
	if peer.ID == "" {
		u.closeShellMenu()
		return
	}
	count := 4
	if peer.Transport == "ipmsg" {
		count--
	}
	menu := u.beginShellMenu(canvas, viewport, count, 0)
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuTalk, Label: i18n.T("发消息"), Icon: u.iconMessage,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuOrganize, Label: i18n.T("移动到分组…"), Icon: u.iconFolder,
	})
	menu.AddItem(components.ContextMenuItem{ID: u.ids.shellMenuProfile, Label: i18n.T("查看资料"), Icon: u.iconUsers})
	if peer.Transport != "ipmsg" {
		menu.AddItem(components.ContextMenuItem{
			ID: u.ids.shellMenuNewGroup, Label: i18n.T("新建群聊"), Icon: u.iconPlus,
		})
	}
	menu.End()

	switch {
	case canvas.Clicked(u.ids.shellMenuTalk):
		u.closeShellMenu()
		u.startDirectFromMembers(peer.ID)
	case canvas.Clicked(u.ids.shellMenuOrganize):
		u.openOrganize(peer.ID)
	case canvas.Clicked(u.ids.shellMenuProfile):
		u.closeShellMenu()
		if u.section == sectionContacts {
			u.showContact(peer.ID)
		} else {
			u.memberProfile = peer.ID
		}
	case canvas.Clicked(u.ids.shellMenuNewGroup):
		u.closeShellMenu()
		u.openCompose("create", "")
	case canvas.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) drawSelfContextMenu(canvas *ui.Canvas, viewport ui.Rect) {
	menu := u.beginShellMenu(canvas, viewport, 1, 0)
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuProfile, Label: i18n.T("编辑个人资料"), Icon: u.iconPencil,
	})
	menu.End()
	switch {
	case canvas.Clicked(u.ids.shellMenuProfile):
		u.closeShellMenu()
		u.openProfile()
	case canvas.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) drawSidebarContextMenu(canvas *ui.Canvas, viewport ui.Rect) {
	items := 2
	if u.section == sectionContacts {
		items = 5
	}
	menu := u.beginShellMenu(canvas, viewport, items, 1)
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuNewGroup, Label: i18n.T("新建群聊"), Icon: u.iconPlus,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.shellMenuConnect, Label: i18n.T("手动连接"), Icon: u.iconUserPlus,
	})
	if u.section == sectionContacts {
		menu.AddSeparator()
		menu.AddItem(components.ContextMenuItem{
			ID: u.ids.shellMenuNewContactGroup, Label: i18n.T("新建联系人分组…"), Icon: u.iconFolder,
		})
		menu.AddItem(components.ContextMenuItem{
			ID: u.ids.shellMenuExpand, Label: i18n.T("展开全部分组"), Icon: u.iconChevronDown,
		})
		menu.AddItem(components.ContextMenuItem{
			ID: u.ids.shellMenuCollapse, Label: i18n.T("折叠全部分组"), Icon: u.iconChevronRight,
		})
	}
	menu.End()

	switch {
	case canvas.Clicked(u.ids.shellMenuNewGroup):
		u.closeShellMenu()
		u.openCompose("create", "")
	case canvas.Clicked(u.ids.shellMenuConnect):
		u.closeShellMenu()
		u.openConnect()
	case canvas.Clicked(u.ids.shellMenuNewContactGroup):
		u.openContactGroupEditor(domain.CreateContactGroup, "")
	case canvas.Clicked(u.ids.shellMenuExpand):
		u.closeShellMenu()
		for name := range u.collapsed {
			delete(u.collapsed, name)
		}
	case canvas.Clicked(u.ids.shellMenuCollapse):
		u.closeShellMenu()
		for _, group := range u.contacts {
			u.collapsed[group.Name] = true
		}
	case canvas.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) drawContactGroupContextMenu(canvas *ui.Canvas, viewport ui.Rect) {
	name := u.shellMenu.target
	menu := u.beginShellMenu(canvas, viewport, 2, 0)
	menu.AddItem(components.ContextMenuItem{ID: u.ids.shellMenuRenameContactGroup, Label: i18n.T("重命名分组…"), Icon: u.iconPencil})
	menu.AddItem(components.ContextMenuItem{ID: u.ids.shellMenuDeleteContactGroup, Label: i18n.T("删除分组"), Variant: components.ContextMenuItemDestructive, Icon: u.iconTrash})
	menu.End()
	switch {
	case canvas.Clicked(u.ids.shellMenuRenameContactGroup):
		u.openContactGroupEditor(domain.RenameContactGroup, name)
	case canvas.Clicked(u.ids.shellMenuDeleteContactGroup):
		u.closeShellMenu()
		u.confirm = confirmation{open: true, title: i18n.T("删除联系人分组？"), body: "“" + name + i18n.T("”内的联系人会移至未分组。"),
			action: func() { u.submit(domain.Command{Kind: domain.DeleteContactGroup, Name: name}) }}
	case canvas.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) openContactGroupEditor(mode domain.CommandKind, group string) {
	u.closeShellMenu()
	u.closeChatPickers()
	u.organize = organizeState{open: true, mode: mode, group: group}
	u.organize.field.SetText(group)
}

func (u *UI) requestDeleteConversation(conversation domain.Conversation) {
	title := u.conversationTitle(conversation)
	if strings.TrimSpace(title) == "" {
		title = i18n.T("该会话")
	}
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("删除会话？"),
		body:  i18n.T("将删除“") + title + i18n.T("”在本机的聊天记录。收到新消息后，会话会重新出现。"),
		action: func() {
			u.submit(domain.Command{Kind: domain.DeleteConversation, ConversationID: conversation.ID})
			delete(u.drafts, conversation.ID)
			delete(u.draftSaved, conversation.ID)
			if u.selected != conversation.ID {
				return
			}
			u.selected = ""
			u.rebuildSelection()
			u.resetChatSearch()
			u.closeChatPickers()
			u.moreOpen = false
			u.memberMenu = ""
			u.memberProfile = ""
			u.closeDrawer()
			u.setChatEditorText("")
			u.chatScrollToEnd = true
		},
	}
}

func (u *UI) openOrganize(peerID string) {
	peer := u.state.Peer(peerID)
	if peer.ID == "" || peer.ID == u.state.Self.ID {
		u.closeShellMenu()
		return
	}
	u.closeShellMenu()
	u.closeChatPickers()
	u.organize = organizeState{open: true, peerID: peerID}
	u.organize.field.SetText(strings.TrimSpace(peer.ContactGroup))
}

func (u *UI) closeOrganize() {
	u.organize = organizeState{}
}

func (u *UI) saveOrganize() {
	if mode := u.organize.mode; mode != "" {
		name := strings.TrimSpace(u.organize.field.Text())
		if name == "" || name == "未分组" || utf8.RuneCountInString(name) > 30 {
			u.organize.errText = i18n.T("请输入 1 至 30 个字符的分组名称")
			return
		}
		u.submit(domain.Command{Kind: mode, Name: name, Text: u.organize.group})
		u.closeOrganize()
		return
	}
	peer := u.state.Peer(u.organize.peerID)
	if peer.ID == "" {
		u.closeOrganize()
		return
	}
	name := strings.TrimSpace(u.organize.field.Text())
	if name == "未分组" {
		name = ""
	}
	if utf8.RuneCountInString(name) > 30 {
		u.organize.errText = i18n.T("分组名称最多 30 个字符")
		return
	}
	u.submit(domain.Command{Kind: domain.SetContactGroup, PeerID: peer.ID, Name: name})
	u.closeOrganize()
}

func (u *UI) drawOrganizeDialog(canvas *ui.Canvas, viewport ui.Rect) {
	peer := u.state.Peer(u.organize.peerID)
	mode := u.organize.mode
	if mode == "" && peer.ID == "" {
		u.closeOrganize()
		return
	}
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 75, Scope: u.ids.organizeScope, DismissID: u.ids.organizeDismiss,
		CloseID: u.ids.organizeClose, InitialFocusID: u.ids.organizeField,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: 92,
		ButtonSize: components.ButtonSizeSmall,
	})
	title := i18n.T("移动到分组")
	if mode == domain.CreateContactGroup {
		title = i18n.T("新建联系人分组")
	}
	if mode == domain.RenameContactGroup {
		title = i18n.T("重命名分组")
	}
	layout := dialog.Begin(title, i18n.T("分组只用于本机整理，不会同步给对方。"))
	var submitted bool
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(10).Children(func() {
			if mode == "" {
				f.Row().Items(flex.Center).Gap(9).Children(func() {
					u.avatarStatus(f, peer.Name, peer.Avatar, 28, peer.Online)
					f.Column().Grow(1).Gap(2).Children(func() {
						f.Text(peer.Name).FontSize(12.5).Weight(ui.WeightMedium).Ellipsis()
						current := strings.TrimSpace(peer.ContactGroup)
						if current == "" {
							current = "未分组"
						}
						f.Text(i18n.T("当前：") + current).FontSize(10.5).Muted().Ellipsis()
					})
				})
			}
			placeholder := i18n.T("输入分组名称")
			if mode == "" {
				placeholder = i18n.T("留空表示未分组")
			}
			field := f.Field(&u.organize.field, u.ids.organizeField, i18n.T("分组名称"), placeholder).Height(38).FillWidth()
			submitted = field.Submitted()
			if u.organize.errText != "" {
				f.Text(u.organize.errText).FontSize(10.5).Color(u.palette.danger)
			} else if mode == "" {
				f.Text(i18n.T("输入现有分组名称可直接归档，也可新建分组。")).FontSize(10.5).Muted()
			}
		})
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.organizeCancel, Label: i18n.T("取消"), Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.organizeSave, Label: i18n.T("保存")},
	)
	dialog.End()

	switch {
	case canvas.Clicked(u.ids.organizeDismiss), canvas.Clicked(u.ids.organizeClose),
		canvas.Clicked(u.ids.organizeCancel):
		u.closeOrganize()
	case submitted, canvas.Clicked(u.ids.organizeSave):
		u.saveOrganize()
	}
}

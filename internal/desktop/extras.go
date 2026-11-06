package desktop

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	shortcutActionSearchInConversation = "会话内搜索"
	shortcutActionQuickFind            = "快速查找"
	shortcutActionCreateGroup          = "创建群聊"
	shortcutActionOpenSettings         = "打开设置"
)

type emojiChoice struct {
	Name  string
	Value string
}

var emojiChoices = []emojiChoice{
	{Name: "grin", Value: "😀"},
	{Name: "smile", Value: "😃"},
	{Name: "laugh", Value: "😄"},
	{Name: "beaming", Value: "😁"},
	{Name: "sweat-smile", Value: "😅"},
	{Name: "joy", Value: "😂"},
	{Name: "slightly-smile", Value: "🙂"},
	{Name: "wink", Value: "😉"},
	{Name: "blush", Value: "😊"},
	{Name: "heart-eyes", Value: "😍"},
	{Name: "star-eyes", Value: "🤩"},
	{Name: "kiss", Value: "😘"},
	{Name: "cool", Value: "😎"},
	{Name: "thinking", Value: "🤔"},
	{Name: "neutral", Value: "😐"},
	{Name: "sleep", Value: "😴"},
	{Name: "cry", Value: "😢"},
	{Name: "sob", Value: "😭"},
	{Name: "angry", Value: "😠"},
	{Name: "mind-blown", Value: "🤯"},
	{Name: "party", Value: "🥳"},
	{Name: "heart", Value: "❤️"},
	{Name: "blue-heart", Value: "💙"},
	{Name: "purple-heart", Value: "💜"},
	{Name: "fire", Value: "🔥"},
	{Name: "sparkles", Value: "✨"},
	{Name: "thumbs-up", Value: "👍"},
	{Name: "thumbs-down", Value: "👎"},
	{Name: "clap", Value: "👏"},
	{Name: "pray", Value: "🙏"},
	{Name: "muscle", Value: "💪"},
	{Name: "handshake", Value: "🤝"},
	{Name: "wave", Value: "👋"},
	{Name: "check", Value: "✅"},
	{Name: "cross", Value: "❌"},
	{Name: "warning", Value: "⚠️"},
	{Name: "bulb", Value: "💡"},
	{Name: "rocket", Value: "🚀"},
	{Name: "gift", Value: "🎁"},
	{Name: "tada", Value: "🎉"},
	{Name: "coffee", Value: "☕"},
	{Name: "beer", Value: "🍺"},
	{Name: "cake", Value: "🎂"},
	{Name: "apple", Value: "🍎"},
	{Name: "sun", Value: "☀️"},
	{Name: "moon", Value: "🌙"},
	{Name: "star", Value: "⭐"},
	{Name: "camera", Value: "📷"},
	{Name: "paperclip", Value: "📎"},
	{Name: "music", Value: "🎵"},
	{Name: "bell", Value: "🔔"},
	{Name: "lock", Value: "🔒"},
	{Name: "eyes", Value: "👀"},
}

func (u *UI) openProfile() {
	u.closeChatPickers()
	u.profile.open = true
	u.profile.err = ""
	name := strings.TrimSpace(u.state.Settings.Nickname)
	if name == "" {
		name = u.state.Self.Name
	}
	u.profile.nickname.SetText(name)
	u.profile.avatar = u.state.Self.Avatar
	if u.profile.avatar == "" {
		u.profile.avatar = u.state.Settings.Avatar
	}
}

func (u *UI) closeProfile() {
	u.profile.open = false
	u.profile.err = ""
}

func (u *UI) chooseProfileAvatar() {
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title:         "选择头像",
		AllowMultiple: false,
		Filters: []app.FileFilter{
			{Name: "图片", Pattern: "*.png;*.jpg;*.jpeg;*.gif"},
			{Name: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) ||
			errors.Is(err, app.ErrUnsupported) {
			return
		}
		u.profile.err = "无法打开图片选择器"
		return
	}
	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return
	}
	path := paths[0]
	u.ensureImage(path)
	bitmap := u.avatarBitmap(path)
	if bitmap.Width <= 0 || bitmap.Height <= 0 {
		u.profile.err = "无法读取这张图片"
		return
	}
	u.profile.avatar = path
	u.profile.err = ""
}

func (u *UI) saveProfile() {
	name := strings.TrimSpace(u.profile.nickname.Text())
	count := utf8.RuneCountInString(name)
	if count == 0 || count > 40 {
		u.profile.err = "昵称需为 1 至 40 个字符"
		return
	}
	avatar := u.profile.avatar
	if avatar != "" {
		u.ensureImage(avatar)
	}
	u.submit(domain.Command{Kind: domain.SetProfile, Name: name, Value: avatar})
	u.closeProfile()
}

func (u *UI) drawProfileDialog(canvas *ui.Canvas, viewport ui.Rect) {
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 74, Scope: u.ids.profileScope, DismissID: u.ids.profileDismiss,
		CloseID: u.ids.profileClose, InitialFocusID: u.ids.profileField,
		ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: 176,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin("个人资料", "昵称和头像会显示给局域网内的联系人。")
	var chooseClicked, submitted bool
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(12).Children(func() {
			f.Row().Items(flex.Center).Gap(14).Children(func() {
				avatarName := strings.TrimSpace(u.profile.nickname.Text())
				if avatarName == "" {
					avatarName = u.state.Self.Name
				}
				u.avatarStatus(f, avatarName, u.profile.avatar, 64, true)
				f.Column().Grow(1).Gap(6).Children(func() {
					f.Text("头像").FontSize(12).Weight(ui.WeightMedium)
					choose := f.Button(u.ids.profileChooseImage, "选择图片").XSmall().Outline()
					chooseClicked = choose.Clicked()
					f.Text("支持 PNG、JPEG 和 GIF。").FontSize(10.5).Muted()
				})
			})
			f.Text("昵称").FontSize(12).Weight(ui.WeightMedium)
			field := f.Field(&u.profile.nickname, u.ids.profileField, "昵称", "输入昵称").
				Height(38).FillWidth()
			submitted = field.Submitted()
			if u.profile.err != "" {
				f.Text(u.profile.err).FontSize(11).Color(u.palette.danger).MaxLines(2)
			} else {
				f.Text("最多 40 个字符。").FontSize(10.5).Muted()
			}
		})
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.profileCancel, Label: "取消", Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.profileSave, Label: "保存"},
	)
	dialog.End()
	switch {
	case canvas.Clicked(u.ids.profileDismiss), canvas.Clicked(u.ids.profileClose),
		canvas.Clicked(u.ids.profileCancel):
		u.closeProfile()
	case chooseClicked:
		u.chooseProfileAvatar()
	case submitted, canvas.Clicked(u.ids.profileSave):
		u.saveProfile()
	}
}

func (u *UI) closeChatPickers() {
	u.emojiOpen = false
	u.closeMention()
}

func (u *UI) closeMention() {
	u.mention.open = false
	u.mention.start = 0
	u.mention.query = ""
	u.mention.selected = 0
	u.mention.candidates = u.mention.candidates[:0]
}

func (u *UI) syncMention() {
	if u.chatEditor == nil {
		u.closeMention()
		return
	}
	conversation := u.state.Conversation(u.selected)
	if conversation.Kind != "group" || u.chatReadOnlyReason(conversation) != "" {
		u.closeMention()
		return
	}
	start, query, ok := mentionQuery(u.chatEditor)
	if !ok {
		u.closeMention()
		return
	}
	queryChanged := !u.mention.open || u.mention.start != start || u.mention.query != query
	u.mention.open = true
	u.mention.start = start
	u.mention.query = query
	if queryChanged {
		u.mention.selected = 0
	}
	group := u.state.Group(conversation.GroupID)
	candidates := u.mention.candidates[:0]
	for _, peer := range orderedMembers(u, group) {
		if peer.ID == "" || peer.ID == u.state.Self.ID {
			continue
		}
		name := u.groupMemberName(group.ID, peer.ID)
		if name == "" || query != "" && !containsFold(name, query) && !containsFold(peer.Name, query) {
			continue
		}
		candidates = append(candidates, mentionCandidate{
			peerID: peer.ID, name: name, online: peer.Online, avatar: peer.Avatar,
		})
		if len(candidates) >= 8 {
			break
		}
	}
	u.mention.candidates = candidates
	if len(candidates) == 0 {
		u.mention.selected = 0
	} else if u.mention.selected >= len(candidates) {
		u.mention.selected = len(candidates) - 1
	}
}

func mentionQuery(editor *text.Editor) (int, string, bool) {
	if editor == nil {
		return 0, "", false
	}
	runes := []rune(editor.String())
	caret := min(max(editor.Caret(), 0), len(runes))
	if caret == 0 {
		return 0, "", false
	}
	for index := caret - 1; index >= 0; index-- {
		char := runes[index]
		if char == '\n' || unicode.IsSpace(char) {
			return 0, "", false
		}
		if char != '@' {
			continue
		}
		if index > 0 && !unicode.IsSpace(runes[index-1]) {
			return 0, "", false
		}
		return index, string(runes[index+1 : caret]), true
	}
	return 0, "", false
}

func (u *UI) moveMention(delta int) {
	if !u.mention.open || len(u.mention.candidates) == 0 {
		return
	}
	next := u.mention.selected + delta
	if next < 0 {
		next = len(u.mention.candidates) - 1
	}
	if next >= len(u.mention.candidates) {
		next = 0
	}
	u.mention.selected = next
}

func (u *UI) insertMention() bool {
	if !u.mention.open || len(u.mention.candidates) == 0 || u.chatEditor == nil {
		return false
	}
	selected := u.mention.selected
	if selected < 0 || selected >= len(u.mention.candidates) {
		selected = 0
	}
	choice := u.mention.candidates[selected]
	end := u.chatEditor.Caret()
	if u.mention.start < 0 || end < u.mention.start {
		u.closeMention()
		return false
	}
	u.chatEditor.SetSelection(text.Range{Start: u.mention.start, End: end})
	u.chatEditor.Insert("@" + choice.name + " ")
	u.closeMention()
	if u.selected != "" {
		u.drafts[u.selected] = u.chatEditor.String()
	}
	u.chatInput.State = app.TextStateOf(u.chatEditor)
	return true
}

func (u *UI) drawMentionPicker(canvas *ui.Canvas, viewport ui.Rect) {
	if !u.mention.open {
		return
	}
	conversation := u.state.Conversation(u.selected)
	anchor := u.composerBox.Rect()
	if anchor.Width <= 0 {
		anchor = u.mentionEditor.Rect()
	}
	if conversation.Kind != "group" || anchor.Width <= 0 {
		u.closeMention()
		return
	}
	bodyHeight := float32(42)
	if len(u.mention.candidates) > 0 {
		bodyHeight = float32(min(len(u.mention.candidates), 8) * 38)
	}
	popover := components.BeginPopover(canvas, u.theme, viewport, anchor, components.PopoverOptions{
		Layer: 58, Scope: u.ids.mentionScope, DismissID: u.ids.mentionDismiss,
		Side: components.PopoverTop, Align: components.PopoverAlignStart,
		SideOffset: 8, ContentSize: ui.Size{Width: 390}, BodyHeight: bodyHeight,
	})
	layout := popover.Begin("提及成员", "↑↓ 选择 · Enter 插入 · Esc 关闭")
	u.mentionView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(2).Children(func() {
			if len(u.mention.candidates) == 0 {
				u.emptyListMessage(f, "没有匹配的成员", "换一个群昵称或昵称试试。")
				return
			}
			for index, candidate := range u.mention.candidates {
				background := ui.Color{}
				if index == u.mention.selected {
					background = u.palette.selected
				}
				row := f.Row().FillWidth().Height(36).PadSides(7, 8, 7, 8).Radius(7).
					Background(background).HoverBackground(u.palette.hover).
					HitWith(u.mentionCandidateIDs[candidate.peerID], "提及 "+candidate.name,
						ui.HitOptions{Role: ui.RoleButton, Selected: index == u.mention.selected}).
					Children(func() {
						u.avatarStatus(f, candidate.name, candidate.avatar, 26, candidate.online)
						f.Text(candidate.name).FontSize(12.5).Weight(ui.WeightMedium).Grow(1).Ellipsis()
						f.Text("@" + candidate.name).FontSize(10.5).Muted().Ellipsis()
					})
				if row.Clicked() {
					u.mention.selected = index
					u.insertMention()
				}
			}
		})
	})
	popover.End()
	if canvas.Clicked(u.ids.mentionDismiss) {
		u.closeMention()
	}
}

func (u *UI) drawEmojiPicker(canvas *ui.Canvas, viewport ui.Rect) {
	if !u.emojiOpen {
		return
	}
	anchor := u.emojiButton.Rect()
	if anchor.Width <= 0 || u.selected == "" {
		u.emojiOpen = false
		return
	}
	popover := components.BeginPopover(canvas, u.theme, viewport, anchor, components.PopoverOptions{
		Layer: 58, Scope: u.ids.emojiScope, DismissID: u.ids.emojiDismiss,
		Side: components.PopoverTop, Align: components.PopoverAlignStart,
		SideOffset: 8, ContentSize: ui.Size{Width: 360}, BodyHeight: 240,
	})
	layout := popover.Begin("插入表情", "点击表情插入到消息输入框。")
	u.emojiView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(2).Children(func() {
			for start := 0; start < len(emojiChoices); start += 8 {
				end := min(start+8, len(emojiChoices))
				f.Row().FillWidth().Height(40).Gap(2).Children(func() {
					for _, choice := range emojiChoices[start:end] {
						tile := f.Row().Width(40).Height(40).Radius(7).Items(flex.Center).
							Justify(flex.Center).HoverBackground(u.palette.hover).
							HitWith(u.emojiIDs[choice.Name], "表情 "+choice.Name,
								ui.HitOptions{Role: ui.RoleButton, Hint: choice.Name}).
							Children(func() {
								f.Text(choice.Value).FontSize(18)
							})
						if tile.Clicked() {
							u.insertChatText(choice.Value)
							u.emojiOpen = false
						}
					}
				})
			}
		})
	})
	popover.End()
	if canvas.Clicked(u.ids.emojiDismiss) {
		u.emojiOpen = false
	}
}

func (u *UI) avatarStatus(f *flex.Frame, name, path string, size float32, online bool) {
	if name == "" {
		name = "?"
	}
	f.Stack().Size(size, size).Children(func() {
		avatar := f.Stack().Size(size, size).Radius(size / 2).Clip().
			Background(u.palette.mutedAvatar)
		avatar.Children(func() {
			bitmap := u.avatarBitmap(path)
			if bitmap.Width > 0 && bitmap.Height > 0 {
				f.Image(bitmap).Fill()
				return
			}
			f.Row().Fill().Items(flex.Center).Justify(flex.Center).Children(func() {
				f.Text(initialOf(name)).FontSize(max(11, size*0.36)).Weight(ui.WeightSemibold).
					Color(u.palette.avatarText).TextCenter()
			})
		})
		dot := max(8, size*0.22)
		color := u.palette.secondaryText
		if online {
			color = u.palette.success
		}
		f.Stack().Size(dot, dot).Absolute().Right(0).Bottom(0).Radius(dot/2).
			Background(color).Border(2, u.palette.chat)
	})
}

func (u *UI) groupNickname(groupID, peerID string) string {
	return u.groupNicknames[groupInviteKey(groupID, peerID)]
}

func (u *UI) groupMemberName(groupID, peerID string) string {
	if name := u.groupNickname(groupID, peerID); name != "" {
		return name
	}
	return u.state.Peer(peerID).Name
}

func (u *UI) messageSenderName(conversation domain.Conversation, message domain.Message) string {
	if conversation.Kind == "group" {
		if name := strings.TrimSpace(message.Nickname); name != "" {
			return name
		}
		return u.groupMemberName(conversation.GroupID, message.SenderID)
	}
	return u.state.Peer(message.SenderID).Name
}

func (u *UI) openGroupNicknameEdit(group domain.Group) {
	u.nicknameOpen = true
	u.nicknameGroup = group.ID
	u.nicknameErr = ""
	u.nicknameField.SetText(u.groupNickname(group.ID, u.state.Self.ID))
}

func (u *UI) saveGroupNickname(group domain.Group) {
	value := strings.TrimSpace(u.nicknameField.Text())
	if utf8.RuneCountInString(value) > 30 {
		u.nicknameErr = "群昵称最多 30 个字符"
		return
	}
	u.submit(domain.Command{Kind: domain.SetGroupNickname, GroupID: group.ID, Value: value})
	u.nicknameOpen = false
	u.nicknameErr = ""
}

func (u *UI) drawGroupNicknameEditor(f *flex.Frame, group domain.Group) {
	if u.nicknameGroup != "" && u.nicknameGroup != group.ID {
		u.nicknameOpen = false
		u.nicknameErr = ""
	}
	current := u.groupNickname(group.ID, u.state.Self.ID)
	display := current
	if display == "" {
		display = "使用本名"
	}
	if !u.nicknameOpen {
		f.Row().Height(40).Items(flex.Center).Gap(10).Children(func() {
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text("我在群里的昵称").FontSize(13)
				f.Text(display).FontSize(10.5).Muted().Ellipsis()
			})
			edit := u.iconButton(f, u.ids.memberStartNickname, u.iconPencil, "修改我的群昵称", 28)
			if edit.Clicked() {
				u.openGroupNicknameEdit(group)
			}
		})
		return
	}
	f.Column().FillWidth().Gap(5).Children(func() {
		f.Row().Height(36).Items(flex.Center).Gap(8).Children(func() {
			field := f.Field(&u.nicknameField, u.ids.memberNickname, "我在群里的昵称",
				"留空使用本名").Height(36).Grow(1)
			if f.Button(u.ids.memberCancelNickname, "取消").XSmall().Outline().Clicked() {
				u.nicknameOpen = false
				u.nicknameErr = ""
			}
			if f.Button(u.ids.memberSaveNickname, "保存").XSmall().Clicked() || field.Submitted() {
				u.saveGroupNickname(group)
			}
		})
		if u.nicknameErr != "" {
			f.Text(u.nicknameErr).FontSize(10.5).Color(u.palette.danger)
		}
	})
}

func groupInviteKey(groupID, peerID string) string {
	return groupID + "/" + peerID
}

func (u *UI) outgoingInvitation(groupID, peerID string, statuses ...string) (domain.Invitation, bool) {
	for _, invitation := range u.outgoingInvites {
		if invitation.GroupID != groupID || invitation.ToID != peerID {
			continue
		}
		for _, status := range statuses {
			if invitation.Status == status || status == "pending" && invitation.Status == "" {
				return invitation, true
			}
		}
	}
	return domain.Invitation{}, false
}

func (u *UI) inviteWithdraw(group domain.Group, peer domain.Peer, invitation domain.Invitation) {
	u.submit(domain.Command{
		Kind:         domain.WithdrawInvite,
		GroupID:      group.ID,
		PeerID:       peer.ID,
		InvitationID: invitation.ID,
	})
}

func (u *UI) inviteAgain(group domain.Group, peer domain.Peer) {
	u.submit(domain.Command{Kind: domain.InviteMembers, GroupID: group.ID, IDs: []string{peer.ID}})
}

func (u *UI) drawGroupInvitations(f *flex.Frame, group domain.Group, owner bool) {
	u.drawOutgoingOwnershipInvites(f, group, owner)

	pending := 0
	for _, peerID := range group.Pending {
		if peerID == "" {
			continue
		}
		if invitation, found := u.outgoingInvitation(group.ID, peerID, "pending", "failed"); found &&
			invitation.Status == "failed" {
			continue
		}
		pending++
	}
	for _, invitation := range u.outgoingInvites {
		if invitation.GroupID == group.ID && invitation.ToID != "" &&
			(invitation.Status == "" || invitation.Status == "pending") &&
			!containsString(group.Pending, invitation.ToID) {
			pending++
		}
	}
	if pending > 0 {
		f.Row().Height(30).Items(flex.Center).Gap(8).Children(func() {
			f.Text("待确认邀请").FontSize(12).Weight(ui.WeightSemibold)
			u.badgeText(f, intString(pending), false)
		})
		for _, peerID := range group.Pending {
			if peerID == "" {
				continue
			}
			invitation, _ := u.outgoingInvitation(group.ID, peerID, "pending", "failed")
			u.drawOutgoingInviteRow(f, group, peerID, invitation, "待确认", owner, "withdraw")
		}
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.ToID == "" ||
				containsString(group.Pending, invitation.ToID) {
				continue
			}
			if invitation.Status != "" && invitation.Status != "pending" {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, "待确认", owner, "withdraw")
		}
	}

	failed := 0
	for _, peerID := range group.Pending {
		if peerID == "" {
			continue
		}
		if invitation, found := u.outgoingInvitation(group.ID, peerID, "failed"); found &&
			invitation.Status == "failed" {
			failed++
		}
	}
	for _, invitation := range u.outgoingInvites {
		if invitation.GroupID == group.ID && invitation.Status == "failed" &&
			invitation.ToID != "" && !containsString(group.Pending, invitation.ToID) {
			failed++
		}
	}
	if failed > 0 {
		f.Row().Height(30).Items(flex.Center).Gap(8).Children(func() {
			f.Text("发送失败的邀请").FontSize(12).Weight(ui.WeightSemibold)
			u.badgeText(f, intString(failed), false)
		})
		for _, peerID := range group.Pending {
			if peerID == "" {
				continue
			}
			invitation, found := u.outgoingInvitation(group.ID, peerID, "failed")
			if !found || invitation.Status != "failed" {
				continue
			}
			u.drawOutgoingInviteRow(f, group, peerID, invitation, "发送失败", owner, "retry")
		}
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.Status != "failed" ||
				invitation.ToID == "" || containsString(group.Pending, invitation.ToID) {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, "发送失败", owner, "retry")
		}
	}

	rejected := 0
	for _, invitation := range u.outgoingInvites {
		if invitation.GroupID == group.ID && invitation.Status == "rejected" && invitation.ToID != "" {
			rejected++
		}
	}
	if rejected > 0 {
		f.Row().Height(30).Items(flex.Center).Gap(8).Children(func() {
			f.Text("已拒绝邀请").FontSize(12).Weight(ui.WeightSemibold)
			u.badgeText(f, intString(rejected), false)
		})
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.Status != "rejected" || invitation.ToID == "" {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, "已拒绝", owner, "retry")
		}
	}
}

func (u *UI) drawOutgoingOwnershipInvites(f *flex.Frame, group domain.Group, owner bool) {
	if !owner {
		return
	}
	count := 0
	for _, invitation := range u.ownershipInvites {
		if invitation.GroupID == group.ID && invitation.ToID != "" {
			count++
		}
	}
	if count == 0 {
		return
	}
	f.Row().Height(30).Items(flex.Center).Gap(8).Children(func() {
		f.Text("群主转让").FontSize(12).Weight(ui.WeightSemibold)
		u.badgeText(f, intString(count), false)
	})
	for _, invitation := range u.ownershipInvites {
		if invitation.GroupID != group.ID || invitation.ToID == "" {
			continue
		}
		peer := u.state.Peer(invitation.ToID)
		status := "等待 " + peer.Name + " 接受群主转让"
		switch invitation.Status {
		case "failed":
			status = "群主转让邀请发送失败"
		case "rejected":
			status = peer.Name + " 已拒绝群主转让"
		case "accepted":
			status = "已接受，正在完成群主转让"
		case "completed":
			status = "群主转让已完成"
		case "expired":
			status = "群主转让邀请已过期"
		}
		f.Row().FillWidth().MinHeight(42).PadX(8).Items(flex.Center).Gap(9).Children(func() {
			u.avatarStatus(f, peer.Name, peer.Avatar, 26, peerOnline(u, peer.ID))
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text("群主转让给 " + peer.Name).FontSize(12).Weight(ui.WeightMedium).Ellipsis()
				f.Text(status).FontSize(10.5).Muted().Ellipsis()
			})
		})
	}
}

func (u *UI) drawOutgoingInviteRow(f *flex.Frame, group domain.Group, peerID string,
	invitation domain.Invitation, status string, owner bool, action string) {
	peer := u.state.Peer(peerID)
	key := groupInviteKey(group.ID, peerID)
	f.Row().Height(42).PadX(8).Items(flex.Center).Gap(9).Children(func() {
		u.avatarStatus(f, peer.Name, peer.Avatar, 26, peerOnline(u, peer.ID))
		f.Column().Grow(1).Gap(2).Children(func() {
			f.Text(peer.Name).FontSize(12).Weight(ui.WeightMedium).Ellipsis()
			f.Text(status).FontSize(10.5).Muted().Ellipsis()
		})
		if !owner {
			return
		}
		if action == "withdraw" {
			withdraw := f.Button(u.inviteWithdrawIDs[key], "撤回").XSmall().Outline()
			if withdraw.Clicked() {
				u.inviteWithdraw(group, peer, invitation)
			}
			return
		}
		if action != "retry" {
			return
		}
		label := "重试"
		if status == "已拒绝" {
			label = "重新邀请"
		}
		if !peer.Online {
			label += "（离线）"
		}
		again := f.Button(u.inviteReinviteIDs[key], label).XSmall().Outline().
			Disabled(!peer.Online)
		if again.Clicked() && peer.Online {
			u.inviteAgain(group, peer)
		}
	})
}

func (u *UI) configuredShortcut(action string) (string, bool) {
	for _, shortcut := range u.state.Settings.Shortcuts {
		if shortcut.Action == action {
			return shortcut.Keys, true
		}
	}
	return "", false
}

func (u *UI) shortcutTriggered(action, fallback string, event ui.KeyEvent) bool {
	keys, found := u.configuredShortcut(action)
	if !found {
		keys = fallback
	}
	return shortcutMatches(keys, event)
}

func shortcutMatches(keys string, event ui.KeyEvent) bool {
	if event.Release {
		return false
	}
	configured, ok := canonicalShortcut(keys)
	if !ok {
		return false
	}
	actual, ok := canonicalEventShortcut(event)
	return ok && configured == actual
}

func canonicalEventShortcut(event ui.KeyEvent) (string, bool) {
	name := ""
	switch event.Key {
	case ui.KeyEnter:
		name = "ENTER"
	case ui.KeySpace:
		name = "SPACE"
	case ui.KeyTab:
		name = "TAB"
	case ui.KeyBackspace:
		name = "BACKSPACE"
	case ui.KeyDelete:
		name = "DELETE"
	case ui.KeyHome:
		name = "HOME"
	case ui.KeyEnd:
		name = "END"
	case ui.KeyPageUp:
		name = "PAGEUP"
	case ui.KeyPageDown:
		name = "PAGEDOWN"
	case ui.KeyInsert:
		name = "INSERT"
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown:
		name = map[ui.Key]string{
			ui.KeyLeft: "LEFT", ui.KeyRight: "RIGHT", ui.KeyUp: "UP", ui.KeyDown: "DOWN",
		}[event.Key]
	case ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6,
		ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12:
		name = "F" + intString(int(event.Key-ui.KeyF1)+1)
	default:
		name = strings.ToUpper(strings.TrimSpace(event.Text))
		if utf8.RuneCountInString(name) == 1 {
			// Keep the character identity; modifier matching below is case-insensitive.
		}
	}
	if name == "" {
		return "", false
	}
	var parts []string
	if event.Modifiers&ui.ModifierControl != 0 {
		parts = append(parts, "CTRL")
	}
	if event.Modifiers&ui.ModifierAlt != 0 {
		parts = append(parts, "ALT")
	}
	if event.Modifiers&ui.ModifierShift != 0 {
		parts = append(parts, "SHIFT")
	}
	if event.Modifiers&ui.ModifierMeta != 0 {
		parts = append(parts, "META")
	}
	parts = append(parts, name)
	return strings.Join(parts, "+"), true
}

func canonicalShortcut(keys string) (string, bool) {
	keys = strings.TrimSpace(keys)
	if keys == "" {
		return "", false
	}
	key := ""
	prefix := keys
	if strings.HasSuffix(keys, "++") {
		key = "+"
		prefix = strings.TrimSuffix(keys, "++")
	} else {
		index := strings.LastIndexByte(keys, '+')
		if index < 0 {
			key = keys
			prefix = ""
		} else {
			key = strings.TrimSpace(keys[index+1:])
			prefix = keys[:index]
		}
	}
	key = shortcutKeyName(key)
	if key == "" {
		return "", false
	}
	var modifiers []string
	if strings.TrimSpace(prefix) != "" {
		for _, part := range strings.Split(prefix, "+") {
			switch strings.ToLower(strings.TrimSpace(part)) {
			case "ctrl", "control":
				modifiers = append(modifiers, "CTRL")
			case "alt", "option":
				modifiers = append(modifiers, "ALT")
			case "shift":
				modifiers = append(modifiers, "SHIFT")
			case "meta", "cmd", "command", "win", "super":
				modifiers = append(modifiers, "META")
			default:
				return "", false
			}
		}
	}
	modifiers = append(modifiers, key)
	return strings.Join(modifiers, "+"), true
}

func shortcutKeyName(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	switch strings.ToLower(key) {
	case "enter", "return":
		return "ENTER"
	case "space", "spacebar":
		return "SPACE"
	case "tab":
		return "TAB"
	case "backspace":
		return "BACKSPACE"
	case "delete", "del":
		return "DELETE"
	case "home":
		return "HOME"
	case "end":
		return "END"
	case "pageup":
		return "PAGEUP"
	case "pagedown":
		return "PAGEDOWN"
	case "insert":
		return "INSERT"
	case "left":
		return "LEFT"
	case "right":
		return "RIGHT"
	case "up":
		return "UP"
	case "down":
		return "DOWN"
	case "esc", "escape":
		return "ESCAPE"
	}
	lower := strings.ToLower(key)
	if len(lower) > 1 && lower[0] == 'f' {
		number := 0
		valid := true
		for _, char := range lower[1:] {
			if char < '0' || char > '9' {
				valid = false
				break
			}
			number = number*10 + int(char-'0')
		}
		if valid && number >= 1 && number <= 12 {
			return "F" + intString(number)
		}
	}
	if utf8.RuneCountInString(key) != 1 {
		return ""
	}
	return strings.ToUpper(key)
}

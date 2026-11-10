package desktop

import (
	"errors"
	"goim/internal/i18n"
	"slices"
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

func (u *UI) openProfile() {
	u.closeShellMenu()
	u.closeOrganize()
	u.closeChatPickers()
	u.profile.open = true
	u.profile.err = ""
	u.profile.nickname.SetText(u.selfDisplayName())
	u.profile.signature.SetText(u.state.Settings.Signature)
	details := u.state.Settings.ProfileDetails
	u.profile.gender = max(0, slices.Index(profileGenderValues[:], details.Gender))
	for i, value := range profileDetailValues(details) {
		u.profile.details[i].SetText(value)
	}
	u.profile.presence = u.state.Settings.Presence
	if u.profile.presence == "" {
		u.profile.presence = domain.PresenceOnline
	}
	u.profile.avatar = u.selfAvatarPath()
}

func (u *UI) closeProfile() {
	u.profile.open = false
	u.profile.err = ""
}

func (u *UI) chooseProfileAvatar() {
	paths, err := u.openFilePicker(app.OpenFileOptions{
		Title:         i18n.T("选择头像"),
		AllowMultiple: false,
		Filters: []app.FileFilter{
			{Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif"},
			{Name: i18n.T("所有文件"), Pattern: "*.*"},
		},
	})
	if err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) ||
			errors.Is(err, app.ErrUnsupported) {
			return
		}
		u.profile.err = i18n.T("无法打开图片选择器")
		return
	}
	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return
	}
	path := paths[0]
	u.markAvatarImage(path)
	u.ensureImage(path)
	u.profile.avatar = path
	u.profile.err = ""
}

func (u *UI) saveProfile() {
	name := strings.TrimSpace(u.profile.nickname.Text())
	count := utf8.RuneCountInString(name)
	if count == 0 || count > 40 {
		u.profile.err = i18n.T("昵称需为 1 至 40 个字符")
		return
	}
	avatar := u.profile.avatar
	signature := strings.TrimSpace(u.profile.signature.Text())
	if utf8.RuneCountInString(signature) > 80 || strings.ContainsAny(signature, "\r\n\x00") {
		u.profile.err = i18n.T("个性签名最多 80 个字符，请保持单行。")
		return
	}
	var values [5]string
	for i := range u.profile.details {
		value := strings.TrimSpace(u.profile.details[i].Text())
		if utf8.RuneCountInString(value) > 80 || strings.ContainsAny(value, "\r\n\x00") {
			u.profile.err = i18n.T("个人资料字段需为单行，最多 80 个字符")
			return
		}
		values[i] = value
	}
	details := &domain.ProfileDetails{Gender: profileGenderValues[u.profile.gender],
		Company: values[0], Department: values[1], Phone: values[2], Mobile: values[3], Email: values[4]}
	cmd := domain.Command{Kind: domain.SetProfile, Name: name, Value: avatar, Signature: signature, Presence: u.profile.presence, ProfileDetails: details}
	if avatar != "" && avatar != u.selfAvatarPath() {
		bitmap, ok := u.avatarUploadBitmap(avatar)
		if !ok {
			if u.profile.err == "" {
				u.profile.err = i18n.T("头像还在处理中，请稍候")
			}
			return
		}
		cmd.AvatarBGRA, cmd.AvatarSize = bitmap.Pix, bitmap.Width
	}
	u.submit(cmd)
	u.closeProfile()
}

func (u *UI) drawProfileDialog(canvas *ui.Canvas, viewport ui.Rect) {
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 74, Scope: u.ids.profileScope, DismissID: u.ids.profileDismiss,
		CloseID: u.ids.profileClose, InitialFocusID: u.ids.profileField,
		ContentSize: ui.Size{Width: 584}, FitBody: true, BodyHeight: 484,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin(i18n.T("个人资料"), i18n.T("已填写的资料对已连接的 GoIM 联系人可见。"))
	var chooseClicked, submitted bool
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(8).Children(func() {
			f.Row().Items(flex.Center).Gap(14).Children(func() {
				avatarName := strings.TrimSpace(u.profile.nickname.Text())
				if avatarName == "" {
					avatarName = u.state.Self.Name
				}
				u.avatarStatus(f, avatarName, u.profile.avatar, 64, true)
				f.Column().Grow(1).Gap(6).Children(func() {
					f.Text(i18n.T("头像")).FontSize(12).Weight(ui.WeightMedium)
					choose := f.Button(u.ids.profileChooseImage, i18n.T("选择图片")).XSmall().Outline()
					chooseClicked = choose.Clicked()
					f.Text(i18n.T("支持 PNG、JPEG 和 GIF。")).FontSize(10.5).Muted()
				})
			})
			f.Row().FillWidth().Gap(16).Children(func() {
				profileFormColumn(f, i18n.T("昵称"), func() {
					submitted = f.Field(&u.profile.nickname, u.ids.profileField, i18n.T("昵称"), i18n.T("输入昵称")).
						Height(38).FillWidth().Submitted()
				})
				profileFormColumn(f, i18n.T("个性签名"), func() {
					submitted = f.Field(&u.profile.signature, u.ids.profileSignature, i18n.T("个性签名"), i18n.T("写一句介绍，最多 80 字")).
						Height(38).FillWidth().Submitted() || submitted
				})
			})
			f.Row().Items(flex.Center).Gap(8).Children(func() {
				f.Text(i18n.T("状态")).FontSize(12).Weight(ui.WeightMedium)
				f.Text(i18n.T(profilePresenceName(u.profile.presence))).FontSize(11).Muted()
			})
			f.Row().FillWidth().Gap(8).Children(func() {
				for index, presence := range profilePresences {
					active := presence.value == u.profile.presence
					tint := presence.color
					if u.isDark() {
						tint = tint.Mix(ui.RGB(255, 255, 255), 0.32)
					}
					label := i18n.T(presence.label)
					button := f.Row().Size(42, 36).Radius(8).Items(flex.Center).Justify(flex.Center).
						Hover(flex.Look{}.Background(u.palette.hover)).
						HitWith(u.ids.profilePresence.At(index), label, ui.HitOptions{Role: ui.RoleButton, Selected: active, Hint: label})
					if active {
						button.Background(u.palette.selected).Stroke(1, u.palette.selectedText)
					}
					if f.Canvas().FocusVisible() == u.ids.profilePresence.At(index) {
						button.Stroke(1.5, u.theme.Ring)
					}
					button.Children(func() { f.Icon(presence.icon).Size(19, 19).Color(tint) })
					if button.Clicked() {
						u.profile.presence = presence.value
					}
				}
			})
			u.drawProfileDetails(f)
			f.Row().FillWidth().Gap(16).Children(func() {
				f.Text(i18n.T("版本") + "  " + defaultIfEmpty(u.state.Self.Version, "—")).FontSize(11).Muted().Grow(1).Ellipsis()
				f.Text("IP  " + defaultIfEmpty(u.state.LocalIP, "—")).FontSize(11).Muted().Grow(1).Ellipsis()
			})
			if u.profile.err != "" {
				f.Text(u.profile.err).FontSize(11).Color(u.palette.danger).MaxLines(2)
			} else {
				f.Text(i18n.T("隐身时不广播上线，已连接的会话仍可收发。")).FontSize(10.5).Muted()
			}
		})
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.profileCancel, Label: i18n.T("取消"), Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.profileSave, Label: i18n.T("保存")},
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

var profileDetailNames = [...]string{"company", "department", "phone", "mobile", "email"}
var profileDetailLabels = [...]string{"单位", "部门", "电话", "手机", "邮箱"}
var profileGenderValues = [...]string{"", "male", "female"}
var profileGenderLabels = [...]string{"不填写", "男", "女"}

func profileDetailValues(details domain.ProfileDetails) [5]string {
	return [5]string{details.Company, details.Department, details.Phone, details.Mobile, details.Email}
}

func profileFormColumn(f *flex.Frame, label string, control func()) {
	f.Column().Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
		f.Text(label).FontSize(12).Weight(ui.WeightMedium)
		control()
	})
}

func (u *UI) drawProfileDetails(f *flex.Frame) {
	field := func(i int) {
		label := i18n.T(profileDetailLabels[i])
		profileFormColumn(f, label, func() {
			f.Field(&u.profile.details[i], u.ids.profileDetails[i], label, i18n.T("选填")).Height(34).FillWidth()
		})
	}
	f.Divider()
	f.Row().FillWidth().Gap(16).Children(func() {
		profileFormColumn(f, i18n.T("性别"), func() {
			f.Row().Height(34).Gap(5).Items(flex.Center).Children(func() {
				for i, label := range profileGenderLabels {
					button := f.Button(u.ids.profileGender.At(i), i18n.T(label)).XSmall()
					if i != u.profile.gender {
						button.Outline()
					}
					if button.Clicked() {
						u.profile.gender = i
					}
				}
			})
		})
		field(0)
	})
	f.Row().FillWidth().Gap(16).Children(func() { field(1); field(2) })
	f.Row().FillWidth().Gap(16).Children(func() { field(3); field(4) })
}

var profilePresences = [...]struct {
	value domain.Presence
	label string
	icon  ui.Icon
	color ui.Color
}{
	{domain.PresenceOnline, "在线", CircleCheck, ui.RGB(31, 139, 83)},
	{domain.PresenceAway, "离开", Clock, ui.RGB(168, 103, 28)},
	{domain.PresenceBusy, "工作中", presenceBriefcase, ui.RGB(174, 63, 68)},
	{domain.PresenceInvisible, "隐身", presenceHidden, ui.RGB(100, 109, 130)},
}

func profilePresenceName(value domain.Presence) string {
	for _, p := range profilePresences {
		if p.value == value {
			return p.label
		}
	}
	return profilePresences[0].label
}

var presenceBriefcase = viewerLineIcon(
	ui.MoveTo(3, 7), ui.LineTo(21, 7), ui.LineTo(21, 20), ui.LineTo(3, 20), ui.ClosePath(),
	ui.MoveTo(8, 7), ui.LineTo(8, 4), ui.LineTo(16, 4), ui.LineTo(16, 7), ui.MoveTo(3, 12), ui.LineTo(21, 12),
)
var presenceHidden = viewerLineIcon(
	ui.MoveTo(3, 3), ui.LineTo(21, 21),
	ui.MoveTo(10, 5.5), ui.CubicTo(15, 3, 20, 8, 22, 12), ui.QuadTo(20, 15, 18, 16.5),
	ui.MoveTo(6, 7), ui.QuadTo(3, 9, 2, 12), ui.CubicTo(6, 18, 12, 21, 16, 18),
)

func peerPresenceLabel(peer domain.Peer) string {
	if !peer.VisibleOnline() {
		return i18n.T("离线")
	}
	for _, status := range profilePresences {
		if peer.Presence == status.value {
			return i18n.T(status.label)
		}
	}
	return i18n.T("在线")
}

func (u *UI) closeChatPickers() {
	u.emojiOpen = false
	u.emojiScrollDrag = false
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
	layout := popover.Begin(i18n.T("提及成员"), i18n.T("↑↓ 选择 · Enter 插入 · Esc 关闭"))
	u.mentionView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(2).Children(func() {
			if len(u.mention.candidates) == 0 {
				u.emptyListMessage(f, i18n.T("没有匹配的成员"), i18n.T("换一个群昵称或昵称试试。"))
				return
			}
			for index, candidate := range u.mention.candidates {
				background := ui.Color{}
				if index == u.mention.selected {
					background = u.palette.selected
				}
				row := f.Row().FillWidth().Height(36).PadSides(7, 8, 7, 8).Radius(7).
					Background(background).Hover(flex.Look{}.Background(u.palette.hover)).
					HitWith(u.mentionCandidateIDs[candidate.peerID], i18n.T("提及 ")+candidate.name,
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

func (u *UI) avatarStatus(f *flex.Frame, name, path string, size float32, online bool) {
	u.avatarStatusOn(f, name, path, size, online, u.palette.chat)
}

func (u *UI) avatarStatusOn(f *flex.Frame, name, path string, size float32, online bool, ring ui.Color) {
	u.avatarWithPresence(f, name, path, size, online, true, ring)
}

func (u *UI) avatarWithPresence(f *flex.Frame, name, path string, size float32, online, presence bool, ring ui.Color) {
	if name == "" {
		name = "?"
	}
	f.Stack().Size(size, size).Children(func() {
		avatar := f.Stack().Size(size, size).Radius(size / 2).Clip().
			Background(u.palette.mutedAvatar)
		avatar.Children(func() {
			bitmap := u.avatarBitmap(path)
			if bitmapReady(bitmap) {
				f.Image(bitmap).Fill()
				return
			}
			f.Row().Fill().Items(flex.Center).Justify(flex.Center).Children(func() {
				f.Text(initialOf(name)).FontSize(max(11, size*0.36)).Weight(ui.WeightSemibold).
					Color(u.palette.avatarText).TextCenter()
			})
		})
		if !presence {
			return
		}
		dot := max(8, size*0.22)
		color := u.palette.secondaryText
		if online {
			color = u.palette.success
		}
		f.Stack().Size(dot, dot).Absolute().Right(0).Bottom(0).Radius(dot/2).
			Background(color).Border(2, ring)
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
		u.nicknameErr = i18n.T("群昵称最多 30 个字符")
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
		display = i18n.T("使用本名")
	}
	if !u.nicknameOpen {
		f.Row().Height(40).Items(flex.Center).Gap(10).Children(func() {
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text(i18n.T("我在群里的昵称")).FontSize(13)
				f.Text(display).FontSize(10.5).Muted().Ellipsis()
			})
			edit := u.iconButton(f, u.ids.memberStartNickname, u.iconPencil, i18n.T("修改我的群昵称"), 28)
			if edit.Clicked() {
				u.openGroupNicknameEdit(group)
			}
		})
		return
	}
	f.Column().FillWidth().Gap(5).Children(func() {
		f.Row().Height(36).Items(flex.Center).Gap(8).Children(func() {
			field := f.Field(&u.nicknameField, u.ids.memberNickname, i18n.T("我在群里的昵称"),
				i18n.T("留空使用本名")).Height(36).Grow(1)
			if f.Button(u.ids.memberCancelNickname, i18n.T("取消")).XSmall().Outline().Clicked() {
				u.nicknameOpen = false
				u.nicknameErr = ""
			}
			if f.Button(u.ids.memberSaveNickname, i18n.T("保存")).XSmall().Clicked() || field.Submitted() {
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
			f.Text(i18n.T("待确认邀请")).FontSize(12).Weight(ui.WeightSemibold)
			u.badgeText(f, intString(pending), false)
		})
		for _, peerID := range group.Pending {
			if peerID == "" {
				continue
			}
			invitation, _ := u.outgoingInvitation(group.ID, peerID, "pending", "failed")
			u.drawOutgoingInviteRow(f, group, peerID, invitation, i18n.T("待确认"), owner, "withdraw")
		}
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.ToID == "" ||
				containsString(group.Pending, invitation.ToID) {
				continue
			}
			if invitation.Status != "" && invitation.Status != "pending" {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, i18n.T("待确认"), owner, "withdraw")
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
			f.Text(i18n.T("发送失败的邀请")).FontSize(12).Weight(ui.WeightSemibold)
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
			u.drawOutgoingInviteRow(f, group, peerID, invitation, i18n.T("发送失败"), owner, "retry")
		}
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.Status != "failed" ||
				invitation.ToID == "" || containsString(group.Pending, invitation.ToID) {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, i18n.T("发送失败"), owner, "retry")
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
			f.Text(i18n.T("已拒绝邀请")).FontSize(12).Weight(ui.WeightSemibold)
			u.badgeText(f, intString(rejected), false)
		})
		for _, invitation := range u.outgoingInvites {
			if invitation.GroupID != group.ID || invitation.Status != "rejected" || invitation.ToID == "" {
				continue
			}
			u.drawOutgoingInviteRow(f, group, invitation.ToID, invitation, i18n.T("已拒绝"), owner, "retry")
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
		f.Text(i18n.T("群主转让")).FontSize(12).Weight(ui.WeightSemibold)
		u.badgeText(f, intString(count), false)
	})
	for _, invitation := range u.ownershipInvites {
		if invitation.GroupID != group.ID || invitation.ToID == "" {
			continue
		}
		peer := u.state.Peer(invitation.ToID)
		status := i18n.T("等待 ") + peer.Name + i18n.T(" 接受群主转让")
		switch invitation.Status {
		case "failed":
			status = i18n.T("群主转让邀请发送失败")
		case "rejected":
			status = peer.Name + i18n.T(" 已拒绝群主转让")
		case "accepted":
			status = i18n.T("已接受，正在完成群主转让")
		case "completed":
			status = i18n.T("群主转让已完成")
		case "expired":
			status = i18n.T("群主转让邀请已过期")
		}
		f.Row().FillWidth().MinHeight(42).PadX(8).Items(flex.Center).Gap(9).Children(func() {
			u.avatarStatus(f, peer.Name, peer.Avatar, 26, peerOnline(u, peer.ID))
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text(i18n.T("群主转让给 ") + peer.Name).FontSize(12).Weight(ui.WeightMedium).Ellipsis()
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
			withdraw := f.Button(u.inviteWithdrawIDs[key], i18n.T("撤回")).XSmall().Outline()
			if withdraw.Clicked() {
				u.inviteWithdraw(group, peer, invitation)
			}
			return
		}
		if action != "retry" {
			return
		}
		label := i18n.T("重试")
		if status == i18n.T("已拒绝") {
			label = i18n.T("重新邀请")
		}
		if !peer.Online {
			label += i18n.T("（离线）")
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

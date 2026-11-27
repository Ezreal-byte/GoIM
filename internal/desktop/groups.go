package desktop

import (
	"errors"
	"github.com/vivaui/viva/gfx"
	"goim/internal/i18n"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (u *UI) drawMemberPane(canvas *gfx.Canvas, panel ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	group := u.state.Group(conversation.GroupID)
	if conversation.Kind != "group" || group.ID == "" || group.Left || group.Dissolved {
		u.closeDrawer()
		return
	}
	u.surface.filesPane = panel
	canvas.Fill(panel, u.palette.sidebar)
	canvas.Fill(ui.Rect{X: panel.X, Y: panel.Y, Width: 1, Height: panel.Height}, u.palette.border)
	owner := group.OwnerID != "" && group.OwnerID == u.state.Self.ID
	body := ui.Rect{X: panel.X + 16, Y: panel.Y + 10, Width: max(0, panel.Width-32), Height: max(0, panel.Height-70)}
	anchor, avatar := u.drawMemberContent(canvas, body, conversation, group, owner)
	footer := ui.Rect{X: body.X, Y: panel.Bottom() - 56, Width: body.Width, Height: 48}
	paintDialogActions(canvas, u.theme, footer, u.membershipAction(owner))
	u.handleMemberActions(canvas, panel, group, owner, anchor, avatar)
}

func (u *UI) drawMemberContent(canvas *gfx.Canvas, body ui.Rect, conversation domain.Conversation, group domain.Group, owner bool) (ui.Rect, bool) {
	native := group.Transport == domain.TransportNeiwangtong
	if u.renameOpen && u.renameGroupID != "" && u.renameGroupID != group.ID {
		u.resetGroupRename()
	}
	if u.memberMenu != "" && !containsString(group.Members, u.memberMenu) {
		u.memberMenu = ""
	}

	var menuAnchor ui.Box
	var avatarClicked bool
	u.drawerPage.Draw(canvas, body, func(f ui.C) {
		ui.Column(f).Fill().Gap(11).Children(func() {
			ui.Row(f).FillWidth().Height(38).Items(ui.Center).Children(func() {
				ui.Text(f, i18n.T("群聊信息")).FontSize(textSize(f, 16)).Weight(gfx.WeightSemibold).Grow(1)
				if u.iconButton(f, u.ids.memberClose, u.iconX, i18n.T("关闭详情"), 32).Clicked() {
					u.requestCloseDrawer()
				}
			})
			ui.Divider(f)
			ui.Row(f).Items(ui.Center).Gap(12).Children(func() {
				avatar := ui.Row(f).Size(46, 46).Radius(23).Items(ui.Center).Justify(ui.Center)
				if owner {
					avatar = avatar.Hover(ui.Look{}.Background(u.palette.hover)).
						HitWith(u.ids.memberAvatar, i18n.T("更换群头像"), gfx.HitOptions{
							Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand,
							Hint: i18n.T("选择图片并调整裁切"),
						})
				}
				avatar.Children(func() {
					u.avatarStatus(f, group.Name, group.Avatar, 46, true)
				})
				if owner && avatar.Clicked() {
					avatarClicked = true
				}
				ui.Column(f).Grow(1).Gap(3).Children(func() {
					ui.Text(f, group.Name).FontSize(textSize(f, 17)).Weight(gfx.WeightSemibold).Ellipsis()
					note := i18n.T("群主可修改群名")
					if !owner {
						note = i18n.T("群成员可查看成员信息")
					}
					if native {
						note = i18n.T("内网通群聊")
					}
					ui.Text(f, note).FontSize(textSize(f, 11)).Muted()
				})
				if owner && !u.renameOpen {
					edit := u.iconButton(f, u.ids.memberStartRename, u.iconPencil, i18n.T("修改群名"), 30)
					if edit.Clicked() {
						u.renameOpen = true
						u.renameGroupID = group.ID
						u.renameField.SetText(group.Name)
						u.memberMenu = ""
					}
				}
			})
			if owner && u.renameOpen {
				ui.Row(f).Height(38).Items(ui.Center).Gap(8).Children(func() {
					field := ui.TextField(f, &u.renameField, i18n.T("群名称"), i18n.T("群名称"), ui.WithID(u.ids.memberRename)).Height(36).Grow(1)
					if namedButton(f, u.ids.memberCancelRename, i18n.T("取消")).XSmall().Outline().Clicked() {
						u.resetGroupRename()
					}
					if namedButton(f, u.ids.memberSaveRename, i18n.T("保存")).XSmall().Clicked() ||
						field.Submitted() {
						u.saveGroupName(group)
					}
				})
			}
			ui.Row(f).Height(26).Items(ui.Center).Children(func() {
				total, _ := u.groupCounts(group)
				ui.Text(f, i18n.T("成员 (")+intString(total)+")").FontSize(textSize(f, 14)).
					Weight(gfx.WeightSemibold)
				ui.Spacer(f)
				if owner {
					label := i18n.T("邀请成员")
					if native {
						label = i18n.T("添加成员")
					}
					invite := namedButton(f, u.ids.memberInvite, label).XSmall().Outline()
					if invite.Clicked() {
						u.openCompose("invite", group.ID)
					}
				}
			})
			ui.TextField(f, &u.memberSearch, i18n.T("搜索群成员"), i18n.T("搜索群成员"), ui.WithID(u.ids.memberSearch)).Height(36)
			ui.Scroll(f.Part("members")).Grow(1).Gap(3).Children(func() {
				query := strings.TrimSpace(u.memberSearch.Text())
				members := orderedMembers(u, group)
				shown := 0
				for _, peer := range members {
					name := u.groupMemberName(group.ID, peer.ID)
					if peer.ID == u.state.Self.ID && u.groupNickname(group.ID, peer.ID) == "" {
						name = i18n.T("我")
					}
					if query != "" && !containsFold(peer.Name, query) && !containsFold(name, query) {
						continue
					}
					shown++
					row := ui.Row(f).FillWidth().Height(58).PadSides(7, 8, 7, 8).Radius(radiusControl).
						Gap(10).Background(func() ui.Color {
						if peer.ID == u.memberMenu {
							return u.palette.selected
						}
						return ui.Color{}
					}()).Hover(ui.Look{}.Background(u.palette.hover))
					if peer.ID != u.state.Self.ID {
						row = row.HitWith(u.memberMenuIDs[peer.ID], peer.Name,
							gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("成员操作")})
					}
					if peer.ID == u.memberMenu {
						menuAnchor = row
					}
					row.Children(func() {
						u.avatarStatus(f, name, peer.Avatar, 36, peerOnline(u, peer.ID))
						ui.Column(f).Grow(1).Gap(3).Justify(ui.Center).Children(func() {
							ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
								ui.Text(f, name).FontSize(textSize(f, 13.5)).Weight(gfx.WeightMedium).Ellipsis()
								if peer.ID == group.OwnerID {
									ui.Row(f).PadSides(2, 7, 2, 7).Radius(8).
										Background(u.palette.mutedAvatar).Children(func() {
										ui.Text(f, i18n.T("群主")).FontSize(textSize(f, 9.5)).Muted()
									})
								}
							})
							status := i18n.T("离线")
							if peerOnline(u, peer.ID) {
								status = i18n.T("在线")
							}
							ui.Text(f, status).FontSize(textSize(f, 10.5)).Muted()
						})
						if peer.ID != u.state.Self.ID {
							ui.Icon(f, u.iconMore).Size(17, 17).Color(u.palette.secondaryText)
						}
					})
					if row.Clicked() {
						u.memberMenu = peer.ID
						menuAnchor = row
					}
				}
				if shown == 0 && query != "" {
					u.emptyListMessage(f, i18n.T("没有匹配的成员"), i18n.T("换一个昵称试试。"))
				}
				u.drawGroupInvitations(f, group, owner)
			})
			ui.Divider(f)
			if !native {
				u.drawGroupNicknameEditor(f, group)
			}
			ui.Row(f).Height(36).Items(ui.Center).Children(func() {
				ui.Text(f, i18n.T("消息免打扰")).FontSize(textSize(f, 13)).Grow(1)
				muted := ui.State[bool](f, "muted")
				*muted = conversation.Muted
				if ui.Switch(f, muted, "").Hit(u.ids.memberMuteToggle, i18n.T("消息免打扰")).Changed() {
					u.submit(domain.Command{Kind: domain.ToggleMute, ConversationID: conversation.ID})
				}
			})
			ui.Row(f).Height(36).Items(ui.Center).Children(func() {
				ui.Text(f, i18n.T("置顶聊天")).FontSize(textSize(f, 13)).Grow(1)
				pinned := ui.State[bool](f, "pinned")
				*pinned = conversation.Pinned
				if ui.Switch(f, pinned, "").Hit(u.ids.memberPinToggle, i18n.T("置顶聊天")).Changed() {
					u.submit(domain.Command{Kind: domain.TogglePin, ConversationID: conversation.ID})
				}
			})
		})
	})

	if menuAnchor.Valid() {
		return menuAnchor.Rect(), avatarClicked
	}
	return ui.Rect{}, avatarClicked
}

func (u *UI) membershipAction(owner bool) components.ButtonAction {
	if owner {
		return components.ButtonAction{ID: u.ids.memberDissolve, Label: i18n.T("解散群聊"), Variant: components.ButtonDestructiveOutline}
	}
	return components.ButtonAction{ID: u.ids.memberLeave, Label: i18n.T("退出群聊"), Variant: components.ButtonDestructiveOutline}
}

func (u *UI) handleMemberActions(canvas *gfx.Canvas, viewport ui.Rect, group domain.Group, owner bool, anchor ui.Rect, avatarClicked bool) {
	if avatarClicked {
		canvas.AfterPaint(func() { u.openGroupAvatar(group) })
		return
	}
	if owner && canvas.Clicked(u.ids.memberDissolve) {
		u.requestDissolve(group)
		return
	}
	if !owner && canvas.Clicked(u.ids.memberLeave) {
		u.requestLeave(group)
		return
	}
	if u.memberMenu != "" {
		if anchor.Width <= 0 {
			u.memberMenu = ""
			return
		}
		u.drawMemberMenu(canvas, viewport, group, anchor)
	}
}

func (u *UI) openGroupAvatar(group domain.Group) {
	if group.ID == "" || group.OwnerID != u.state.Self.ID || group.Left || group.Dissolved {
		return
	}
	paths, err := u.openFilePicker(app.OpenFileOptions{
		Title:         i18n.T("选择群头像"),
		AllowMultiple: false,
		Filters: []app.FileFilter{
			{Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif"},
			{Name: i18n.T("所有文件"), Pattern: "*.*"},
		},
	})
	if err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) {
			return
		}
		u.showToast(i18n.T("无法选择群头像"), filePickerProblem(err), components.ToastError)
		return
	}
	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return
	}
	path := paths[0]
	u.avatarGroup = group.ID
	u.groupAvatar = profileAvatarCropState{pending: true, source: path}
	u.requestAvatarImage(path)
}

func (u *UI) syncGroupAvatarCrop(path string) {
	if (!u.groupAvatar.open && !u.groupAvatar.pending) || u.groupAvatar.source != path {
		return
	}
	if bitmap, ok := u.imageCache[path]; ok && bitmapReady(bitmap) {
		u.groupAvatar.open, u.groupAvatar.pending = true, false
		u.groupAvatar.err = ""
	}
}

func (u *UI) closeGroupAvatarEditor() {
	u.groupAvatar = profileAvatarCropState{}
	u.avatarGroup = ""
}

func (u *UI) drawGroupAvatarEditor(canvas *gfx.Canvas, viewport ui.Rect) {
	group := u.state.Group(u.avatarGroup)
	if group.ID == "" || group.OwnerID != u.state.Self.ID || group.Left || group.Dissolved {
		u.closeGroupAvatarEditor()
		return
	}
	crop := &u.groupAvatar
	bitmap := u.imageCache[crop.source]
	stageSize := float32(456)
	controlHeight := max(float32(32), u.theme.Metrics.ControlHeight)
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer:          78,
		Scope:          u.ids.groupAvatarScope,
		DismissID:      u.ids.groupAvatarDismiss,
		CloseID:        u.ids.groupAvatarClose,
		InitialFocusID: u.ids.groupAvatarCanvas,
		ContentSize:    ui.Size{Width: 520},
		FitBody:        true,
		BodyHeight:     stageSize + controlHeight + 12,
		ButtonSize:     dialogActionButtonSize,
	})
	dialogLayout := dialog.Begin(i18n.T("更换群头像"), "")
	options := components.AvatarEditorOptions{
		IDs: components.AvatarEditorIDs{
			Canvas: u.ids.groupAvatarCanvas, Zoom: u.ids.groupAvatarZoom,
			ZoomOut: u.ids.groupAvatarOut, ZoomIn: u.ids.groupAvatarIn,
		},
		Labels: components.AvatarEditorLabels{
			Hint:        i18n.T("拖动图片调整位置，滚轮、滑杆或按钮缩放；方形区域将作为群头像。"),
			Canvas:      i18n.T("群头像裁切区域"),
			Zoom:        i18n.T("缩放"),
			ZoomOut:     i18n.T("缩小"),
			ZoomIn:      i18n.T("放大"),
			Loading:     i18n.T("正在读取图片…"),
			Unavailable: i18n.T("无法读取这张图片"),
		},
		Bitmap:       bitmap,
		State:        crop.state,
		Backdrop:     u.theme.Popover,
		MaxStageSize: stageSize,
		Circular:     true,
		Loading:      !bitmapReady(bitmap) && crop.err == "",
		Error:        crop.err,
	}
	layout := components.BeginAvatarEditor(canvas, u.theme, dialogLayout.Body, options).Paint()
	crop.state, crop.layout = layout.State, layout
	actions := make([]components.ButtonAction, 0, 3)
	if group.Avatar != "" || group.AvatarHash != "" || group.AvatarMD5 != "" {
		actions = append(actions, components.ButtonAction{
			ID: u.ids.groupAvatarRemove, Label: i18n.T("移除群头像"),
			Variant: components.ButtonDestructiveOutline,
		})
	}
	actions = append(actions,
		components.ButtonAction{ID: u.ids.groupAvatarCancel, Label: i18n.T("取消"), Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.groupAvatarConfirm, Label: i18n.T("保存"), Disabled: !layout.Ready},
	)
	paintDialogActions(canvas, u.theme, dialogLayout.Footer, actions...)
	dialog.End()
	switch {
	case canvas.Clicked(u.ids.groupAvatarDismiss), canvas.Clicked(u.ids.groupAvatarClose),
		canvas.Clicked(u.ids.groupAvatarCancel):
		u.closeGroupAvatarEditor()
	case canvas.Clicked(u.ids.groupAvatarRemove):
		u.submit(domain.Command{Kind: domain.SetGroupAvatar, GroupID: group.ID, RemoveAvatar: true})
		u.closeGroupAvatarEditor()
	case canvas.Clicked(u.ids.groupAvatarConfirm) && layout.Ready:
		canvas.AfterPaint(func() {
			u.commitGroupAvatarCrop()
			canvas.RequestFrame()
		})
	}
}

func (u *UI) commitGroupAvatarCrop() {
	group := u.state.Group(u.avatarGroup)
	crop := &u.groupAvatar
	source, ok := u.imageCache[crop.source]
	if group.ID == "" || !ok || !crop.layout.Ready || !bitmapReady(source) {
		crop.err = i18n.T("群头像还在处理中，请稍候")
		return
	}
	preview, ok := cropScaledBitmap(source, crop.layout.Crop, 256)
	if !ok || !bitmapReady(preview) {
		crop.err = i18n.T("无法读取这张图片")
		return
	}
	u.submit(domain.Command{
		Kind: domain.SetGroupAvatar, GroupID: group.ID,
		AvatarBGRA: preview.Pix, AvatarSize: preview.Width,
	})
	u.closeGroupAvatarEditor()
}

func (u *UI) drawMemberMenu(canvas *gfx.Canvas, viewport ui.Rect, group domain.Group, anchor ui.Rect) {
	peer := u.state.Peer(u.memberMenu)
	if peer.ID == "" {
		u.memberMenu = ""
		return
	}
	owner := group.OwnerID == u.state.Self.ID
	canTransfer := owner && group.Transport == domain.TransportGoIM && peerOnline(u, peer.ID)
	items := 2
	if owner {
		items++
		if canTransfer {
			items++
		}
	}
	height := float32(10 + items*32)
	width := float32(190)
	x := min(max(anchor.X+anchor.Width-width-6, viewport.X+8), viewport.X+viewport.Width-width-8)
	y := min(max(anchor.Y+anchor.Height+4, viewport.Y+8), viewport.Y+viewport.Height-height-8)
	menu := components.BeginContextMenu(canvas, u.theme,
		ui.Rect{X: x, Y: y, Width: width, Height: height},
		components.ContextMenuOptions{
			Layer: 72, Scope: u.ids.memberMenuScope, DismissID: u.ids.memberMenuDismiss,
			Viewport: ui.Rect{Width: u.lastBody.Width, Height: u.lastBody.Y + u.lastBody.Height},
		})
	menu.AddItem(components.ContextMenuItem{
		ID: u.memberInfoIDs[peer.ID], Label: i18n.T("查看资料"), Icon: u.iconEye,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.memberMessageIDs[peer.ID], Label: i18n.T("发消息"), Icon: u.iconMessage,
	})
	if canTransfer {
		menu.AddItem(components.ContextMenuItem{
			ID: u.memberTransferIDs[peer.ID], Label: i18n.T("转让群主"), Icon: u.iconCrown,
		})
	}
	if owner {
		menu.AddItem(components.ContextMenuItem{
			ID: u.memberRemoveIDs[peer.ID], Label: i18n.T("移出群聊"), Variant: components.ContextMenuItemDestructive,
			Icon: u.iconUserMinus,
		})
	}
	menu.End()

	switch {
	case canvas.Clicked(u.memberInfoIDs[peer.ID]):
		u.memberMenu = ""
		u.memberProfile = peer.ID
	case canvas.Clicked(u.memberMessageIDs[peer.ID]):
		u.memberMenu = ""
		u.startDirectFromMembers(peer.ID)
	case canvas.Clicked(u.memberTransferIDs[peer.ID]):
		u.memberMenu = ""
		u.requestTransferOwner(group, peer)
	case canvas.Clicked(u.memberRemoveIDs[peer.ID]):
		u.memberMenu = ""
		u.requestRemoveMember(group, peer)
	case canvas.Clicked(u.ids.memberMenuDismiss):
		u.memberMenu = ""
	}
}

func (u *UI) drawMemberProfile(canvas *gfx.Canvas, viewport ui.Rect) {
	peer := u.state.Peer(u.memberProfile)
	if peer.ID == "" {
		u.memberProfile = ""
		return
	}
	groupID := u.state.Conversation(u.selected).GroupID
	if u.section == sectionContacts {
		groupID = u.contactGroup
	}
	group := u.state.Group(groupID)
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 74, Scope: u.ids.memberProfileScope, DismissID: u.ids.memberProfileDismiss,
		CloseID: u.ids.memberProfileClose, InitialFocusID: u.ids.memberProfileTalk,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: min(478, viewport.Height*0.66),
		ButtonSize: dialogActionButtonSize,
	})
	title := i18n.T("联系人资料")
	if group.ID != "" && slices.Contains(group.Members, peer.ID) {
		title = i18n.T("成员资料")
	}
	layout := dialog.Begin(title, "")
	u.dialogPage.Draw(canvas, layout.Body, func(f ui.C) {
		ui.Scroll(f.Part("member-profile")).Fill().Gap(8).Children(func() {
			u.drawContactProfile(f, peer, group, 52)
		})
	})
	self := peer.ID == u.state.Self.ID
	action := i18n.T("发消息")
	if self {
		action = i18n.T("编辑资料")
	}
	paintDialogActions(canvas, u.theme, layout.Footer,
		components.ButtonAction{ID: u.ids.memberProfileTalk, Label: action})
	dialog.End()
	switch {
	case canvas.Clicked(u.ids.memberProfileDismiss), canvas.Clicked(u.ids.memberProfileClose):
		u.memberProfile = ""
	case canvas.Clicked(u.ids.memberProfileTalk):
		if self {
			u.memberProfile = ""
			u.openProfile()
		} else {
			u.startDirectFromMembers(peer.ID)
		}
	}
}

func (u *UI) startDirectFromMembers(peerID string) {
	open := func() {
		u.memberMenu = ""
		u.memberProfile = ""
		if u.presentMemberChat != nil {
			u.presentMemberChat(peerID)
			return
		}
		u.closeDrawer()
		u.setSection(sectionMessages)
		u.startChat(peerID)
	}
	if u.groupRenameDirty() {
		u.requestDiscardGroupRename(open)
		return
	}
	open()
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (u *UI) saveGroupName(group domain.Group) {
	if u.renameGroupID != "" && u.renameGroupID != group.ID {
		u.resetGroupRename()
		return
	}
	name := strings.TrimSpace(u.renameField.Text())
	if name == "" || name == group.Name {
		u.resetGroupRename()
		return
	}
	if len([]rune(name)) > 30 {
		name = string([]rune(name)[:30])
	}
	u.submit(domain.Command{Kind: domain.RenameGroup, GroupID: group.ID, Name: name})
	u.resetGroupRename()
}

func orderedMembers(u *UI, group domain.Group) []domain.Peer {
	ids := make([]string, 0, len(group.Members)+1)
	seen := make(map[string]bool, len(group.Members)+1)
	if group.ID != "" && u.state.Self.ID != "" && !containsString(group.Members, u.state.Self.ID) {
		ids = append(ids, u.state.Self.ID)
		seen[u.state.Self.ID] = true
	}
	for _, id := range group.Members {
		if id == "" || seen[id] {
			continue
		}
		ids = append(ids, id)
		seen[id] = true
	}
	peers := make([]domain.Peer, 0, len(ids))
	for _, id := range ids {
		if id == u.state.Self.ID {
			peers = append(peers, u.state.Self)
			continue
		}
		peers = append(peers, u.state.Peer(id))
	}
	sort.SliceStable(peers, func(i, j int) bool {
		if peers[i].ID == u.state.Self.ID {
			return true
		}
		if peers[j].ID == u.state.Self.ID {
			return false
		}
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return peers[i].Name < peers[j].Name
	})
	return peers
}

func peerOnline(u *UI, id string) bool {
	if id == "" {
		return false
	}
	if id == u.state.Self.ID {
		return true
	}
	peer := u.state.Peer(id)
	return peer.ID != "" && peer.Online
}

func (u *UI) requestRemoveMember(group domain.Group, peer domain.Peer) {
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("移出群聊？"),
		body:  i18n.T("确定将 ") + peer.Name + i18n.T(" 移出「") + group.Name + i18n.T("」吗？对方将不再收到新的群消息。"),
		action: func() {
			u.submit(domain.Command{Kind: domain.RemoveMember, GroupID: group.ID, PeerID: peer.ID})
		},
	}
}

func (u *UI) requestTransferOwner(group domain.Group, peer domain.Peer) {
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("转让群主？"),
		body:  i18n.T("将群主转让给 ") + peer.Name + i18n.T("，对方确认后生效。"),
		action: func() {
			u.submit(domain.Command{Kind: domain.TransferOwner, GroupID: group.ID, PeerID: peer.ID})
		},
	}
}

func (u *UI) requestLeave(group domain.Group) {
	active := u.groupActiveTransfers(group)
	body := i18n.T("退出「") + group.Name + i18n.T("」后将不再接收新的群消息，本机历史会保留。")
	title := i18n.T("退出群聊？")
	if len(active) > 0 {
		title = i18n.T("退出群聊并取消传输？")
		body += i18n.T("仍有 ") + intString(len(active)) + i18n.T(" 个未完成文件任务（") +
			u.transferPeerNames(active) + i18n.T("）；确认后将取消这些任务，其他会话不受影响。")
	}
	u.confirm = confirmation{
		open:  true,
		title: title,
		body:  body,
		action: func() {
			u.cancelTransfers(active)
			u.submit(domain.Command{Kind: domain.LeaveGroup, GroupID: group.ID})
			u.closeDrawer()
		},
	}
}

func (u *UI) requestDissolve(group domain.Group) {
	active := u.groupActiveTransfers(group)
	body := i18n.T("解散「") + group.Name + i18n.T("」后群将不可再发送消息，本机历史会保留。此操作不可撤销。")
	title := i18n.T("解散群聊？")
	if len(active) > 0 {
		title = i18n.T("解散群聊并取消传输？")
		body += i18n.T("仍有 ") + intString(len(active)) + i18n.T(" 个未完成文件任务（") +
			u.transferPeerNames(active) + i18n.T("）；确认后将取消这些任务，其他会话不受影响。")
	}
	u.confirm = confirmation{
		open:  true,
		title: title,
		body:  body,
		action: func() {
			u.cancelTransfers(active)
			u.submit(domain.Command{Kind: domain.DissolveGroup, GroupID: group.ID})
			u.closeDrawer()
		},
	}
}

func (u *UI) groupActiveTransfers(group domain.Group) []domain.Transfer {
	var active []domain.Transfer
	for _, transfer := range u.state.Transfers {
		if !transferNeedsAttention(transfer) || !u.transferBelongsToGroup(transfer, group.ID) {
			continue
		}
		active = append(active, transfer)
	}
	return active
}

func (u *UI) transferBelongsToGroup(transfer domain.Transfer, groupID string) bool {
	if groupID == "" {
		return false
	}
	if conversationGroupID(u.state, transfer.ConversationID) == groupID {
		return true
	}
	file := u.state.File(transfer.FileID)
	return conversationGroupID(u.state, file.ConversationID) == groupID
}

func conversationGroupID(state domain.State, conversationID string) string {
	if strings.HasPrefix(conversationID, "group:") {
		return strings.TrimPrefix(conversationID, "group:")
	}
	return state.Conversation(conversationID).GroupID
}

func (u *UI) transferPeerNames(transfers []domain.Transfer) string {
	names := make([]string, 0, len(transfers))
	seen := make(map[string]bool, len(transfers))
	for _, transfer := range transfers {
		name := u.state.Peer(transfer.PeerID).Name
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return i18n.T("无接收者信息")
	}
	if len(names) > 4 {
		return strings.Join(names[:4], "、") + i18n.T(" 等 ") + intString(len(names)) + i18n.T(" 人")
	}
	return strings.Join(names, "、")
}

func (u *UI) cancelTransfers(transfers []domain.Transfer) {
	for _, transfer := range transfers {
		u.submit(domain.Command{Kind: domain.CancelTransfer, TransferID: transfer.ID})
	}
}

func (u *UI) closeDrawer() {
	u.activeVoiceRecord = domain.Message{}
	u.activePrintID = ""
	u.drawer = ""
	u.activeFileID = ""
	u.fileReturn = ""
	u.memberMenu = ""
	u.memberProfile = ""
	u.resetGroupRename()
	u.nicknameOpen = false
	u.nicknameGroup = ""
	u.nicknameErr = ""
	u.memberSearch.SetText("")
}

func (u *UI) requestCloseDrawer() {
	if u.groupRenameDirty() {
		u.requestDiscardGroupRename(u.closeDrawer)
		return
	}
	u.closeDrawer()
}

func (u *UI) groupRenameDirty() bool {
	if !u.renameOpen {
		return false
	}
	name := strings.TrimSpace(u.renameField.Text())
	group := u.state.Group(u.renameGroupID)
	if group.ID == "" {
		return name != ""
	}
	return name != group.Name
}

func (u *UI) resetGroupRename() {
	u.renameOpen = false
	u.renameGroupID = ""
	u.renameField.SetText("")
}

func (u *UI) guardGroupRenameNavigation(next func()) bool {
	if !u.groupRenameDirty() {
		return true
	}
	u.requestDiscardGroupRename(next)
	return false
}

func (u *UI) requestDiscardGroupRename(next func()) {
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("放弃未保存的群名？"),
		body:  i18n.T("群名修改尚未保存。"),
		action: func() {
			u.resetGroupRename()
			if next != nil {
				next()
			}
		},
	}
}

// drawChatFilesPane paints the file list and file detail as a docked section
// of the chat column, so the conversation stays visible and usable beside it.
func (u *UI) drawChatFilesPane(canvas *gfx.Canvas, viewport ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	if conversation.ID == "" || viewport.Width <= 0 || viewport.Height <= 0 {
		u.closeDrawer()
		return
	}
	if u.activeFileID != "" && u.state.File(u.activeFileID).ID == "" {
		u.activeFileID = ""
		u.fileReturn = ""
	}
	surface := u.surface
	surface.filesPane = viewport
	detail := u.activeFileID != "" || u.activePrintID != "" || u.activeVoiceRecord.ID != ""
	title := i18n.T("聊天文件")
	if u.activeFileID != "" {
		title = i18n.T("文件详情")
	}
	if u.activePrintID != "" {
		title = i18n.T("打印详情")
	}
	if u.activeVoiceRecord.ID != "" {
		title = i18n.T("通话详情")
	}
	canvas.Fill(viewport, u.palette.sidebar)
	canvas.Fill(ui.Rect{X: viewport.X, Y: viewport.Y, Width: 1, Height: viewport.Height}, u.palette.border)
	u.drawerPage.Draw(canvas, viewport, func(f ui.C) {
		ui.Column(f).Fill().Children(func() {
			ui.Row(f).FillWidth().Height(u.textExtent(60, 32)).PadX(20).Items(ui.Center).Gap(12).Children(func() {
				ui.Text(f, title).FontSize(textSize(f, 16)).Weight(gfx.WeightSemibold).Grow(1)
				if u.iconButton(f, u.ids.fileClose, u.iconX, i18n.T("关闭详情"), 36).Clicked() {
					u.closeDrawer()
					u.activePrintID = ""
				}
			})
			ui.Divider(f)
			ui.Column(f).FillWidth().Height(max(viewport.Height-u.textExtent(60, 32)-1, 0)).Pad(20).Children(func() {
				if u.activeVoiceRecord.ID != "" {
					u.drawVoiceRecordDetail(f, u.activeVoiceRecord)
				} else if u.activePrintID != "" {
					u.drawPrintDetail(f, u.state.PrintJob(u.activePrintID))
				} else if detail {
					u.drawFileDetail(f, u.state.File(u.activeFileID))
				} else {
					u.drawFileList(f)
				}
			})
		})
	})
}

func (u *UI) drawFileList(f ui.C) {
	ui.Column(f).Fill().Gap(10).Children(func() {
		ui.Row(f).Height(32).Gap(7).Children(func() {
			labels := []struct {
				id    gfx.ID
				label string
				value string
			}{
				{u.ids.fileFilterAll, i18n.T("全部"), "all"},
				{u.ids.fileFilterImage, i18n.T("图片"), "images"},
				{u.ids.fileFilterFile, i18n.T("文件"), "files"},
			}
			for _, item := range labels {
				active := u.fileFilter == item.value
				background := ui.Color{}
				if active {
					background = u.palette.selected
				}
				filter := ui.Row(f).Height(30).PadX(12).Radius(8).Items(ui.Center).
					Background(background).Hover(ui.Look{}.Background(u.palette.hover)).
					HitWith(item.id, i18n.T("筛选")+item.label,
						gfx.HitOptions{Role: gfx.RoleButton, Selected: active}).
					Children(func() {
						foreground := u.palette.secondaryText
						if active {
							foreground = u.palette.selectedText
						}
						ui.Text(f, item.label).FontSize(textSize(f, 11.5)).Weight(gfx.WeightMedium).Color(foreground)
					})
				if filter.Clicked() {
					u.fileFilter = item.value
				}
			}
			ui.Spacer(f)
			ui.Icon(f, u.iconSearch).Size(14, 14).Color(u.palette.secondaryText)
		})
		ui.TextField(f, &u.fileSearch, i18n.T("搜索聊天文件"), i18n.T("按文件名搜索"), ui.WithID(u.ids.fileSearch)).Height(36)
		ui.Scroll(f.Part("conversation-files")).Grow(1).Gap(4).Children(func() {
			query := strings.TrimSpace(u.fileSearch.Text())
			shown := 0
			for _, file := range u.files {
				if !fileMatchesFilter(file, u.fileFilter, query) {
					continue
				}
				shown++
				row := ui.Row(f).FillWidth().MinHeight(64).Pad(10).Radius(radiusControl).Gap(10).
					Hover(ui.Look{}.Background(u.palette.hover)).
					HitWith(u.fileRowIDs[file.ID], file.Name,
						gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("文件详情")}).
					Children(func() {
						drawFileIcon(f, u.theme, fileTypeIcon(file), 36, 19)
						ui.Column(f).Grow(1).Gap(3).Justify(ui.Center).Children(func() {
							ui.Text(f, file.Name).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).Ellipsis()
							sender := i18n.T("我")
							if file.SenderID != "" && file.SenderID != u.state.Self.ID {
								sender = u.state.Peer(file.SenderID).Name
							}
							ui.Text(f, sender+" · "+shortFileDate(file.CreatedAt)+" · "+formatBytes(file.Size)).
								FontSize(textSize(f, 10.5)).Muted().Ellipsis()
							ui.Text(f, fileAvailability(u, file)).FontSize(textSize(f, 10.5)).
								Color(u.availabilityColor(file)).Ellipsis()
						})
						ui.Icon(f, u.iconChevronRight).Size(15, 15).Color(u.palette.secondaryText)
					})
				if row.Clicked() {
					u.openFileDetail(file.ID, "list")
				}
			}
			if len(u.files) == 0 {
				u.emptyListMessage(f, i18n.T("还没有聊天文件"), i18n.T("发送或接收的文件会出现在这里。"))
			} else if shown == 0 {
				u.emptyListMessage(f, i18n.T("没有匹配的文件"), i18n.T("换一个文件名试试。"))
			}
		})
		ui.Row(f).Height(34).Items(ui.Center).Gap(7).Children(func() {
			refresh := u.iconButton(f, u.ids.fileRefresh, u.iconRefresh, i18n.T("刷新文件状态"), 28)
			if refresh.Clicked() {
				u.submit(domain.Command{Kind: domain.RefreshShares})
			}
			ui.Text(f, i18n.T("文件来源为发送方或本机已接收副本")).FontSize(textSize(f, 10)).Muted().Ellipsis()
		})
	})
}

func (u *UI) drawFileDetail(f ui.C, file domain.File) {
	ui.Scroll(f.Part("inspector-content")).Fill().Scrollbars().Gap(16).Children(func() {
		if u.fileReturn == "list" {
			if namedButton(f, u.ids.fileBack, i18n.T("返回文件列表")).Height(36).Ghost().Clicked() {
				u.activeFileID = ""
				u.fileReturn = ""
			}
		}
		ui.Row(f).Items(ui.Center).Gap(13).Children(func() {
			drawFileIcon(f, u.theme, fileTypeIcon(file), 52, 25)
			ui.Column(f).Grow(1).Gap(4).Children(func() {
				ui.Wrap(f, file.Name).FontSize(textSize(f, 17)).Weight(gfx.WeightSemibold)
				ui.Text(f, formatBytes(file.Size)+fileTypeSuffix(file)).FontSize(textSize(f, 12)).Muted()
			})
		})
		if fileIsImage(file) {
			if bitmap := u.fileBitmap(u.fileLocalPath[file.ID]); bitmap.Width > 0 && bitmap.Height > 0 {
				ui.Stack(f).FillWidth().Height(190).Radius(radiusControl).Clip().
					Background(u.palette.mutedAvatar).Children(func() {
					ui.Image(f, bitmap).Fill()
				})
			}
		}
		ui.Divider(f)
		u.fileInfoRow(f, i18n.T("发送者"), senderName(u.state, file))
		u.fileInfoRow(f, i18n.T("发送时间"), longDate(file.CreatedAt))
		u.fileInfoRow(f, i18n.T("所在会话"), u.conversationTitle(u.state.Conversation(file.ConversationID)))
		u.fileInfoRow(f, i18n.T("本机状态"), fileAvailability(u, file))
		if path := u.fileLocalPath[file.ID]; path != "" {
			u.fileInfoRow(f, i18n.T("本地路径"), path)
		}
		ui.Divider(f)
		ui.Text(f, i18n.T("传输状态")).FontSize(textSize(f, 13)).Weight(gfx.WeightSemibold)
		ui.Column(f).FillWidth().Gap(10).Children(func() {
			transfers := u.transfersFor[file.ID]
			if len(transfers) == 0 {
				ui.Row(f).PadSides(10, 8, 10, 8).Children(func() {
					ui.Text(f, fileAvailability(u, file)).FontSize(textSize(f, 11.5)).Muted()
				})
			}
			for _, entry := range transfers {
				u.drawTransferRow(f, entry)
			}
		})
		u.drawFileActions(f, file)
	})
}

func (u *UI) drawTransferRow(f ui.C, transfer domain.Transfer) {
	peer := u.state.Peer(transfer.PeerID)
	ui.Column(f).FillWidth().Pad(14).Radius(10).Background(u.palette.incoming).Gap(12).Children(func() {
		ui.Row(f).FillWidth().Items(ui.Center).Gap(10).Children(func() {
			u.avatarStatus(f, peer.Name, peer.Avatar, 32, peerOnline(u, peer.ID))
			ui.Column(f).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Text(f, transferPeerLabel(u, transfer)).FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).Ellipsis()
				ui.Wrap(f, transferStatusText(transfer)).FontSize(textSize(f, 13)).Muted()
			})
		})
		if transfer.Status == "transferring" || transfer.Status == "verifying" {
			ui.Progress(f, transferFraction(transfer)).FillWidth().Height(5)
			ui.Text(f, u.transferProgressLabel(transfer)).FontSize(textSize(f, 11)).Muted().Ellipsis()
		}
		ui.Flow(f).FillWidth().Gap(8).Items(ui.Center).Children(func() { u.drawTransferRowActions(f, transfer) })
	})
}

func (u *UI) drawTransferRowActions(f ui.C, transfer domain.Transfer) {
	if transfer.Revoked {
		return
	}
	switch {
	case transfer.Direction == "receive" && (transfer.Status == "offered" || transfer.Status == "waiting"):
		if namedButton(f, u.fileAcceptIDs[transfer.ID], i18n.T("接收文件")).Height(u.textExtent(40, 18)).FontSize(textSize(f, 14)).Clicked() {
			u.submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID})
		}
		if namedButton(f, u.fileSaveIDs[transfer.ID], i18n.T("另存为")).Height(u.textExtent(40, 18)).FontSize(textSize(f, 14)).Outline().Clicked() {
			u.chooseTransferDestination(f.Canvas(), transfer)
		}
		if namedButton(f, u.fileRejectIDs[transfer.ID], i18n.T("拒绝")).Height(u.textExtent(40, 18)).FontSize(textSize(f, 14)).Ghost().Clicked() {
			u.submit(domain.Command{Kind: domain.RejectTransfer, TransferID: transfer.ID})
		}
	case transfer.Direction == "receive" && transferNeedsAttention(transfer):
		if namedButton(f, u.fileCancelIDs[transfer.ID], i18n.T("取消接收")).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Outline().Clicked() {
			u.submit(domain.Command{Kind: domain.CancelTransfer, TransferID: transfer.ID})
		}
	case transfer.Direction != "receive" && transferNeedsAttention(transfer):
		if namedButton(f, u.fileCancelIDs[transfer.ID], i18n.T("取消发送")).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Outline().Clicked() {
			u.submit(domain.Command{Kind: domain.CancelTransfer, TransferID: transfer.ID})
		}
	case transfer.Status == "failed" || transfer.Status == "cancelled" || transfer.Status == "rejected":
		label := i18n.T("重试发送")
		if transfer.Direction == "receive" {
			label = i18n.T("重试接收")
		}
		if namedButton(f, u.fileRetryIDs[transfer.ID], label).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Clicked() {
			u.submit(domain.Command{Kind: domain.RetryTransfer, TransferID: transfer.ID})
		}
		if transfer.CanRelocateFailedReceive() && namedButton(f, u.fileSaveIDs[transfer.ID], i18n.T("另存为")).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Outline().Clicked() {
			u.chooseTransferDestination(f.Canvas(), transfer)
		}
	}
}

func (u *UI) drawFileActions(f ui.C, file domain.File) {
	localPath := u.fileLocalPath[file.ID]
	localAvailable := u.fileAvailable[file.ID]
	ui.Row(f).MinHeight(u.textExtent(36, 16)).Gap(8).Items(ui.Center).Children(func() {
		if localPath != "" && localAvailable {
			if namedButton(f, u.ids.fileOpen, i18n.T("打开文件")).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).
				Disabled(u.panels.localActionPending(localPath, false)).Clicked() {
				f.Canvas().AfterPaint(func() { u.openChatFile(localPath, false) })
			}
			if namedButton(f, u.ids.fileReveal, i18n.T("打开文件夹")).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Outline().
				Disabled(u.panels.localActionPending(localPath, true)).Clicked() {
				f.Canvas().AfterPaint(func() { u.openChatFile(localPath, true) })
			}
		}
		if cancellable := u.fileCancelCount(file.ID); cancellable > 1 {
			label := i18n.T("取消全部（") + intString(cancellable) + "）…"
			if namedButton(f, u.ids.fileBatchCancel, label).Height(u.textExtent(36, 16)).FontSize(textSize(f, 13)).Outline().Clicked() {
				u.requestBatchCancel(file)
			}
		}
	})
}

func (u *UI) openChatFile(path string, reveal bool) {
	u.panels.queueLocalAction(mediaJob{path: path, reveal: reveal}, func(message string) {
		u.showToast(i18n.T("无法打开本地项目"), message, components.ToastError)
	})
}

func (u *UI) fileCancelCount(fileID string) int {
	count := 0
	for _, transfer := range u.transfersFor[fileID] {
		if transferNeedsAttention(transfer) {
			count++
		}
	}
	return count
}

func (u *UI) requestBatchCancel(file domain.File) {
	active := u.groupedFileTransfers(file.ID)
	if len(active) == 0 {
		return
	}
	u.confirm = confirmation{
		open:  true,
		title: i18n.T("取消未完成传输？"),
		body: i18n.T("将取消 ") + intString(len(active)) + i18n.T(" 个文件任务（") +
			u.transferPeerNames(active) + i18n.T("）。已完成的文件不会删除，其他会话不受影响。"),
		action: func() {
			u.cancelTransfers(active)
		},
	}
}

func (u *UI) groupedFileTransfers(fileID string) []domain.Transfer {
	var active []domain.Transfer
	for _, transfer := range u.transfersFor[fileID] {
		if transferNeedsAttention(transfer) {
			active = append(active, transfer)
		}
	}
	return active
}

func (u *UI) chooseTransferDestination(canvas *gfx.Canvas, transfer domain.Transfer) {
	if u.filePickerOpen {
		return
	}
	u.filePickerOpen = true
	canvas.AfterPaint(func() {
		defer func() { u.filePickerOpen = false; canvas.RequestFrame() }()
		suggested := ""
		for _, file := range u.state.Files {
			if file.ID == transfer.FileID {
				suggested = file.Name
				break
			}
		}
		path, err := app.SaveFile(app.SaveFileOptions{
			Title: i18n.T("选择接收位置"), SuggestedName: suggested,
		})
		if err != nil {
			if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) {
				return
			}
			u.showToast(i18n.T("无法选择保存位置："), filePickerProblem(err), components.ToastError)
			return
		}
		if path == "" {
			return
		}
		u.submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID, Paths: []string{path}})
	})
}

func (u *UI) fileInfoRow(f ui.C, label, value string) {
	ui.Row(f).MinHeight(30).Items(ui.Center).Gap(12).Children(func() {
		ui.Text(f, label).Width(u.textExtent(86, 60)).Shrink(0).FontSize(textSize(f, 13)).Muted()
		ui.Wrap(f, value).FontSize(textSize(f, 13)).Grow(1).Basis(0).MinWidth(0)
	})
}

func (u *UI) availabilityColor(file domain.File) ui.Color {
	if u.fileAvailable[file.ID] {
		return u.palette.success
	}
	return u.palette.secondaryText
}

func fileMatchesFilter(file domain.File, filter, query string) bool {
	if filter == "images" && !fileIsImage(file) {
		return false
	}
	if filter == "files" && fileIsImage(file) {
		return false
	}
	if query != "" && !containsFold(file.Name, query) {
		return false
	}
	return true
}

func senderName(state domain.State, file domain.File) string {
	if file.SenderID == "" {
		return i18n.T("未知")
	}
	if file.SenderID == state.Self.ID {
		return i18n.T("我")
	}
	return state.Peer(file.SenderID).Name
}

func transferPeerLabel(u *UI, transfer domain.Transfer) string {
	name := u.state.Peer(transfer.PeerID).Name
	if transfer.PeerID == u.state.Self.ID || transfer.PeerID == "" {
		name = i18n.T("我")
	}
	if transfer.Direction == "send" {
		return i18n.T("发送给 ") + name
	}
	return i18n.T("接收自 ") + name
}

func transferStatusText(transfer domain.Transfer) string {
	if transfer.Revoked {
		return i18n.T("发送方已撤回")
	}
	switch transfer.Status {
	case "offered":
		return i18n.T("等待接收")
	case "waiting":
		return i18n.T("等待连接")
	case "retry_requested":
		return i18n.T("等待对方确认重试")
	case "transferring":
		return i18n.T("传输中 · ") + intString(int(transferFraction(transfer)*100)) + "%"
	case "verifying":
		return i18n.T("校验中")
	case "completed":
		return i18n.T("已接收")
	case "rejected":
		return i18n.T("已拒绝")
	case "cancelled":
		return i18n.T("已取消")
	case "failed":
		if transfer.Error != "" {
			return i18n.T("失败 · ") + transfer.Error
		}
		return i18n.T("失败")
	default:
		return transfer.Status
	}
}

func fileTypeSuffix(file domain.File) string {
	if file.Folder {
		return " · " + i18n.T("文件夹")
	}
	if ext := strings.TrimPrefix(strings.ToLower(filepathExt(file.Name)), "."); ext != "" {
		return " · " + strings.ToUpper(ext)
	}
	return ""
}

func filepathExt(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index < 0 {
		return ""
	}
	return name[index:]
}

func shortFileDate(at time.Time) string {
	if at.IsZero() {
		return i18n.T("时间未知")
	}
	local := at.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("01-02")
}

func longDate(at time.Time) string {
	if at.IsZero() {
		return i18n.T("时间未知")
	}
	return at.Local().Format("2006-01-02 15:04")
}

func (u *UI) drawChatMenu(canvas *gfx.Canvas) {
	conversation := u.state.Conversation(u.selected)
	if conversation.ID == "" {
		u.moreOpen = false
		return
	}
	group := u.state.Group(conversation.GroupID)
	items := 3
	if conversation.Kind == "group" && group.ID != "" && !group.Left && !group.Dissolved {
		items = 4
	}
	height := float32(12 + items*32 + 9)
	width := float32(210)
	viewport := ui.Rect{Width: u.lastBody.Width, Height: u.lastBody.Y + u.lastBody.Height}
	x := min(u.lastChat.X+u.lastChat.Width-width-24, viewport.Width-width-12)
	y := u.lastChat.Y + 78
	menu := components.BeginContextMenu(canvas, u.theme,
		ui.Rect{X: x, Y: y, Width: width, Height: height},
		components.ContextMenuOptions{
			Layer: 64, Scope: u.ids.chatMenuScope, DismissID: u.ids.chatMenuDismiss,
			Viewport: viewport,
		})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.chatMenuPin, Label: i18n.T("置顶聊天"), Kind: components.ContextMenuCheckbox,
		Checked: conversation.Pinned, Icon: u.iconPin,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.chatMenuMute, Label: i18n.T("消息免打扰"), Kind: components.ContextMenuCheckbox,
		Checked: conversation.Muted, Icon: u.iconBellOff,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.chatMenuRead, Label: i18n.T("标为已读"), Icon: u.iconCheckCheck,
		Disabled: conversation.Unread == 0,
	})
	if items == 4 {
		menu.AddSeparator()
		leaveLabel := i18n.T("退出群聊")
		if group.OwnerID == u.state.Self.ID {
			leaveLabel = i18n.T("解散群聊")
		}
		menu.AddItem(components.ContextMenuItem{
			ID: u.ids.chatMenuLeave, Label: leaveLabel,
			Variant: components.ContextMenuItemDestructive, Icon: u.iconLogOut,
		})
	}
	menu.End()

	switch {
	case canvas.Clicked(u.ids.chatMenuPin):
		u.moreOpen = false
		u.submit(domain.Command{Kind: domain.TogglePin, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.chatMenuMute):
		u.moreOpen = false
		u.submit(domain.Command{Kind: domain.ToggleMute, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.chatMenuRead):
		u.moreOpen = false
		u.submit(domain.Command{Kind: domain.MarkRead, ConversationID: conversation.ID})
	case canvas.Clicked(u.ids.chatMenuLeave):
		u.moreOpen = false
		if group.OwnerID == u.state.Self.ID {
			u.requestDissolve(group)
		} else {
			u.requestLeave(group)
		}
	case canvas.Clicked(u.ids.chatMenuDismiss):
		u.moreOpen = false
	}
}

func (u *UI) drawConnectDialog(canvas *gfx.Canvas, viewport ui.Rect) {
	u.dialogPage.Draw(canvas, viewport, func(f ui.C) {
		dialog := ui.Dialog(f, ui.DialogSpec{
			Title: i18n.T("手动连接"), Description: i18n.T("输入对方设备地址。连接成功后，对端联系人会出现在列表中。"),
			Width: 500, Layer: 70, Scope: u.ids.connectScope, DismissID: u.ids.connectDismiss,
			CloseID: u.ids.connectClose, InitialFocusID: u.ids.connectField,
		})
		var submitted, canceled bool
		dialog.Body(func() {
			submitted = connectionFields(f, u.state, panelConnectionFields{
				route: &u.connectRoute, address: &u.connectField, addressID: u.ids.connectField,
			})
			connectionFeedbackText(f, u.theme, u.state)
		})
		dialog.Footer(func() {
			canceled = dialogActionButton(f, u.ids.connectCancel, i18n.T("取消")).Outline().Clicked()
			enabled := connectionAvailable(u.state, u.connectRoute) && strings.TrimSpace(u.connectField.Text()) != "" && !u.connectPending && !u.state.ConnectionFeedback.Pending
			submitted = dialogActionButton(f, u.ids.connectSubmit, i18n.T("连接")).Disabled(!enabled).Clicked() || submitted
		})
		if dialog.Dismissed() || canceled {
			u.connectOpen = false
			return
		}
		if address := strings.TrimSpace(u.connectField.Text()); submitted && address != "" && !u.connectPending && !u.state.ConnectionFeedback.Pending {
			u.connectPending = true
			u.submit(domain.Command{Kind: domain.Connect, Address: address, Transport: u.connectRoute.transport()})
		}
	})
}

func (u *UI) drawComposeDialog(canvas *gfx.Canvas, viewport ui.Rect) {
	create := u.compose.mode == "create"
	group := u.state.Group(u.compose.groupID)
	if !create && group.ID == "" {
		u.compose.open = false
		return
	}
	title, description := i18n.T("创建群聊"), i18n.T("选择使用同一种客户端的在线联系人。")
	if !create {
		title = i18n.T("邀请成员")
		description = i18n.T("邀请在线联系人加入「") + group.Name + i18n.T("」，对方确认后计入成员。")
		if group.Transport == domain.TransportNeiwangtong {
			title = i18n.T("添加成员")
			description = i18n.T("请选择在线的内网通联系人")
		}
	}
	initialFocus := u.ids.composeSearch
	if create {
		initialFocus = u.ids.composeName
	}
	bodyHeight := float32(390)
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 70, Scope: u.ids.composeScope, DismissID: u.ids.composeDismiss,
		CloseID: u.ids.composeClose, InitialFocusID: initialFocus,
		ContentSize: ui.Size{Width: 760}, FitBody: true, BodyHeight: bodyHeight,
		ButtonSize: dialogActionButtonSize,
	})
	layout := dialog.Begin(title, description)
	u.dialogPage.Draw(canvas, layout.Body, func(f ui.C) {
		u.drawComposeBody(f, create, group)
	})
	selected := u.selectedPeersForCompose()
	native := len(selected) > 0 && u.state.Peer(selected[0]).Transport == domain.TransportNeiwangtong
	ready := u.composeReady(selected)
	if !create {
		ready = len(selected) >= 1
	}
	label := i18n.T("创建并邀请（") + intString(len(selected)) + "）"
	if create && native {
		label = i18n.T("创建群聊") + "（" + intString(len(selected)) + "）"
	} else if !create && group.Transport == domain.TransportNeiwangtong {
		label = i18n.T("添加成员") + "（" + intString(len(selected)) + "）"
	} else if !create {
		label = i18n.T("邀请（") + intString(len(selected)) + "）"
	}
	if u.compose.pending {
		if create {
			label = i18n.T("正在创建…")
		} else {
			label = i18n.T("正在邀请…")
		}
	}
	paintDialogActions(canvas, u.theme, layout.Footer,
		components.ButtonAction{
			ID: u.ids.composeCancel, Label: i18n.T("取消"), Variant: components.ButtonOutline,
			Disabled: u.compose.pending,
		},
		components.ButtonAction{
			ID: u.ids.composeCreate, Label: label, Disabled: !ready || u.compose.pending,
		},
	)
	dialog.End()

	if canvas.Clicked(u.ids.composeDismiss) || canvas.Clicked(u.ids.composeClose) ||
		canvas.Clicked(u.ids.composeCancel) {
		u.requestCloseCompose()
		return
	}
	if canvas.Clicked(u.ids.composeCreate) && ready && !u.compose.pending {
		u.submitCompose()
	}
}

func (u *UI) drawComposeBody(f ui.C, create bool, group domain.Group) {
	ui.Column(f).Fill().Gap(10).Children(func() {
		if create {
			ui.Row(f).Height(36).Items(ui.Center).Gap(12).Children(func() {
				ui.Text(f, i18n.T("群名称（可选）")).Width(96).FontSize(textSize(f, 12)).Weight(gfx.WeightMedium)
				ui.TextField(f, &u.compose.name, i18n.T("群名称"), i18n.T("留空将按成员昵称生成"), ui.WithID(u.ids.composeName)).
					Height(36).Grow(1)
			})
		}
		ui.TextField(f, &u.compose.search, i18n.T("搜索联系人"), i18n.T("搜索昵称或分组"), ui.WithID(u.ids.composeSearch)).
			Height(36)
		ui.Divider(f)
		ui.Row(f).Grow(1).Gap(18).Children(func() {
			ui.Column(f).Grow(1).Gap(6).Children(func() {
				ui.Text(f, i18n.T("可邀请联系人")).FontSize(textSize(f, 12)).Weight(gfx.WeightSemibold)
				ui.Scroll(f.Part("candidate-members")).Grow(1).Gap(2).Children(func() {
					u.drawComposeCandidates(f, create, group)
				})
			})
			ui.Row(f).Width(1).Background(u.palette.border)
			ui.Column(f).Width(236).Gap(7).Children(func() {
				selected := u.selectedPeersForCompose()
				ui.Row(f).Height(24).Items(ui.Center).Children(func() {
					ui.Text(f, i18n.T("已选择 ")+intString(len(selected))+i18n.T(" 人")).FontSize(textSize(f, 12)).
						Weight(gfx.WeightSemibold)
					ui.Spacer(f)
				})
				ui.Scroll(f.Part("selected-members")).Grow(1).Gap(4).Children(func() {
					if len(selected) == 0 {
						ui.Column(f).PadSides(10, 12, 8, 12).Gap(5).Children(func() {
							ui.Text(f, i18n.T("尚未选择联系人")).FontSize(textSize(f, 12)).Weight(gfx.WeightMedium)
							ui.Text(f, i18n.T("勾选左侧在线联系人。")).FontSize(textSize(f, 10.5)).Muted()
						})
					}
					for _, id := range selected {
						peer := u.state.Peer(id)
						ui.Row(f).Height(38).Items(ui.Center).Gap(8).Children(func() {
							u.avatarStatus(f, peer.Name, peer.Avatar, 26, true)
							ui.Text(f, peer.Name).FontSize(textSize(f, 12)).Grow(1).Ellipsis()
							remove := ui.Row(f).Size(24, 24).Radius(radiusControl).Items(ui.Center).
								Justify(ui.Center).Hover(ui.Look{}.Background(u.palette.hover)).
								HitWith(u.groupRemoveIDs[peer.ID], i18n.T("移除 ")+peer.Name,
									gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("移除")}).
								Children(func() {
									ui.Icon(f, u.iconX).Size(14, 14).Color(u.palette.secondaryText)
								})
							if remove.Clicked() {
								delete(u.compose.selected, peer.ID)
							}
						})
					}
				})
				ui.Text(f, i18n.T("你将自动加入群聊。")).FontSize(textSize(f, 10.5)).Muted()
				if u.compose.pending {
					ui.Row(f).Height(26).Items(ui.Center).Gap(8).Children(func() {
						ui.Spinner(f)
						ui.Text(f, i18n.T("正在提交…")).FontSize(textSize(f, 11)).Muted()
					})
				}
				if u.compose.err != "" {
					ui.Text(f, u.compose.err).FontSize(textSize(f, 10.5)).Color(u.palette.danger).MaxLines(3)
				}
			})
		})
		ui.Row(f).Height(20).Items(ui.Center).Children(func() {
			note := i18n.T("邀请尚待确认时仍可进入群聊。")
			if create {
				note = i18n.T("内网通群至少选 1 人，GoIM 群至少选 2 人。")
			} else if group.Transport == domain.TransportNeiwangtong {
				note = i18n.T("新成员会在内网通看到这个群聊。")
			}
			ui.Text(f, note).FontSize(textSize(f, 10.5)).Muted().Ellipsis()
		})
	})
}

func (u *UI) drawComposeCandidates(f ui.C, create bool, group domain.Group) {
	query := strings.TrimSpace(u.compose.search.Text())
	transport, fixedTransport := group.Transport, !create
	if create {
		for id, selected := range u.compose.selected {
			peer := u.state.Peer(id)
			if selected && peer.Online && !peer.ContactHidden && peer.SupportsGroups() {
				transport, fixedTransport = peer.Transport, true
				break
			}
		}
	}
	shown := 0
	for _, contactGroup := range u.contacts {
		peers := make([]domain.Peer, 0, len(contactGroup.Peers))
		for _, peer := range contactGroup.Peers {
			if peer.ID == u.state.Self.ID {
				continue
			}
			if !create && (containsString(group.Members, peer.ID) || containsString(group.Pending, peer.ID)) {
				continue
			}
			if query != "" && !containsFold(peer.Name, query) && !containsFold(peer.ContactGroup, query) {
				continue
			}
			peers = append(peers, peer)
		}
		if len(peers) == 0 {
			continue
		}
		shown += len(peers)
		collapsed := u.collapsed[contactGroup.Name] && query == ""
		headerID := u.composeGroupHeadIDs[contactGroup.Name]
		header := ui.Row(f).Height(28).Items(ui.Center).Gap(6).Radius(radiusControl).
			Hover(ui.Look{}.Background(u.palette.hover)).
			HitWith(headerID, contactGroup.Name,
				gfx.HitOptions{Role: gfx.RoleButton, Expanded: !collapsed}).
			Children(func() {
				icon := u.iconChevronDown
				if collapsed {
					icon = u.iconChevronRight
				}
				ui.Icon(f, icon).Size(14, 14).Color(u.palette.secondaryText)
				ui.Text(f, contactGroup.Name).FontSize(textSize(f, 12)).Weight(gfx.WeightSemibold).Ellipsis()
				ui.Spacer(f)
				ui.Text(f, intString(len(peers))).FontSize(textSize(f, 10.5)).Muted()
			})
		if header.Clicked() {
			u.collapsed[contactGroup.Name] = !collapsed
		}
		if collapsed {
			continue
		}
		for _, peer := range peers {
			selected := u.compose.selected[peer.ID]
			supported := peer.SupportsGroups()
			matches := !fixedTransport || peer.Transport == transport
			available := peer.Online && (selected || supported && matches)
			row := ui.Row(f.Key(peer.ID)).FillWidth().Height(46).PadSides(5, 8, 5, 8).Radius(radiusControl).Gap(8).
				Background(func() ui.Color {
					if selected {
						return u.palette.selected
					}
					return ui.Color{}
				}()).Hover(ui.Look{}.Background(u.palette.hover))
			if available {
				row = row.HitWith(u.groupSelectRows[peer.ID], peer.Name,
					gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
			}
			var picked bool
			row.Children(func() {
				u.avatarStatus(f, peer.Name, peer.Avatar, 30, peer.Online)
				ui.Column(f).Grow(1).Gap(2).Justify(ui.Center).Children(func() {
					ui.Text(f, peer.Name).FontSize(textSize(f, 12.5)).Weight(gfx.WeightMedium).Ellipsis()
					status := i18n.T("在线")
					if !peer.Online {
						status = i18n.T("离线 · 上线后可邀请")
					} else if selected {
						status = i18n.T("已选择")
					} else if !supported {
						status = i18n.T("此联系人不支持群聊")
					} else if !matches {
						status = i18n.T("此群需要同一种客户端")
					} else if peer.Transport == domain.TransportNeiwangtong {
						status = i18n.T("内网通")
					}
					ui.Text(f, status).FontSize(textSize(f, 10.5)).Muted().Ellipsis()
				})
				checked := ui.State[bool](f, "selected")
				*checked = selected
				picked = ui.Checkbox(f, checked, "").Hit(u.groupSelectIDs[peer.ID], i18n.T("选择 ")+peer.Name).
					Disabled(!available).Changed()
			})
			if available && (row.Clicked() || picked) {
				u.compose.selected[peer.ID] = !selected
			}
		}
	}
	if shown == 0 {
		u.emptyListMessage(f, i18n.T("没有可邀请的联系人"), i18n.T("联系人上线后会出现在这里。"))
	}
}

func (u *UI) drawConfirmation(canvas *gfx.Canvas, viewport ui.Rect) {
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 76, Scope: u.ids.confirmScope, DismissID: u.ids.confirmDismiss,
		CloseID: u.ids.confirmClose, InitialFocusID: u.ids.confirmCancel,
		ContentSize: ui.Size{Width: 440}, FitBody: true, BodyHeight: 8,
		ButtonSize: dialogActionButtonSize,
	})
	layout := dialog.Begin(u.confirm.title, u.confirm.body)
	u.dialogPage.Draw(canvas, layout.Body, func(f ui.C) {
		ui.Row(f).Fill()
	})
	paintDialogActions(canvas, u.theme, layout.Footer,
		components.ButtonAction{ID: u.ids.confirmCancel, Label: i18n.T("取消"), Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.confirmRun, Label: i18n.T("确认"), Variant: components.ButtonDestructive},
	)
	dialog.End()

	run := false
	switch {
	case canvas.Clicked(u.ids.confirmDismiss), canvas.Clicked(u.ids.confirmClose),
		canvas.Clicked(u.ids.confirmCancel):
		u.confirm.open = false
	case canvas.Clicked(u.ids.confirmRun):
		run = true
	}
	if run {
		action := u.confirm.action
		u.confirm = confirmation{}
		if action != nil {
			action()
		}
	}
}

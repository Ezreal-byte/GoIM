package desktop

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func contactClientLabel(peer domain.Peer) string {
	if peer.ClientType == domain.ClientNeiwangtong || peer.Transport == domain.TransportNeiwangtong {
		return i18n.T("内网通")
	}
	if peer.Transport == domain.TransportIPMSG {
		return i18n.T("飞鸽／飞秋")
	}
	return "GoIM"
}

func (u *UI) showContact(id string) {
	u.contactPeer, u.contactGroup = id, ""
}

func (u *UI) contactGroupRow(f ui.C, c domain.Conversation) {
	group := u.state.Group(c.GroupID)
	if group.ID == "" {
		return
	}
	selected := u.contactGroup == group.ID
	background := ui.Color{}
	if selected {
		background = u.palette.selected
	}
	row := ui.Row(f).FillWidth().Height(57).PadX(9).Radius(9).Gap(10).Items(ui.Center).
		Background(background).Hover(ui.Look{}.Background(u.palette.hover)).
		HitWith(u.convRowIDs[c.ID], group.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
		Children(func() {
			u.initialsAvatar(f, group.Name, 38)
			ui.Column(f).Grow(1).Gap(3).Children(func() {
				ui.Text(f, group.Name).FontSize(14).Weight(gfx.WeightMedium).Ellipsis()
				ui.Text(f, intString(len(group.Members))+" "+i18n.T("位成员")).FontSize(11).Muted()
			})
		})
	if row.Clicked() {
		u.contactGroup, u.contactPeer = group.ID, ""
	}
}

func (u *UI) drawContactDetails(canvas *gfx.Canvas, bounds ui.Rect) {
	canvas.Fill(bounds, u.palette.chat)
	peer := u.state.Peer(u.contactPeer)
	group := u.state.Group(u.contactGroup)
	isGroup := group.ID != "" && !group.Left && !group.Dissolved
	compact := bounds.Width < 450
	surface := u.theme.Background
	if u.isDark() {
		surface = u.palette.sidebar
	}
	u.chatPage.Draw(canvas, bounds, func(f ui.C) {
		if peer.ID == "" && !isGroup {
			u.drawContactEmpty(f)
			return
		}
		title, selection := i18n.T("联系人资料"), peer.ID
		if isGroup {
			title, selection = i18n.T("群聊资料"), group.ID
		} else if peer.ID == u.state.Self.ID {
			title = i18n.T("个人资料")
		}
		ui.Scroll(f.Part("contact-details").Key(selection)).Fill().Children(func() {
			ui.Row(f).FillWidth().Justify(ui.Center).PadSides(28, 18, 24, 18).Children(func() {
				ui.Column(f).FillWidth().MaxWidth(620).Gap(12).Children(func() {
					ui.Text(f, title).FontSize(16).Weight(gfx.WeightSemibold)
					ui.Column(f).FillWidth().Pad(20).Gap(16).Radius(12).
						Border(1, u.palette.border).Background(surface).Children(func() {
						if isGroup {
							u.drawContactGroupProfile(f, group, compact)
							return
						}
						u.contactIdentity(f, peer, peer.Name, 62, !compact)
						if compact {
							u.contactPeerAction(f, peer)
						}
						u.contactProfileFacts(f, peer, domain.Group{}, true, !compact)
					})
				})
			})
		})
	})
}

func (u *UI) drawContactEmpty(f ui.C) {
	ui.Column(f).Fill().Pad(24).Items(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
		ui.Row(f).Size(62, 62).Radius(18).Background(u.palette.mutedAvatar).Items(ui.Center).Justify(ui.Center).
			Children(func() {
				ui.Icon(f, u.iconUsers).Size(28, 28).Color(u.palette.secondaryText)
			})
		ui.Text(f, i18n.T("选择联系人或群聊")).FontSize(16).Weight(gfx.WeightMedium)
		ui.Paragraph(f, i18n.T("从左侧选择一项，查看资料或开始聊天。"), 2).
			MaxWidth(340).FontSize(13).Muted().TextCenter().LineHeight(20)
	})
}

func (u *UI) contactPeerAction(f ui.C, peer domain.Peer) {
	self := peer.ID == u.state.Self.ID
	label := i18n.T("发消息")
	if self {
		label = i18n.T("编辑资料")
	}
	if namedButton(f, u.ids.contactTalk, label).MinWidth(88).Height(36).PadX(14).Clicked() {
		if self {
			u.openProfile()
		} else {
			u.startDirectFromMembers(peer.ID)
		}
	}
}

func (u *UI) contactIdentity(f ui.C, peer domain.Peer, name string, avatarSize float32, action bool) {
	statusColor := u.palette.secondaryText
	if peer.VisibleOnline() {
		switch peer.Presence {
		case domain.PresenceBusy:
			statusColor = u.palette.danger
		case domain.PresenceAway:
			statusColor = u.palette.icon
		default:
			statusColor = u.palette.success
		}
	}
	ui.Row(f).FillWidth().Items(ui.Center).Gap(13).Children(func() {
		u.avatarWithPresence(f, name, peer.Avatar, avatarSize, false, false, u.theme.Background)
		ui.Column(f).Grow(1).Gap(5).Children(func() {
			ui.Text(f, name).FontSize(22).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Row(f).Gap(8).Items(ui.Center).Children(func() {
				ui.Row(f).Size(7, 7).Radius(3.5).Background(statusColor)
				ui.Text(f, peerPresenceLabel(peer)).FontSize(12).Muted()
				ui.Row(f).Height(21).PadX(7).Items(ui.Center).Radius(6).
					Background(u.palette.selected).Children(func() {
					ui.Text(f, contactClientLabel(peer)).FontSize(11).Color(u.palette.selectedText)
				})
			})
			if peer.Signature != "" {
				ui.Paragraph(f, peer.Signature, 2).FillWidth().FontSize(13).Muted().LineHeight(19)
			}
		})
		if action {
			u.contactPeerAction(f, peer)
		}
	})
}

func (u *UI) drawContactProfile(f ui.C, peer domain.Peer, group domain.Group, avatarSize float32) {
	name := peer.Name
	if group.ID != "" && slices.Contains(group.Members, peer.ID) {
		name = u.groupMemberName(group.ID, peer.ID)
	}
	u.contactIdentity(f, peer, name, avatarSize, false)
	u.contactProfileFacts(f, peer, group, false, false)
}

func (u *UI) contactFact(f ui.C, label, value string) {
	if value == "" {
		return
	}
	ui.Row(f).FillWidth().MinHeight(28).Gap(12).Items(ui.Center).Children(func() {
		ui.Text(f, i18n.T(label)).Width(83).FontSize(12).Muted()
		ui.Text(f, value).FontSize(13).Grow(1).Ellipsis()
	})
}

type contactValue struct{ label, value string }

func (u *UI) contactValues(f ui.C, fields []contactValue, wide bool) {
	if !wide {
		for _, field := range fields {
			u.contactFact(f, field.label, field.value)
		}
		return
	}
	for len(fields) > 0 {
		if fields[0].value == "" {
			fields = fields[1:]
			continue
		}
		left, right := fields[0], contactValue{}
		fields = fields[1:]
		for len(fields) > 0 && fields[0].value == "" {
			fields = fields[1:]
		}
		if len(fields) > 0 {
			right, fields = fields[0], fields[1:]
		}
		ui.Row(f).FillWidth().Gap(18).Children(func() {
			for _, field := range [2]contactValue{left, right} {
				ui.Column(f).Grow(1).Basis(0).Gap(3).Children(func() {
					if field.value != "" {
						ui.Text(f, i18n.T(field.label)).FontSize(12).Muted().Ellipsis()
						ui.Text(f, field.value).FontSize(13).Ellipsis()
					}
				})
			}
		})
	}
}

func (u *UI) contactProfileFacts(f ui.C, peer domain.Peer, group domain.Group, allowRefresh, wide bool) {
	inGroup := group.ID != "" && slices.Contains(group.Members, peer.ID)
	gender := slices.Index(profileGenderValues[:], peer.Details.Gender)
	values := profileDetailValues(peer.Details)
	hasDetails := inGroup || peer.ContactGroup != "" || gender > 0
	for _, value := range values {
		hasDetails = hasDetails || value != ""
	}
	if hasDetails {
		ui.Divider(f)
		ui.Column(f).FillWidth().Gap(8).Children(func() {
			ui.Text(f, i18n.T("联系资料")).FontSize(14).Weight(gfx.WeightSemibold)
			var facts [9]contactValue
			count := 0
			if inGroup {
				role := i18n.T("群成员")
				if group.OwnerID == peer.ID {
					role = i18n.T("群主")
				}
				facts[0], facts[1], count = contactValue{"群聊", group.Name}, contactValue{"群内角色", role}, 2
			}
			facts[count] = contactValue{"联系人分组", peer.ContactGroup}
			count++
			if gender > 0 {
				facts[count] = contactValue{"性别", i18n.T(profileGenderLabels[gender])}
				count++
			}
			for i, value := range values {
				facts[count] = contactValue{profileDetailLabels[i], value}
				count++
			}
			u.contactValues(f, facts[:count], wide)
		})
	}
	refresh := allowRefresh && peer.Transport == domain.TransportNeiwangtong && peer.VisibleOnline()
	if peer.Address != "" || peer.Version != "" || refresh {
		ui.Divider(f)
		ui.Column(f).FillWidth().Gap(8).Children(func() {
			ui.Row(f).FillWidth().MinHeight(26).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(f, i18n.T("设备信息")).FontSize(14).Weight(gfx.WeightSemibold).Grow(1)
				if refresh && namedButton(f, u.ids.contactRefreshAvatar, i18n.T("刷新头像")).Small().Outline().Clicked() {
					u.submit(domain.Command{Kind: domain.RefreshPeerAvatar, PeerID: peer.ID})
				}
			})
			u.contactValues(f, []contactValue{{"设备地址", peer.Address}, {"版本", peer.Version}}, wide)
		})
	}
}

func (u *UI) drawContactGroupProfile(f ui.C, group domain.Group, compact bool) {
	ui.Row(f).FillWidth().Items(ui.Center).Gap(13).Children(func() {
		u.initialsAvatar(f, group.Name, 62)
		ui.Column(f).Grow(1).Gap(5).Children(func() {
			ui.Text(f, group.Name).FontSize(22).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, intString(len(group.Members))+" "+i18n.T("位成员")).FontSize(12).Muted()
		})
		if !compact {
			u.contactGroupAction(f, group.ID)
		}
	})
	if compact {
		u.contactGroupAction(f, group.ID)
	}
	ui.Divider(f)
	ui.Column(f).FillWidth().Gap(3).Children(func() {
		u.contactFact(f, "群主", u.groupMemberName(group.ID, group.OwnerID))
		role := i18n.T("群成员")
		if group.OwnerID == u.state.Self.ID {
			role = i18n.T("群主")
		}
		u.contactFact(f, "我的角色", role)
	})
	ui.Divider(f)
	ui.Column(f).FillWidth().Gap(4).Children(func() {
		ui.Text(f, i18n.T("群成员")).FontSize(14).Weight(gfx.WeightSemibold)
		for _, id := range group.Members {
			peer := u.state.Peer(id)
			name := u.groupMemberName(group.ID, id)
			row := ui.Row(f).FillWidth().Height(40).PadX(5).Radius(7).Items(ui.Center).Gap(9).
				Hover(ui.Look{}.Background(u.palette.hover)).
				HitWith(u.contactMemberIDs[id], name, gfx.HitOptions{Role: gfx.RoleButton}).
				Children(func() {
					u.avatarStatus(f, name, peer.Avatar, 30, peer.VisibleOnline())
					ui.Text(f, name).Ellipsis()
				})
			if row.Clicked() && peer.ID != "" {
				u.memberProfile = id
			}
		}
	})
}

func (u *UI) contactGroupAction(f ui.C, groupID string) {
	if !namedButton(f, u.ids.contactTalk, i18n.T("进入群聊")).Height(36).PadX(14).Clicked() {
		return
	}
	u.setSection(sectionMessages)
	for _, conversation := range u.groupChats {
		if conversation.GroupID == groupID {
			u.selectConversation(conversation.ID)
			return
		}
	}
}

func (u *UI) chatPeerAvatar(f ui.C, hit gfx.ID, peerID, name, path string, size float32) {
	if peerID == "" {
		u.avatarWithPresence(f, name, path, size, false, false, u.palette.chat)
		return
	}
	ui.Stack(f).Size(size, size).
		HitWith(hit, i18n.T("查看资料")+" · "+name, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true}).
		Children(func() { u.avatarWithPresence(f, name, path, size, false, false, u.palette.chat) })
	if event, ok := f.Canvas().PointerOn(hit); ok && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		u.openShellMenu(shellMenuAvatar, peerID, event.X, event.Y)
		f.Canvas().Consume()
	}
}

func (u *UI) drawAvatarContextMenu(canvas *gfx.Canvas, viewport ui.Rect) {
	menu := u.beginShellMenu(canvas, viewport, 1, 0)
	menu.AddItem(components.ContextMenuItem{ID: u.ids.shellMenuProfile, Label: i18n.T("查看资料"), Icon: u.iconUsers})
	menu.End()
	if canvas.Clicked(u.ids.shellMenuProfile) {
		id := u.shellMenu.target
		u.closeShellMenu()
		u.memberProfile = id
	} else if canvas.Clicked(u.ids.shellMenuDismiss) {
		u.closeShellMenu()
	}
}

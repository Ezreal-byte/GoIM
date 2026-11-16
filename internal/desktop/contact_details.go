package desktop

import (
	"github.com/vivaui/viva/gfx"
	"slices"

	"github.com/vivaui/viva/components"
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

func contactConnectionLabel(peer domain.Peer) string {
	switch peer.Transport {
	case domain.TransportIPMSG:
		return i18n.T("飞鸽／飞秋兼容")
	case domain.TransportNeiwangtong:
		return i18n.T("内网通原生")
	default:
		return "GoIM · TLS"
	}
}

func (u *UI) showContact(id string) {
	u.contactPeer, u.contactGroup = id, ""
	u.contactLink = contactLinkOption{}
	u.refreshContactLinkChoices()
}

type contactLinkOption struct {
	id    string
	label string
}

func (u *UI) refreshContactLinkChoices() {
	u.contactLinks = u.contactLinks[:0]
	selected := u.contactLink.id
	u.contactLink = contactLinkOption{}
	peer := u.state.Peer(u.contactPeer)
	if peer.ID == "" || !peer.Online || peer.ClientType != domain.ClientNeiwangtong ||
		peer.CompatibilityAliasOf != "" {
		return
	}
	for _, other := range u.state.Peers {
		if other.CompatibilityAliasOf == peer.ID {
			return
		}
	}
	for _, other := range u.state.Peers {
		if other.Online && other.CompatibilityAliasOf == "" && other.ClientType == domain.ClientNeiwangtong &&
			other.Transport != peer.Transport && (other.Transport == domain.TransportIPMSG || other.Transport == domain.TransportNeiwangtong) {
			link := contactLinkOption{id: other.ID, label: other.Name + " · " + other.Address}
			u.contactLinks = append(u.contactLinks, link)
			if selected == other.ID {
				u.contactLink = link
			}
		}
	}
}

func (u *UI) drawCompatibilityContactActions(f ui.C, peer domain.Peer) {
	if peer.ClientType != domain.ClientNeiwangtong {
		return
	}
	if peer.Transport == domain.TransportNeiwangtong && peer.Online {
		if namedButton(f, u.ids.contactRefreshAvatar, i18n.T("刷新头像")).Outline().Clicked() {
			u.submit(domain.Command{Kind: domain.RefreshPeerAvatar, PeerID: peer.ID})
		}
	}
	if len(u.contactLinks) == 0 {
		return
	}
	ui.Column(f).FillWidth().Gap(6).Children(func() {
		ui.Text(f, i18n.T("关联兼容连接")).Weight(gfx.WeightMedium)
		ui.Paragraph(f, i18n.T("请选择同一台内网通设备。关联后优先使用原生连接，现有聊天记录会保留。"), 3).Muted()
		ui.Select(f.Key(peer.ID), &u.contactLink, u.contactLinks, func(link contactLinkOption) string { return link.label }).
			FillWidth().Height(38).Label(i18n.T("关联兼容连接")).Placeholder(i18n.T("选择同一设备的连接"))
		if namedButton(f, u.ids.contactLink, i18n.T("确认关联")).Outline().Disabled(u.contactLink.id == "").Clicked() {
			u.submit(domain.Command{Kind: domain.LinkCompatibility, PeerID: peer.ID, RelatedPeerID: u.contactLink.id})
			u.contactLink = contactLinkOption{}
		}
	})
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
	u.chatPage.Draw(canvas, bounds, func(f ui.C) {
		ui.Scroll(f.Part("contact-details")).Fill().Children(func() {
			ui.Row(f).FillWidth().Justify(ui.Center).PadSides(32, 22, 28, 22).Children(func() {
				ui.Column(f).FillWidth().MaxWidth(540).Gap(14).Children(func() {
					switch {
					case peer.ID != "":
						u.drawContactProfile(f, peer, domain.Group{}, 68)
						u.drawCompatibilityContactActions(f, peer)
						self := peer.ID == u.state.Self.ID
						action, icon := i18n.T("发消息"), u.iconMessage
						if self {
							action, icon = i18n.T("编辑资料"), u.iconPencil
						}
						if u.contactPrimaryAction(f, icon, action).Clicked() {
							if self {
								u.openProfile()
							} else {
								u.startDirectFromMembers(peer.ID)
							}
						}
					case group.ID != "" && !group.Left && !group.Dissolved:
						u.drawContactGroupProfile(f, group)
					default:
						ui.Text(f, i18n.T("选择联系人或群聊")).FontSize(21).Weight(gfx.WeightSemibold)
						ui.Paragraph(f, i18n.T("在左侧选择一项查看资料，或从消息页继续聊天。"), 3).Muted()
					}
				})
			})
		})
	})
}

func (u *UI) contactPrimaryAction(f ui.C, icon gfx.Icon, label string) ui.Box {
	var action ui.Box
	green := ui.RGBA(7, 193, 96, 1)
	greenHover := ui.RGBA(6, 173, 86, 1)
	white := ui.RGBA(255, 255, 255, 1)
	ui.Row(f).FillWidth().PadY(20).Justify(ui.Center).Children(func() {
		action = ui.Row(f).Width(168).Height(40).Radius(6).Gap(8).Items(ui.Center).Justify(ui.Center).
			Background(green).Hover(ui.Look{}.Background(greenHover)).
			HitWith(u.ids.contactTalk, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label}).
			Children(func() {
				ui.Icon(f, icon).Size(22, 22).Color(white)
				ui.Text(f, label).FontSize(14).Weight(gfx.WeightMedium).Color(white)
			})
	})
	return action
}

func (u *UI) drawContactProfile(f ui.C, peer domain.Peer, group domain.Group, avatarSize float32) {
	name := peer.Name
	inGroup := group.ID != "" && slices.Contains(group.Members, peer.ID)
	if inGroup {
		name = u.groupMemberName(group.ID, peer.ID)
	}
	ui.Row(f).FillWidth().Items(ui.Center).Gap(14).Children(func() {
		u.avatarStatus(f, name, peer.Avatar, avatarSize, peer.VisibleOnline())
		ui.Column(f).Grow(1).Gap(4).Children(func() {
			ui.Text(f, name).FontSize(20).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, peerPresenceLabel(peer)).Muted()
		})
	})
	ui.Divider(f)
	u.fileInfoRow(f, i18n.T("用户类型"), contactClientLabel(peer))
	u.fileInfoRow(f, i18n.T("连接方式"), contactConnectionLabel(peer))
	if inGroup {
		u.fileInfoRow(f, i18n.T("群聊"), group.Name)
		role := i18n.T("群成员")
		if group.OwnerID == peer.ID {
			role = i18n.T("群主")
		}
		u.fileInfoRow(f, i18n.T("群内角色"), role)
	}
	u.fileInfoRow(f, i18n.T("联系人分组"), defaultIfEmpty(peer.ContactGroup, i18n.T("未分组")))
	u.fileInfoRow(f, i18n.T("设备地址"), defaultIfEmpty(peer.Address, i18n.T("未报告地址")))
	add := func(label, value string) {
		if value != "" {
			u.fileInfoRow(f, i18n.T(label), value)
		}
	}
	add("个性签名", peer.Signature)
	if gender := slices.Index(profileGenderValues[:], peer.Details.Gender); gender > 0 {
		add("性别", i18n.T(profileGenderLabels[gender]))
	}
	for i, value := range profileDetailValues(peer.Details) {
		add(profileDetailLabels[i], value)
	}
	add("版本", peer.Version)
	ui.Divider(f)
}

func (u *UI) drawContactGroupProfile(f ui.C, group domain.Group) {
	ui.Row(f).Items(ui.Center).Gap(14).Children(func() {
		u.initialsAvatar(f, group.Name, 68)
		ui.Column(f).Grow(1).Gap(4).Children(func() {
			ui.Text(f, group.Name).FontSize(20).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, i18n.T("群聊资料")).Muted()
		})
	})
	ui.Divider(f)
	u.fileInfoRow(f, i18n.T("群主"), u.groupMemberName(group.ID, group.OwnerID))
	role := i18n.T("群成员")
	if group.OwnerID == u.state.Self.ID {
		role = i18n.T("群主")
	}
	u.fileInfoRow(f, i18n.T("我的角色"), role)
	if u.contactPrimaryAction(f, u.iconMessage, i18n.T("进入群聊")).Clicked() {
		u.setSection(sectionMessages)
		for _, c := range u.groupChats {
			if c.GroupID == group.ID {
				u.selectConversation(c.ID)
				break
			}
		}
	}
	ui.Divider(f)
	ui.Text(f, i18n.T("群成员")+" · "+intString(len(group.Members))).Weight(gfx.WeightSemibold)
	for _, id := range group.Members {
		peer := u.state.Peer(id)
		name := u.groupMemberName(group.ID, id)
		row := ui.Row(f).FillWidth().Height(42).PadX(6).Radius(7).Items(ui.Center).Gap(9).
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

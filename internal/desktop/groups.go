package desktop

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (u *UI) drawSideDrawer(canvas *ui.Canvas) {
	switch u.drawer {
	case "members":
		u.drawMemberDrawer(canvas, u.lastChat)
	case "files":
		u.drawFilesDrawer(canvas, u.lastChat)
	}
}

func (u *UI) drawMemberDrawer(canvas *ui.Canvas, viewport ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	group := u.state.Group(conversation.GroupID)
	if conversation.Kind != "group" || group.ID == "" || group.Left || group.Dissolved {
		u.drawer = ""
		u.activeFileID = ""
		u.fileReturn = ""
		return
	}
	owner := u.state.Self.ID != "" && group.OwnerID == u.state.Self.ID
	extent := min(float32(430), max(viewport.Width-24, 300))
	drawer := components.BeginDrawer(canvas, u.theme, viewport, components.DrawerOptions{
		Layer: 60, Scope: u.ids.memberScope, DismissID: u.ids.memberDismiss,
		CloseID: u.ids.memberClose, Side: components.DrawerRight, ContentExtent: extent,
		InitialFocusID: u.ids.memberSearch, ButtonSize: components.ButtonSizeSmall,
	})
	layout := drawer.Begin("群聊信息", group.Name)
	if u.memberMenu != "" && !containsString(group.Members, u.memberMenu) {
		u.memberMenu = ""
	}

	var menuAnchor flex.Box
	u.drawerView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(11).Children(func() {
			f.Row().Items(flex.Center).Gap(12).Children(func() {
				u.initialsAvatarStatus(f, group.Name, 46, true)
				f.Column().Grow(1).Gap(3).Children(func() {
					f.Text(group.Name).FontSize(17).Weight(ui.WeightSemibold).Ellipsis()
					note := "群主可修改群名"
					if !owner {
						note = "群成员可查看成员信息"
					}
					f.Text(note).FontSize(11).Muted()
				})
				if owner && !u.renameOpen {
					edit := u.iconButton(f, u.ids.memberStartRename, u.iconPencil, "修改群名", 30)
					if edit.Clicked() {
						u.renameOpen = true
						u.renameField.SetText(group.Name)
						u.memberMenu = ""
					}
				}
			})
			if owner && u.renameOpen {
				f.Row().Height(38).Items(flex.Center).Gap(8).Children(func() {
					field := f.Field(&u.renameField, u.ids.memberRename, "群名称", "群名称").Height(36).Grow(1)
					if f.Button(u.ids.memberCancelRename, "取消").XSmall().Outline().Clicked() {
						u.renameOpen = false
					}
					if f.Button(u.ids.memberSaveRename, "保存").XSmall().Clicked() ||
						field.Submitted() {
						u.saveGroupName(group)
					}
				})
			}
			f.Row().Height(26).Items(flex.Center).Children(func() {
				total, _ := u.groupCounts(group)
				f.Text("成员 (" + intString(total) + ")").FontSize(14).
					Weight(ui.WeightSemibold)
				f.Spacer()
				if owner {
					invite := f.Button(u.ids.memberInvite, "邀请成员").XSmall().Outline()
					if invite.Clicked() {
						u.openCompose("invite", group.ID)
					}
				}
			})
			f.Field(&u.memberSearch, u.ids.memberSearch, "搜索群成员", "搜索群成员").Height(36)
			f.ScrollArea(u.ids.memberListScroll).Grow(1).Gap(3).Children(func() {
				query := strings.TrimSpace(u.memberSearch.Text())
				members := orderedMembers(u, group)
				shown := 0
				for _, peer := range members {
					name := peer.Name
					if peer.ID == u.state.Self.ID {
						name = "我"
					}
					if query != "" && !containsFold(peer.Name, query) && !containsFold(name, query) {
						continue
					}
					shown++
					row := f.Row().FillWidth().Height(58).PadSides(7, 8, 7, 8).Radius(9).
						Gap(10).Background(func() ui.Color {
						if peer.ID == u.memberMenu {
							return u.palette.selected
						}
						return ui.Color{}
					}()).HoverBackground(u.palette.hover)
					if owner && peer.ID != u.state.Self.ID {
						row = row.HitWith(u.memberMenuIDs[peer.ID], peer.Name,
							ui.HitOptions{Role: ui.RoleButton, Hint: "成员操作"})
					}
					if peer.ID == u.memberMenu {
						menuAnchor = row
					}
					row.Children(func() {
						u.initialsAvatarStatus(f, peer.Name, 36, peerOnline(u, peer.ID))
						f.Column().Grow(1).Gap(3).Justify(flex.Center).Children(func() {
							f.Row().Items(flex.Center).Gap(6).Children(func() {
								f.Text(name).FontSize(13.5).Weight(ui.WeightMedium).Ellipsis()
								if peer.ID == group.OwnerID {
									f.Row().PadSides(2, 7, 2, 7).Radius(8).
										Background(u.palette.mutedAvatar).Children(func() {
										f.Text("群主").FontSize(9.5).Muted()
									})
								}
							})
							status := "离线"
							if peerOnline(u, peer.ID) {
								status = "在线"
							}
							f.Text(status).FontSize(10.5).Muted()
						})
						if owner && peer.ID != u.state.Self.ID {
							f.Icon(u.iconMore).Size(17, 17).Color(u.palette.secondaryText)
						}
					})
					if row.Clicked() {
						u.memberMenu = peer.ID
						menuAnchor = row
					}
				}
				if shown == 0 && query != "" {
					u.emptyListMessage(f, "没有匹配的成员", "换一个昵称试试。")
				}
				if len(group.Pending) > 0 {
					f.Row().Height(30).Items(flex.Center).Gap(8).Children(func() {
						f.Text("待确认邀请").FontSize(12).Weight(ui.WeightSemibold)
						u.badgeText(f, intString(len(group.Pending)), false)
					})
					for _, id := range group.Pending {
						peer := u.state.Peer(id)
						f.Row().Height(38).PadX(8).Items(flex.Center).Gap(9).Children(func() {
							f.Stack().Size(24, 24).Radius(12).Background(u.palette.mutedAvatar).
								Children(func() {
									f.Text(initialOf(peer.Name)).FontSize(10).Muted().TextCenter()
								})
							f.Text(peer.Name).FontSize(12).Grow(1).Ellipsis()
							f.Text("待确认").FontSize(10.5).Muted()
						})
					}
				}
			})
			f.Divider()
			f.Row().Height(36).Items(flex.Center).Children(func() {
				f.Text("消息免打扰").FontSize(13).Grow(1)
				if f.Switch(u.ids.memberMuteToggle, conversation.Muted).Label("消息免打扰").Clicked() {
					u.submit(domain.Command{Kind: domain.ToggleMute, ConversationID: conversation.ID})
				}
			})
			f.Row().Height(36).Items(flex.Center).Children(func() {
				f.Text("置顶聊天").FontSize(13).Grow(1)
				if f.Switch(u.ids.memberPinToggle, conversation.Pinned).Label("置顶聊天").Clicked() {
					u.submit(domain.Command{Kind: domain.TogglePin, ConversationID: conversation.ID})
				}
			})
		})
	})

	if owner {
		drawer.Actions(layout, components.ButtonAction{
			ID: u.ids.memberDissolve, Label: "解散群聊", Variant: components.ButtonDestructiveOutline,
		})
	} else {
		drawer.Actions(layout, components.ButtonAction{
			ID: u.ids.memberLeave, Label: "退出群聊", Variant: components.ButtonDestructiveOutline,
		})
	}
	drawer.End()

	if canvas.Clicked(u.ids.memberDismiss) || canvas.Clicked(u.ids.memberClose) {
		u.requestCloseDrawer()
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
		anchor := menuAnchor.Rect()
		if anchor.Width <= 0 {
			u.memberMenu = ""
			return
		}
		u.drawMemberMenu(canvas, viewport, group, anchor)
	}
}

func (u *UI) drawMemberMenu(canvas *ui.Canvas, viewport ui.Rect, group domain.Group, anchor ui.Rect) {
	peer := u.state.Peer(u.memberMenu)
	if peer.ID == "" {
		u.memberMenu = ""
		return
	}
	owner := group.OwnerID == u.state.Self.ID
	items := 2
	if owner {
		items++
		if peerOnline(u, peer.ID) {
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
		ID: u.memberInfoIDs[peer.ID], Label: "查看资料", Icon: u.iconEye,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.memberMessageIDs[peer.ID], Label: "发消息", Icon: u.iconMessage,
	})
	if owner && peerOnline(u, peer.ID) {
		menu.AddItem(components.ContextMenuItem{
			ID: u.memberTransferIDs[peer.ID], Label: "转让群主", Icon: u.iconCrown,
		})
	}
	if owner {
		menu.AddItem(components.ContextMenuItem{
			ID: u.memberRemoveIDs[peer.ID], Label: "移出群聊", Variant: components.ContextMenuItemDestructive,
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
		u.setSection(sectionMessages)
		u.startChat(peer.ID)
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

func (u *UI) drawMemberProfile(canvas *ui.Canvas, viewport ui.Rect) {
	peer := u.state.Peer(u.memberProfile)
	if peer.ID == "" {
		u.memberProfile = ""
		return
	}
	group := u.state.Group(u.state.Conversation(u.selected).GroupID)
	role := "群成员"
	if peer.ID == group.OwnerID {
		role = "群主"
	}
	if peer.ID == u.state.Self.ID {
		role = "我 · 群主"
		if peer.ID != group.OwnerID {
			role = "我"
		}
	}
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 74, Scope: u.ids.memberProfileScope, DismissID: u.ids.memberProfileDismiss,
		CloseID: u.ids.memberProfileClose, InitialFocusID: u.ids.memberProfileTalk,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: 164,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin("成员资料", "群聊中的公开信息")
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(12).Children(func() {
			f.Row().Items(flex.Center).Gap(13).Children(func() {
				u.initialsAvatarStatus(f, peer.Name, 52, peerOnline(u, peer.ID))
				f.Column().Grow(1).Gap(4).Children(func() {
					f.Text(peer.Name).FontSize(18).Weight(ui.WeightSemibold).Ellipsis()
					status := "离线"
					if peerOnline(u, peer.ID) {
						status = "在线"
					}
					f.Text(status + " · " + role).FontSize(11).Muted()
				})
			})
			f.Divider()
			u.fileInfoRow(f, "群聊", group.Name)
			u.fileInfoRow(f, "联系人分组", defaultIfEmpty(peer.ContactGroup, "未分组"))
			u.fileInfoRow(f, "设备地址", defaultIfEmpty(peer.Address, "未报告地址"))
		})
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.memberProfileTalk, Label: "发消息"},
	)
	dialog.End()

	switch {
	case canvas.Clicked(u.ids.memberProfileDismiss), canvas.Clicked(u.ids.memberProfileClose):
		u.memberProfile = ""
	case canvas.Clicked(u.ids.memberProfileTalk):
		u.closeDrawer()
		u.setSection(sectionMessages)
		u.startChat(peer.ID)
	}
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (u *UI) saveGroupName(group domain.Group) {
	name := strings.TrimSpace(u.renameField.Text())
	if name == "" || name == group.Name {
		u.renameOpen = false
		return
	}
	if len([]rune(name)) > 30 {
		name = string([]rune(name)[:30])
	}
	u.submit(domain.Command{Kind: domain.RenameGroup, GroupID: group.ID, Name: name})
	u.renameOpen = false
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
		title: "移出群聊？",
		body:  "确定将 " + peer.Name + " 移出「" + group.Name + "」吗？对方将不再收到新的群消息。",
		action: func() {
			u.submit(domain.Command{Kind: domain.RemoveMember, GroupID: group.ID, PeerID: peer.ID})
		},
	}
}

func (u *UI) requestTransferOwner(group domain.Group, peer domain.Peer) {
	u.confirm = confirmation{
		open:  true,
		title: "转让群主？",
		body:  "将群主转让给 " + peer.Name + "，对方确认后生效。",
		action: func() {
			u.submit(domain.Command{Kind: domain.TransferOwner, GroupID: group.ID, PeerID: peer.ID})
		},
	}
}

func (u *UI) requestLeave(group domain.Group) {
	u.confirm = confirmation{
		open:  true,
		title: "退出群聊？",
		body:  "退出「" + group.Name + "」后将不再接收新的群消息，本机历史会保留。",
		action: func() {
			u.submit(domain.Command{Kind: domain.LeaveGroup, GroupID: group.ID})
			u.closeDrawer()
		},
	}
}

func (u *UI) requestDissolve(group domain.Group) {
	u.confirm = confirmation{
		open:  true,
		title: "解散群聊？",
		body:  "解散「" + group.Name + "」后群将不可再发送消息，本机历史会保留。此操作不可撤销。",
		action: func() {
			u.submit(domain.Command{Kind: domain.DissolveGroup, GroupID: group.ID})
			u.closeDrawer()
		},
	}
}

func (u *UI) closeDrawer() {
	u.drawer = ""
	u.activeFileID = ""
	u.fileReturn = ""
	u.memberMenu = ""
	u.memberProfile = ""
	u.renameOpen = false
	u.memberSearch.SetText("")
}

func (u *UI) requestCloseDrawer() {
	if u.drawer == "members" && u.renameOpen {
		group := u.state.Group(u.state.Conversation(u.selected).GroupID)
		name := strings.TrimSpace(u.renameField.Text())
		if group.ID != "" && name != "" && name != group.Name {
			u.confirm = confirmation{
				open:  true,
				title: "放弃未保存的群名？",
				body:  "群名修改尚未保存。",
				action: func() {
					u.closeDrawer()
				},
			}
			return
		}
	}
	u.closeDrawer()
}

func (u *UI) drawFilesDrawer(canvas *ui.Canvas, viewport ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	if conversation.ID == "" {
		u.closeDrawer()
		return
	}
	if u.activeFileID != "" && u.state.File(u.activeFileID).ID == "" {
		u.activeFileID = ""
		u.fileReturn = ""
	}
	detail := u.activeFileID != ""
	title := "聊天文件"
	if detail {
		title = "文件详情"
	}
	extent := min(float32(460), max(viewport.Width-24, 320))
	initialFocus := u.ids.fileSearch
	if detail {
		initialFocus = u.ids.fileBack
	}
	drawer := components.BeginDrawer(canvas, u.theme, viewport, components.DrawerOptions{
		Layer: 60, Scope: u.ids.fileScope, DismissID: u.ids.fileDismiss,
		CloseID: u.ids.fileClose, Side: components.DrawerRight, ContentExtent: extent,
		InitialFocusID: initialFocus, ButtonSize: components.ButtonSizeSmall,
	})
	layout := drawer.Begin(title, u.conversationTitle(conversation))
	u.drawerView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		if detail {
			u.drawFileDetail(f, u.state.File(u.activeFileID))
		} else {
			u.drawFileList(f)
		}
	})
	drawer.End()
	if canvas.Clicked(u.ids.fileDismiss) || canvas.Clicked(u.ids.fileClose) {
		u.closeDrawer()
	}
}

func (u *UI) drawFileList(f *flex.Frame) {
	f.Column().Fill().Gap(10).Children(func() {
		f.Row().Height(32).Gap(7).Children(func() {
			labels := []struct {
				id    ui.ID
				label string
				value string
			}{
				{u.ids.fileFilterAll, "全部", "all"},
				{u.ids.fileFilterImage, "图片", "images"},
				{u.ids.fileFilterFile, "文件", "files"},
			}
			for _, item := range labels {
				active := u.fileFilter == item.value
				background := ui.Color{}
				if active {
					background = u.palette.selected
				}
				filter := f.Row().Height(30).PadX(12).Radius(8).Items(flex.Center).
					Background(background).HoverBackground(u.palette.hover).
					HitWith(item.id, "筛选"+item.label,
						ui.HitOptions{Role: ui.RoleButton, Selected: active}).
					Children(func() {
						foreground := u.palette.secondaryText
						if active {
							foreground = u.palette.selectedText
						}
						f.Text(item.label).FontSize(11.5).Weight(ui.WeightMedium).Color(foreground)
					})
				if filter.Clicked() {
					u.fileFilter = item.value
				}
			}
			f.Spacer()
			f.Icon(u.iconSearch).Size(14, 14).Color(u.palette.secondaryText)
		})
		f.Field(&u.fileSearch, u.ids.fileSearch, "搜索聊天文件", "按文件名搜索").Height(36)
		f.ScrollArea(u.ids.fileListScroll).Grow(1).Gap(4).Children(func() {
			query := strings.TrimSpace(u.fileSearch.Text())
			shown := 0
			for _, file := range u.files {
				if !fileMatchesFilter(file, u.fileFilter, query) {
					continue
				}
				shown++
				row := f.Row().FillWidth().MinHeight(64).Pad(10).Radius(9).Gap(10).
					HoverBackground(u.palette.hover).
					HitWith(u.fileRowIDs[file.ID], file.Name,
						ui.HitOptions{Role: ui.RoleButton, Hint: "文件详情"}).
					Children(func() {
						f.Row().Size(36, 36).Radius(9).Items(flex.Center).Justify(flex.Center).
							Background(fileIconTint(file)).Children(func() {
							f.Icon(u.fileIcon(file)).Size(19, 19).Color(fileIconColor(file))
						})
						f.Column().Grow(1).Gap(3).Justify(flex.Center).Children(func() {
							f.Text(file.Name).FontSize(13).Weight(ui.WeightMedium).Ellipsis()
							sender := "我"
							if file.SenderID != "" && file.SenderID != u.state.Self.ID {
								sender = u.state.Peer(file.SenderID).Name
							}
							f.Text(sender + " · " + shortFileDate(file.CreatedAt) + " · " + formatBytes(file.Size)).
								FontSize(10.5).Muted().Ellipsis()
							f.Text(fileAvailability(u, file)).FontSize(10.5).
								Color(u.availabilityColor(file)).Ellipsis()
						})
						f.Icon(u.iconChevronRight).Size(15, 15).Color(u.palette.secondaryText)
					})
				if row.Clicked() {
					u.openFileDetail(file.ID, "list")
				}
			}
			if len(u.files) == 0 {
				u.emptyListMessage(f, "还没有聊天文件", "发送或接收的文件会出现在这里。")
			} else if shown == 0 {
				u.emptyListMessage(f, "没有匹配的文件", "换一个文件名试试。")
			}
		})
		f.Row().Height(34).Items(flex.Center).Gap(7).Children(func() {
			refresh := u.iconButton(f, u.ids.fileRefresh, u.iconRefresh, "刷新文件状态", 28)
			if refresh.Clicked() {
				u.submit(domain.Command{Kind: domain.RefreshShares})
			}
			f.Text("文件来源为发送方或本机已接收副本").FontSize(10).Muted().Ellipsis()
		})
	})
}

func (u *UI) drawFileDetail(f *flex.Frame, file domain.File) {
	transfer, hasTransfer := u.primaryFileTransfer(file)
	f.Column().Fill().Gap(12).Children(func() {
		if u.fileReturn == "list" {
			if f.Button(u.ids.fileBack, "返回文件列表").XSmall().Outline().Clicked() {
				u.activeFileID = ""
				u.fileReturn = ""
			}
		}
		f.Row().Items(flex.Center).Gap(13).Children(func() {
			f.Row().Size(52, 52).Radius(12).Items(flex.Center).Justify(flex.Center).
				Background(fileIconTint(file)).Children(func() {
				f.Icon(u.fileIcon(file)).Size(25, 25).Color(fileIconColor(file))
			})
			f.Column().Grow(1).Gap(4).Children(func() {
				f.Text(file.Name).FontSize(17).Weight(ui.WeightSemibold).MaxLines(2)
				f.Text(formatBytes(file.Size) + fileTypeSuffix(file)).FontSize(11).Muted()
			})
		})
		if fileIsImage(file) {
			if bitmap := u.fileBitmap(u.fileLocalPath[file.ID]); bitmap.Width > 0 && bitmap.Height > 0 {
				f.Stack().FillWidth().Height(190).Radius(10).Clip().
					Background(u.palette.mutedAvatar).Children(func() {
					f.Image(bitmap).Fill()
				})
			}
		}
		f.Divider()
		u.fileInfoRow(f, "发送者", senderName(u.state, file))
		u.fileInfoRow(f, "发送时间", longDate(file.CreatedAt))
		u.fileInfoRow(f, "所在群聊", u.conversationTitle(u.state.Conversation(file.ConversationID)))
		u.fileInfoRow(f, "本机状态", fileAvailability(u, file))
		if path := u.fileLocalPath[file.ID]; path != "" {
			u.fileInfoRow(f, "本地路径", path)
		}
		f.Divider()
		f.Text("传输状态").FontSize(13).Weight(ui.WeightSemibold)
		f.ScrollArea(u.ids.fileDetailScroll).MaxHeight(190).Grow(1).Gap(4).Children(func() {
			transfers := u.transfersFor[file.ID]
			if len(transfers) == 0 {
				f.Row().PadSides(10, 8, 10, 8).Children(func() {
					f.Text(fileAvailability(u, file)).FontSize(11.5).Muted()
				})
			}
			for _, entry := range transfers {
				peer := u.state.Peer(entry.PeerID)
				f.Row().FillWidth().MinHeight(54).Pad(9).Radius(8).
					Background(u.palette.incoming).Gap(9).Items(flex.Center).Children(func() {
					u.initialsAvatarStatus(f, peer.Name, 28, peerOnline(u, peer.ID))
					f.Column().Grow(1).Gap(3).Children(func() {
						label := transferPeerLabel(u, entry)
						f.Text(label).FontSize(12).Weight(ui.WeightMedium).Ellipsis()
						f.Text(transferStatusText(entry)).FontSize(10.5).Muted().Ellipsis()
					})
					if entry.Status == "transferring" || entry.Status == "verifying" {
						f.Progress(transferFraction(entry)).Width(78).Height(5)
					}
				})
			}
		})
		if hasTransfer && transfer.Error != "" {
			f.Text(transfer.Error).FontSize(11).Color(u.palette.danger).MaxLines(3)
		}
		f.Spacer()
		u.drawFileActions(f, file, transfer, hasTransfer)
	})
}

func (u *UI) drawFileActions(f *flex.Frame, file domain.File, transfer domain.Transfer, hasTransfer bool) {
	localPath := u.fileLocalPath[file.ID]
	localAvailable := u.fileAvailable[file.ID]
	f.Row().Height(36).Gap(8).Items(flex.Center).Children(func() {
		if localPath != "" && localAvailable {
			if f.Button(u.ids.fileOpen, "打开文件").Small().Clicked() {
				_ = app.OpenPath(localPath)
			}
			if f.Button(u.ids.fileReveal, "打开文件夹").Small().Outline().Clicked() {
				_ = app.RevealPath(localPath)
			}
		}
		if !hasTransfer {
			return
		}
		switch transfer.Status {
		case "offered", "waiting":
			if transfer.Direction == "receive" {
				if f.Button(u.ids.fileAccept, "接收").Small().Clicked() {
					u.submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID})
				}
				if f.Button(u.ids.fileSave, "另存为…").Small().Outline().Clicked() {
					u.chooseTransferDestination(transfer)
				}
				if f.Button(u.ids.fileReject, "拒绝").Small().Outline().Clicked() {
					u.submit(domain.Command{Kind: domain.RejectTransfer, TransferID: transfer.ID})
				}
			} else {
				if f.Button(u.ids.fileCancel, "取消发送").Small().Outline().Clicked() {
					u.submit(domain.Command{Kind: domain.CancelTransfer, TransferID: transfer.ID})
				}
			}
		case "transferring", "verifying":
			if f.Button(u.ids.fileCancel, "取消传输").Small().Outline().Clicked() {
				u.submit(domain.Command{Kind: domain.CancelTransfer, TransferID: transfer.ID})
			}
		case "failed", "cancelled", "rejected":
			if f.Button(u.ids.fileRetry, "重试").Small().Clicked() {
				u.submit(domain.Command{Kind: domain.RetryTransfer, TransferID: transfer.ID})
			}
		}
	})
}

func (u *UI) chooseTransferDestination(transfer domain.Transfer) {
	suggested := ""
	for _, file := range u.state.Files {
		if file.ID == transfer.FileID {
			suggested = file.Name
			break
		}
	}
	path, err := app.SaveFile(app.SaveFileOptions{
		Title: "选择接收位置", SuggestedName: suggested,
	})
	if err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) ||
			errors.Is(err, app.ErrUnsupported) {
			return
		}
		return
	}
	if path == "" {
		return
	}
	u.submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID, Paths: []string{path}})
}

func (u *UI) primaryFileTransfer(file domain.File) (domain.Transfer, bool) {
	transfers := u.transfersFor[file.ID]
	if len(transfers) == 0 {
		return domain.Transfer{}, false
	}
	for _, transfer := range transfers {
		if file.SenderID != "" && file.SenderID != u.state.Self.ID && transfer.Direction == "receive" {
			return transfer, true
		}
		if file.SenderID == u.state.Self.ID && transfer.Direction == "send" {
			return transfer, true
		}
	}
	return transfers[0], true
}

func (u *UI) fileInfoRow(f *flex.Frame, label, value string) {
	f.Row().MinHeight(30).Items(flex.Center).Gap(12).Children(func() {
		f.Text(label).Width(82).FontSize(11.5).Muted()
		f.Text(value).FontSize(11.5).Grow(1).Ellipsis()
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
		return "未知"
	}
	if file.SenderID == state.Self.ID {
		return "我"
	}
	return state.Peer(file.SenderID).Name
}

func transferPeerLabel(u *UI, transfer domain.Transfer) string {
	name := u.state.Peer(transfer.PeerID).Name
	if transfer.PeerID == u.state.Self.ID || transfer.PeerID == "" {
		name = "我"
	}
	if transfer.Direction == "send" {
		return "发送给 " + name
	}
	return "接收自 " + name
}

func transferStatusText(transfer domain.Transfer) string {
	switch transfer.Status {
	case "offered":
		return "等待接收"
	case "waiting":
		return "等待连接"
	case "transferring":
		return "传输中 · " + intString(int(transferFraction(transfer)*100)) + "%"
	case "verifying":
		return "校验中"
	case "completed":
		return "已接收"
	case "rejected":
		return "已拒绝"
	case "cancelled":
		return "已取消"
	case "failed":
		if transfer.Error != "" {
			return "失败 · " + transfer.Error
		}
		return "失败"
	default:
		return transfer.Status
	}
}

func fileTypeSuffix(file domain.File) string {
	if file.MIME != "" {
		return " · " + file.MIME
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
		return "时间未知"
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
		return "时间未知"
	}
	return at.Local().Format("2006-01-02 15:04")
}

func (u *UI) drawChatMenu(canvas *ui.Canvas) {
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
		ID: u.ids.chatMenuPin, Label: "置顶聊天", Kind: components.ContextMenuCheckbox,
		Checked: conversation.Pinned, Icon: u.iconPin,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.chatMenuMute, Label: "消息免打扰", Kind: components.ContextMenuCheckbox,
		Checked: conversation.Muted, Icon: u.iconBellOff,
	})
	menu.AddItem(components.ContextMenuItem{
		ID: u.ids.chatMenuRead, Label: "标为已读", Icon: u.iconCheckCheck,
		Disabled: conversation.Unread == 0,
	})
	if items == 4 {
		menu.AddSeparator()
		leaveLabel := "退出群聊"
		if group.OwnerID == u.state.Self.ID {
			leaveLabel = "解散群聊"
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

func (u *UI) drawConnectDialog(canvas *ui.Canvas, viewport ui.Rect) {
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 70, Scope: u.ids.connectScope, DismissID: u.ids.connectDismiss,
		CloseID: u.ids.connectClose, InitialFocusID: u.ids.connectField,
		ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: 108,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin("手动连接", "输入对方设备地址。连接成功后，对端联系人会出现在列表中。")
	var connect flex.Box
	var submitted bool
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Column().Fill().Gap(8).Children(func() {
			f.Text("设备地址").FontSize(12).Weight(ui.WeightMedium)
			connect = f.Field(&u.connectField, u.ids.connectField, "设备地址",
				"例如 192.168.1.12:7331").Height(38)
			f.Text("地址只保存在本机连接记录中。").FontSize(10.5).Muted()
			if u.state.Error != "" {
				f.Text(u.state.Error).FontSize(11).Color(u.palette.danger).MaxLines(2)
			}
		})
	})
	submitted = connect.Submitted()
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.connectCancel, Label: "取消", Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.connectSubmit, Label: "连接"},
	)
	dialog.End()

	if canvas.Clicked(u.ids.connectDismiss) || canvas.Clicked(u.ids.connectClose) ||
		canvas.Clicked(u.ids.connectCancel) {
		u.connectOpen = false
		return
	}
	if submitted || canvas.Clicked(u.ids.connectSubmit) {
		address := strings.TrimSpace(u.connectField.Text())
		if address == "" {
			return
		}
		u.submit(domain.Command{Kind: domain.Connect, Address: address})
		u.connectOpen = false
	}
}

func (u *UI) drawComposeDialog(canvas *ui.Canvas, viewport ui.Rect) {
	create := u.compose.mode == "create"
	group := u.state.Group(u.compose.groupID)
	if !create && group.ID == "" {
		u.compose.open = false
		return
	}
	title, description := "创建群聊", "选择至少 2 位在线联系人，邀请确认后加入群聊。"
	if !create {
		title = "邀请成员"
		description = "邀请在线联系人加入「" + group.Name + "」，对方确认后计入成员。"
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
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin(title, description)
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		u.drawComposeBody(f, create, group)
	})
	selected := u.selectedPeersForCompose()
	ready := len(selected) >= 2
	if !create {
		ready = len(selected) >= 1
	}
	label := "创建并邀请（" + intString(len(selected)) + "）"
	if !create {
		label = "邀请（" + intString(len(selected)) + "）"
	}
	if u.compose.pending {
		if create {
			label = "正在创建…"
		} else {
			label = "正在邀请…"
		}
	}
	dialog.Actions(layout,
		components.ButtonAction{
			ID: u.ids.composeCancel, Label: "取消", Variant: components.ButtonOutline,
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

func (u *UI) drawComposeBody(f *flex.Frame, create bool, group domain.Group) {
	f.Column().Fill().Gap(10).Children(func() {
		if create {
			f.Row().Height(36).Items(flex.Center).Gap(12).Children(func() {
				f.Text("群名称（可选）").Width(96).FontSize(12).Weight(ui.WeightMedium)
				f.Field(&u.compose.name, u.ids.composeName, "群名称", "留空将按成员昵称生成").
					Height(36).Grow(1)
			})
		}
		f.Field(&u.compose.search, u.ids.composeSearch, "搜索联系人", "搜索昵称或分组").
			Height(36)
		f.Divider()
		f.Row().Grow(1).Gap(18).Children(func() {
			f.Column().Grow(1).Gap(6).Children(func() {
				f.Text("可邀请联系人").FontSize(12).Weight(ui.WeightSemibold)
				f.ScrollArea(u.ids.composeScroll).Grow(1).Gap(2).Children(func() {
					u.drawComposeCandidates(f, create, group)
				})
			})
			f.Row().Width(1).Background(u.palette.border)
			f.Column().Width(236).Gap(7).Children(func() {
				selected := u.selectedPeersForCompose()
				f.Row().Height(24).Items(flex.Center).Children(func() {
					f.Text("已选择 " + intString(len(selected)) + " 人").FontSize(12).
						Weight(ui.WeightSemibold)
					f.Spacer()
				})
				f.ScrollArea(u.ids.composePicked).Grow(1).Gap(4).Children(func() {
					if len(selected) == 0 {
						f.Column().PadSides(10, 12, 8, 12).Gap(5).Children(func() {
							f.Text("尚未选择联系人").FontSize(12).Weight(ui.WeightMedium)
							f.Text("勾选左侧在线联系人。").FontSize(10.5).Muted()
						})
					}
					for _, id := range selected {
						peer := u.state.Peer(id)
						f.Row().Height(38).Items(flex.Center).Gap(8).Children(func() {
							u.initialsAvatarStatus(f, peer.Name, 26, true)
							f.Text(peer.Name).FontSize(12).Grow(1).Ellipsis()
							remove := f.Row().Size(24, 24).Radius(6).Items(flex.Center).
								Justify(flex.Center).HoverBackground(u.palette.hover).
								HitWith(u.groupRemoveIDs[peer.ID], "移除 "+peer.Name,
									ui.HitOptions{Role: ui.RoleButton, Hint: "移除"}).
								Children(func() {
									f.Icon(u.iconX).Size(14, 14).Color(u.palette.secondaryText)
								})
							if remove.Clicked() {
								delete(u.compose.selected, peer.ID)
							}
						})
					}
				})
				f.Text("你将自动加入群聊。").FontSize(10.5).Muted()
				if u.compose.pending {
					f.Row().Height(26).Items(flex.Center).Gap(8).Children(func() {
						f.Spinner()
						f.Text("正在提交…").FontSize(11).Muted()
					})
				}
				if u.compose.err != "" {
					f.Text(u.compose.err).FontSize(10.5).Color(u.palette.danger).MaxLines(3)
				}
			})
		})
		f.Row().Height(20).Items(flex.Center).Children(func() {
			f.Text("邀请尚待确认时仍可进入群聊。").FontSize(10.5).Muted().Ellipsis()
		})
	})
}

func (u *UI) drawComposeCandidates(f *flex.Frame, create bool, group domain.Group) {
	query := strings.TrimSpace(u.compose.search.Text())
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
		header := f.Row().Height(28).Items(flex.Center).Gap(6).Radius(6).
			HoverBackground(u.palette.hover).
			HitWith(headerID, contactGroup.Name,
				ui.HitOptions{Role: ui.RoleButton, Expanded: !collapsed}).
			Children(func() {
				icon := u.iconChevronDown
				if collapsed {
					icon = u.iconChevronRight
				}
				f.Icon(icon).Size(14, 14).Color(u.palette.secondaryText)
				f.Text(contactGroup.Name).FontSize(12).Weight(ui.WeightSemibold).Ellipsis()
				f.Spacer()
				f.Text(intString(len(peers))).FontSize(10.5).Muted()
			})
		if header.Clicked() {
			u.collapsed[contactGroup.Name] = !collapsed
		}
		if collapsed {
			continue
		}
		for _, peer := range peers {
			selected := u.compose.selected[peer.ID]
			available := peer.Online
			row := f.Row().FillWidth().Height(46).PadSides(5, 8, 5, 8).Radius(7).Gap(8).
				Background(func() ui.Color {
					if selected {
						return u.palette.selected
					}
					return ui.Color{}
				}()).HoverBackground(u.palette.hover)
			if available {
				row = row.HitWith(u.groupSelectRows[peer.ID], peer.Name,
					ui.HitOptions{Role: ui.RoleButton, Selected: selected})
			}
			var checkbox flex.Box
			row.Children(func() {
				u.initialsAvatarStatus(f, peer.Name, 30, peer.Online)
				f.Column().Grow(1).Gap(2).Justify(flex.Center).Children(func() {
					f.Text(peer.Name).FontSize(12.5).Weight(ui.WeightMedium).Ellipsis()
					status := "在线"
					if !peer.Online {
						status = "离线 · 上线后可邀请"
					} else if selected {
						status = "已选择"
					}
					f.Text(status).FontSize(10.5).Muted().Ellipsis()
				})
				checkbox = f.Checkbox(u.groupSelectIDs[peer.ID], selected).
					Disabled(!available).Label("选择 " + peer.Name)
			})
			if available && (row.Clicked() || checkbox.Clicked()) {
				u.compose.selected[peer.ID] = !u.compose.selected[peer.ID]
			}
		}
	}
	if shown == 0 {
		u.emptyListMessage(f, "没有可邀请的联系人", "联系人上线后会出现在这里。")
	}
}

func (u *UI) drawConfirmation(canvas *ui.Canvas, viewport ui.Rect) {
	dialog := components.BeginDialog(canvas, u.theme, viewport, components.DialogOptions{
		Layer: 76, Scope: u.ids.confirmScope, DismissID: u.ids.confirmDismiss,
		CloseID: u.ids.confirmClose, InitialFocusID: u.ids.confirmCancel,
		ContentSize: ui.Size{Width: 440}, FitBody: true, BodyHeight: 8,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin(u.confirm.title, u.confirm.body)
	u.dialogView.Draw(canvas, u.theme, layout.Body, func(f *flex.Frame) {
		f.Row().Fill()
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: u.ids.confirmCancel, Label: "取消", Variant: components.ButtonOutline},
		components.ButtonAction{ID: u.ids.confirmRun, Label: "确认", Variant: components.ButtonDestructive},
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

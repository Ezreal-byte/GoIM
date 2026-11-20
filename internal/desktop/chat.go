package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) drawChat(canvas *gfx.Canvas, bounds ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	u.surface.filesPane = ui.Rect{}
	if u.drawer != "" && conversation.ID == "" {
		u.closeDrawer()
	}
	if u.drawer != "" {
		panel := bounds
		// On compact windows the inspector occupies the workspace, with a clear
		// close action. Keep hidden chat controls out of the hit tree entirely.
		if bounds.Width >= 800 {
			panel.Width = min(float32(400)*u.theme.Metrics.EffectiveTextScale(), bounds.Width*.46)
			panel.X = bounds.Right() - panel.Width
			bounds.Width -= panel.Width
			u.drawChatSurface(canvas, bounds, conversation)
		}
		if u.drawer == "members" {
			u.drawMemberPane(canvas, panel)
		} else {
			u.drawChatFilesPane(canvas, panel)
		}
	} else {
		u.drawChatSurface(canvas, bounds, conversation)
	}
	// The image viewer is a shell-level overlay so it stays above menus and
	// dialogs; the shell paints drawChatImageViewer after drawOverlays.
}

// chatTimeline records the last layout for unread handling and search. Viva
// owns the live scroll state and can reuse it when a scroll area disappears.
type chatTimeline struct {
	conversation string
	offset       float32
	atEnd        bool
}

func (u *UI) drawChatSurface(canvas *gfx.Canvas, bounds ui.Rect, conversation domain.Conversation) {
	canvas.Fill(bounds, u.palette.chat)
	if u.drawChatBackground(canvas, bounds) {
		// Viva composites images after shapes in each layer. Move the tint,
		// message cards, and controls above the wallpaper as one surface.
		canvas.PushLayer(1)
		defer canvas.PopLayer()
		canvas.Fill(bounds, u.chatBackgroundTint())
	}
	if conversation.ID == "" {
		u.timeline = chatTimeline{}
		u.chatPage.Draw(canvas, bounds, func(f ui.C) {
			ui.Column(f).Fill().Children(func() {
				if u.state.Error != "" {
					u.drawChatNotice(f)
				}
				u.drawChatEmpty(f)
			})
		})
		return
	}
	surface := u.surface
	readOnlyReason := u.chatReadOnlyReason(conversation)
	composerMinimum := chatComposerMinHeight
	if readOnlyReason != "" {
		composerMinimum = chatReadOnlyMinHeight
	}
	var timeline ui.Box
	u.locateMessageBox = ui.Box{}
	u.chatPage.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Children(func() {
			u.drawChatHeader(f, conversation)
			searchVisible := u.chatFind.open
			if searchVisible {
				u.drawChatSearchBar(f)
			}
			surface.layoutHistory(bounds.Height-(u.textExtent(chatHeaderExtent, 32)-chatHeaderExtent), composerMinimum, searchVisible)
			ui.Column(f).Height(surface.history.Pos).Children(func() {
				u.drawChatNotice(f)
				timeline = u.drawChatTimeline(f, conversation)
			})
			ui.Splitter(f, &surface.history).
				Mins(chatHistoryMinHeight, composerMinimum).
				Label(i18n.T("调整聊天记录和输入框高度"))
			ui.Row(f).Grow(1).Basis(0).Children(func() {
				if readOnlyReason != "" {
					u.drawChatReadOnly(f, conversation, readOnlyReason)
				} else {
					u.drawComposer(f)
				}
			})
		})
	})
	u.timeline = chatTimeline{conversation: conversation.ID, atEnd: true}
	if timeline.Valid() {
		scroll := timeline.ScrollState()
		if u.chatFind.restorePending && !u.chatFind.savedAtEnd {
			// The full history has been measured again. Resume following from
			// the restored position, so it stays put if it is above the end.
			scroll.Follow(true)
			scroll.ScrollTo(scroll.Offset)
		}
		u.timeline.offset, u.timeline.atEnd = scroll.Offset, scroll.AtEnd()
		if u.locateMessageID != "" && u.locateMessageBox.Valid() {
			rect := u.locateMessageBox.Rect()
			scroll.ScrollTo(scroll.Offset + rect.Y - timeline.Rect().Y - max((timeline.Rect().Height-rect.Height)/2, 0))
			u.locateMessageID = ""
			canvas.RequestFrame()
		}
	}
	u.chatFind.restorePending = false
	if u.dragging {
		region := u.composerBox.Rect()
		overlay := ui.Rect{
			X: region.X + chatContentInset, Y: region.Y + 8,
			Width: max(region.Width-2*chatContentInset, 0), Height: max(region.Height-16, 0),
		}
		canvas.PushLayer(10)
		defer canvas.PopLayer()
		canvas.Rounded(overlay, radiusControl, u.palette.chat.WithAlpha(0.96))
		canvas.DashedRoundedOutline(overlay, radiusControl, 1.5, u.theme.MutedForeground)
		canvas.LabelCenter(i18n.T("松开以发送文件"), overlay, gfx.TextStyle{
			Size: 16 * u.theme.Metrics.EffectiveTextScale(), Weight: gfx.WeightMedium, Color: u.theme.Foreground,
		})
	}
}

func (u *UI) drawChatEmpty(f ui.C) {
	ui.Column(f).Fill().Items(ui.Center).Justify(ui.Center).Pad(42).Gap(12).Children(func() {
		ui.Row(f).Size(62, 62).Radius(18).Background(u.palette.mutedAvatar).Items(ui.Center).
			Justify(ui.Center).Children(func() {
			ui.Icon(f, u.iconMessage).Size(28, 28).Color(u.palette.secondaryText)
		})
		ui.Text(f, i18n.T("选择一段会话开始聊天")).FontSize(16).Weight(gfx.WeightMedium)
		ui.Paragraph(f, i18n.T("消息只在局域网内直连。联系人上线后会自动出现，也可以手动输入地址连接。"), 3).
			MaxWidth(430).FontSize(13).Muted().TextCenter().LineHeight(20)
		ui.Row(f).Gap(10).Justify(ui.Center).Children(func() {
			connect := namedButton(f, u.ids.chatEmptyConnect, i18n.T("手动连接")).Outline()
			if connect.Clicked() {
				u.openConnect()
			}
			compose := namedButton(f, u.ids.chatEmptyCompose, i18n.T("创建群聊"))
			if compose.Clicked() {
				u.openCompose("create", "")
			}
		})
		ui.Row(f).Height(10)
	})
}

func (u *UI) drawChatHeader(f ui.C, conversation domain.Conversation) {
	title := u.conversationTitle(conversation)
	avatarName := title
	avatar := ""
	online := false
	subtitle := i18n.T("离线")
	if conversation.Kind == "group" {
		group := u.state.Group(conversation.GroupID)
		avatar = group.Avatar
		total, active := u.groupCounts(group)
		if total > 0 {
			title += " (" + intString(total) + ")"
		}
		online = active > 0
		subtitle = intString(active) + i18n.T(" 人在线")
		switch {
		case group.Dissolved:
			online = false
			subtitle = i18n.T("群聊已解散 · 仅可查看历史")
		case group.Left:
			online = false
			subtitle = i18n.T("已退出群聊 · 仅可查看历史")
		}
	} else if peer := u.state.Peer(conversation.PeerID); peer.ID != "" {
		online = peer.VisibleOnline()
		if online {
			subtitle = peerPresenceLabel(peer)
			if peer.Address != "" {
				subtitle += " · " + peer.Address
			}
		} else if !peer.LastSeen.IsZero() {
			subtitle = i18n.T("上次在线 ") + relativeTime(peer.LastSeen)
		}
	}

	var searchButton, fileButton, memberButton, moreButton ui.Box
	memberShown := false
	ui.Row(f).Height(u.textExtent(chatHeaderExtent, 32)).FillWidth().PadX(chatContentInset).Items(ui.Center).Gap(10).Children(func() {
		if conversation.Kind == "direct" {
			peer := u.state.Peer(conversation.PeerID)
			avatar = peer.Avatar
			if peer.Name != "" {
				avatarName = peer.Name
			}
		}
		if conversation.Kind == "direct" {
			u.chatPeerAvatar(f, u.ids.chatHeaderAvatar, conversation.PeerID, avatarName, avatar, 38)
		} else {
			u.avatarWithPresence(f, avatarName, avatar, 38, online, false, u.palette.chat)
		}
		ui.Column(f).Grow(1).Basis(0).MinWidth(0).Gap(1).Justify(ui.Center).Children(func() {
			username := ""
			if conversation.Kind == "direct" {
				username = remarkUsername(u.state.Peer(conversation.PeerID))
			}
			u.peerNameLine(f, title, username, 17)
			ui.Row(f).Items(ui.Center).Gap(7).Children(func() {
				dot := u.palette.secondaryText
				if online {
					dot = u.palette.success
				}
				ui.Stack(f).Size(6, 6).Radius(3).Background(dot)
				ui.Text(f, subtitle).FontSize(11.5).Muted().Ellipsis()
			})
		})
		ui.Row(f).Items(ui.Center).Gap(3).Children(func() {
			searchButton = u.sizedIconButton(f, u.ids.chatHeaderSearch, u.iconSearch, i18n.T("搜索当前会话"), 34, 20)
			fileButton = u.sizedIconButton(f, u.ids.chatHeaderFile, u.iconFolder, i18n.T("聊天文件"), 34, 20)
			if conversation.Kind == "group" {
				group := u.state.Group(conversation.GroupID)
				if group.ID == "" || (!group.Left && !group.Dissolved) {
					memberButton = u.sizedIconButton(f, u.ids.chatHeaderMembers, u.iconUsers, i18n.T("群聊成员"), 34, 20)
					memberShown = true
				}
			}
			moreButton = u.sizedIconButton(f, u.ids.chatHeaderMore, u.iconMore, i18n.T("更多会话操作"), 34, 20)
		})
	})
	if searchButton.Clicked() {
		u.moreOpen = false
		u.openChatSearch()
	}
	if fileButton.Clicked() {
		u.toggleFilesPane()
	}
	if memberShown && memberButton.Clicked() {
		u.closeChatPickers()
		if u.drawer == "members" {
			u.requestCloseDrawer()
			return
		}
		u.drawer = "members"
		u.memberSearch.SetText("")
		u.activeFileID = ""
		u.memberMenu = ""
		u.moreOpen = false
	}
	if moreButton.Clicked() {
		u.moreOpen = !u.moreOpen
	}
}

func (u *UI) drawChatNotice(f ui.C) {
	if u.state.Error == "" {
		return
	}
	ui.Row(f).FillWidth().PadSides(8, chatContentInset, 8, chatContentInset).Height(38).Children(func() {
		ui.Row(f).Fill().PadSides(8, 10, 8, 12).Radius(8).
			Background(ui.RGBA(220, 48, 54, 0.08)).Items(ui.Center).Gap(8).Children(func() {
			ui.Icon(f, u.iconAlert).Size(16, 16).Color(u.palette.danger)
			ui.Text(f, i18n.T(u.state.Error)).FontSize(12).Color(u.palette.danger).Grow(1).Ellipsis()
			dismiss := u.iconButton(f, u.ids.chatNoticeDismiss, u.iconX, i18n.T("关闭错误提示"), 24)
			if dismiss.Clicked() {
				u.submit(domain.Command{Kind: domain.ClearError})
			}
		})
	})
}

func (u *UI) drawChatTimeline(f ui.C, conversation domain.Conversation) ui.Box {
	messages := u.visibleChatMessages()
	if len(messages) == 0 {
		ui.Column(f).Grow(1).Items(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			ui.Row(f).Size(50, 50).Radius(15).Background(u.palette.mutedAvatar).Items(ui.Center).
				Justify(ui.Center).Children(func() {
				icon := u.iconMessageSquare
				if u.chatFind.open && u.chatFind.query != "" {
					icon = u.iconSearch
				}
				ui.Icon(f, icon).Size(23, 23).Color(u.palette.secondaryText)
			})
			if u.chatFind.open && u.chatFind.query != "" {
				ui.Text(f, i18n.T("没有匹配的消息")).FontSize(16).Weight(gfx.WeightMedium)
				ui.Text(f, i18n.T("换一个关键词，搜索消息文字或文件名。")).FontSize(12).Muted().TextCenter()
			} else {
				ui.Text(f, i18n.T("还没有消息")).FontSize(16).Weight(gfx.WeightMedium)
				hint := i18n.T("发送一条消息开始聊天。")
				if conversation.Kind == "group" {
					hint = i18n.T("发送第一条群消息，邀请会在成员确认后生效。")
				}
				ui.Text(f, hint).FontSize(12).Muted().TextCenter()
			}
		})
		return ui.Box{}
	}

	restoreOffset := u.chatFind.restorePending && !u.chatFind.savedAtEnd
	area := ui.Scroll(f.Part("message-history")).Grow(1).Follow(!restoreOffset).Scrollbars().MarginBottom(12)
	if !u.chatFind.open {
		area.Justify(ui.End)
	}
	scroll := area.ScrollState()
	if restoreOffset {
		scroll.ScrollTo(u.chatFind.savedOffset)
	} else if u.chatFind.restorePending {
		scroll.ScrollToEnd()
	}
	area.Children(func() {
		ui.Column(f).FillWidth().PadSides(10, chatContentInset, 2, chatContentInset).Gap(14).Children(func() {
			lastDay := ""
			for _, message := range messages {
				day := message.CreatedAt.Local().Format("2006-01-02")
				if message.CreatedAt.IsZero() {
					day = ""
				}
				if day != lastDay {
					if day != "" {
						u.drawDateSeparator(f, message.CreatedAt)
					}
					lastDay = day
				}
				box := ui.Column(f.Key(message.ID)).FillWidth()
				if u.highlightMessageID == message.ID {
					box.Background(u.palette.selected).Radius(10).PadY(6)
				}
				box.Children(func() { u.drawMessage(f, conversation, message) })
				if u.locateMessageID == message.ID {
					u.locateMessageBox = box
				}
			}
		})
	})
	if u.chatScrollToEnd {
		scroll.ScrollToEnd()
		u.chatScrollToEnd = false
	}
	return area
}

func (u *UI) drawDateSeparator(f ui.C, at time.Time) {
	ui.Row(f).FillWidth().Height(26).Items(ui.Center).Justify(ui.Center).Children(func() {
		ui.Text(f, friendlyDay(at)).FontSize(12).Muted().Ellipsis()
	})
}

func (u *UI) drawMessage(f ui.C, conversation domain.Conversation, message domain.Message) {
	if message.Recalled {
		u.drawRecalledMessage(f, conversation, message)
		return
	}
	if isVoiceRecord(message) {
		u.drawVoiceRecord(f, message)
		return
	}
	if message.Kind == "system" {
		ui.Row(f).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(f).MaxWidthPercent(80).PadSides(7, 12, 7, 12).Radius(8).
				Background(u.palette.mutedAvatar).Children(func() {
				ui.Text(f, message.Text).FontSize(11).Muted().TextCenter().MaxLines(3)
			})
		})
		return
	}
	own := message.SenderID == u.state.Self.ID
	if conversation.Kind == "direct" && message.SenderID == "" {
		own = false
	}
	bubbleWidth := min(float32(620), max(float32(250), f.Bounds().Width*0.61))

	switch message.Kind {
	case "image":
		u.drawImageMessage(f, conversation, message, own, bubbleWidth)
	case "file":
		u.drawFileMessage(f, conversation, message, own, bubbleWidth)
	case "print":
		u.drawPrintMessage(f, conversation, message, own, bubbleWidth)
	default:
		u.drawTextMessage(f, conversation, message, own, bubbleWidth)
	}
}

func (u *UI) drawTextMessage(f ui.C, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	if own {
		ui.Row(f).FillWidth().Justify(ui.End).Items(ui.End).Gap(9).Children(func() {
			u.messageTime(f, message, true)
			u.messageBubble(f, conversation, message, true, maxWidth)
			u.ownMessageAvatar(f)
		})
		return
	}
	ui.Row(f).FillWidth().Justify(ui.Start).Items(ui.End).Gap(10).Children(func() {
		peer := u.state.Peer(message.SenderID)
		name := u.messageSenderName(conversation, message)
		u.chatPeerAvatar(f, u.messageAvatarIDs[message.ID], peer.ID, name, peer.Avatar, 34)
		ui.Column(f).Gap(4).Items(ui.Start).Children(func() {
			if conversation.Kind == "group" {
				ui.Text(f, name).FontSize(11.5).Muted().Ellipsis()
			}
			ui.Row(f).Items(ui.End).Gap(9).Children(func() {
				u.messageBubble(f, conversation, message, false, maxWidth)
				u.messageTime(f, message, false)
			})
		})
	})
}

func (u *UI) messageBubble(f ui.C, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) ui.Box {
	background := u.palette.incoming
	if own {
		background = u.palette.own
	}
	bubble := ui.Column(f).MaxWidth(maxWidth).PadSides(10, 13, 9, 13).Radius(radiusControl).
		HitWith(u.messageIDs[message.ID], i18n.T("聊天消息"), gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
		Background(background).Border(1, u.palette.bubbleBorder).Gap(3).Children(func() {
		if message.Text != "" {
			u.messageParagraph(f, message, 60, 14, 21)
		}
		if message.Kind == "text" && message.Text == "" {
			ui.Text(f, i18n.T("（空消息）")).FontSize(13).Muted()
		}
	})
	u.messageContextPointer(f.Canvas(), u.messageIDs[message.ID], message)
	return bubble
}

func (u *UI) messageTime(f ui.C, message domain.Message, own bool) {
	status := ""
	if own {
		// Keep actionable pending/error states; ordinary sends need no receipt text.
		switch message.Status {
		case "sent", "delivered":
		default:
			status = messageStatus(message.Status)
		}
	}
	label := shortTime(message.CreatedAt)
	if status != "" {
		if label != "" {
			label += " · "
		}
		label += status
	}
	if label == "" {
		return
	}
	ui.Column(f).PadX(1).Children(func() {
		ui.Text(f, label).FontSize(10.5).Muted()
	})
}

func (u *UI) ownMessageAvatar(f ui.C) {
	self := u.state.Self
	u.avatarWithPresence(f, self.Name, self.Avatar, 34, false, false, ui.Color{})
}

func (u *UI) drawImageMessage(f ui.C, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	file := u.state.File(message.FileID)
	path := u.fileLocalPath[file.ID]
	bitmap := u.fileBitmap(path)
	if !bitmapReady(bitmap) {
		u.drawFileMessage(f, conversation, message, own, maxWidth)
		return
	}
	width, height := chatImageThumbnail(bitmap, maxWidth)
	justify := ui.Start
	align := ui.Start
	if own {
		justify, align = ui.End, ui.End
	}
	ui.Row(f).FillWidth().Justify(justify).Items(ui.Start).Gap(10).Children(func() {
		if !own {
			peer := u.state.Peer(message.SenderID)
			u.chatPeerAvatar(f, u.messageAvatarIDs[message.ID], peer.ID, u.messageSenderName(conversation, message), peer.Avatar, 34)
		}
		ui.Column(f).Gap(4).Items(align).Children(func() {
			if !own && conversation.Kind == "group" {
				ui.Text(f, u.messageSenderName(conversation, message)).FontSize(11.5).Muted().Ellipsis()
			}
			picture := ui.Stack(f).Width(width).Height(height).Radius(radiusControl).Clip().
				Background(u.palette.mutedAvatar)
			if file.ID != "" {
				picture = picture.HitWith(u.fileButtonIDs[file.ID], i18n.T("查看图片 ")+displayFileName(file, message),
					gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("打开图片查看器")})
			}
			picture.Children(func() {
				ui.Image(f, bitmap).Fill()
			})
			u.messageContextPointer(f.Canvas(), u.fileButtonIDs[file.ID], message)
			if picture.Clicked() {
				u.openImageViewer(file.ID)
			}
			if message.Text != "" && message.Text != file.Name {
				ui.Column(f).MaxWidth(width).PadSides(8, 12, 7, 12).Radius(radiusControl).
					Background(u.palette.incoming).Children(func() {
					u.messageParagraph(f, message, 4, 13, 19)
				})
			}
			ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
				u.messageTime(f, message, own)
			})
		})
		if own {
			u.ownMessageAvatar(f)
		}
	})
}

func chatImageThumbnail(bitmap gfx.Bitmap, maxWidth float32) (float32, float32) {
	if bitmap.Width <= 0 || bitmap.Height <= 0 {
		return 0, 0
	}
	maxW := min(float32(330), maxWidth)
	maxH := float32(250)
	if maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	scale := min(maxW/float32(bitmap.Width), maxH/float32(bitmap.Height))
	return max(1, float32(bitmap.Width)*scale), max(1, float32(bitmap.Height)*scale)
}

func (u *UI) drawFileMessage(f ui.C, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	file := u.state.File(message.FileID)
	if file.Name == "" {
		file.Name = displayFileName(file, message)
	}
	justify := ui.Start
	if own {
		justify = ui.End
	}
	ui.Row(f).FillWidth().Justify(justify).Items(ui.Start).Gap(10).Children(func() {
		if !own {
			peer := u.state.Peer(message.SenderID)
			u.chatPeerAvatar(f, u.messageAvatarIDs[message.ID], peer.ID, u.messageSenderName(conversation, message), peer.Avatar, 34)
		}
		ui.Column(f).Gap(4).Children(func() {
			if !own && conversation.Kind == "group" {
				ui.Text(f, u.messageSenderName(conversation, message)).FontSize(11.5).Muted().Ellipsis()
			}
			action := "查看详情"
			if file.Video() && u.fileLocalPath[file.ID] != "" {
				action = "播放视频"
			}
			cardWidth := min(float32(380), maxWidth)
			card := ui.Column(f).Width(cardWidth).Pad(14).Radius(radiusControl).
				Background(u.palette.fileCard).Border(1, u.palette.fileBorder).Gap(10).
				HitWith(u.fileButtonIDs[file.ID], i18n.T("查看 ")+displayFileName(file, message)+i18n.T(" 详情"),
					gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("查看文件详情")}).
				Children(func() {
					ui.Row(f).Items(ui.Center).Gap(11).Children(func() {
						drawFileIcon(f, u.theme, fileTypeIcon(file), 40, 21)
						ui.Column(f).Grow(1).Gap(3).Children(func() {
							ui.Text(f, displayFileName(file, message)).FontSize(14).
								Weight(gfx.WeightSemibold).MaxLines(2)
							ui.Text(f, fileMeta(file, message)).FontSize(12).Muted().Ellipsis()
						})
					})
					if len(u.transfersFor[file.ID]) > 1 {
						ui.Text(f, u.transferSummary(file)).FontSize(11.5).Muted().Ellipsis()
					}
					activeShown := false
					for _, transfer := range u.transfersFor[file.ID] {
						if !transferNeedsAttention(transfer) {
							continue
						}
						activeShown = true
						if transfer.Status == "transferring" || transfer.Status == "verifying" {
							ui.Progress(f, transferFraction(transfer)).Height(5)
						}
						ui.Row(f).Items(ui.Center).Children(func() {
							ui.Text(f, u.transferProgressLabel(transfer)).FontSize(12).Muted().Ellipsis()
							ui.Spacer(f)
							ui.Text(f, i18n.T(action)).FontSize(11).Weight(gfx.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
					if !activeShown {
						ui.Row(f).Items(ui.Center).Children(func() {
							ui.Text(f, fileAvailability(u, file)).FontSize(12).Muted().Ellipsis()
							ui.Spacer(f)
							ui.Text(f, i18n.T(action)).FontSize(11).Weight(gfx.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
					if !own {
						for _, transfer := range u.transfersFor[file.ID] {
							if !transfer.Revoked && transfer.Direction == "receive" && (transfer.Status == "offered" || transfer.Status == "waiting") {
								ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
									if namedButton(f, u.fileAcceptIDs[transfer.ID], i18n.T("接收文件")).Height(u.textExtent(40, 18)).FontSize(14).Clicked() {
										u.submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID})
									}
									ui.Text(f, i18n.T("更多操作 ›")).FontSize(12).Color(u.palette.selectedText)
								})
								break
							}
						}
					}
				})
			u.messageContextPointer(f.Canvas(), u.fileButtonIDs[file.ID], message)
			if card.Clicked() {
				if file.Video() && u.fileLocalPath[file.ID] != "" {
					u.openImageViewer(file.ID)
				} else {
					u.openFileDetail(file.ID, "chat")
				}
			}
			ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
				u.messageTime(f, message, own)
			})
		})
		if own {
			u.ownMessageAvatar(f)
		}
	})
}

func (u *UI) drawPrintMessage(f ui.C, conversation domain.Conversation, message domain.Message,
	own bool, maxWidth float32) {
	job := u.state.PrintJob(message.PrintJobID)
	if job.ID == "" {
		u.drawFileMessage(f, conversation, message, own, maxWidth)
		return
	}
	justify := ui.Start
	if own {
		justify = ui.End
	}
	status, kind := printStatusPresentation(job.Status)
	statusColor := u.palette.secondaryText
	switch kind {
	case "completed":
		statusColor = u.palette.success
	case "failed":
		statusColor = u.palette.danger
	case "active", "queued":
		statusColor = u.palette.selectedText
	}
	ui.Row(f).FillWidth().Justify(justify).Items(ui.Start).Gap(10).Children(func() {
		if !own {
			peer := u.state.Peer(message.SenderID)
			u.chatPeerAvatar(f, u.messageAvatarIDs[message.ID], peer.ID,
				u.messageSenderName(conversation, message), peer.Avatar, 34)
		}
		ui.Column(f).Gap(4).Children(func() {
			if !own && conversation.Kind == "group" {
				ui.Text(f, u.messageSenderName(conversation, message)).FontSize(11.5).Muted().Ellipsis()
			}
			cardWidth := min(float32(380), maxWidth)
			card := ui.Column(f).Width(cardWidth).Pad(14).Radius(radiusControl).
				Background(u.palette.fileCard).Border(1, u.palette.fileBorder).Gap(9).
				HitWith(u.messageIDs[message.ID], i18n.T("打印记录 ")+job.FileName,
					gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("查看打印详情")}).
				Children(func() {
					ui.Row(f).Items(ui.Center).Gap(11).Children(func() {
						ui.Row(f).Size(40, 40).Radius(radiusControl).Items(ui.Center).Justify(ui.Center).
							Background(panelTint(u.theme.Primary, .10, u.palette.fileCard)).Children(func() {
							ui.Icon(f, panelPrintIcon).Size(21, 21).Color(u.theme.Primary)
						})
						ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
							ui.Text(f, job.FileName).FontSize(14).Weight(gfx.WeightSemibold).Ellipsis()
							ui.Text(f, i18n.T("打印机 ")+job.PrinterName).FontSize(11).Muted().Ellipsis()
						})
						ui.Row(f).Gap(6).Items(ui.Center).Children(func() {
							ui.Row(f).Size(7, 7).Radius(4).Background(statusColor)
							ui.Text(f, status).FontSize(11).Color(statusColor)
						})
					})
					ui.Row(f).Items(ui.Center).Gap(7).Children(func() {
						ui.Text(f, printOptionSummary(job.Options)).FontSize(11).Muted().Ellipsis()
						if job.Error != "" {
							ui.Spacer(f)
							ui.Text(f, job.Error).FontSize(10.5).Color(u.palette.danger).Ellipsis()
						}
					})
				})
			if card.Clicked() {
				u.closeDrawer()
				u.activePrintID = job.ID
				u.activeFileID = ""
				u.drawer = "files"
			}
			u.messageContextPointer(f.Canvas(), u.messageIDs[message.ID], message)
			ui.Row(f).Items(ui.Center).Gap(6).Children(func() {
				u.messageTime(f, message, own)
			})
		})
		if own {
			u.ownMessageAvatar(f)
		}
	})
}

func (u *UI) drawComposer(f ui.C) {
	u.composerBox = ui.Column(f).Fill().Background(u.palette.chat).Children(func() {
		u.drawComposerToolbar(f)
		u.mentionEditor = ui.TextAreaID(f, u.ids.chatEditorField, &u.chatEditorField, i18n.T("消息输入框"), "").
			FillWidth().Grow(1).Basis(0).MinHeight(chatEditorMinHeight).
			PadSides(8, chatContentInset, 8, chatContentInset).
			Background(ui.Color{}).Border(0, ui.Color{}).Shadow(gfx.ShadowNone)
		u.drawComposerFooter(f)
	})
}

func (u *UI) drawComposerToolbar(f ui.C) {
	ui.Row(f).FillWidth().Height(chatComposerToolHeight).PadX(8).Items(ui.Center).Gap(2).Children(func() {
		u.emojiButton = u.composerActionButton(f, u.ids.chatEmoji, u.iconSmile, i18n.T("插入表情"), "")
		if u.emojiButton.Clicked() {
			u.emojiOpen = !u.emojiOpen
			if u.emojiOpen {
				u.closeMention()
			}
		}
		files := u.composerActionButton(f, u.ids.chatFile, u.iconPaperclip, i18n.T("发送文件"), "")
		if files.Clicked() {
			u.chooseChatFiles(f.Canvas(), false)
		}
		conversation := u.state.Conversation(u.selected)
		if u.chatFolderAvailable(conversation) && u.composerActionButton(f, u.ids.chatFolder, u.iconFolder, i18n.T("发送文件夹"), "").Clicked() {
			u.chooseChatFolder(f.Canvas(), conversation.ID)
		}
		shortcut, _ := u.configuredShortcut(domain.ShortcutScreenshot)
		snip := u.composerActionButton(f, u.ids.chatScreenshot, Scissors, i18n.T("截图标注"), shortcut)
		if snip.Clicked() && u.requestScreenshot != nil {
			u.requestScreenshot()
		}
		images := u.composerActionButton(f, u.ids.chatImage, u.iconImage, i18n.T("发送图片"), "")
		if images.Clicked() {
			u.chooseChatFiles(f.Canvas(), true)
		}
		ui.Spacer(f)
		if conversation.Kind == "direct" {
			peer := u.state.Peer(conversation.PeerID)
			call := u.composerActionButton(f, u.ids.chatHeaderCall, u.iconPhone, i18n.T("语音通话"), "").
				Disabled(peer.ID == "")
			if call.Clicked() {
				u.startVoiceCall(conversation.PeerID)
			}
		}
	})
}

func (u *UI) drawComposerFooter(f ui.C) {
	ui.Row(f).FillWidth().Height(chatComposerFooterHeight).PadX(chatContentInset).
		Items(ui.Center).Gap(12).Children(func() {
		ui.Text(f, u.composerSendHint()).FontSize(11.5).Muted().Grow(1).Basis(0).Ellipsis()
		label := i18n.T("发送")
		hasText := u.chatEditor != nil && strings.TrimSpace(u.chatEditor.String()) != ""
		background := u.palette.chat.Mix(u.theme.Foreground, 0.08)
		foreground := u.theme.MutedForeground
		_, waiting := u.pendingTexts[u.selected]
		send := ui.ButtonBase(f, label).Hit(u.ids.chatSend, label).Size(76, 30).Radius(radiusControl).Disabled(!hasText || waiting)
		if hasText {
			background, foreground = u.theme.Primary, u.theme.PrimaryForeground
			send.Hover(ui.Look{}.Background(background.Mix(foreground, 0.1))).
				Pressed(ui.Look{}.Background(background.Mix(u.palette.chat, 0.18))).
				Focus(ui.Look{}.Ring(1.5, u.theme.Ring))
		}
		send.Background(background).Children(func() {
			ui.Text(f, label).FontSize(12.5).Weight(gfx.WeightSemibold).Color(foreground)
		})
		if send.Clicked() {
			u.sendChatText()
		}
	})
}

func (u *UI) composerActionButton(f ui.C, id gfx.ID, icon gfx.Icon, label, shortcut string) ui.Box {
	hint := label
	if shortcut != "" {
		hint += "  " + shortcut
	}
	background := ui.Color{}
	if id == u.ids.chatEmoji && u.emojiOpen {
		background = u.palette.selected
	}
	return ui.Row(f).Size(34, 34).Radius(radiusControl).Background(background).Center().
		Hover(ui.Look{}.Background(u.palette.chat.Mix(u.theme.Foreground, 0.09))).
		Focus(ui.Look{}.Ring(1.5, u.theme.Ring)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: background.Alpha > 0, Hint: hint}).
		Children(func() {
			ui.Icon(f, icon).Size(18, 18).Color(u.theme.MutedForeground)
		})
}

func (u *UI) composerSendHint() string {
	if u.state.Settings.SendKey == "Ctrl+Enter" {
		return i18n.T("Ctrl+Enter 发送，Enter 换行")
	}
	return i18n.T("Enter 发送，Shift+Enter 换行")
}

func (u *UI) drawChatReadOnly(f ui.C, conversation domain.Conversation, reason string) {
	title := i18n.T("该群聊已关闭")
	if conversation.Kind == "direct" {
		title = i18n.T("历史会话")
	} else if group := u.state.Group(conversation.GroupID); !group.Left && !group.Dissolved {
		title = i18n.T("正在等待成员加入")
	}
	ui.Column(f).Fill().PadX(chatContentInset).
		Background(u.palette.chat).Justify(ui.Center).Children(func() {
		ui.Row(f).FillWidth().PadSides(10, 14, 10, 14).Radius(radiusControl).Background(u.palette.incoming).
			Items(ui.Center).Gap(10).Children(func() {
			ui.Row(f).Size(30, 30).Radius(8).Items(ui.Center).Justify(ui.Center).
				Background(u.palette.mutedAvatar).Children(func() {
				ui.Icon(f, u.iconLock).Size(15, 15).Color(u.palette.secondaryText)
			})
			ui.Column(f).Grow(1).Gap(2).Children(func() {
				ui.Text(f, title).FontSize(12.5).Weight(gfx.WeightMedium)
				ui.Text(f, reason).FontSize(10.5).Muted().Ellipsis()
			})
			if conversation.Kind == "direct" {
				peer := u.state.Peer(conversation.PeerID)
				label := i18n.T("查看联系人")
				if peer.CompatibilityAliasOf != "" {
					label = i18n.T("打开当前会话")
				}
				if namedButton(f, u.ids.chatCurrentContact, label).Small().Outline().Clicked() {
					f.Canvas().AfterPaint(func() { u.openArchiveTarget(peer.CompatibilityAliasOf) })
				}
			}
		})
	})
}

func (u *UI) openArchiveTarget(peerID string) {
	if u.presentArchiveTarget != nil {
		u.presentArchiveTarget(peerID)
		return
	}
	if peerID == "" {
		u.setSection(sectionContacts)
		return
	}
	u.startDirectFromMembers(peerID)
}

func (u *UI) chatReadOnlyReason(conversation domain.Conversation) string {
	if conversation.Kind == "direct" {
		return i18n.T(u.state.DirectReadOnlyReason(u.state.Peer(conversation.PeerID)))
	}
	if conversation.Kind != "group" {
		return ""
	}
	group := u.state.Group(conversation.GroupID)
	switch {
	case group.ID == "":
		return ""
	case group.Dissolved:
		return i18n.T("群聊已解散，本机历史仍可查看。")
	case group.Left:
		return i18n.T("你已退出该群聊，本机历史仍可查看。")
	case len(group.Members) < 2:
		return i18n.T("等待成员确认邀请后即可发送消息或文件。")
	default:
		return ""
	}
}

func (u *UI) chatTextInput() *app.TextInput {
	if u.chatInput.OnEvent == nil {
		u.chatInput.OnEvent = u.onChatTextEvent
	}
	u.syncChatInput(false)
	return &u.chatInput
}

func (u *UI) syncChatInput(redraw bool) {
	if u.chatEditor == nil {
		return
	}
	u.chatInput.State = app.TextStateOf(u.chatEditor)
	u.chatInput.State.Redraw = redraw
}

func (u *UI) onChatTextEvent(event app.TextEvent) (app.TextState, error) {
	if u.chatEditor == nil {
		return app.TextState{Value: "", Selection: app.TextRange{}}, nil
	}
	if event.Kind == app.TextPasteNoText {
		u.pasteClipboardImage()
		state := app.TextStateOf(u.chatEditor)
		state.Redraw = true
		return state, nil
	}
	state := app.TextStateOf(u.chatEditor)
	if event.Kind == app.TextCommandEvent && event.Command == app.TextInsertNewline {
		if state.Composing {
			return state, nil
		}
		if u.mention.open {
			if u.insertMention() {
				next := app.TextStateOf(u.chatEditor)
				next.Redraw = true
				return next, nil
			}
		}
		if u.selected != "" && u.chatReadOnlyReason(u.state.Conversation(u.selected)) == "" &&
			u.state.Settings.SendKey != "Ctrl+Enter" {
			u.sendChatText()
			next := app.TextStateOf(u.chatEditor)
			next.Redraw = true
			return next, nil
		}
	}
	event.Apply(u.chatEditor)
	if u.emojiOpen {
		u.emojiOpen = false
	}
	u.syncMention()
	next := app.TextStateOf(u.chatEditor)
	next.Redraw = true
	if u.selected != "" {
		u.drafts[u.selected] = next.Value
	}
	return next, nil
}

func (u *UI) sendChatText() {
	if u.selected == "" || u.chatEditor == nil ||
		u.chatReadOnlyReason(u.state.Conversation(u.selected)) != "" {
		return
	}
	raw := u.chatEditor.String()
	value := strings.TrimSpace(raw)
	if _, waiting := u.pendingTexts[u.selected]; waiting || value == "" || u.backend == nil {
		return
	}
	u.closeMention()
	u.emojiOpen = false
	// Store the draft before submission so a rejected command or window closure cannot lose it.
	u.persistDraft(u.selected, raw)
	if u.pendingTexts == nil {
		u.pendingTexts = make(map[string]pendingChatText)
	}
	done := make(chan bool, 1)
	u.pendingTexts[u.selected] = pendingChatText{raw: raw, done: done}
	u.submit(domain.Command{Kind: domain.SendText, ConversationID: u.selected, Text: value, OnResult: func(err error) {
		done <- err == nil
		u.invalidator.Invalidate()
	}})
	u.finishChatSubmissions()
}

type pendingChatText struct {
	raw  string
	done <-chan bool
}

// Called on the window thread, including when a native dialog or a detached chat is active.
func (u *UI) finishChatSubmissions() {
	for id, pending := range u.pendingTexts {
		select {
		case accepted := <-pending.done:
			delete(u.pendingTexts, id)
			if !accepted {
				continue
			}
			u.draftSaved[id] = ""
			if id == u.selected && u.chatEditor != nil {
				if u.chatEditor.String() == pending.raw {
					u.setChatEditorText("")
					u.drafts[id] = ""
				}
				u.chatScrollToEnd = true
				u.syncChatInput(true)
			} else if u.drafts[id] == pending.raw {
				u.drafts[id] = ""
			} else {
				u.persistDraft(id, u.drafts[id])
			}
		default:
		}
	}
}

func (u *UI) insertChatText(value string) {
	if u.chatEditor == nil || u.selected == "" {
		return
	}
	u.chatEditor.Insert(value)
	u.closeMention()
	u.drafts[u.selected] = u.chatEditor.String()
	u.syncChatInput(true)
}

func (u *UI) setChatEditorText(value string) {
	if u.chatEditor == nil {
		return
	}
	u.chatEditor.SetSelection(text.Range{Start: 0, End: u.chatEditor.RuneCount()})
	u.chatEditor.Insert(value)
	u.closeMention()
	u.emojiOpen = false
	u.syncChatInput(true)
}

func (u *UI) chatFolderAvailable(c domain.Conversation) bool {
	return c.Kind == "direct" && u.state.Peer(c.PeerID).Transport == domain.TransportNeiwangtong ||
		c.Kind == "group" && u.state.Group(c.GroupID).Transport == domain.TransportNeiwangtong
}

func (u *UI) chooseChatFolder(canvas *gfx.Canvas, conversationID string) {
	canvas.AfterPaint(func() {
		if u.filePickerOpen {
			return
		}
		u.filePickerOpen = true
		defer func() { u.filePickerOpen = false }()
		path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: i18n.T("发送文件夹")})
		if err != nil {
			if !errors.Is(err, app.ErrCanceled) && !errors.Is(err, app.ErrNoDriverResponse) {
				u.showToast(i18n.T("无法选择文件夹"), filePickerProblem(err), components.ToastError)
			}
			return
		}
		if path != "" {
			u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: conversationID, Paths: []string{path}})
		}
	})
}
func (u *UI) chooseChatFiles(canvas *gfx.Canvas, imagesOnly bool) {
	if u.selected == "" || u.chatReadOnlyReason(u.state.Conversation(u.selected)) != "" {
		return
	}
	conversationID := u.selected
	options := app.OpenFileOptions{
		Title:         i18n.T("发送文件"),
		AllowMultiple: true,
	}
	if imagesOnly {
		options.Title = i18n.T("发送图片")
		options.Filters = []app.FileFilter{
			{Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp"},
			{Name: i18n.T("所有文件"), Pattern: "*.*"},
		}
	}
	canvas.AfterPaint(func() {
		paths, err := u.openFilePicker(options)
		if err != nil {
			if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) {
				return
			}
			u.showToast(i18n.T("无法选择文件"), filePickerProblem(err), components.ToastError)
			return
		}
		if len(paths) == 0 {
			return
		}
		u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: conversationID, Paths: append([]string(nil), paths...)})
	})
}

func (u *UI) pasteClipboardImage() {
	if u.selected == "" || u.chatReadOnlyReason(u.state.Conversation(u.selected)) != "" {
		return
	}
	readClipboard := u.clipboardImage
	if readClipboard == nil {
		readClipboard = app.ClipboardImagePNG
	}
	data, err := readClipboard()
	if err != nil {
		u.reportClipboardFailure(i18n.T("无法读取剪贴板中的图片：") + err.Error())
		return
	}
	if len(data) == 0 {
		u.reportClipboardFailure(i18n.T("剪贴板中没有可发送的图片"))
		return
	}
	cacheDir := u.clipboardCacheDir
	if cacheDir == nil {
		cacheDir = os.UserCacheDir
	}
	cache, err := cacheDir()
	if err != nil || cache == "" {
		u.reportClipboardFailure(i18n.T("无法定位剪贴板图片缓存目录"))
		return
	}
	directory := filepath.Join(cache, "GoIM", "clipboard")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		u.reportClipboardFailure(i18n.T("无法创建剪贴板图片缓存目录：") + err.Error())
		return
	}
	path := filepath.Join(directory, fmt.Sprintf("clipboard-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		u.reportClipboardFailure(i18n.T("无法保存剪贴板图片：") + err.Error())
		return
	}
	u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: u.selected, Paths: []string{path}})
}

func (u *UI) reportClipboardFailure(message string) {
	u.submit(domain.Command{Kind: domain.ReportError, Text: message})
}

func (u *UI) groupCounts(group domain.Group) (total, online int) {
	if group.ID == "" {
		return 0, 0
	}
	total = len(group.Members)
	for _, id := range group.Members {
		if peerOnline(u, id) {
			online++
		}
	}
	if u.state.Self.ID != "" && !containsString(group.Members, u.state.Self.ID) {
		total++
		online++
	}
	return total, online
}

func displayFileName(file domain.File, message domain.Message) string {
	if file.Name != "" {
		return file.Name
	}
	if message.Text != "" {
		return message.Text
	}
	return i18n.T("未命名文件")
}

func fileMeta(file domain.File, message domain.Message) string {
	size := formatBytes(file.Size)
	if size != "" {
		return size + fileTypeSuffix(file)
	}
	if message.Text != "" {
		return message.Text
	}
	return i18n.T("文件")
}

func transferFraction(transfer domain.Transfer) float32 {
	if transfer.Size <= 0 {
		return 0
	}
	return min(max(float32(transfer.Done)/float32(transfer.Size), 0), 1)
}

func transferNeedsAttention(transfer domain.Transfer) bool {
	if transfer.Revoked {
		return false
	}
	switch transfer.Status {
	case "transferring", "verifying", "offered", "waiting", "retry_requested":
		return true
	}
	return false
}

func transferRateLabel(t domain.Transfer) string {
	if t.Status == "transferring" && t.Speed > 0 {
		return panelFormatBytes(int64(t.Speed)) + "/s"
	}
	return ""
}

func (u *UI) transferProgressLabel(transfer domain.Transfer) string {
	fraction := transferFraction(transfer)
	label := intString(int(fraction*100)) + "%"
	if rate := transferRateLabel(transfer); rate != "" {
		label += " · " + rate
	}
	prefix := transferPeerLabel(u, transfer) + " · "
	if transfer.Status == "verifying" {
		return prefix + i18n.T("校验中 · ") + label
	}
	if transfer.Status == "offered" {
		return prefix + i18n.T("等待接收")
	}
	if transfer.Status == "waiting" {
		return prefix + i18n.T("等待连接 · ") + label
	}
	if transfer.Status == "retry_requested" {
		return prefix + i18n.T("等待对方确认重试")
	}
	if transfer.Direction == "receive" {
		return prefix + i18n.T("接收中 · ") + label
	}
	return prefix + i18n.T("发送中 · ") + label
}

func (u *UI) transferSummary(file domain.File) string {
	transfers := u.transfersFor[file.ID]
	if len(transfers) == 0 {
		return i18n.T("文件邀请 · 等待状态同步")
	}
	received, transferring, waiting, failed := 0, 0, 0, 0
	for _, transfer := range transfers {
		switch transfer.Status {
		case "completed":
			received++
		case "transferring", "verifying":
			transferring++
		case "waiting", "offered", "retry_requested":
			waiting++
		case "failed":
			failed++
		}
	}
	parts := make([]string, 0, 4)
	if received > 0 {
		parts = append(parts, intString(received)+i18n.T(" 人已接收"))
	}
	if transferring > 0 {
		parts = append(parts, intString(transferring)+i18n.T(" 人传输中"))
	}
	if waiting > 0 {
		parts = append(parts, intString(waiting)+i18n.T(" 人等待中"))
	}
	if failed > 0 {
		parts = append(parts, intString(failed)+i18n.T(" 人失败"))
	}
	if len(parts) == 0 {
		return i18n.T("暂无可用接收者")
	}
	return strings.Join(parts, " · ")
}

func fileAvailability(u *UI, file domain.File) string {
	if path := u.fileLocalPath[file.ID]; path != "" {
		if u.fileAvailable[file.ID] {
			return i18n.T("本机可用")
		}
		return i18n.T("本机文件不可用")
	}
	if file.ID == "" {
		return i18n.T("文件记录尚未同步")
	}
	for _, transfer := range u.transfersFor[file.ID] {
		if transfer.Direction == "receive" {
			return transferStatusText(transfer)
		}
	}
	if peerOnline(u, file.SenderID) {
		return i18n.T("尚未接收")
	}
	return i18n.T("等待发送方上线")
}

func sendKeyHint(value string) string {
	if value == "Ctrl+Enter" {
		return i18n.T("Ctrl+Enter 发送 · Enter 换行")
	}
	return i18n.T("Enter 发送 · Shift+Enter 换行")
}

func messageStatus(status string) string {
	switch status {
	case "pending":
		return i18n.T("发送中")
	case "sent":
		return i18n.T("已发送")
	case "delivered":
		return i18n.T("已送达")
	case "failed":
		return i18n.T("发送失败")
	default:
		return ""
	}
}

func formatBytes(size int64) string {
	if size <= 0 {
		return ""
	}
	if size < 1024 {
		return intString(int(size)) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := ""
	for _, candidate := range units {
		value /= 1024
		unit = candidate
		if value < 1024 {
			break
		}
	}
	if value >= 100 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func friendlyDay(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	local := at.Local()
	now := time.Now()
	y, m, d := local.Date()
	nowY, nowM, nowD := now.Date()
	label := ""
	switch {
	case y == nowY && m == nowM && d == nowD:
		label = i18n.T("今天")
	case local.Year() == now.AddDate(0, 0, -1).Year() && local.YearDay() == now.AddDate(0, 0, -1).YearDay():
		label = i18n.T("昨天")
	}
	if i18n.Language() == i18n.English {
		date := local.Format("Mon, Jan 2, 2006")
		if label == "Today" || label == "Yesterday" {
			return label + " · " + date
		}
		return date
	}
	weekdays := [...]string{"日", "一", "二", "三", "四", "五", "六"}
	if label != "" {
		label += " "
	}
	return label + local.Format("2006-01-02") + " 星期" + weekdays[local.Weekday()]
}

func relativeTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return i18n.T("刚刚")
	case elapsed < time.Hour:
		return intString(int(elapsed/time.Minute)) + i18n.T(" 分钟前")
	case elapsed < 24*time.Hour:
		return intString(int(elapsed/time.Hour)) + i18n.T(" 小时前")
	default:
		return at.Local().Format("01-02 15:04")
	}
}

func (u *UI) toggleFilesPane() {
	if !u.guardGroupRenameNavigation(u.toggleFilesPane) {
		return
	}
	u.resetGroupRename()
	if u.drawer == "files" {
		u.closeDrawer()
		return
	}
	u.closeChatPickers()
	u.drawer = "files"
	u.activeFileID = ""
	u.fileReturn = ""
	u.fileFilter = "all"
	u.moreOpen = false
	u.memberMenu = ""
}

func (u *UI) openFileDetail(fileID, from string) {
	if !u.guardGroupRenameNavigation(func() { u.openFileDetail(fileID, from) }) {
		return
	}
	u.resetGroupRename()
	u.activeVoiceRecord = domain.Message{}
	u.activePrintID = ""
	if fileID == "" {
		return
	}
	u.drawer = "files"
	u.activeFileID = fileID
	u.fileReturn = from
	u.moreOpen = false
	u.memberMenu = ""
}

func (u *UI) ensureChatEditor() *text.Editor {
	if u.chatEditor == nil {
		u.chatEditor = text.NewEditor("")
	}
	return u.chatEditor
}

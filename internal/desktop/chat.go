package desktop

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/gfx"
	"goim/internal/i18n"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (u *UI) drawChat(canvas *gfx.Canvas, bounds ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	chatBounds := bounds
	surface := u.surface
	surface.filesPane = ui.Rect{}
	if u.drawer == "files" && conversation.ID == "" {
		u.closeDrawer()
	}
	if u.drawer == "files" && conversation.ID != "" {
		paneWidth := chatFilesPaneWidth(bounds.Width)
		if paneWidth <= 0 || bounds.Width-paneWidth-1 <= 0 {
			u.drawChatSurface(canvas, chatBounds, conversation)
			return
		}
		chatBounds = ui.Rect{X: bounds.X, Y: bounds.Y, Width: max(bounds.Width-paneWidth-1, 0), Height: bounds.Height}
		pane := ui.Rect{
			X: bounds.X + chatBounds.Width + 1, Y: bounds.Y,
			Width: paneWidth, Height: bounds.Height,
		}
		u.drawChatSurface(canvas, chatBounds, conversation)
		canvas.Fill(ui.Rect{X: chatBounds.X + chatBounds.Width, Y: bounds.Y, Width: 1, Height: bounds.Height},
			u.palette.border)
		u.drawChatFilesPane(canvas, pane)
	} else {
		u.drawChatSurface(canvas, chatBounds, conversation)
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
	var timeline ui.Box
	u.chatPage.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Children(func() {
			u.drawChatHeader(f, conversation)
			searchDeclared := false
			if u.chatFind.open {
				searchDeclared = true
				ui.Divider(f)
				u.drawChatSearchBar(f)
			}
			ui.Divider(f)
			// The header can open or close search during this declaration, so
			// size the history pane after it to observe the current state.
			u.prepareChatHistory(bounds.Height, conversation, searchDeclared)
			ui.Column(f).Height(surface.history.Pos).Children(func() {
				u.drawChatNotice(f)
				timeline = u.drawChatTimeline(f, conversation)
			})
			ui.Splitter(f, &surface.history).
				Mins(chatHistoryMinHeight, u.chatComposerMinimum(conversation)).
				Label(i18n.T("调整聊天记录和输入框高度"))
			ui.Row(f).Grow(1).Basis(0).Children(func() {
				if reason := u.chatReadOnlyReason(conversation); reason != "" {
					u.drawChatReadOnly(f, reason)
				} else {
					u.drawComposer(f, conversation)
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
	}
	u.chatFind.restorePending = false
	if u.dragging {
		overlay := ui.Rect{
			X: bounds.X + 18, Y: bounds.Y + 98,
			Width: max(bounds.Width-36, 0), Height: max(bounds.Height-282, 0),
		}
		canvas.RoundedBorder(overlay, 12, 1, u.theme.Primary, ui.RGBA(31, 96, 221, 0.06))
		canvas.LabelCenter(i18n.T("松开以发送文件"), overlay, gfx.TextStyle{
			Size: 16, Weight: gfx.WeightMedium, Color: u.theme.Primary,
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
		ui.Paragraph(f, "消息只在局域网内直连。联系人上线后会自动出现，也可以手动输入地址连接。", 3).
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
	online := false
	subtitle := i18n.T("离线")
	if conversation.Kind == "group" {
		group := u.state.Group(conversation.GroupID)
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
	ui.Row(f).Height(chatHeaderExtent).FillWidth().PadSides(7, 22, 7, 24).Items(ui.Center).Gap(12).Children(func() {
		avatar := ""
		if conversation.Kind == "direct" {
			peer := u.state.Peer(conversation.PeerID)
			avatar = peer.Avatar
			if peer.Name != "" {
				avatarName = peer.Name
			}
		}
		if conversation.Kind == "direct" {
			u.chatPeerAvatar(f, u.ids.chatHeaderAvatar, conversation.PeerID, avatarName, avatar, 42)
		} else {
			u.avatarWithPresence(f, avatarName, avatar, 42, online, false, u.palette.chat)
		}
		ui.Column(f).Grow(1).Gap(3).Justify(ui.Center).Children(func() {
			ui.Text(f, title).FontSize(17).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Row(f).Items(ui.Center).Gap(7).Children(func() {
				dot := u.palette.secondaryText
				if online {
					dot = u.palette.success
				}
				ui.Stack(f).Size(7, 7).Radius(4).Background(dot)
				ui.Text(f, subtitle).FontSize(12).Muted().Ellipsis()
			})
		})
		searchButton = u.sizedIconButton(f, u.ids.chatHeaderSearch, u.iconSearch, i18n.T("搜索当前会话"), 38, 23)
		fileButton = u.sizedIconButton(f, u.ids.chatHeaderFile, u.iconFolder, i18n.T("聊天文件"), 38, 23)
		if conversation.Kind == "group" {
			group := u.state.Group(conversation.GroupID)
			if group.ID == "" || (!group.Left && !group.Dissolved) {
				memberButton = u.sizedIconButton(f, u.ids.chatHeaderMembers, u.iconUsers, i18n.T("群聊成员"), 38, 23)
				memberShown = true
			}
		}
		moreButton = u.sizedIconButton(f, u.ids.chatHeaderMore, u.iconMore, i18n.T("更多会话操作"), 38, 23)
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
		u.drawer = "members"
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
	ui.Row(f).FillWidth().PadSides(8, 28, 8, 28).Height(38).Children(func() {
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
	area := ui.Scroll(f.Part("message-history")).Grow(1).Follow(!restoreOffset)
	scroll := area.ScrollState()
	if restoreOffset {
		scroll.ScrollTo(u.chatFind.savedOffset)
	} else if u.chatFind.restorePending {
		scroll.ScrollToEnd()
	}
	area.PadSides(0, 0, 18, 0)
	area.Children(func() {
		ui.Column(f).FillWidth().PadSides(22, 36, 18, 36).Gap(17).Children(func() {
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
				u.drawMessage(f, conversation, message)
			}
			ui.Row(f).Height(2)
		})
	})
	if u.chatScrollToEnd {
		scroll.ScrollToEnd()
		u.chatScrollToEnd = false
	}
	return area
}

func (u *UI) drawDateSeparator(f ui.C, at time.Time) {
	ui.Row(f).FillWidth().Height(32).Items(ui.Center).Gap(12).Children(func() {
		ui.Row(f).Grow(1).Height(1).Background(u.palette.border)
		ui.Text(f, friendlyDay(at)).FontSize(11).Muted()
		ui.Row(f).Grow(1).Height(1).Background(u.palette.border)
	})
}

func (u *UI) drawMessage(f ui.C, conversation domain.Conversation, message domain.Message) {
	if message.Recalled {
		u.drawRecalledMessage(f, conversation, message)
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
	bubble := ui.Column(f).MaxWidth(maxWidth).PadSides(10, 13, 9, 13).Radius(11).
		HitWith(u.messageIDs[message.ID], i18n.T("聊天消息"), gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
		Background(background).Border(1, u.palette.bubbleBorder).Gap(3).Children(func() {
		if message.Text != "" {
			ui.Paragraph(f, message.Text, 60).FontSize(14).LineHeight(21)
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
			picture := ui.Stack(f).Width(width).Height(height).Radius(11).Clip().
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
				ui.Column(f).MaxWidth(width).PadSides(8, 12, 7, 12).Radius(9).
					Background(u.palette.incoming).Children(func() {
					ui.Paragraph(f, message.Text, 4).FontSize(13).LineHeight(19)
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
			cardWidth := min(float32(480), max(maxWidth, float32(310)))
			card := ui.Column(f).Width(cardWidth).Pad(14).Radius(11).
				Background(u.palette.fileCard).Border(1, u.palette.fileBorder).Gap(10).
				HitWith(u.fileButtonIDs[file.ID], i18n.T("查看 ")+displayFileName(file, message)+i18n.T(" 详情"),
					gfx.HitOptions{Role: gfx.RoleButton, Hint: i18n.T("查看文件详情")}).
				Children(func() {
					ui.Row(f).Items(ui.Center).Gap(11).Children(func() {
						ui.Row(f).Size(40, 40).Radius(9).Items(ui.Center).Justify(ui.Center).
							Background(fileIconTint(file)).Children(func() {
							ui.Icon(f, u.fileIcon(file)).Size(21, 21).Color(fileIconColor(file))
						})
						ui.Column(f).Grow(1).Gap(3).Children(func() {
							ui.Text(f, displayFileName(file, message)).FontSize(14).
								Weight(gfx.WeightSemibold).Ellipsis()
							ui.Text(f, fileMeta(file, message)).FontSize(11).Muted().Ellipsis()
						})
					})
					ui.Text(f, u.transferSummary(file)).FontSize(11.5).Muted().Ellipsis()
					activeShown := false
					for _, transfer := range u.transfersFor[file.ID] {
						if !transferNeedsAttention(transfer) {
							continue
						}
						activeShown = true
						fraction := transferFraction(transfer)
						ui.Progress(f, fraction).Height(6)
						ui.Row(f).Items(ui.Center).Children(func() {
							ui.Text(f, u.transferProgressLabel(transfer)).FontSize(10.5).Muted().Ellipsis()
							ui.Spacer(f)
							ui.Text(f, i18n.T("查看详情")).FontSize(11).Weight(gfx.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
					if !activeShown {
						ui.Row(f).Items(ui.Center).Children(func() {
							ui.Text(f, fileAvailability(u, file)).FontSize(10.5).Muted().Ellipsis()
							ui.Spacer(f)
							ui.Text(f, i18n.T("查看详情")).FontSize(11).Weight(gfx.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
				})
			u.messageContextPointer(f.Canvas(), u.fileButtonIDs[file.ID], message)
			if card.Clicked() {
				u.openFileDetail(file.ID, "chat")
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

func (u *UI) drawComposer(f ui.C, conversation domain.Conversation) {
	hasText := u.chatEditor != nil && strings.TrimSpace(u.chatEditor.String()) != ""
	composer := ui.Column(f).FillWidth().FillHeight().PadSides(2, 9, 8, 9).
		Background(u.palette.header).Children(func() {
		ui.Column(f).Fill().Radius(11).Background(u.palette.header).
			PadSides(1, 5, 4, 5).Children(func() {
			editor := ui.TextArea(f, &u.chatEditorField,
				i18n.T("消息输入框"), i18n.T("输入消息…")).
				FillWidth().Grow(1).Basis(0).MinHeight(40).Border(0, gfx.Color{})
			u.mentionEditor = editor

			ui.Row(f).Height(34).Items(ui.Center).Gap(3).Children(func() {
				emoji := u.iconButton(f, u.ids.chatEmoji, u.iconSmile, i18n.T("插入表情"), 30)
				u.emojiButton = emoji
				imageButton := u.iconButton(f, u.ids.chatImage, u.iconImage, i18n.T("发送图片"), 30)
				fileButton := u.iconButton(f, u.ids.chatFile, u.iconPaperclip, i18n.T("发送文件"), 30)
				snip := u.iconButton(f, u.ids.chatScreenshot, Scissors, i18n.T("截图标注"), 30)
				if emoji.Clicked() {
					u.emojiOpen = !u.emojiOpen
					if u.emojiOpen {
						u.closeMention()
					}
				}
				if imageButton.Clicked() {
					u.chooseChatFiles(f.Canvas(), true)
				}
				if fileButton.Clicked() {
					u.chooseChatFiles(f.Canvas(), false)
				}
				if snip.Clicked() && u.requestScreenshot != nil {
					u.requestScreenshot()
				}
				ui.Spacer(f)
				send := namedButton(f, u.ids.chatSend, i18n.T("发送")).Width(88).Height(30).Disabled(!hasText)
				if send.Clicked() && hasText {
					u.sendChatText()
				}
			})
		})
	})
	u.composerBox = composer
}

func (u *UI) drawChatReadOnly(f ui.C, reason string) {
	title := i18n.T("该群聊已关闭")
	if strings.HasPrefix(reason, "等待成员") {
		title = i18n.T("正在等待成员加入")
	}
	ui.Column(f).FillWidth().FillHeight().PadSides(8, 28, 8, 28).
		Background(u.palette.header).Justify(ui.Center).Children(func() {
		ui.Row(f).Fill().PadSides(10, 14, 10, 14).Radius(10).Background(u.palette.incoming).
			Items(ui.Center).Gap(10).Children(func() {
			ui.Row(f).Size(30, 30).Radius(8).Items(ui.Center).Justify(ui.Center).
				Background(u.palette.mutedAvatar).Children(func() {
				ui.Icon(f, u.iconLock).Size(15, 15).Color(u.palette.secondaryText)
			})
			ui.Column(f).Grow(1).Gap(2).Children(func() {
				ui.Text(f, title).FontSize(12.5).Weight(gfx.WeightMedium)
				ui.Text(f, reason).FontSize(10.5).Muted().Ellipsis()
			})
		})
	})
}

func (u *UI) chatReadOnlyReason(conversation domain.Conversation) string {
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
	u.chatInput.State = app.TextStateOf(u.chatEditor)
	return &u.chatInput
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
	value := strings.TrimSpace(u.chatEditor.String())
	if value == "" {
		return
	}
	u.closeMention()
	u.emojiOpen = false
	u.submit(domain.Command{Kind: domain.SendText, ConversationID: u.selected, Text: value})
	u.setChatEditorText("")
	u.drafts[u.selected] = ""
	u.draftSaved[u.selected] = ""
	u.chatScrollToEnd = true
	u.chatInput.State = app.TextStateOf(u.chatEditor)
}

func (u *UI) insertChatText(value string) {
	if u.chatEditor == nil || u.selected == "" {
		return
	}
	u.chatEditor.Insert(value)
	u.closeMention()
	u.drafts[u.selected] = u.chatEditor.String()
	u.chatInput.State = app.TextStateOf(u.chatEditor)
}

func (u *UI) setChatEditorText(value string) {
	if u.chatEditor == nil {
		return
	}
	u.chatEditor.SetSelection(text.Range{Start: 0, End: u.chatEditor.RuneCount()})
	u.chatEditor.Insert(value)
	u.closeMention()
	u.emojiOpen = false
	u.chatInput.State = app.TextStateOf(u.chatEditor)
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
			if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) ||
				errors.Is(err, app.ErrUnsupported) {
				return
			}
			u.showToast(i18n.T("无法选择文件"), err.Error(), components.ToastError)
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

func (u *UI) fileIcon(file domain.File) gfx.Icon {
	switch {
	case fileIsImage(file):
		return u.iconImage
	case strings.EqualFold(filepath.Ext(file.Name), ".zip") ||
		strings.EqualFold(filepath.Ext(file.Name), ".rar") ||
		strings.EqualFold(filepath.Ext(file.Name), ".7z"):
		return u.iconFileArchive
	case strings.EqualFold(filepath.Ext(file.Name), ".txt") ||
		strings.EqualFold(filepath.Ext(file.Name), ".md"):
		return u.iconFileText
	default:
		return u.iconFile
	}
}

func fileIconTint(file domain.File) ui.Color {
	if fileIsImage(file) {
		return ui.RGBA(34, 176, 94, 0.11)
	}
	return ui.RGBA(31, 96, 221, 0.09)
}

func fileIconColor(file domain.File) ui.Color {
	if fileIsImage(file) {
		return ui.RGB(28, 145, 76)
	}
	return ui.RGB(31, 96, 221)
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
		return size
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
	switch transfer.Status {
	case "transferring", "verifying", "offered", "waiting", "retry_requested":
		return true
	}
	return false
}

func (u *UI) transferProgressLabel(transfer domain.Transfer) string {
	fraction := transferFraction(transfer)
	label := intString(int(fraction*100)) + "%"
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
	label := local.Format("2006-01-02")
	switch {
	case y == nowY && m == nowM && d == nowD:
		label = i18n.T("今天")
	case now.Sub(local) < 48*time.Hour:
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
	return label + " " + local.Format("2006-01-02") + " 星期" + weekdays[local.Weekday()]
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
	u.syncFilesWindow()
}

func (u *UI) openFileDetail(fileID, from string) {
	if fileID == "" {
		return
	}
	u.drawer = "files"
	u.activeFileID = fileID
	u.fileReturn = from
	u.moreOpen = false
	u.memberMenu = ""
	u.syncFilesWindow()
}

func (u *UI) ensureChatEditor() *text.Editor {
	if u.chatEditor == nil {
		u.chatEditor = text.NewEditor("")
	}
	return u.chatEditor
}

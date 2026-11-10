package desktop

import (
	"errors"
	"fmt"
	"goim/internal/i18n"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func (u *UI) drawChat(canvas *ui.Canvas, bounds ui.Rect) {
	conversation := u.state.Conversation(u.selected)
	chatBounds := bounds
	surface := u.chatSurface()
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

func (u *UI) drawChatSurface(canvas *ui.Canvas, bounds ui.Rect, conversation domain.Conversation) {
	canvas.Fill(bounds, u.palette.chat)
	if u.drawChatBackground(canvas, bounds) {
		// Viva composites images after shapes in each layer. Move the tint,
		// message cards, and controls above the wallpaper as one surface.
		canvas.PushLayer(1)
		defer canvas.PopLayer()
		canvas.Fill(bounds, u.chatBackgroundTint())
	}
	if conversation.ID == "" {
		u.chatView.Draw(canvas, u.theme, bounds, func(f *flex.Frame) {
			f.Column().Fill().Children(func() {
				if u.state.Error != "" {
					u.drawChatNotice(f)
				}
				u.drawChatEmpty(f)
			})
		})
		return
	}
	surface := u.chatSurface()
	u.chatView.Draw(canvas, u.theme, bounds, func(f *flex.Frame) {
		f.Column().Fill().Children(func() {
			u.drawChatHeader(f, conversation)
			searchDeclared := false
			if u.chatFind.open {
				searchDeclared = true
				f.Divider()
				u.drawChatSearchBar(f)
			}
			f.Divider()
			// The header can open or close search during this declaration, so
			// size the history pane after it to observe the current state.
			u.prepareChatHistory(bounds.Height, conversation, searchDeclared)
			f.Column().Height(surface.history.Pos).Children(func() {
				u.drawChatNotice(f)
				u.drawChatTimeline(f, conversation)
			})
			f.Splitter(surface.ids.historySplitter, &surface.history).
				Mins(chatHistoryMinHeight, u.chatComposerMinimum(conversation)).
				Label(i18n.T("调整聊天记录和输入框高度"))
			f.Row().Grow(1).Basis(0).Children(func() {
				if reason := u.chatReadOnlyReason(conversation); reason != "" {
					u.drawChatReadOnly(f, reason)
				} else {
					u.drawComposer(f, conversation)
				}
			})
		})
	})
	if u.dragging {
		overlay := ui.Rect{
			X: bounds.X + 18, Y: bounds.Y + 98,
			Width: max(bounds.Width-36, 0), Height: max(bounds.Height-282, 0),
		}
		canvas.RoundedBorder(overlay, 12, 1, u.theme.Primary, ui.RGBA(31, 96, 221, 0.06))
		canvas.LabelCenter(i18n.T("松开以发送文件"), overlay, ui.TextStyle{
			Size: 16, Weight: ui.WeightMedium, Color: u.theme.Primary,
		})
	}
}

func (u *UI) drawChatEmpty(f *flex.Frame) {
	f.Column().Fill().Items(flex.Center).Justify(flex.Center).Pad(42).Gap(12).Children(func() {
		f.Row().Size(62, 62).Radius(18).Background(u.palette.mutedAvatar).Items(flex.Center).
			Justify(flex.Center).Children(func() {
			f.Icon(u.iconMessage).Size(28, 28).Color(u.palette.secondaryText)
		})
		f.Text(i18n.T("选择一段会话开始聊天")).FontSize(16).Weight(ui.WeightMedium)
		f.Paragraph("消息只在局域网内直连。联系人上线后会自动出现，也可以手动输入地址连接。", 3).
			MaxWidth(430).FontSize(13).Muted().TextCenter().LineHeight(20)
		f.Row().Gap(10).Justify(flex.Center).Children(func() {
			connect := f.Button(u.ids.chatEmptyConnect, i18n.T("手动连接")).Outline()
			if connect.Clicked() {
				u.openConnect()
			}
			compose := f.Button(u.ids.chatEmptyCompose, i18n.T("创建群聊"))
			if compose.Clicked() {
				u.openCompose("create", "")
			}
		})
		f.Row().Height(10)
		u.connectionPill(f)
	})
}

func (u *UI) drawChatHeader(f *flex.Frame, conversation domain.Conversation) {
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

	var searchButton, fileButton, memberButton, moreButton flex.Box
	memberShown := false
	f.Row().Height(chatHeaderExtent).FillWidth().PadSides(7, 22, 7, 24).Items(flex.Center).Gap(12).Children(func() {
		avatar := ""
		if conversation.Kind == "direct" {
			peer := u.state.Peer(conversation.PeerID)
			avatar = peer.Avatar
			if peer.Name != "" {
				avatarName = peer.Name
			}
		}
		u.avatarWithPresence(f, avatarName, avatar, 42, online, false, u.palette.chat)
		f.Column().Grow(1).Gap(3).Justify(flex.Center).Children(func() {
			f.Text(title).FontSize(17).Weight(ui.WeightSemibold).Ellipsis()
			f.Row().Items(flex.Center).Gap(7).Children(func() {
				dot := u.palette.secondaryText
				if online {
					dot = u.palette.success
				}
				f.Stack().Size(7, 7).Radius(4).Background(dot)
				f.Text(subtitle).FontSize(12).Muted().Ellipsis()
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

func (u *UI) drawChatNotice(f *flex.Frame) {
	notice := u.localNotice
	if notice == "" {
		notice = u.state.Notice
		if notice == "已保存" || notice == "个人资料已保存" {
			notice = ""
		}
	}
	if u.state.Error == "" || u.localNotice != "" {
		if notice == "" {
			return
		}
		f.Row().FillWidth().PadSides(8, 28, 8, 28).Height(34).Children(func() {
			f.Row().Fill().PadX(12).Radius(8).Background(u.palette.fileCard).Items(flex.Center).Gap(8).
				Children(func() {
					f.Icon(u.iconCircleCheck).Size(15, 15).Color(u.palette.success)
					f.Text(notice).FontSize(12).Color(u.palette.secondaryText).Ellipsis()
				})
		})
		return
	}
	f.Row().FillWidth().PadSides(8, 28, 8, 28).Height(38).Children(func() {
		f.Row().Fill().PadSides(8, 10, 8, 12).Radius(8).
			Background(ui.RGBA(220, 48, 54, 0.08)).Items(flex.Center).Gap(8).Children(func() {
			f.Icon(u.iconAlert).Size(16, 16).Color(u.palette.danger)
			f.Text(i18n.T(u.state.Error)).FontSize(12).Color(u.palette.danger).Grow(1).Ellipsis()
			dismiss := u.iconButton(f, u.ids.chatNoticeDismiss, u.iconX, i18n.T("关闭错误提示"), 24)
			if dismiss.Clicked() {
				u.submit(domain.Command{Kind: domain.ClearError})
			}
		})
	})
}

func (u *UI) drawChatTimeline(f *flex.Frame, conversation domain.Conversation) {
	messages := u.visibleChatMessages()
	restoreFrame := u.chatFind.restorePending
	applyRestore := u.chatFind.restoreApply
	u.chatFind.restorePending = false
	u.chatFind.restoreApply = false
	if len(messages) == 0 {
		f.Column().Grow(1).Items(flex.Center).Justify(flex.Center).Gap(10).Children(func() {
			f.Row().Size(50, 50).Radius(15).Background(u.palette.mutedAvatar).Items(flex.Center).
				Justify(flex.Center).Children(func() {
				icon := u.iconMessageSquare
				if u.chatFind.open && u.chatFind.query != "" {
					icon = u.iconSearch
				}
				f.Icon(icon).Size(23, 23).Color(u.palette.secondaryText)
			})
			if u.chatFind.open && u.chatFind.query != "" {
				f.Text(i18n.T("没有匹配的消息")).FontSize(16).Weight(ui.WeightMedium)
				f.Text(i18n.T("换一个关键词，搜索消息文字或文件名。")).FontSize(12).Muted().TextCenter()
			} else {
				f.Text(i18n.T("还没有消息")).FontSize(16).Weight(ui.WeightMedium)
				hint := i18n.T("发送一条消息开始聊天。")
				if conversation.Kind == "group" {
					hint = i18n.T("发送第一条群消息，邀请会在成员确认后生效。")
				}
				f.Text(hint).FontSize(12).Muted().TextCenter()
			}
		})
		return
	}

	area := f.ScrollArea(u.ids.chatMessages).Grow(1)
	switch {
	case restoreFrame:
		area = area.Follow(false)
		if state := u.chatView.ScrollState(u.ids.chatMessages); state != nil {
			state.ScrollTo(u.chatFind.savedOffset)
		}
	case applyRestore:
		area = area.Follow(true)
		if state := u.chatView.ScrollState(u.ids.chatMessages); state != nil {
			state.ScrollTo(u.chatFind.savedOffset)
		}
	default:
		area = area.Follow(true)
	}
	area.PadSides(0, 0, 18, 0)
	area.Children(func() {
		f.Column().FillWidth().PadSides(22, 36, 18, 36).Gap(17).Children(func() {
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
			f.Row().Height(2)
		})
	})
	if u.chatScrollToEnd {
		u.chatView.ScrollState(u.ids.chatMessages).ScrollToEnd()
		u.chatScrollToEnd = false
	}
	if restoreFrame {
		u.chatFind.restoreApply = true
		u.invalidator.Invalidate()
	}
}

func (u *UI) drawDateSeparator(f *flex.Frame, at time.Time) {
	f.Row().FillWidth().Height(32).Items(flex.Center).Gap(12).Children(func() {
		f.Row().Grow(1).Height(1).Background(u.palette.border)
		f.Text(friendlyDay(at)).FontSize(11).Muted()
		f.Row().Grow(1).Height(1).Background(u.palette.border)
	})
}

func (u *UI) drawMessage(f *flex.Frame, conversation domain.Conversation, message domain.Message) {
	if message.Recalled {
		u.drawRecalledMessage(f, conversation, message)
		return
	}
	if message.Kind == "system" {
		f.Row().FillWidth().Justify(flex.Center).Children(func() {
			f.Column().MaxWidthPercent(80).PadSides(7, 12, 7, 12).Radius(8).
				Background(u.palette.mutedAvatar).Children(func() {
				f.Text(message.Text).FontSize(11).Muted().TextCenter().MaxLines(3)
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

func (u *UI) drawTextMessage(f *flex.Frame, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	if own {
		f.Row().FillWidth().Justify(flex.End).Items(flex.End).Gap(9).Children(func() {
			u.messageTime(f, message, true)
			u.messageBubble(f, conversation, message, true, maxWidth)
			u.ownMessageAvatar(f)
		})
		return
	}
	f.Row().FillWidth().Justify(flex.Start).Items(flex.End).Gap(10).Children(func() {
		peer := u.state.Peer(message.SenderID)
		name := u.messageSenderName(conversation, message)
		u.avatarWithPresence(f, name, peer.Avatar, 34, false, false, u.palette.chat)
		f.Column().Gap(4).Items(flex.Start).Children(func() {
			if conversation.Kind == "group" {
				f.Text(name).FontSize(11.5).Muted().Ellipsis()
			}
			f.Row().Items(flex.End).Gap(9).Children(func() {
				u.messageBubble(f, conversation, message, false, maxWidth)
				u.messageTime(f, message, false)
			})
		})
	})
}

func (u *UI) messageBubble(f *flex.Frame, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) flex.Box {
	background := u.palette.incoming
	if own {
		background = u.palette.own
	}
	bubble := f.Column().MaxWidth(maxWidth).PadSides(10, 13, 9, 13).Radius(11).
		HitWith(u.messageIDs[message.ID], i18n.T("聊天消息"), ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true}).
		Background(background).Border(1, u.palette.bubbleBorder).Gap(3).Children(func() {
		if message.Text != "" {
			f.Paragraph(message.Text, 60).FontSize(14).LineHeight(21)
		}
		if message.Kind == "text" && message.Text == "" {
			f.Text(i18n.T("（空消息）")).FontSize(13).Muted()
		}
	})
	u.messageContextPointer(f.Canvas(), u.messageIDs[message.ID], message)
	return bubble
}

func (u *UI) messageTime(f *flex.Frame, message domain.Message, own bool) {
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
	f.Column().PadX(1).Children(func() {
		f.Text(label).FontSize(10.5).Muted()
	})
}

func (u *UI) ownMessageAvatar(f *flex.Frame) {
	self := u.state.Self
	u.avatarWithPresence(f, self.Name, self.Avatar, 34, false, false, ui.Color{})
}

func (u *UI) drawImageMessage(f *flex.Frame, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	file := u.state.File(message.FileID)
	path := u.fileLocalPath[file.ID]
	bitmap := u.fileBitmap(path)
	if !bitmapReady(bitmap) {
		u.drawFileMessage(f, conversation, message, own, maxWidth)
		return
	}
	width, height := chatImageThumbnail(bitmap, maxWidth)
	justify := flex.Start
	align := flex.Start
	if own {
		justify, align = flex.End, flex.End
	}
	f.Row().FillWidth().Justify(justify).Items(flex.Start).Gap(10).Children(func() {
		if !own {
			peer := u.state.Peer(message.SenderID)
			u.avatarWithPresence(f, u.messageSenderName(conversation, message), peer.Avatar, 34, false, false, u.palette.chat)
		}
		f.Column().Gap(4).Items(align).Children(func() {
			if !own && conversation.Kind == "group" {
				f.Text(u.messageSenderName(conversation, message)).FontSize(11.5).Muted().Ellipsis()
			}
			picture := f.Stack().Width(width).Height(height).Radius(11).Clip().
				Background(u.palette.mutedAvatar)
			if file.ID != "" {
				picture = picture.HitWith(u.fileButtonIDs[file.ID], i18n.T("查看图片 ")+displayFileName(file, message),
					ui.HitOptions{Role: ui.RoleButton, Hint: i18n.T("打开图片查看器")})
			}
			picture.Children(func() {
				f.Image(bitmap).Fill()
			})
			u.messageContextPointer(f.Canvas(), u.fileButtonIDs[file.ID], message)
			if picture.Clicked() {
				u.openImageViewer(file.ID)
			}
			if message.Text != "" && message.Text != file.Name {
				f.Column().MaxWidth(width).PadSides(8, 12, 7, 12).Radius(9).
					Background(u.palette.incoming).Children(func() {
					f.Paragraph(message.Text, 4).FontSize(13).LineHeight(19)
				})
			}
			f.Row().Items(flex.Center).Gap(6).Children(func() {
				u.messageTime(f, message, own)
			})
		})
		if own {
			u.ownMessageAvatar(f)
		}
	})
}

func chatImageThumbnail(bitmap ui.Bitmap, maxWidth float32) (float32, float32) {
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

func (u *UI) drawFileMessage(f *flex.Frame, conversation domain.Conversation, message domain.Message, own bool, maxWidth float32) {
	file := u.state.File(message.FileID)
	justify := flex.Start
	if own {
		justify = flex.End
	}
	f.Row().FillWidth().Justify(justify).Items(flex.Start).Gap(10).Children(func() {
		if !own {
			peer := u.state.Peer(message.SenderID)
			u.avatarWithPresence(f, u.messageSenderName(conversation, message), peer.Avatar, 34, false, false, u.palette.chat)
		}
		f.Column().Gap(4).Children(func() {
			if !own && conversation.Kind == "group" {
				f.Text(u.messageSenderName(conversation, message)).FontSize(11.5).Muted().Ellipsis()
			}
			cardWidth := min(float32(480), max(maxWidth, float32(310)))
			card := f.Column().Width(cardWidth).Pad(14).Radius(11).
				Background(u.palette.fileCard).Border(1, u.palette.fileBorder).Gap(10).
				HitWith(u.fileButtonIDs[file.ID], i18n.T("查看 ")+displayFileName(file, message)+i18n.T(" 详情"),
					ui.HitOptions{Role: ui.RoleButton, Hint: i18n.T("查看文件详情")}).
				Children(func() {
					f.Row().Items(flex.Center).Gap(11).Children(func() {
						f.Row().Size(40, 40).Radius(9).Items(flex.Center).Justify(flex.Center).
							Background(fileIconTint(file)).Children(func() {
							f.Icon(u.fileIcon(file)).Size(21, 21).Color(fileIconColor(file))
						})
						f.Column().Grow(1).Gap(3).Children(func() {
							f.Text(displayFileName(file, message)).FontSize(14).
								Weight(ui.WeightSemibold).Ellipsis()
							f.Text(fileMeta(file, message)).FontSize(11).Muted().Ellipsis()
						})
					})
					f.Text(u.transferSummary(file)).FontSize(11.5).Muted().Ellipsis()
					activeShown := false
					for _, transfer := range u.transfersFor[file.ID] {
						if !transferNeedsAttention(transfer) {
							continue
						}
						activeShown = true
						fraction := transferFraction(transfer)
						f.Progress(fraction).Height(6)
						f.Row().Items(flex.Center).Children(func() {
							f.Text(u.transferProgressLabel(transfer)).FontSize(10.5).Muted().Ellipsis()
							f.Spacer()
							f.Text(i18n.T("查看详情")).FontSize(11).Weight(ui.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
					if !activeShown {
						f.Row().Items(flex.Center).Children(func() {
							f.Text(fileAvailability(u, file)).FontSize(10.5).Muted().Ellipsis()
							f.Spacer()
							f.Text(i18n.T("查看详情")).FontSize(11).Weight(ui.WeightMedium).
								Color(u.palette.selectedText)
						})
					}
				})
			u.messageContextPointer(f.Canvas(), u.fileButtonIDs[file.ID], message)
			if card.Clicked() {
				u.openFileDetail(file.ID, "chat")
			}
			f.Row().Items(flex.Center).Gap(6).Children(func() {
				u.messageTime(f, message, own)
			})
		})
		if own {
			u.ownMessageAvatar(f)
		}
	})
}

func (u *UI) drawComposer(f *flex.Frame, conversation domain.Conversation) {
	hasText := u.chatEditor != nil && strings.TrimSpace(u.chatEditor.String()) != ""
	composer := f.Column().FillWidth().FillHeight().PadSides(2, 9, 8, 9).
		Background(u.palette.header).Children(func() {
		border := u.theme.Border
		if f.Canvas().Focused() == u.ids.chatEditor {
			border = u.palette.selectedText
		}
		f.Column().Fill().Radius(11).Border(1, border).Background(u.palette.header).
			PadSides(1, 5, 4, 5).Children(func() {
			focused := f.Canvas().Focused() == u.ids.chatEditor
			selection := u.chatEditor.Selection()
			composition, _ := u.chatEditor.Composition()
			caret := u.chatEditor.Caret()
			editor := f.TextareaBase(components.TextareaSnapshot{
				ID: u.ids.chatEditor, Label: i18n.T("消息输入框"), Value: u.chatEditor.String(),
				Placeholder: i18n.T("输入消息…"), Hint: sendKeyHint(u.state.Settings.SendKey),
				Focused: focused, NativeEditing: false, HideEditorText: true, ShowCaret: true,
				Layout: &u.chatLayout, Caret: caret,
				SelectionStart: selection.Start, SelectionEnd: selection.End,
				CompositionStart: composition.Start, CompositionEnd: composition.End,
				InsetBottom: 4,
			}).FillWidth().Grow(1).Basis(0).MinHeight(40)
			u.mentionEditor = editor

			f.Row().Height(34).Items(flex.Center).Gap(3).Children(func() {
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
					u.chooseChatFiles(true)
				}
				if fileButton.Clicked() {
					u.chooseChatFiles(false)
				}
				if snip.Clicked() && u.requestScreenshot != nil {
					u.requestScreenshot()
				}
				f.Spacer()
				send := f.Button(u.ids.chatSend, i18n.T("发送")).Width(88).Height(30).Disabled(!hasText)
				if send.Clicked() && hasText {
					u.sendChatText()
				}
			})
		})
	})
	u.composerBox = composer
}

func (u *UI) drawChatReadOnly(f *flex.Frame, reason string) {
	title := i18n.T("该群聊已关闭")
	if strings.HasPrefix(reason, "等待成员") {
		title = i18n.T("正在等待成员加入")
	}
	f.Column().FillWidth().FillHeight().PadSides(8, 28, 8, 28).
		Background(u.palette.header).Justify(flex.Center).Children(func() {
		f.Row().Fill().PadSides(10, 14, 10, 14).Radius(10).Background(u.palette.incoming).
			Items(flex.Center).Gap(10).Children(func() {
			f.Row().Size(30, 30).Radius(8).Items(flex.Center).Justify(flex.Center).
				Background(u.palette.mutedAvatar).Children(func() {
				f.Icon(u.iconLock).Size(15, 15).Color(u.palette.secondaryText)
			})
			f.Column().Grow(1).Gap(2).Children(func() {
				f.Text(title).FontSize(12.5).Weight(ui.WeightMedium)
				f.Text(reason).FontSize(10.5).Muted().Ellipsis()
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

func (u *UI) chooseChatFiles(imagesOnly bool) {
	if u.selected == "" || u.chatReadOnlyReason(u.state.Conversation(u.selected)) != "" {
		return
	}
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
	paths, err := u.openFilePicker(options)
	if err != nil {
		if errors.Is(err, app.ErrCanceled) || errors.Is(err, app.ErrNoDriverResponse) ||
			errors.Is(err, app.ErrUnsupported) {
			return
		}
		return
	}
	if len(paths) == 0 {
		return
	}
	u.submit(domain.Command{Kind: domain.SendFiles, ConversationID: u.selected, Paths: append([]string(nil), paths...)})
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

func (u *UI) connectionPill(f *flex.Frame) {
	f.Row().PadSides(6, 12, 6, 12).Radius(12).Background(u.palette.mutedAvatar).
		Items(flex.Center).Gap(6).Children(func() {
		color := u.palette.danger
		label := i18n.T("等待局域网")
		if u.state.ListenAddress != "" {
			color = u.palette.success
			label = u.state.ListenAddress
		}
		f.Stack().Size(6, 6).Radius(3).Background(color)
		f.Text(label).FontSize(10.5).Color(u.palette.secondaryText).Ellipsis()
	})
}

func (u *UI) fileIcon(file domain.File) ui.Icon {
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

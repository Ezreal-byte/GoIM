package desktop

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) messageContextPointer(c *ui.Canvas, id ui.ID, m domain.Message) {
	if id.IsZero() || m.ID == "" || m.Kind == "system" {
		return
	}
	if event, ok := c.PointerOn(id); ok && event.Kind == ui.PointerDown && event.Button == ui.PointerButtonSecondary {
		u.openShellMenu(shellMenuMessage, m.ID, event.X, event.Y)
		c.Consume()
		c.RequestFrame()
	}
}

func (u *UI) drawRecalledMessage(f *flex.Frame, c domain.Conversation, m domain.Message) {
	id := u.messageIDs[m.ID]
	label := i18n.T("你撤回了一条消息")
	if m.SenderID != u.state.Self.ID {
		label = u.messageSenderName(c, m) + i18n.T(" 撤回了一条消息")
	}
	f.Row().FillWidth().Justify(flex.Center).Children(func() {
		f.Row().PadSides(6, 10, 6, 10).Radius(7).Background(u.palette.mutedAvatar).
			HitWith(id, label, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true}).Children(func() {
			f.Text(label).FontSize(11).Muted()
		})
	})
	u.messageContextPointer(f.Canvas(), id, m)
}

func (u *UI) drawMessageContextMenu(c *ui.Canvas, viewport ui.Rect) {
	var m domain.Message
	for _, candidate := range u.messages {
		if candidate.ID == u.shellMenu.target {
			m = candidate
			break
		}
	}
	if m.ID == "" || m.ConversationID != u.selected || u.isPanelSection() {
		u.closeShellMenu()
		return
	}
	conv := u.state.Conversation(m.ConversationID)
	canRecall := !m.Recalled && m.SenderID == u.state.Self.ID && u.state.Peer(conv.PeerID).Transport != "ipmsg"
	f := u.state.File(m.FileID)
	isAttachment := !m.Recalled && (m.Kind == "file" || m.Kind == "image")
	path := u.fileLocalPath[f.ID]
	available := path != "" && u.fileAvailable[f.ID]
	var offer domain.Transfer
	if isAttachment && !available {
		for _, t := range u.transfersFor[f.ID] {
			if t.Direction == "receive" && t.Status == "offered" && !t.Revoked {
				offer = t
				break
			}
		}
	}
	rows := 1 // Delete is also available for a recall placeholder.
	if !m.Recalled {
		rows++
	}
	if isAttachment {
		rows++
	}
	if canRecall {
		rows++
	}
	menu := u.beginShellMenu(c, viewport, rows, 0)
	if !m.Recalled {
		menu.AddItem(components.ContextMenuItem{ID: u.ids.messageMenuCopy, Label: i18n.T("复制"), Icon: Copy,
			Disabled: isAttachment && !available || !isAttachment && m.Text == ""})
	}
	if isAttachment {
		menu.AddItem(components.ContextMenuItem{ID: u.ids.messageMenuSave, Label: i18n.T("另存为…"), Icon: Download, Disabled: !available && offer.ID == ""})
	}
	if canRecall {
		menu.AddItem(components.ContextMenuItem{ID: u.ids.messageMenuRecall, Label: i18n.T("撤回"), Icon: RotateCcw})
	}
	menu.AddItem(components.ContextMenuItem{ID: u.ids.messageMenuDelete, Label: i18n.T("删除"), Icon: Trash2, Variant: components.ContextMenuItemDestructive})
	menu.End()
	switch {
	case c.Clicked(u.ids.messageMenuCopy):
		u.closeShellMenu()
		var err error
		if isAttachment && available {
			err = app.SetClipboardFiles([]string{path})
		} else if !isAttachment && !m.Recalled {
			err = app.SetClipboardText(m.Text)
		}
		if err != nil {
			u.submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("复制失败：") + err.Error()})
		}
	case c.Clicked(u.ids.messageMenuSave):
		u.closeShellMenu()
		if available {
			u.saveMessageAttachment(f, path)
		} else if offer.ID != "" {
			u.chooseTransferDestination(offer)
		}
	case c.Clicked(u.ids.messageMenuRecall):
		u.closeShellMenu()
		if canRecall {
			u.submit(domain.Command{Kind: domain.RecallMessage, ConversationID: m.ConversationID, MessageID: m.ID})
		}
	case c.Clicked(u.ids.messageMenuDelete):
		u.closeShellMenu()
		u.confirm = confirmation{open: true, title: i18n.T("删除这条消息？"), body: i18n.T("只会从本机的聊天记录删除。"), action: func() {
			u.submit(domain.Command{Kind: domain.DeleteMessage, ConversationID: m.ConversationID, MessageID: m.ID})
		}}
	case c.Clicked(u.ids.shellMenuDismiss):
		u.closeShellMenu()
	}
}

func (u *UI) saveMessageAttachment(file domain.File, source string) {
	name := filepath.Base(file.Name)
	if name == "" || name == "." {
		name = filepath.Base(source)
	}
	path, err := app.SaveFile(app.SaveFileOptions{Title: i18n.T("另存为…"), SuggestedName: name,
		DefaultExtension: strings.TrimPrefix(filepath.Ext(name), ".")})
	if errors.Is(err, app.ErrCanceled) || (err == nil && path == "") {
		return
	}
	if err != nil {
		u.submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法保存文件：") + err.Error()})
		return
	}
	go func() {
		if err := copyViewerImage(source, path); err != nil {
			u.submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法保存文件：") + err.Error()})
		}
	}()
}

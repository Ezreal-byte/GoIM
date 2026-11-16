package desktop

import (
	"errors"
	"github.com/vivaui/viva/gfx"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const panelPageTop = 32

func panelPageInset(width float32) float32 {
	if width < 760 {
		return 20
	}
	return 25
}

func drawPanelPageHeading(f ui.C, width float32, title, description string) {
	ui.Column(f).Grow(1).MinWidth(112).Gap(6).Children(func() {
		ui.Text(f, title).FontSize(29).Weight(gfx.WeightSemibold).Ellipsis()
		if width >= 760 {
			ui.Text(f, description).Muted().FontSize(15).Ellipsis()
		}
	})
}

type transferActionIDs struct {
	more, accept, saveAs, reject, cancel, retry, open, reveal gfx.ID
}

func newTransferActionIDs(tree *gfx.Tree, scope gfx.Scope, entry panelTransferEntry) (ids transferActionIDs) {
	ids.more = tree.ID(scope, "more")
	if entry.accept {
		ids.accept = tree.ID(scope, "accept")
		ids.saveAs = tree.ID(scope, "save-as")
		ids.reject = tree.ID(scope, "reject")
	}
	if entry.cancel {
		ids.cancel = tree.ID(scope, "cancel")
	}
	if entry.retry {
		ids.retry = tree.ID(scope, "retry")
	}
	if entry.open {
		ids.open = tree.ID(scope, "open")
	}
	if entry.reveal {
		ids.reveal = tree.ID(scope, "reveal")
	}
	return ids
}

func (p *Panels) drawTransfers(f ui.C, theme ui.Theme, bounds ui.Rect, state domain.State, submit func(domain.Command)) {
	pad := panelPageInset(bounds.Width)
	compact := bounds.Width < 800
	ui.Column(f).Fill().PadSides(panelPageTop, pad, 18, pad).Children(func() {
		p.drawTransferHeader(f, theme, bounds.Width, state.Settings.DownloadDir)
		if p.localError != "" || state.Error != "" {
			state.Notice = ""
			ui.Column(f).FillWidth().PadSides(12, 0, 0, 0).Gap(6).Children(func() {
				p.panelFeedback(f, theme, state, submit, p.ids.transfersDismiss, p.ids.transfersClearError)
			})
		}
		if len(p.transferIncoming) > 0 {
			p.drawIncomingTransfers(f, theme, bounds, submit)
		}
		p.drawTransferTabs(f, compact)
		ui.Divider(f).FillWidth()
		ui.Scroll(f.KeyInt(int(p.transferFilter))).Grow(1).FillWidth().Children(func() {
			ui.Column(f).FillWidth().Children(func() {
				entries := p.transferPending
				emptyTitle, emptyDescription := i18n.T("暂无进行中的传输"), i18n.T("收到文件邀请或开始发送后，任务会显示在这里。")
				if p.transferFilter == panelTransferFinished {
					entries = p.transferFinished
					emptyTitle, emptyDescription = i18n.T("暂无已结束的传输"), i18n.T("已完成、已拒绝、已取消和失败的任务会显示在这里。")
				} else if len(p.transferIncoming) > 0 {
					emptyDescription = i18n.T("接收上方文件后，可在这里查看传输进度。")
				}
				if len(entries) == 0 {
					ui.Column(f).FillWidth().PadSides(24, 0, 0, 0).Children(func() {
						panelEmpty(f, theme, emptyTitle, emptyDescription)
					})
				}
				for i := range entries {
					p.drawTransferRow(f, theme, &entries[i], bounds.Width-2*pad, compact, submit)
				}
			})
		})
	})
}

func (p *Panels) drawTransferTabs(f ui.C, compact bool) {
	top := float32(29)
	if compact || len(p.transferIncoming) == 0 {
		top = 21
	}
	const size = 16
	var widest float32
	for _, label := range p.transferLabels {
		widest = max(widest, f.Canvas().TextWidthWeight(label, size, gfx.WeightMedium))
	}
	ui.Row(f).FillWidth().PadSides(top, 0, 0, 0).Children(func() {
		tabs := ui.Tabs(f.Part("transfer-filter"), &p.transferFilter, transferFilters[:],
			func(filter transferFilter) string { return p.transferLabels[filter] }).
			Line().FontSize(size).Width(2*max(104, widest+24) + 4)
		if tabs.Changed() {
			p.transferMenu = ""
		}
	})
}

func (p *Panels) drawTransferHeader(f ui.C, theme ui.Theme, width float32, directory string) {
	ui.Row(f).FillWidth().Gap(12).Items(ui.Start).Children(func() {
		drawPanelPageHeading(f, width, i18n.T("文件传输"), i18n.T("接收新文件，并跟进发送和接收进度。"))
		label := i18n.T("接收目录")
		height, size := float32(45), float32(15)
		if width < 760 {
			height, size = 39, 13
		}
		button := ui.Row(f).Height(height).PadX(14).Gap(9).Items(ui.Center).Radius(8).
			Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
			HitWith(p.ids.transferDirectory, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: directory == ""})
		button.Children(func() {
			ui.Icon(f, FolderOpen).Size(18, 18).Color(theme.MutedForeground)
			ui.Text(f, label).FontSize(size).Weight(gfx.WeightMedium)
		})
		if button.Clicked() {
			p.queueLocalAction(mediaJob{path: directory, reveal: true, directory: true, ensureDirectory: true})
		}
	})
}

func (p *Panels) drawIncomingTransfers(f ui.C, theme ui.Theme, bounds ui.Rect, submit func(domain.Command)) {
	rowHeight, top, side := 71, float32(37), float32(21)
	if bounds.Width < 800 {
		rowHeight, top, side = 65, 22, 16
	}
	limit := 3
	if bounds.Height < 580 {
		limit = 2
	}
	ui.Column(f).FillWidth().PadSides(top, 0, 0, 0).Children(func() {
		tint := panelTint(panelAccentText(theme), .077, theme.Background)
		if panelDark(theme) {
			tint = panelAccentSurface(theme)
		}
		ui.Column(f).FillWidth().Radius(8).Background(tint).PadSides(14, side, 11, side).Gap(12).Children(func() {
			ui.Text(f, p.transferIncomingLabel).FontSize(19).Weight(gfx.WeightSemibold)
			drawRows := func() {
				for i := range p.transferIncoming {
					entry := &p.transferIncoming[i]
					if i > 0 {
						ui.Divider(f).FillWidth()
					}
					ui.Row(f).FillWidth().Height(float32(rowHeight)).Gap(12).Items(ui.Center).Children(func() {
						drawTransferIdentity(f, theme, entry)
						width := float32(90)
						if bounds.Width < 760 {
							width = 68
						}
						if namedButton(f, entry.ids.saveAs, i18n.T("另存为…")).Outline().FontSize(15).Height(38).MinWidth(width + 8).Disabled(p.transferPickerOpen).Clicked() {
							p.saveTransferAs(f.Canvas(), submit, entry.transfer)
						}
						if namedButton(f, entry.ids.accept, i18n.T("接收")).FontSize(15).Height(38).MinWidth(width).Clicked() {
							submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: entry.transfer.ID})
						}
						p.drawTransferMore(f, theme, entry, true)
					})
				}
			}
			if len(p.transferIncoming) > limit {
				ui.Scroll(f.Part("incoming-transfers")).FillWidth().Height(float32(limit * (rowHeight + 1))).Children(drawRows)
			} else {
				drawRows()
			}
		})
	})
}

func drawTransferIdentity(f ui.C, theme ui.Theme, entry *panelTransferEntry) {
	icon, ink, fill := transferFileIcon(theme, entry.fileKind)
	ui.Row(f).Grow(1).MinWidth(0).Gap(15).Items(ui.Center).Children(func() {
		ui.Row(f).Size(40, 40).Radius(8).Background(fill).Center().Children(func() {
			ui.Icon(f, icon).Size(23, 23).Color(ink)
		})
		ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(f, entry.name).FontSize(18).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, entry.meta).FontSize(15).Muted().Ellipsis()
		})
	})
}

func transferFileIcon(theme ui.Theme, kind string) (gfx.Icon, ui.Color, ui.Color) {
	switch kind {
	case "archive":
		ink := ui.RGB(181, 121, 16)
		if panelDark(theme) {
			ink = ui.RGB(244, 189, 78)
		}
		return FileArchive, ink, ink.WithAlpha(.16)
	case "sheet":
		return FileText, theme.Success, theme.Success.WithAlpha(.14)
	case "image":
		return shareProjectIcon(theme, panelShareEntry{iconKind: "image"})
	default:
		return FileText, panelAccentText(theme), panelAccentSurface(theme)
	}
}

func (p *Panels) drawTransferRow(f ui.C, theme ui.Theme, entry *panelTransferEntry, width float32, compact bool, submit func(domain.Command)) {
	ui.Column(f).FillWidth().MinHeight(85).PadSides(17, 18, 17, 18).Gap(7).Justify(ui.Center).Children(func() {
		ui.Row(f).FillWidth().Gap(12).Items(ui.Center).Children(func() {
			drawTransferIdentity(f, theme, entry)
			if !compact {
				ui.Row(f).Width(width * .37).Items(ui.Center).Children(func() {
					drawTransferStatus(f, theme, entry)
				})
			}
			ui.Row(f).MinWidth(91).Gap(10).Items(ui.Center).Justify(ui.End).Children(func() {
				p.drawTransferActions(f, theme, entry, submit)
			})
		})
		if compact {
			ui.Row(f).FillWidth().PadSides(0, 0, 0, 52).Children(func() {
				drawTransferStatus(f, theme, entry)
			})
		}
		if entry.err != "" {
			ui.Paragraph(f, i18n.T(entry.err), 2).PadSides(0, 0, 0, 52).FontSize(13).Color(theme.Danger)
		}
	})
	ui.Divider(f).FillWidth()
}

func drawTransferStatus(f ui.C, theme ui.Theme, entry *panelTransferEntry) {
	if entry.percent == "" {
		ui.Text(f, entry.status).FontSize(15).Color(panelStatusColor(theme, entry.transfer.Status)).Ellipsis()
		return
	}
	ui.Column(f).Grow(1).MinWidth(0).MaxWidth(310).Gap(5).Children(func() {
		ui.Row(f).FillWidth().Gap(12).Items(ui.Center).Children(func() {
			ui.Progress(f, entry.fraction).Grow(1).MinWidth(32).Height(7)
			ui.Text(f, entry.percent).Muted().FontSize(15)
		})
		ui.Text(f, entry.progress).Muted().FontSize(14).Ellipsis()
		if entry.transfer.Status != "transferring" {
			ui.Text(f, entry.status).Muted().FontSize(13).Ellipsis()
		}
	})
}

func (p *Panels) drawTransferActions(f ui.C, theme ui.Theme, entry *panelTransferEntry, submit func(domain.Command)) {
	if entry.retry && namedButton(f, entry.ids.retry, i18n.T("重试")).Small().Outline().Clicked() {
		submit(domain.Command{Kind: domain.RetryTransfer, TransferID: entry.transfer.ID})
	}
	if entry.open && namedButton(f, entry.ids.open, i18n.T("打开")).Small().Outline().Clicked() {
		panelOpenLocal(f.Canvas(), p, entry.path, false)
	}
	if entry.cancel && panelActionIcon(f, theme, entry.ids.cancel, X, i18n.T("取消传输"), false, false).Clicked() {
		submit(domain.Command{Kind: domain.CancelTransfer, TransferID: entry.transfer.ID})
	}
	p.drawTransferMore(f, theme, entry, false)
}

func (p *Panels) drawTransferMore(f ui.C, theme ui.Theme, entry *panelTransferEntry, bordered bool) {
	label := i18n.T("更多传输操作")
	open := p.transferMenu == entry.transfer.ID
	more := panelActionIcon(f, theme, entry.ids.more, MoreHorizontal, label, open, false).
		HitWith(entry.ids.more, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Expandable: true, Expanded: open})
	if bordered && !open && f.Canvas().FocusVisible() != entry.ids.more {
		more.Size(41, 37).Background(theme.Background).Stroke(1, theme.Border)
	} else if bordered {
		more.Size(41, 37)
	}
	if more.Clicked() {
		if open {
			p.transferMenu = ""
		} else {
			p.transferMenu = entry.transfer.ID
		}
	}
	if p.transferMenu == entry.transfer.ID {
		p.transferAnchor, p.transferMenuEntry = more, entry
	}
}

func (p *Panels) drawTransferMenu(c *gfx.Canvas, theme ui.Theme, viewport ui.Rect, submit func(domain.Command)) {
	if p.transferMenu == "" {
		return
	}
	entry := p.transferMenuEntry
	anchor := p.transferAnchor.Rect()
	if entry == nil || anchor.Width <= 0 || anchor.Y < viewport.Y || anchor.Y+anchor.Height > viewport.Y+viewport.Height {
		p.transferMenu = ""
		return
	}
	height := float32(44)
	if entry.reveal {
		height += 32
	}
	if entry.reject {
		height += 41
	}
	placement := components.OverlayPlacement{Side: components.OverlayBottom, Align: components.OverlayAlignEnd, SideOffset: 5, Margin: 8}
	bounds, _ := components.PlaceOverlay(anchor, ui.Size{Width: 196, Height: height}, viewport, placement)
	menu := components.BeginContextMenu(c, theme, bounds, components.ContextMenuOptions{
		Layer: 64, Scope: p.ids.transferMenuScope, DismissID: p.ids.transferMenuDismiss, Viewport: viewport,
	})
	menu.AddItem(components.ContextMenuItem{ID: p.ids.transferMenuCopy, Label: i18n.T("复制文件名"), Icon: Copy})
	if entry.reveal {
		menu.AddItem(components.ContextMenuItem{ID: entry.ids.reveal, Label: i18n.T("打开文件夹"), Icon: FolderOpen})
	}
	if entry.reject {
		menu.AddSeparator()
		menu.AddItem(components.ContextMenuItem{ID: entry.ids.reject, Label: i18n.T("拒绝"), Icon: X, Variant: components.ContextMenuItemDestructive})
	}
	menu.End()
	switch {
	case c.Clicked(p.ids.transferMenuCopy):
		name := entry.name
		c.AfterPaint(func() {
			if err := app.SetClipboardText(name); err != nil {
				p.localError = i18n.T("复制失败") + "：" + err.Error()
				c.RequestFrame()
			}
		})
	case entry.reveal && c.Clicked(entry.ids.reveal):
		panelOpenLocal(c, p, entry.path, true)
	case entry.reject && c.Clicked(entry.ids.reject):
		submit(domain.Command{Kind: domain.RejectTransfer, TransferID: entry.transfer.ID})
	case c.Clicked(p.ids.transferMenuDismiss):
	default:
		return
	}
	p.transferMenu = ""
	c.RequestFrame()
}

func (p *Panels) saveTransferAs(canvas *gfx.Canvas, submit func(domain.Command), transfer domain.Transfer) {
	if p.transferPickerOpen {
		return
	}
	p.transferPickerOpen = true
	canvas.AfterPaint(func() {
		defer func() { p.transferPickerOpen = false; canvas.RequestFrame() }()
		path, err := app.SaveFile(app.SaveFileOptions{
			Title:         i18n.T("选择接收文件位置"),
			SuggestedName: transfer.Name,
		})
		switch {
		case errors.Is(err, app.ErrCanceled):
			return
		case err != nil:
			p.localError = i18n.T("无法选择保存位置：") + err.Error()
			return
		case strings.TrimSpace(path) == "":
			return
		}
		p.localError = ""
		submit(domain.Command{
			Kind:       domain.AcceptTransfer,
			TransferID: transfer.ID,
			Paths:      []string{path},
		})
	})
}

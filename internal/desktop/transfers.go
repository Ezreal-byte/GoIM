package desktop

import (
	"errors"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	panelPageTop          = 32
	panelTabFontSize      = float32(16)
	panelTableRowHeight   = float32(56)
	panelTableHeadHeight  = float32(38)
	panelTableActionWidth = float32(116)
)

func panelPageInset(width float32) float32 {
	if width < 760 {
		return 20
	}
	return 25
}

func drawPanelPageHeading(f ui.C, width float32, title, description string) {
	ui.Column(f).Grow(1).MinWidth(112).Gap(6).Children(func() {
		ui.Text(f, title).FontSize(textSize(f, 29)).Weight(gfx.WeightSemibold).Ellipsis()
		if width >= 760 {
			ui.Text(f, description).Muted().FontSize(textSize(f, 15)).Ellipsis()
		}
	})
}

type transferActionIDs struct {
	more, accept, saveAs, reject, cancel, retry, open, reveal, preview gfx.ID
}

func newTransferActionIDs(tree *gfx.Tree, scope gfx.Scope, entry panelTransferEntry) (ids transferActionIDs) {
	ids.more = tree.ID(scope, "more")
	if entry.accept {
		ids.accept = tree.ID(scope, "accept")
		ids.reject = tree.ID(scope, "reject")
	}
	if entry.saveAs {
		ids.saveAs = tree.ID(scope, "save-as")
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
	if entry.preview {
		ids.preview = tree.ID(scope, "preview")
	}
	if entry.reveal {
		ids.reveal = tree.ID(scope, "reveal")
	}
	return ids
}

func (p *Panels) drawTransfers(f ui.C, theme ui.Theme, bounds ui.Rect, state domain.State, submit func(domain.Command)) {
	pad := panelPageInset(bounds.Width)
	columns := measureTransferColumns(bounds.Width - 2*pad)
	ui.Column(f).Fill().PadSides(panelPageTop, pad, 18, pad).Children(func() {
		p.drawTransferHeader(f, theme, bounds.Width, state.Settings.DownloadDir)
		if p.localError != "" || state.Error != "" {
			state.Notice = ""
			ui.Column(f).FillWidth().PadSides(12, 0, 0, 0).Gap(6).Children(func() {
				p.panelFeedback(f, theme, state, submit, p.ids.transfersDismiss, p.ids.transfersClearError)
			})
		}
		drawTableSearch(f, p.ids.transferSearch, &p.transferSearch, i18n.T("搜索文件"))
		p.drawTransferTabs(f, bounds.Width-2*pad)
		ui.Divider(f).FillWidth()
		p.drawTransferTableHead(f, columns)
		entries := p.transferEntries()
		ui.Scroll(f.KeyInt(int(p.transferFilter))).Grow(1).MinHeight(0).FillWidth().AlwaysScrollbars().Children(func() {
			ui.Column(f).FillWidth().Children(func() {
				if len(entries) == 0 {
					ui.Column(f).FillWidth().PadSides(24, 0, 0, 0).Children(func() {
						panelEmpty(f, theme, i18n.T("暂无匹配的传输"), i18n.T("换个筛选或关键词试试。"))
					})
					return
				}
				for i := range entries {
					p.drawTransferRow(f, theme, columns, &entries[i], submit)
				}
			})
		})
	})
}

func measureTransferColumns(width float32) panelTableColumns {
	actions := panelTableActionWidth
	if width < 760 {
		actions = 92
	}
	if width >= 1180 {
		return panelTableColumns{
			peer:    140,
			source:  84,
			size:    96,
			status:  220,
			actions: actions,
		}
	}
	if width >= 900 {
		return panelTableColumns{
			peer:    100,
			source:  70,
			size:    84,
			status:  156,
			actions: actions,
		}
	}
	return panelTableColumns{
		peer:    70,
		source:  60,
		size:    74,
		status:  118,
		actions: actions,
	}
}

func drawTableSearch(f ui.C, id gfx.ID, field *ui.Field, placeholder string) {
	ui.Row(f).FillWidth().PadSides(9, 0, 0, 0).Children(func() {
		box := ui.TextFieldID(f, id, field, placeholder, placeholder).Height(33).Width(220)
		box.Addons().SetEnd(components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: Search})
	})
}

func (p *Panels) transferEntries() []panelTransferEntry {
	entries := p.transferRows
	if p.transferFilter != panelTransferAll {
		switch p.transferFilter {
		case panelTransferReceived:
			entries = p.transferReceived
		case panelTransferIncoming:
			entries = p.transferIncoming
		case panelTransferPending:
			entries = p.transferPending
		case panelTransferFinished:
			entries = p.transferFinished
		}
	}
	return panelFilterTransfers(entries, strings.TrimSpace(p.transferSearch.Text()))
}

func (p *Panels) drawTransferTabs(f ui.C, available float32) {
	size := textSize(f, panelTabFontSize)
	var widest float32
	for _, label := range p.transferLabels {
		widest = max(widest, f.Canvas().TextWidthWeight(label, size, gfx.WeightMedium))
	}
	tabWidth := float32(len(transferFilters))*max(85, widest+18) + 4
	label := func(filter transferFilter) string { return p.transferLabels[filter] }
	ui.Row(f).FillWidth().PadSides(6, 0, 0, 0).Children(func() {
		if tabWidth > available {
			if ui.Select(f.Part("transfer-filter-menu"), &p.transferFilter, transferFilters[:], label).
				Label(i18n.T("文件传输")).FontSize(size).Width(min(available, widest+64)).Height(max(36, size+14)).Changed() {
				p.transferMenu = ""
			}
			return
		}
		tabs := ui.Tabs(f.Part("transfer-filter"), &p.transferFilter, transferFilters[:], label).
			Line().FontSize(size).Width(tabWidth)
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
		fontSize := textSize(f, size)
		buttonWidth := 28 + 18 + 9 + f.Canvas().TextWidthWeight(label, fontSize, gfx.WeightMedium)
		button := ui.Row(f).Width(buttonWidth+2).Shrink(0).Height(max(height, fontSize+14)).PadX(14).Gap(9).Items(ui.Center).Radius(8).
			Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
			HitWith(p.ids.transferDirectory, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: directory == ""})
		button.Children(func() {
			ui.Icon(f, FolderOpen).Size(18, 18).Color(theme.MutedForeground)
			ui.Text(f, label).FontSize(fontSize).Weight(gfx.WeightMedium)
		})
		if button.Clicked() {
			p.queueLocalAction(mediaJob{path: directory, reveal: true, directory: true, ensureDirectory: true}, nil)
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
			ui.Text(f, p.transferIncomingLabel).FontSize(textSize(f, 19)).Weight(gfx.WeightSemibold)
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
						if entry.saveAs && namedButton(f, entry.ids.saveAs, i18n.T("另存为…")).Outline().FontSize(textSize(f, 15)).Height(38).MinWidth(width+8).Disabled(p.transferPickerOpen).Clicked() {
							p.saveTransferAs(f.Canvas(), submit, entry.transfer)
						}
						if namedButton(f, entry.ids.accept, i18n.T("接收")).FontSize(textSize(f, 15)).Height(38).MinWidth(width).Clicked() {
							submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: entry.transfer.ID})
						}
						p.drawTransferMore(f, theme, entry, true)
					})
				}
			}
			if len(p.transferIncoming) > limit {
				ui.Scroll(f.Part("incoming-transfers")).AlwaysScrollbars().FillWidth().Height(float32(limit * (rowHeight + 1))).Children(drawRows)
			} else {
				drawRows()
			}
		})
	})
}

func (p *Panels) drawTransferTableHead(f ui.C, columns panelTableColumns) {
	ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
		apply := func() {
			p.transferMenu, p.transferMenuEntry = "", nil
			p.sortTransfers()
		}
		p.transferSort.header(f, "文件名", 0, apply)
		p.transferSort.header(f, "对方", columns.peer, apply)
		p.transferSort.header(f, "来源", columns.source, apply)
		p.transferSort.header(f, "大小", columns.size, apply)
		p.transferSort.header(f, "状态", columns.status, apply)
		ui.Row(f).Width(columns.actions)
	})
}

func drawTransferIdentity(f ui.C, theme ui.Theme, entry *panelTransferEntry) {

	ui.Row(f).Grow(1).MinWidth(0).Gap(15).Items(ui.Center).Children(func() {
		drawFileIcon(f, theme, entry.icon, 40, 23)
		ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(f, entry.name).FontSize(textSize(f, 18)).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, entry.meta).FontSize(textSize(f, 15)).Muted().Ellipsis()
		})
	})
}

func panelFilterTransfers(entries []panelTransferEntry, query string) []panelTransferEntry {
	if query == "" {
		return entries
	}
	filtered := make([]panelTransferEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.name), strings.ToLower(query)) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (p *Panels) drawTransferRow(f ui.C, theme ui.Theme, columns panelTableColumns, entry *panelTransferEntry, submit func(domain.Command)) {
	rowID, hint := entry.ids.more, entry.name
	if entry.preview {
		rowID, hint = entry.ids.preview, i18n.T("预览")+" · "+entry.name
	}
	tooltip := hint
	if entry.progress != "" {
		tooltip += "\n" + entry.progress
	}
	row := ui.Row(f).FillWidth().MinHeight(panelTableRowHeight).PadSides(4, 15, 4, 15).Gap(12).Items(ui.Center).Radius(radiusControl).
		Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(rowID, entry.name, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint}).
		Tooltip(tooltip)
	row.Children(func() {
		ui.Row(f).Grow(1).MinWidth(0).Gap(11).Items(ui.Center).Children(func() {

			drawFileIcon(f, theme, entry.icon, 32, 20)
			ui.Text(f, entry.name).FontSize(textSize(f, 15)).Weight(gfx.WeightMedium).MinWidth(0).Ellipsis()
		})
		ui.Text(f, entry.peer).Width(columns.peer).FontSize(textSize(f, 13)).Ellipsis()
		ui.Text(f, entry.source).Width(columns.source).FontSize(textSize(f, 13)).Ellipsis()
		ui.Text(f, entry.size).Width(columns.size).FontSize(textSize(f, 13)).Ellipsis()
		drawTransferStatus(f, theme, entry, columns.status)
		p.drawTransferActions(f, theme, columns, entry, submit)
	})
	if entry.preview && row.Clicked() {
		p.openTransferPreview(entry)
	}
	ui.Divider(f).FillWidth()
}

func (p *Panels) openTransferPreview(entry *panelTransferEntry) {
	if p.previewTransfer != nil {
		p.previewTransfer(domain.File{ID: entry.transfer.ID, Name: entry.name, SourcePath: entry.path})
	}
}

func drawTransferStatus(f ui.C, theme ui.Theme, entry *panelTransferEntry, width float32) ui.Box {
	if entry.err != "" {
		return ui.Text(f, i18n.T(entry.status)+" · "+i18n.T(entry.err)).Width(width).
			FontSize(textSize(f, 13)).Color(theme.Danger).Ellipsis()
	}
	if entry.percent == "" {
		return ui.Text(f, entry.status).Width(width).FontSize(textSize(f, 13)).Color(panelStatusColor(theme, entry.transfer.Status)).Ellipsis()
	}
	return ui.Column(f).Width(width).Gap(2).Justify(ui.Center).Children(func() {
		ui.Row(f).FillWidth().Gap(7).Items(ui.Center).Clip().Children(func() {
			ui.Progress(f, entry.fraction).Grow(1).Basis(0).MinWidth(12).Height(6)
			ui.Text(f, entry.percent).Muted().FontSize(textSize(f, 13)).Shrink(0)
		})
		if rate := transferRateLabel(entry.transfer); rate != "" {
			ui.Text(f, rate).Muted().FontSize(textSize(f, 12)).Ellipsis()
		}
	})
}

func (p *Panels) drawTransferActions(f ui.C, theme ui.Theme, columns panelTableColumns, entry *panelTransferEntry, submit func(domain.Command)) {
	ui.Row(f).Width(columns.actions).Gap(5).Items(ui.Center).Justify(ui.End).Children(func() {
		if entry.accept && namedButton(f, entry.ids.accept, i18n.T("接收")).Small().Clicked() {
			submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: entry.transfer.ID})
		}
		if entry.retry && namedButton(f, entry.ids.retry, i18n.T("重试")).Small().Outline().Clicked() {
			submit(domain.Command{Kind: domain.RetryTransfer, TransferID: entry.transfer.ID})
		}
		if entry.open {
			label := i18n.T("打开")
			if entry.preview {
				label = i18n.T("预览")
			}
			if namedButton(f, entry.ids.open, label).Small().Outline().Clicked() {
				if entry.preview {
					p.openTransferPreview(entry)
				} else {
					p.requestLocalAction(entry.path, false, entry.directory)
				}
			}
		}
		if entry.cancel && panelActionIcon(f, theme, entry.ids.cancel, X, i18n.T("取消传输"), false, false).Clicked() {
			submit(domain.Command{Kind: domain.CancelTransfer, TransferID: entry.transfer.ID})
		}
		p.drawTransferMore(f, theme, entry, false)
	})
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
	if entry.saveAs {
		height += 41
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
	if entry.saveAs {
		menu.AddSeparator()
		menu.AddItem(components.ContextMenuItem{ID: entry.ids.saveAs, Label: i18n.T("另存为…"), Icon: FolderOpen})
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
		p.requestLocalAction(entry.path, true, entry.directory)
	case entry.saveAs && c.Clicked(entry.ids.saveAs):
		p.saveTransferAs(c, submit, entry.transfer)
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
			p.localError = i18n.T("无法选择保存位置：") + filePickerProblem(err)
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

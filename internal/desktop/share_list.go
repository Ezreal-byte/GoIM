package desktop

import (
	"errors"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type shareMenuKind uint8

const (
	shareMenuClosed shareMenuKind = iota
	shareMenuAdd
	shareMenuDevice
	shareMenuProject
)

type shareMenuState struct {
	kind    shareMenuKind
	project string
	anchor  ui.Rect
	pointer bool
}

type shareToastState struct {
	message  components.ToastSnapshot
	deadline time.Duration
	fresh    bool
}

type shareColumns struct {
	kind, access, date float32
}

var shareHomeIcon = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(3, 10), ui.LineTo(12, 3), ui.LineTo(21, 10),
	ui.MoveTo(5, 9), ui.LineTo(5, 21), ui.LineTo(19, 21), ui.LineTo(19, 9),
	ui.MoveTo(9, 21), ui.LineTo(9, 14), ui.LineTo(15, 14), ui.LineTo(15, 21),
}}

var shareQRIcon = ui.Icon{ViewBox: 24, Stroke: 2, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(3, 3), ui.LineTo(9, 3), ui.LineTo(9, 9), ui.LineTo(3, 9), ui.ClosePath(),
	ui.MoveTo(15, 3), ui.LineTo(21, 3), ui.LineTo(21, 9), ui.LineTo(15, 9), ui.ClosePath(),
	ui.MoveTo(3, 15), ui.LineTo(9, 15), ui.LineTo(9, 21), ui.LineTo(3, 21), ui.ClosePath(),
	ui.MoveTo(15, 15), ui.LineTo(18, 15), ui.LineTo(18, 18), ui.LineTo(21, 18), ui.LineTo(21, 21),
	ui.MoveTo(15, 21), ui.LineTo(15, 20), ui.MoveTo(21, 14), ui.LineTo(21, 15),
}}

func measureShareColumns(width float32) shareColumns {
	if width >= 1020 {
		return shareColumns{width * .106, width * .143, width * .22}
	}
	if width >= 740 {
		return shareColumns{0, 124, 152}
	}
	return shareColumns{0, 98, 0}
}

func (p *Panels) drawShares(f *flex.Frame, theme ui.Theme, bounds ui.Rect, state domain.State, submit func(domain.Command)) {
	pad := panelPageInset(bounds.Width)
	columns := measureShareColumns(bounds.Width - 2*pad)
	f.Column().Fill().PadSides(panelPageTop, pad, 18, pad).Children(func() {
		p.drawShareToolbar(f, theme, state.DeviceShare, bounds.Width)
		f.Row().FillWidth().PadSides(18, 0, 0, 0).Items(flex.Center).Children(func() {
			if next, clicked := f.Tabs(p.ids.sharesFilters, p.shareLabels, p.shareFilter).Line().TabClicked(); clicked && next != p.shareFilter {
				p.shareFilter = next
				p.shareSelected = ""
				p.shareMenu = shareMenuState{}
				if scroll := p.view.ScrollState(p.ids.sharesScroll); scroll != nil {
					scroll.ScrollTo(0)
				}
				if next == panelSharesRemote {
					submit(domain.Command{Kind: domain.RefreshShares})
				}
			}
			f.Spacer()
			if p.shareFilter == panelSharesRemote &&
				panelActionIcon(f, theme, p.ids.sharesRefresh, RefreshCw, i18n.T("刷新"), false, false).Clicked() {
				submit(domain.Command{Kind: domain.RefreshShares})
			}
		})
		f.Divider().FillWidth()
		entries := p.sharesMine
		if p.shareFilter == panelSharesRemote {
			entries = p.sharesRemote
		}
		if len(entries) > 0 {
			p.drawShareTableHead(f, theme, columns)
		}
		f.ScrollArea(p.ids.sharesScroll).FillWidth().Grow(1).Children(func() {
			f.Column().FillWidth().Children(func() {
				if len(entries) == 0 {
					message := i18n.T("点击「新增分享」选择文件或文件夹。")
					if p.shareFilter == panelSharesRemote {
						message = i18n.T("其他成员分享的只读文件会显示在这里。")
					}
					f.Column().FillWidth().PadSides(25, 0, 0, 0).Children(func() {
						panelEmpty(f, theme, i18n.T("暂无共享文件"), message)
					})
					return
				}
				for _, entry := range entries {
					p.drawShareProjectRow(f, theme, columns, entry, submit)
				}
			})
		})
		p.drawShareDialogs(f, theme, submit)
	})
}

func (p *Panels) drawShareToolbar(f *flex.Frame, theme ui.Theme, device domain.DeviceShare, width float32) {
	height, labelSize := float32(45), float32(15)
	if width < 760 {
		height, labelSize = 39, 13
	}
	f.Row().FillWidth().Gap(12).Items(flex.Start).Children(func() {
		drawPanelPageHeading(f, width, i18n.T("文件共享"), i18n.T("查看和管理你创建的分享，以及成员与你共享的文件。"))
		label := i18n.T("设备分享主页")
		if width < 760 {
			label = i18n.T("设备主页")
		}
		deviceButton := f.Row().Height(height).Gap(9).PadX(14).Items(flex.Center).Radius(8).
			Border(1, theme.Border).Hover(flex.Look{}.Background(theme.Hover)).
			HitWith(p.ids.deviceShareMenu, label, ui.HitOptions{Role: ui.RoleButton, Expandable: true, Expanded: p.shareMenu.kind == shareMenuDevice})
		deviceButton.Children(func() {
			f.Icon(shareHomeIcon).Size(18, 18).Color(theme.MutedForeground)
			f.Text(label).FontSize(labelSize).Weight(ui.WeightMedium)
			if device.URL != "" {
				f.Row().Size(6, 6).Radius(3).Background(theme.Success)
				if width >= 760 {
					f.Text(i18n.T("已启用")).Muted().FontSize(12)
				}
			}
			f.Icon(ChevronDown).Size(14, 14).Color(theme.MutedForeground)
		})
		if deviceButton.Clicked() {
			p.toggleShareMenu(shareMenuDevice, "")
		}
		if p.shareMenu.kind == shareMenuDevice {
			p.shareAnchor = deviceButton
		}
		addButton := f.Row().Height(height).Gap(9).PadX(14).Items(flex.Center).Radius(8).
			Background(theme.Primary).Hover(flex.Look{}.Background(theme.Primary.WithAlpha(.87))).
			HitWith(p.ids.sharesAdd, i18n.T("新增分享"), ui.HitOptions{Role: ui.RoleButton, Disabled: p.sharePickerOpen, Expandable: true, Expanded: p.shareMenu.kind == shareMenuAdd})
		addButton.Children(func() {
			f.Icon(Plus).Size(17, 17).Color(theme.PrimaryForeground)
			f.Text(i18n.T("新增分享")).Color(theme.PrimaryForeground).FontSize(labelSize).Weight(ui.WeightSemibold)
			f.Row().Size(1, 17).Background(theme.PrimaryForeground.WithAlpha(.32))
			f.Icon(ChevronDown).Size(14, 14).Color(theme.PrimaryForeground)
		})
		if addButton.Clicked() && !p.sharePickerOpen {
			p.toggleShareMenu(shareMenuAdd, "")
		}
		if p.shareMenu.kind == shareMenuAdd {
			p.shareAnchor = addButton
		}
	})
}

func (p *Panels) drawShareTableHead(f *flex.Frame, theme ui.Theme, columns shareColumns) {
	f.Row().FillWidth().Height(59).PadX(15).Gap(12).Items(flex.Center).Children(func() {
		f.Text(i18n.T("名称")).Muted().Grow(1).FontSize(13)
		if columns.kind > 0 {
			f.Text(i18n.T("类型")).Muted().Width(columns.kind).FontSize(13)
		}
		f.Text(i18n.T("访问方式")).Muted().Width(columns.access).FontSize(13)
		if columns.date > 0 {
			f.Text(i18n.T("分享时间")).Muted().Width(columns.date).FontSize(13)
		}
		f.Row().Width(92)
	})
}

func panelActionIcon(f *flex.Frame, theme ui.Theme, id ui.ID, icon ui.Icon, label string, active, disabled bool) flex.Box {
	box := f.Row().Size(36, 32).Radius(7).Items(flex.Center).Justify(flex.Center).
		Hover(flex.Look{}.Background(theme.Hover)).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Hint: label, Disabled: disabled, Expanded: active})
	if active || f.Canvas().FocusVisible() == id {
		box.Background(panelAccentSurface(theme)).Stroke(1, theme.Ring)
	}
	if f.Canvas().Hovered() == id && !disabled {
		box.Stroke(1, theme.Border)
	}
	color := theme.Foreground
	if disabled {
		color = theme.MutedForeground.WithAlpha(.48)
	}
	return box.Children(func() { f.Icon(icon).Size(18, 18).Color(color) })
}

func (p *Panels) drawShareProjectRow(f *flex.Frame, theme ui.Theme, columns shareColumns, entry panelShareEntry, submit func(domain.Command)) {
	selected := p.shareSelected == entry.file.ID
	menuOpen := p.shareMenu.kind == shareMenuProject && p.shareMenu.project == entry.file.ID
	canvas := f.Canvas()
	height := float32(72)
	if columns.kind > 0 {
		height = 80
	}
	row := f.Row().FillWidth().Height(height).PadX(15).Gap(12).Items(flex.Center).Radius(8).
		Hover(flex.Look{}.Background(theme.Hover)).
		HitWith(entry.rowID, entry.label, ui.HitOptions{Role: ui.RoleButton, Selected: selected})
	if selected || canvas.FocusVisible() == entry.rowID {
		row.Background(panelAccentSurface(theme)).Stroke(1, panelAccentText(theme))
	}
	row.Children(func() {
		f.Row().MinWidth(0).Grow(1).Gap(13).Items(flex.Center).Children(func() {
			icon, ink, fill := shareProjectIcon(theme, entry)
			f.Row().Size(36, 36).Radius(8).Background(fill).Items(flex.Center).Justify(flex.Center).Children(func() {
				f.Icon(icon).Size(23, 23).Color(ink)
			})
			f.Column().MinWidth(0).Grow(1).Gap(4).Children(func() {
				f.Row().MinWidth(0).Gap(7).Items(flex.Center).Children(func() {
					f.Text(entry.title).FontSize(16).Weight(ui.WeightSemibold).MinWidth(0).Ellipsis()
					if entry.file.ShareProtected && columns.kind > 0 {
						panelPill(f, theme, i18n.T("密码保护"), panelAccentText(theme))
					}
				})
				meta := entry.meta
				if columns.date == 0 {
					meta = entry.compactMeta
				}
				f.Text(meta).Muted().FontSize(12).Ellipsis()
			})
		})
		if columns.kind > 0 {
			f.Text(entry.kind).Width(columns.kind).FontSize(13)
		}
		f.Row().Width(columns.access).Gap(7).Items(flex.Center).Children(func() {
			icon, color := Users, theme.MutedForeground
			if entry.file.ShareProtected {
				icon, color = Lock, theme.Foreground
			} else if entry.file.ShareURL != "" {
				icon, color = Link, theme.Success
			}
			f.Icon(icon).Size(16, 16).Color(color)
			f.Text(entry.access).FontSize(13).MinWidth(0).Ellipsis()
		})
		if columns.date > 0 {
			f.Text(entry.date).Width(columns.date).Muted().FontSize(13).Ellipsis()
		}
		f.Row().Width(92).Gap(7).Items(flex.Center).Justify(flex.End).Children(func() {
			if entry.file.ShareURL != "" {
				if panelActionIcon(f, theme, entry.copyID, Copy, i18n.T("复制链接"), false, false).Clicked() {
					p.copyShareLink(canvas, entry.file.ShareURL)
				}
			} else if !entry.own {
				if panelActionIcon(f, theme, entry.copyID, Download, i18n.T("获取文件"), false, false).Clicked() {
					submit(domain.Command{Kind: domain.GetShared, FileID: entry.file.ID})
				}
			}
			more := panelActionIcon(f, theme, entry.moreID, MoreHorizontal, i18n.T("更多分享操作"), menuOpen, false)
			if more.Clicked() {
				p.shareSelected = entry.file.ID
				p.toggleShareMenu(shareMenuProject, entry.file.ID)
			}
			if p.shareMenu.kind == shareMenuProject && p.shareMenu.project == entry.file.ID && !p.shareMenu.pointer {
				p.shareAnchor = more
			}
		})
	})
	if row.Clicked() {
		p.shareSelected = entry.file.ID
	}
	if e, ok := canvas.PointerOn(entry.rowID); ok && e.Kind == ui.PointerDown && e.Button == ui.PointerButtonSecondary {
		p.shareSelected = entry.file.ID
		p.shareMenu = shareMenuState{kind: shareMenuProject, project: entry.file.ID, pointer: true, anchor: ui.Rect{X: e.X, Y: e.Y}}
		canvas.Consume()
		canvas.RequestFrame()
	}
	f.Divider().FillWidth()
}

func shareProjectIcon(theme ui.Theme, entry panelShareEntry) (ui.Icon, ui.Color, ui.Color) {
	if entry.file.ShareFolder {
		ink := ui.RGB(181, 121, 16)
		if panelDark(theme) {
			ink = ui.RGB(244, 189, 78)
		}
		return Folder, ink, ink.WithAlpha(.14)
	}
	switch entry.iconKind {
	case "image":
		ink := ui.RGB(104, 94, 224)
		if panelDark(theme) {
			ink = ui.RGB(164, 153, 255)
		}
		return Image, ink, ink.WithAlpha(.13)
	case "sheet":
		return FileText, theme.Success, theme.Success.WithAlpha(.13)
	}
	return FileText, panelAccentText(theme), panelAccentSurface(theme)
}

func (p *Panels) toggleShareMenu(kind shareMenuKind, project string) {
	if p.shareMenu.kind == kind && p.shareMenu.project == project {
		p.shareMenu = shareMenuState{}
		return
	}
	p.shareMenu = shareMenuState{kind: kind, project: project}
}

type shareMenuDecl struct {
	item      components.ContextMenuItem
	action    func()
	separator bool
}

func (p *Panels) drawShareMenu(c *ui.Canvas, theme ui.Theme, viewport ui.Rect, device domain.DeviceShare, submit func(domain.Command)) {
	if p.shareMenu.kind == shareMenuClosed || p.shareModalOpen() {
		return
	}
	var actions [12]shareMenuDecl
	items := actions[:0]
	add := func(id ui.ID, label string, icon ui.Icon, action func()) {
		items = append(items, shareMenuDecl{item: components.ContextMenuItem{ID: id, Label: i18n.T(label), Icon: icon}, action: action})
	}
	addDanger := func(id ui.ID, label string, icon ui.Icon, action func()) {
		items = append(items, shareMenuDecl{separator: true})
		add(id, label, icon, action)
		items[len(items)-1].item.Variant = components.ContextMenuItemDestructive
	}
	var title string
	width := float32(206)
	switch p.shareMenu.kind {
	case shareMenuAdd:
		add(p.ids.sharePickFiles, "选择文件…", File, func() { p.pickShare(c, false) })
		add(p.ids.sharePickFolder, "选择文件夹…", Folder, func() { p.pickShare(c, true) })
	case shareMenuDevice:
		width = 228
		if device.URL == "" {
			add(p.ids.deviceShareStart, "启用设备主页", shareHomeIcon, func() { submit(domain.Command{Kind: domain.SetDeviceShare, Bool: true}) })
		} else {
			add(p.ids.deviceShareCopy, "复制设备主页", Copy, func() { p.copyShareLink(c, device.URL) })
			add(p.ids.deviceShareQR, "主页二维码", shareQRIcon, func() {
				p.openShareQR(domain.File{ID: "device-home", Name: i18n.T("设备分享主页"), ShareURL: device.URL, ShareQR: device.QR})
			})
			add(p.ids.deviceShareInbox, "接收目录", FolderOpen, func() { p.requestLocalAction(device.ReceiveDir, true, true) })
			addDanger(p.ids.deviceShareStop, "关闭主页", X, func() { submit(domain.Command{Kind: domain.SetDeviceShare}) })
		}
	case shareMenuProject:
		entry, ok := p.shareEntry(p.shareMenu.project)
		if !ok {
			p.shareMenu = shareMenuState{}
			return
		}
		width, title = 246, entry.title
		file := entry.file
		if file.ShareURL != "" {
			add(p.ids.shareMenuCopy, "复制项目链接", Link, func() { p.copyShareLink(c, file.ShareURL) })
		}
		if len(file.ShareQR) != 0 {
			add(p.ids.shareMenuQR, "查看二维码", shareQRIcon, func() { p.openShareQR(file) })
		}
		if entry.own {
			if file.ShareURL == "" {
				add(p.ids.shareMenuEnableLink, "生成项目链接", Link, func() { submit(domain.Command{Kind: domain.SetSharePassword, FileID: file.ID}) })
			}
			add(p.ids.shareMenuPassword, sharePasswordLabel(true, file.ShareProtected), Lock, func() { p.openSharePassword(file) })
			if file.ShareProtected {
				add(p.ids.shareMenuClearPass, "清除访问密码", Lock, func() { submit(domain.Command{Kind: domain.SetSharePassword, FileID: file.ID}) })
			}
		} else {
			add(p.ids.shareMenuGet, "获取文件", Download, func() { submit(domain.Command{Kind: domain.GetShared, FileID: file.ID}) })
		}
		if entry.path != "" && (entry.available || file.ShareFolder) {
			if !file.ShareFolder {
				add(p.ids.shareMenuOpen, "打开本地项目", FolderOpen, func() { p.requestLocalAction(entry.path, false, false) })
			}
			add(p.ids.shareMenuFolder, "在文件夹中显示", ExternalLink, func() { p.requestLocalAction(entry.path, true, file.ShareFolder) })
		}
		if entry.own {
			addDanger(p.ids.shareMenuUnshare, "取消分享", Trash2, func() { submit(domain.Command{Kind: domain.RemoveShare, FileID: file.ID}) })
		}
	}
	if len(items) == 0 {
		return
	}
	height := float32(12)
	if title != "" {
		height += 33
	}
	for _, item := range items {
		if item.separator {
			height += 9
		} else {
			height += 32
		}
	}
	anchor := p.shareMenu.anchor
	if !p.shareMenu.pointer {
		anchor = p.shareAnchor.Rect()
		if anchor.Width <= 0 {
			p.shareMenu = shareMenuState{}
			return
		}
	}
	placement := components.OverlayPlacement{Side: components.OverlayBottom, Align: components.OverlayAlignEnd, SideOffset: 5, Margin: 8}
	if p.shareMenu.pointer {
		placement.Align = components.OverlayAlignStart
	}
	bounds, _ := components.PlaceOverlay(anchor, ui.Size{Width: width, Height: height}, viewport, placement)
	menu := components.BeginContextMenu(c, theme, bounds, components.ContextMenuOptions{
		Layer: 64, Scope: p.ids.shareMenuScope, DismissID: p.ids.shareMenuDismiss, Viewport: viewport,
	})
	if title != "" {
		menu.AddLabel(title, false)
		menu.AddSeparator()
	}
	for _, item := range items {
		if item.separator {
			menu.AddSeparator()
		} else {
			menu.AddItem(item.item)
		}
	}
	menu.End()
	for _, item := range items {
		if !item.separator && c.Clicked(item.item.ID) {
			p.shareMenu = shareMenuState{}
			item.action()
			c.RequestFrame()
			return
		}
	}
	if c.Clicked(p.ids.shareMenuDismiss) {
		p.shareMenu = shareMenuState{}
		c.RequestFrame()
	}
}

func (p *Panels) shareEntry(id string) (panelShareEntry, bool) {
	for _, entry := range p.sharesAll {
		if entry.file.ID == id {
			return entry, true
		}
	}
	return panelShareEntry{}, false
}

func (p *Panels) pickShare(c *ui.Canvas, folder bool) {
	if p.sharePickerOpen {
		return
	}
	p.sharePickerOpen = true
	c.AfterPaint(func() {
		defer func() { p.sharePickerOpen = false; c.RequestFrame() }()
		var paths []string
		var err error
		if folder {
			var path string
			path, err = app.OpenDirectory(app.OpenDirectoryOptions{Title: i18n.T("选择要分享的文件夹")})
			if path != "" {
				paths = []string{path}
			}
		} else {
			paths, err = app.OpenFiles(app.OpenFileOptions{Title: i18n.T("选择要分享的文件"), AllowMultiple: true})
		}
		if errors.Is(err, app.ErrCanceled) || p.module != panelModuleShares {
			return
		}
		if err != nil {
			p.setShareToast("无法打开系统选择器", err.Error(), components.ToastError)
			return
		}
		p.openShareDialogFor(paths, folder)
	})
}

func (p *Panels) copyShareLink(c *ui.Canvas, link string) {
	if link == "" {
		p.setShareToast("此分享暂无可用链接", "", components.ToastError)
		c.RequestFrame()
		return
	}
	c.AfterPaint(func() {
		if err := app.SetClipboardText(link); err != nil {
			p.setShareToast("复制失败", err.Error(), components.ToastError)
		} else {
			p.setShareToast("分享链接已复制", "", components.ToastSuccess)
		}
		c.RequestFrame()
	})
}

func (p *Panels) setShareToast(title, description string, kind components.ToastType) {
	p.shareToast = shareToastState{fresh: true, message: components.ToastSnapshot{
		Title: i18n.T(title), Description: i18n.T(description), Type: kind,
	}}
}

func (p *Panels) drawShareToast(c *ui.Canvas, theme ui.Theme, bounds ui.Rect, event domain.ShareFeedback) {
	if event.Sequence > p.lastShareFeedback {
		p.lastShareFeedback = event.Sequence
		kind := components.ToastSuccess
		if event.Failed {
			kind = components.ToastError
		}
		p.setShareToast(event.Title, event.Detail, kind)
	}
	if p.shareToast.message.Title == "" {
		return
	}
	now := c.Now()
	if p.shareToast.fresh {
		p.shareToast.fresh = false
		p.shareToast.deadline = now + components.ToastQueueTimeout
	}
	if now >= p.shareToast.deadline || c.Clicked(p.ids.shareToastDismiss) {
		p.shareToast = shareToastState{}
		return
	}
	components.PaintSonner(c, theme, bounds, []components.SonnerToast{{
		Snapshot: p.shareToast.message, Scope: p.ids.shareToastScope, DismissID: p.ids.shareToastDismiss,
	}}, components.SonnerOptions{Layer: 62, Position: components.ToastBottomRight, Limit: 1})
	c.RequestFrameIn(p.shareToast.deadline - now)
}

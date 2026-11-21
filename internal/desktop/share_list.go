package desktop

import (
	"errors"
	"github.com/vivaui/viva/gfx"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
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
	size, access, date, actions float32
}

var shareHomeIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 10), gfx.LineTo(12, 3), gfx.LineTo(21, 10),
	gfx.MoveTo(5, 9), gfx.LineTo(5, 21), gfx.LineTo(19, 21), gfx.LineTo(19, 9),
	gfx.MoveTo(9, 21), gfx.LineTo(9, 14), gfx.LineTo(15, 14), gfx.LineTo(15, 21),
}}

var shareQRIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 3), gfx.LineTo(9, 3), gfx.LineTo(9, 9), gfx.LineTo(3, 9), gfx.ClosePath(),
	gfx.MoveTo(15, 3), gfx.LineTo(21, 3), gfx.LineTo(21, 9), gfx.LineTo(15, 9), gfx.ClosePath(),
	gfx.MoveTo(3, 15), gfx.LineTo(9, 15), gfx.LineTo(9, 21), gfx.LineTo(3, 21), gfx.ClosePath(),
	gfx.MoveTo(15, 15), gfx.LineTo(18, 15), gfx.LineTo(18, 18), gfx.LineTo(21, 18), gfx.LineTo(21, 21),
	gfx.MoveTo(15, 21), gfx.LineTo(15, 20), gfx.MoveTo(21, 14), gfx.LineTo(21, 15),
}}

func measureShareColumns(width float32) shareColumns {
	if width >= 1020 {
		return shareColumns{width * .16, width * .143, width * .17, 142}
	}
	if width >= 740 {
		return shareColumns{0, 105, 130, 140}
	}
	return shareColumns{0, 88, 0, 135}
}

func (p *Panels) drawShares(f ui.C, theme ui.Theme, bounds ui.Rect, state domain.State, submit func(domain.Command)) {
	if p.memberProject != nil {
		if entry, ok := p.shareEntry(p.memberProject.id); ok && entry.online && entry.file.MemberURL == p.memberProject.address {
			p.drawMemberProject(f, theme, bounds, entry.file, submit)
			return
		}
		p.closeMemberProject()
	}
	pad := panelPageInset(bounds.Width)
	columns := measureShareColumns(bounds.Width - 2*pad)
	ui.Column(f).Fill().PadSides(panelPageTop, pad, 18, pad).Children(func() {
		p.drawShareToolbar(f, theme, state.DeviceShare, bounds.Width)
		p.drawTableSearch(f, theme, p.ids.shareSearch, &p.shareSearch, i18n.T("搜索文件"))
		ui.Row(f).FillWidth().PadSides(18, 0, 0, 0).Items(ui.Center).Children(func() {
			if ui.Tabs(f.Part("share-filter"), &p.shareFilter, shareFilters[:],
				func(filter shareFilter) string { return p.shareLabels[filter] }).Line().
				FontSize(panelTabFontSize).Changed() {
				p.shareSelected = ""
				clear(p.shareChecked)
				p.shareMenu = shareMenuState{}
				if p.shareFilter == panelSharesRemote {
					submit(domain.Command{Kind: domain.RefreshShares})
				}
			}
			ui.Spacer(f)
			if p.shareFilter == panelSharesRemote &&
				panelActionIcon(f, theme, p.ids.sharesRefresh, RefreshCw, i18n.T("刷新"), false, false).Clicked() {
				submit(domain.Command{Kind: domain.RefreshShares})
			}
		})
		ui.Divider(f).FillWidth()
		entries := p.sharesMine
		if p.shareFilter == panelSharesRemote {
			entries = p.sharesRemote
		}
		entries = panelFilterShares(entries, strings.TrimSpace(p.shareSearch.Text()))
		if p.shareFilter == panelSharesRemote && len(entries) > 0 {
			if p.shareChecked == nil {
				p.shareChecked = make(map[string]bool)
			}
			count, eligible := 0, 0
			for _, entry := range entries {
				if entry.canDownload() {
					eligible++
					if p.shareChecked[entry.file.ID] {
						count++
					}
				}
			}
			ui.Row(f.Part("member-downloads")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
				if ui.Button(f.Part("select-projects"), shareSelectAllLabel(eligible > 0 && count == eligible)).Ghost().Disabled(eligible == 0).Clicked() {
					for _, entry := range entries {
						if entry.canDownload() {
							if count == eligible {
								delete(p.shareChecked, entry.file.ID)
							} else {
								p.shareChecked[entry.file.ID] = true
							}
						}
					}
				}
				if ui.Button(f.Part("download-projects"), downloadSelectedLabel(count)).Outline().Disabled(count == 0).Clicked() {
					var selected []domain.File
					for _, entry := range entries {
						if entry.canDownload() && p.shareChecked[entry.file.ID] {
							selected = append(selected, entry.file)
						}
					}
					p.downloadProjects(selected, submit)
				}
			})
		}
		if len(entries) > 0 {
			p.drawShareTableHead(f, theme, columns)
		}
		ui.Scroll(f.KeyInt(int(p.shareFilter))).FillWidth().Grow(1).Children(func() {
			ui.Column(f).FillWidth().Children(func() {
				if len(entries) == 0 {
					message := i18n.T("点击「新增分享」选择文件或文件夹。")
					if p.shareFilter == panelSharesRemote {
						message = i18n.T("成员分享的文件和文件夹会显示在这里。")
					}
					ui.Column(f).FillWidth().PadSides(25, 0, 0, 0).Children(func() {
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

func (p *Panels) drawShareToolbar(f ui.C, theme ui.Theme, device domain.DeviceShare, width float32) {
	height, labelSize := float32(45), float32(15)
	if width < 760 {
		height, labelSize = 39, 13
	}
	ui.Row(f).FillWidth().Gap(12).Items(ui.Start).Children(func() {
		drawPanelPageHeading(f, width, i18n.T("文件共享"), i18n.T("查看和管理你创建的分享，以及成员与你共享的文件。"))
		label := i18n.T("设备分享主页")
		if width < 760 {
			label = i18n.T("设备主页")
		}
		deviceButton := ui.Row(f).Height(height).Gap(9).PadX(14).Items(ui.Center).Radius(8).
			Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
			HitWith(p.ids.deviceShareMenu, label, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: p.shareMenu.kind == shareMenuDevice})
		deviceButton.Children(func() {
			ui.Icon(f, shareHomeIcon).Size(18, 18).Color(theme.MutedForeground)
			ui.Text(f, label).FontSize(labelSize).Weight(gfx.WeightMedium)
			if device.URL != "" {
				ui.Row(f).Size(6, 6).Radius(3).Background(theme.Success)
				if width >= 760 {
					ui.Text(f, i18n.T("已启用")).Muted().FontSize(12)
				}
			}
			ui.Icon(f, ChevronDown).Size(14, 14).Color(theme.MutedForeground)
		})
		if deviceButton.Clicked() {
			p.toggleShareMenu(shareMenuDevice, "")
		}
		if p.shareMenu.kind == shareMenuDevice {
			p.shareAnchor = deviceButton
		}
		addButton := ui.Row(f).Height(height).Gap(9).PadX(14).Items(ui.Center).Radius(8).
			Background(theme.Primary).Hover(ui.Look{}.Background(theme.Primary.WithAlpha(.87))).
			HitWith(p.ids.sharesAdd, i18n.T("新增分享"), gfx.HitOptions{Role: gfx.RoleButton, Disabled: p.sharePickerOpen, Expandable: true, Expanded: p.shareMenu.kind == shareMenuAdd})
		addButton.Children(func() {
			ui.Icon(f, Plus).Size(17, 17).Color(theme.PrimaryForeground)
			ui.Text(f, i18n.T("新增分享")).Color(theme.PrimaryForeground).FontSize(labelSize).Weight(gfx.WeightSemibold)
			ui.Row(f).Size(1, 17).Background(theme.PrimaryForeground.WithAlpha(.32))
			ui.Icon(f, ChevronDown).Size(14, 14).Color(theme.PrimaryForeground)
		})
		if addButton.Clicked() && !p.sharePickerOpen {
			p.toggleShareMenu(shareMenuAdd, "")
		}
		if p.shareMenu.kind == shareMenuAdd {
			p.shareAnchor = addButton
		}
	})
}

func (p *Panels) drawShareTableHead(f ui.C, theme ui.Theme, columns shareColumns) {
	ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
		ui.Text(f, i18n.T("名称")).Muted().Grow(1).FontSize(13)
		ui.Text(f, i18n.T("大小 · 项目数")).Muted().Width(columns.size).FontSize(13)
		ui.Text(f, i18n.T("访问方式")).Muted().Width(columns.access).FontSize(13)
		if columns.date > 0 {
			ui.Text(f, i18n.T("分享时间")).Muted().Width(columns.date).FontSize(13)
		}
		ui.Row(f).Width(columns.actions)
	})
}

func panelFilterShares(entries []panelShareEntry, query string) []panelShareEntry {
	if query == "" {
		return entries
	}
	filtered := make([]panelShareEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.title), strings.ToLower(query)) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func panelActionIcon(f ui.C, theme ui.Theme, id gfx.ID, icon gfx.Icon, label string, active, disabled bool) ui.Box {
	box := ui.Row(f).Size(36, 32).Radius(radiusControl).Items(ui.Center).Justify(ui.Center).
		Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: disabled, Expanded: active})
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
	return box.Children(func() { ui.Icon(f, icon).Size(18, 18).Color(color) })
}

func (p *Panels) drawShareProjectRow(f ui.C, theme ui.Theme, columns shareColumns, entry panelShareEntry, submit func(domain.Command)) {
	selected := p.shareSelected == entry.file.ID
	menuOpen := p.shareMenu.kind == shareMenuProject && p.shareMenu.project == entry.file.ID
	canvas := f.Canvas()
	row := ui.Row(f).FillWidth().Height(panelTableRowHeight).PadX(15).Gap(12).Items(ui.Center).Radius(radiusControl).
		Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(entry.rowID, entry.title, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected, Hint: entry.title}).
		Tooltip(entry.title)
	if selected || canvas.FocusVisible() == entry.rowID {
		row.Background(panelAccentSurface(theme)).Stroke(1, panelAccentText(theme))
	}
	row.Children(func() {
		ui.Row(f).MinWidth(0).Grow(1).Gap(13).Items(ui.Center).Children(func() {
			if !entry.own && selectionCheckbox(f.Key(entry.file.ID), p.shareChecked[entry.file.ID], entry.title, !entry.canDownload()) {
				toggleShareSelection(p.shareChecked, entry.file.ID)
			}
			drawFileIcon(f, theme, entry.icon, 32, 23)
			ui.Text(f, entry.title).FontSize(15).Weight(gfx.WeightMedium).MinWidth(0).Ellipsis()
		})
		ui.Text(f, entry.meta).Width(columns.size).FontSize(13).Ellipsis()
		ui.Row(f).Width(columns.access).Gap(7).Items(ui.Center).Children(func() {
			icon, color := Users, theme.MutedForeground
			if entry.file.ShareUnavailable {
				icon, color = X, theme.Danger
			} else if entry.file.ShareProtected {
				icon, color = Lock, theme.Foreground
			} else if entry.file.ShareURL != "" {
				icon, color = Link, theme.Success
			}
			ui.Icon(f, icon).Size(16, 16).Color(color)
			ui.Text(f, entry.access).FontSize(13).MinWidth(0).Ellipsis()
		})
		if columns.date > 0 {
			ui.Text(f, entry.date).Width(columns.date).Muted().FontSize(13).Ellipsis()
		}
		ui.Row(f).Width(columns.actions).Gap(5).Items(ui.Center).Justify(ui.End).Children(func() {
			if entry.memberWebProject() {
				disabled := !entry.online || entry.file.MemberURL == ""
				if panelActionIcon(f, theme, entry.copyID, FolderOpen, i18n.T("打开共享"), false, disabled).Clicked() && !disabled {
					p.openMemberProject(entry.file)
				}
				if panelActionIcon(f, theme, entry.downloadID, Download, i18n.T("下载"), false, disabled).Clicked() && !disabled {
					p.downloadProjects([]domain.File{entry.file}, submit)
				}
			} else if entry.own && entry.file.ShareUnavailable {
				if ui.Button(f.Key(entry.file.ID).Part("replace"), i18n.T("修改共享源")).Small().Outline().Clicked() {
					p.toggleShareMenu(shareMenuProject, entry.file.ID)
				}
			} else if entry.own && entry.file.ShareURL != "" {
				if panelActionIcon(f, theme, entry.copyID, Copy, i18n.T("复制链接"), false, false).Clicked() {
					p.copyShareLink(canvas, entry.file.ShareURL)
				}
			} else if !entry.own {
				if panelActionIcon(f, theme, entry.copyID, Download, i18n.T("获取文件"), false, false).Clicked() {
					p.downloadProjects([]domain.File{entry.file}, submit)
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
		if entry.memberWebProject() && entry.online && entry.file.MemberURL != "" {
			p.openMemberProject(entry.file)
		}
	}
	if e, ok := canvas.PointerOn(entry.rowID); ok && e.Kind == gfx.PointerDown && e.Button == gfx.PointerButtonSecondary {
		p.shareSelected = entry.file.ID
		p.shareMenu = shareMenuState{kind: shareMenuProject, project: entry.file.ID, pointer: true, anchor: ui.Rect{X: e.X, Y: e.Y}}
		canvas.Consume()
		canvas.RequestFrame()
	}
	ui.Divider(f).FillWidth()
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

func (p *Panels) drawShareMenu(c *gfx.Canvas, theme ui.Theme, viewport ui.Rect, device domain.DeviceShare, submit func(domain.Command)) {
	if p.shareMenu.kind == shareMenuClosed || p.shareModalOpen() {
		return
	}
	var actions [12]shareMenuDecl
	items := actions[:0]
	add := func(id gfx.ID, label string, icon gfx.Icon, action func()) {
		items = append(items, shareMenuDecl{item: components.ContextMenuItem{ID: id, Label: i18n.T(label), Icon: icon}, action: action})
	}
	addDanger := func(id gfx.ID, label string, icon gfx.Icon, action func()) {
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
		if entry.own && file.ShareURL != "" {
			add(p.ids.shareMenuCopy, "复制项目链接", Link, func() { p.copyShareLink(c, file.ShareURL) })
		}
		if entry.own && len(file.ShareQR) != 0 {
			add(p.ids.shareMenuQR, "查看二维码", shareQRIcon, func() { p.openShareQR(file) })
		}
		if entry.own && file.ShareUnavailable {
			add(p.ids.shareMenuReplaceFile, "重新选择文件…", File, func() { p.chooseShareSource(c, false, file.ID, submit) })
			add(p.ids.shareMenuReplaceFolder, "重新选择文件夹…", Folder, func() { p.chooseShareSource(c, true, file.ID, submit) })
		} else if entry.own {
			if file.ShareURL == "" {
				add(p.ids.shareMenuEnableLink, "生成项目链接", Link, func() { submit(domain.Command{Kind: domain.SetSharePassword, FileID: file.ID}) })
			}
			add(p.ids.shareMenuPassword, sharePasswordLabel(true, file.ShareProtected), Lock, func() { p.openSharePassword(file) })
			if file.ShareProtected {
				add(p.ids.shareMenuClearPass, "清除访问密码", Lock, func() { submit(domain.Command{Kind: domain.SetSharePassword, FileID: file.ID}) })
			}
		} else if entry.memberWebProject() {
			add(p.ids.shareMenuVisit, "打开共享", FolderOpen, func() { p.openMemberProject(file) })
			items[len(items)-1].item.Disabled = !entry.online || file.MemberURL == ""
			add(p.ids.shareMenuGet, "下载共享项目", Download, func() { p.downloadProjects([]domain.File{file}, submit) })
			items[len(items)-1].item.Disabled = !entry.online || file.MemberURL == ""
		} else {
			add(p.ids.shareMenuGet, "获取文件", Download, func() { p.downloadProjects([]domain.File{file}, submit) })
		}
		if !file.ShareUnavailable && entry.path != "" && (entry.available || file.ShareFolder) {
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
		if !item.separator && !item.item.Disabled && c.Clicked(item.item.ID) {
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

func (p *Panels) pickShare(c *gfx.Canvas, folder bool) {
	p.chooseShareSource(c, folder, "", nil)
}
func (p *Panels) chooseShareSource(c *gfx.Canvas, folder bool, replace string, submit func(domain.Command)) {
	if p.sharePickerOpen {
		return
	}
	p.sharePickerOpen = true
	c.AfterPaint(func() {
		defer func() { p.sharePickerOpen = false; c.RequestFrame() }()
		title := i18n.T("选择要分享的文件")
		if folder {
			title = i18n.T("选择要分享的文件夹")
		}
		if replace != "" {
			title = i18n.T("更换共享源（保留原链接和访问密码）")
		}
		var paths []string
		var err error
		if folder {
			var path string
			path, err = app.OpenDirectory(app.OpenDirectoryOptions{Title: title})
			if path != "" {
				paths = []string{path}
			}
		} else {
			paths, err = app.OpenFiles(app.OpenFileOptions{Title: title, AllowMultiple: replace == ""})
		}
		if errors.Is(err, app.ErrCanceled) || p.module != panelModuleShares {
			return
		}
		if err != nil {
			p.setShareToast("无法打开系统选择器", filePickerProblem(err), components.ToastError)
			return
		}
		if replace != "" {
			if len(paths) == 1 {
				submit(domain.Command{Kind: domain.ReplaceShareSource, FileID: replace, Paths: paths})
			}
			return
		}
		p.openShareDialogFor(paths, folder)
	})
}

func (entry panelShareEntry) memberWebProject() bool {
	return !entry.own && (entry.file.ShareFolder || entry.file.ShareProtected)
}

func (p *Panels) copyShareLink(c *gfx.Canvas, link string) {
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

func (p *Panels) drawShareToast(c *gfx.Canvas, theme ui.Theme, bounds ui.Rect, event domain.ShareFeedback) {
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

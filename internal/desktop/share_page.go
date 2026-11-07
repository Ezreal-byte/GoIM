package desktop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type shareMenuTarget uint8

const (
	shareMenuLocal shareMenuTarget = iota
	shareMenuShared
)

type shareMenuState struct {
	open   bool
	target shareMenuTarget
	x      float32
	y      float32
	local  shareDirEntry
	share  panelShareEntry
}

type shareSendDialogState struct {
	open     bool
	paths    []string
	password flex.Field
}

type sharePasswordDialogState struct {
	open      bool
	fileID    string
	protected bool
	field     flex.Field
}

type shareQRDialogState struct {
	open    bool
	fileID  string
	name    string
	url     string
	key     shareQRKey
	hasKey  bool
	bitmap  ui.Bitmap
	loading bool
	err     string
}

func (p *Panels) closeShareInteractions() {
	p.shareMenu = shareMenuState{}
	p.shareDialog = shareSendDialogState{}
	p.sharePassword = sharePasswordDialogState{}
	p.shareQR = shareQRDialogState{}
}

func (p *Panels) shareModalOpen() bool {
	return p.shareDialog.open || p.sharePassword.open || p.shareQR.open
}

func (p *Panels) ensureShareBrowserStarted() {
	if p.shareBrowserStarted {
		return
	}
	p.shareBrowserStarted = true
	p.startShareRequest(shareBrowseRequest{kind: shareBrowseHome}, "主目录")
}

func (p *Panels) startShareRequest(request shareBrowseRequest, title string) {
	p.shareLastRequest = request
	p.sharePendingGen = p.shareBrowser.start(request)
	p.shareLoading = true
	p.shareLoadError = ""
	p.shareTruncated = false
	p.shareEntries = p.shareEntries[:0]
	p.shareLocationTitle = title
	p.shareParent = ""
	if request.kind == shareBrowsePath {
		p.shareDir = request.path
	} else {
		p.shareDir = ""
	}
	p.clearShareSelection()
	p.shareClickAdditive = false
	p.shareClickTarget = ui.ID{}
	p.localNotice = ""
}

func (p *Panels) applyShareBrowserEvents() {
	for _, event := range p.shareBrowser.take() {
		if event.dir != nil {
			result := event.dir
			if result.generation != p.sharePendingGen {
				continue
			}
			p.shareLoading = false
			p.shareEntries = append(p.shareEntries[:0], result.entries...)
			p.shareDir = result.path
			p.shareParent = result.parent
			if result.title != "" {
				p.shareLocationTitle = result.title
			}
			p.shareLoadError = result.err
			p.shareTruncated = result.truncated
			if result.err != "" {
				p.shareEntries = p.shareEntries[:0]
			}
			p.trimShareSelection()
		}
		if event.qr != nil {
			result := *event.qr
			if p.shareQRCache == nil {
				p.shareQRCache = make(map[shareQRKey]shareQRResult)
			}
			if len(p.shareQRCache) >= 64 {
				for key := range p.shareQRCache {
					if key != result.key {
						delete(p.shareQRCache, key)
						break
					}
				}
			}
			p.shareQRCache[result.key] = result
			if p.shareQR.open && p.shareQR.hasKey && p.shareQR.key == result.key {
				p.shareQR.loading = false
				p.shareQR.bitmap = result.bitmap
				if !result.ok {
					p.shareQR.err = "二维码图片无法读取"
				} else {
					p.shareQR.err = ""
				}
			}
		}
	}
}

func (p *Panels) trimShareSelection() {
	if len(p.shareSelection) == 0 {
		return
	}
	present := make(map[string]bool, len(p.shareEntries))
	for _, entry := range p.shareEntries {
		if !entry.dir {
			present[sharePathKey(entry.path)] = true
		}
	}
	for key := range p.shareSelection {
		if !present[key] {
			delete(p.shareSelection, key)
		}
	}
}

func (p *Panels) drawShares(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	p.ensureShareBrowserStarted()
	f.Column().Fill().PadSides(12, 18, 12, 18).Gap(10).Children(func() {
		p.drawShareToolbar(f, theme)
		p.panelShareFeedback(f, theme, state, submit)

		f.Row().FillWidth().Grow(1).Gap(12).Items(flex.Stretch).Children(func() {
			p.drawLocalPane(f, theme)
			f.Divider().FillHeight()
			p.drawSharedPane(f, theme, submit)
		})
		p.drawShareDialogs(f, theme, submit)
	})
}

func (p *Panels) drawShareToolbar(f *flex.Frame, theme ui.Theme) {
	selected := p.shareSelectionCount()
	f.Row().FillWidth().Items(flex.Center).Gap(10).Children(func() {
		f.Row().Gap(7).Items(flex.Center).Children(func() {
			f.Icon(panelIconFolder).Size(18, 18).Color(panelAccentText(theme))
			f.Text("文件").FontSize(17).Weight(ui.WeightSemibold)
			f.Text("本机与共享").Muted().FontSize(theme.Metrics.FontSizeS)
		})
		f.Spacer()
		if selected > 0 {
			f.Text(fmt.Sprintf("已选 %d 个文件", selected)).Muted().
				FontSize(theme.Metrics.FontSizeS)
		}
		if f.Button(p.ids.sharesAdd, "分享所选").Small().Disabled(selected == 0).Clicked() &&
			selected > 0 {
			p.openShareSendDialog()
		}
	})
}

func (p *Panels) panelShareFeedback(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	if p.localError != "" {
		panelBannerWithAction(f, theme, p.localError, true, p.ids.sharesDismiss, func() {
			p.localError = ""
		})
	}
	if state.Error != "" {
		panelBannerWithAction(f, theme, state.Error, true, p.ids.sharesClearError, func() {
			submit(domain.Command{Kind: domain.ClearError})
		})
	}
	if strings.HasPrefix(state.Notice, "分享") {
		panelBanner(f, theme, state.Notice, false, nil)
	}
	if p.localNotice != "" {
		panelBannerWithAction(f, theme, p.localNotice, false, p.ids.sharesNoticeOff, func() {
			p.localNotice = ""
		})
	}
}

func (p *Panels) drawLocalPane(f *flex.Frame, theme ui.Theme) {
	f.Column().Grow(3).Basis(0).MinWidth(300).FillHeight().Background(theme.Card).
		Border(1, theme.Border).Radius(10).Clip().Children(func() {
		f.Column().Fill().PadSides(10, 12, 8, 12).Gap(7).Children(func() {
			f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
				f.Text("本机文件").FontSize(13).Weight(ui.WeightSemibold)
				f.Spacer()
				if p.shareLoading {
					f.Text("正在读取…").Muted().FontSize(theme.Metrics.FontSizeS)
				} else if count := len(p.shareEntries); count > 0 {
					f.Text(fmt.Sprintf("%d 项", count)).Muted().FontSize(theme.Metrics.FontSizeS)
				}
			})
			location := p.shareDir
			if location == "" {
				location = p.shareLocationTitle
			}
			f.Text(location).FontSize(theme.Metrics.FontSizeS).Muted().FillWidth().Ellipsis()

			f.Row().FillWidth().Items(flex.Center).Gap(5).Children(func() {
				up := f.Button(p.ids.shareUp, "上一级").XSmall().Ghost().
					Disabled(p.shareParent == "" || p.shareLoading)
				if up.Clicked() && p.shareParent != "" && !p.shareLoading {
					p.openShareDirectory(p.shareParent)
				}
				path := f.Field(&p.shareAddress, p.ids.sharePathField, "本机文件夹路径", "输入文件夹路径").Grow(1).Height(29)
				if f.Button(p.ids.sharePathGo, "前往").XSmall().Ghost().Clicked() || path.Submitted() {
					request := strings.Trim(strings.TrimSpace(p.shareAddress.Text()), `"`)
					p.shareAddress.SetText("")
					if !filepath.IsAbs(request) {
						p.localError = "请输入完整的本机文件夹路径"
					} else {
						p.localError = ""
						p.openShareDirectory(request)
					}
				}
				if f.Button(p.ids.shareLocalReload, "刷新").XSmall().Ghost().Clicked() {
					p.reloadShareDirectory()
				}
			})

			f.Row().FillWidth().Items(flex.Center).Gap(2).Children(func() {
				if f.Button(p.ids.shareHome, "主目录").XSmall().Ghost().Clicked() {
					p.startShareRequest(shareBrowseRequest{kind: shareBrowseHome}, "主目录")
				}
				if f.Button(p.ids.shareDownloads, "下载").XSmall().Ghost().Clicked() {
					p.startShareRequest(shareBrowseRequest{
						kind: shareBrowseDownloads, path: p.shareDownloadDir,
					}, "下载")
				}
				if f.Button(p.ids.shareDrives, "驱动器").XSmall().Ghost().Clicked() {
					p.startShareRequest(shareBrowseRequest{kind: shareBrowseDrives}, "此电脑")
				}
				if f.Button(p.ids.shareRoot, "根目录").XSmall().Ghost().Clicked() {
					base := p.shareDir
					p.startShareRequest(shareBrowseRequest{kind: shareBrowseRoot, path: base}, "根目录")
				}
				f.Spacer()
			})
			f.Divider().FillWidth()

			f.ScrollArea(p.ids.shareLocalScroll).Grow(1).FillWidth().Children(func() {
				f.Column().FillWidth().PadSides(1, 0, 2, 0).Gap(1).Children(func() {
					switch {
					case p.shareLoading && len(p.shareEntries) == 0:
						f.Column().FillWidth().PadY(38).Gap(8).Items(flex.Center).Children(func() {
							f.Spinner().Size(18, 18)
							f.Text("正在读取文件夹").Muted().FontSize(theme.Metrics.FontSizeS)
						})
					case p.shareLoadError != "":
						panelEmpty(f, theme, "无法打开此文件夹", p.shareLoadError)
						if f.Button(p.ids.shareLocalRetry, "重试").Small().Outline().Clicked() {
							p.reloadShareDirectory()
						}
					case len(p.shareEntries) == 0:
						panelEmpty(f, theme, "空文件夹", "这里没有可分享的文件或文件夹。")
					default:
						entries := p.shareEntries
						for index := range entries {
							p.drawShareLocalRow(f, theme, index, entries[index])
						}
						if p.shareTruncated {
							f.Text(fmt.Sprintf("仅显示前 %d 项", maxShareDirectoryEntries)).
								Muted().FontSize(theme.Metrics.FontSizeS).PadY(8)
						}
					}
				})
			})
			f.Text("右键文件可分享、复制链接或管理访问密码").Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
}

func (p *Panels) drawShareLocalRow(f *flex.Frame, theme ui.Theme, index int, entry shareDirEntry) {
	id := p.ids.shareLocalRows.At(index)
	key := sharePathKey(entry.path)
	selected := !entry.dir && p.shareSelection[key]
	row := f.Row().FillWidth().Height(40).PadSides(0, 7, 0, 7).Gap(8).Items(flex.Center).Radius(7).
		Hit(id, panelShareLocalLabel(entry))
	if selected {
		row.Background(panelAccentSurface(theme))
	} else {
		row.Hover(flex.Look{}.Background(theme.Hover))
	}
	row.Children(func() {
		icon := panelIconFile
		if entry.dir {
			icon = panelIconFolder
		}
		f.Icon(icon).Size(15, 15).Color(panelAccentText(theme))
		f.Column().Grow(1).Gap(1).Children(func() {
			f.Text(entry.name).FontSize(theme.Metrics.FontSizeS + 1.5).
				Weight(ui.WeightMedium).Ellipsis()
			f.Text(shareLocalMeta(entry)).Muted().FontSize(theme.Metrics.FontSizeS - 1).
				Ellipsis()
		})
		if selected {
			f.Icon(panelIconCheck).Size(14, 14).Color(panelAccentText(theme))
		}
	})

	canvas := f.Canvas()
	if event, ok := canvas.PointerOn(id); ok && event.Kind == ui.PointerDown {
		switch event.Button {
		case ui.PointerButtonSecondary:
			p.shareClickAdditive = false
			p.shareClickTarget = ui.ID{}
			p.openShareLocalMenu(entry, event.X, event.Y)
			canvas.Consume()
			return
		case ui.PointerButtonPrimary:
			p.shareClickAdditive = event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0
			p.shareClickTarget = id
		}
	}
	if !row.Clicked() {
		return
	}
	additive := p.shareClickAdditive && p.shareClickTarget == id
	p.shareClickAdditive = false
	p.shareClickTarget = ui.ID{}
	if entry.dir {
		p.openShareDirectory(entry.path)
		return
	}
	if additive {
		if selected {
			delete(p.shareSelection, key)
		} else {
			p.shareSelection[key] = true
		}
		return
	}
	p.clearShareSelection()
	p.shareSelection[key] = true
}

func panelShareLocalLabel(entry shareDirEntry) string {
	if entry.dir {
		return "打开文件夹 " + entry.name
	}
	return "选择文件 " + entry.name
}

func shareLocalMeta(entry shareDirEntry) string {
	if entry.dir {
		return "文件夹"
	}
	meta := panelFormatBytes(entry.size)
	if !entry.modified.IsZero() {
		meta += " · " + entry.modified.Format("2006-01-02 15:04")
	}
	return meta
}

func (p *Panels) drawSharedPane(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	if p.shareFilter != panelSharesRemote {
		p.shareFilter = panelSharesMine
	}
	entries := p.sharesMine
	if p.shareFilter == panelSharesRemote {
		entries = p.sharesRemote
	}
	f.Column().Grow(2).Basis(0).MinWidth(300).FillHeight().Background(theme.Card).
		Border(1, theme.Border).Radius(10).Clip().Children(func() {
		f.Column().Fill().PadSides(10, 12, 8, 12).Gap(8).Children(func() {
			f.Row().FillWidth().Items(flex.Center).Gap(8).Children(func() {
				f.Text("共享").FontSize(13).Weight(ui.WeightSemibold)
				f.Text(fmt.Sprintf("%d 项", len(entries))).Muted().
					FontSize(theme.Metrics.FontSizeS)
				f.Spacer()
				if f.Button(p.ids.sharesRefresh, "刷新").XSmall().Ghost().Clicked() {
					submit(domain.Command{Kind: domain.RefreshShares})
				}
			})
			if len(p.shareLabels) >= 2 {
				choice := f.Choice(p.ids.sharesFilters, p.shareLabels,
					min(max(p.shareFilter, 0), 1)).FillWidth()
				if index, ok := choice.ChosenIndex(); ok {
					p.shareFilter = index
				}
			}
			f.Divider().FillWidth()
			f.ScrollArea(p.ids.sharesScroll).Grow(1).FillWidth().Children(func() {
				f.Column().FillWidth().PadSides(1, 0, 2, 0).Children(func() {
					if len(entries) == 0 {
						empty := "分享本机文件后，可在这里管理链接、二维码和访问密码。"
						if p.shareFilter == panelSharesRemote {
							empty = "其他成员分享的只读文件会显示在这里。"
						}
						panelEmpty(f, theme, "暂无共享文件", empty)
						return
					}
					for index := range entries {
						p.drawSharedRow(f, theme, index, entries[index], submit)
					}
				})
			})
			f.Text("成员共享为只读；右键可复制链接或查看二维码").Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
}

func (p *Panels) drawSharedRow(f *flex.Frame, theme ui.Theme, index int, entry panelShareEntry, submit func(domain.Command)) {
	rowID := p.ids.shareRow.At(index)
	main := f.Row().FillWidth().PadY(8).Gap(8).Items(flex.Center).
		Hit(rowID, "共享文件 "+panelShareName(entry.file))
	main.Hover(flex.Look{}.Background(theme.Hover)).Children(func() {
		f.Row().Width(32).Height(32).Radius(8).Background(theme.Muted).
			Items(flex.Center).Justify(flex.Center).Children(func() {
			f.Icon(panelIconFile).Size(16, 16).Color(panelAccentText(theme))
		})
		f.Column().Grow(1).Gap(2).Children(func() {
			f.Text(panelShareName(entry.file)).FontSize(theme.Metrics.FontSizeS + 1.5).
				Weight(ui.WeightMedium).Ellipsis()
			f.Text(entry.meta).Muted().FontSize(theme.Metrics.FontSizeS - 1).Ellipsis()
			f.Text(entry.state).FontSize(theme.Metrics.FontSizeS - 1).
				Color(panelStatusColor(theme, entry.state))
		})
		switch {
		case entry.own && entry.file.ShareProtected:
			panelPill(f, theme, "密码保护", panelAccentText(theme))
		case entry.own:
			panelPill(f, theme, "我分享的", panelAccentText(theme))
		default:
			panelPill(f, theme, "只读", theme.MutedForeground)
		}
	})
	f.Row().FillWidth().Gap(6).Items(flex.Center).Children(func() {
		if entry.own && entry.file.ShareURL != "" {
			if f.Button(p.ids.shareCopyLink.At(index), "复制链接").XSmall().Outline().Clicked() {
				p.copyShareLink(f.Canvas(), entry.file.ShareURL)
			}
			if f.Button(p.ids.shareShowQR.At(index), "二维码").XSmall().Ghost().Clicked() {
				p.openShareQR(entry.file)
			}
			f.Spacer()
			more := f.Button(p.ids.shareMore.At(index), "更多").XSmall().Ghost()
			if more.Clicked() {
				p.shareMoreAnchor, p.shareMoreEntry = more, entry
			}
			return
		}
		if entry.path != "" {
			open := f.Button(p.ids.shareOpen.At(index), "打开").XSmall().Outline().
				Disabled(!entry.available)
			if open.Clicked() && entry.available {
				panelOpenLocal(f.Canvas(), p, entry.path, false)
			}
			reveal := f.Button(p.ids.shareReveal.At(index), "打开文件夹").XSmall().Outline().
				Disabled(!entry.available)
			if reveal.Clicked() && entry.available {
				panelOpenLocal(f.Canvas(), p, entry.path, true)
			}
		}
		f.Spacer()
		if entry.own {
			if f.Button(p.ids.shareRemove.At(index), "移除分享").XSmall().
				DestructiveOutline().Clicked() {
				submit(domain.Command{Kind: domain.RemoveShare, FileID: entry.file.ID})
			}
		} else {
			if f.Button(p.ids.shareGet.At(index), "获取").XSmall().Clicked() {
				submit(domain.Command{Kind: domain.GetShared, FileID: entry.file.ID})
			}
		}
	})
	if event, ok := f.Canvas().PointerOn(rowID); ok && event.Kind == ui.PointerDown &&
		event.Button == ui.PointerButtonSecondary {
		p.openShareSharedMenu(entry, event.X, event.Y)
		f.Canvas().Consume()
	}
	f.Divider().FillWidth()
}

func panelShareName(file domain.File) string {
	if strings.TrimSpace(file.Name) != "" {
		return file.Name
	}
	return "未命名文件"
}

func (p *Panels) shareSelectionCount() int {
	count := 0
	for _, entry := range p.shareEntries {
		if !entry.dir && p.shareSelection[sharePathKey(entry.path)] {
			count++
		}
	}
	return count
}

func (p *Panels) selectedSharePaths() []string {
	paths := make([]string, 0, p.shareSelectionCount())
	for _, entry := range p.shareEntries {
		if !entry.dir && p.shareSelection[sharePathKey(entry.path)] {
			paths = append(paths, entry.path)
		}
	}
	return paths
}

func (p *Panels) clearShareSelection() {
	clear(p.shareSelection)
}

func (p *Panels) openShareDirectory(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	p.startShareRequest(shareBrowseRequest{kind: shareBrowsePath, path: path}, "")
}

func (p *Panels) reloadShareDirectory() {
	request := p.shareLastRequest
	if request.kind == 0 && request.path == "" {
		request = shareBrowseRequest{kind: shareBrowseHome}
	}
	p.startShareRequest(request, p.shareLocationTitle)
}

func (p *Panels) openShareLocalMenu(entry shareDirEntry, x, y float32) {
	p.shareMenu = shareMenuState{open: true, target: shareMenuLocal, x: x, y: y, local: entry}
	if entry.dir {
		p.clearShareSelection()
		return
	}
	key := sharePathKey(entry.path)
	if !p.shareSelection[key] {
		p.clearShareSelection()
		p.shareSelection[key] = true
	}
}

func (p *Panels) openShareSharedMenu(entry panelShareEntry, x, y float32) {
	p.shareMenu = shareMenuState{open: true, target: shareMenuShared, x: x, y: y, share: entry}
}

type shareMenuDecl struct {
	item      components.ContextMenuItem
	separator bool
	action    func()
}

func (p *Panels) drawShareContextMenu(canvas *ui.Canvas, theme ui.Theme, viewport ui.Rect, submit func(domain.Command)) {
	if !p.shareMenu.open || p.shareModalOpen() {
		return
	}
	decls := p.shareMenuItems(canvas, submit)
	if len(decls) == 0 {
		p.shareMenu = shareMenuState{}
		return
	}
	items, separators := 0, 0
	for _, decl := range decls {
		if decl.separator {
			separators++
		} else {
			items++
		}
	}
	height := float32(12 + items*32 + separators*9)
	menu := components.BeginContextMenu(canvas, theme,
		ui.Rect{X: p.shareMenu.x, Y: p.shareMenu.y, Width: 240, Height: height},
		components.ContextMenuOptions{
			Layer: 64, Scope: p.ids.shareMenuScope, DismissID: p.ids.shareMenuDismiss,
			Viewport: viewport,
		})
	for _, decl := range decls {
		if decl.separator {
			menu.AddSeparator()
			continue
		}
		menu.AddItem(decl.item)
	}
	menu.End()

	for _, decl := range decls {
		if decl.separator || decl.item.ID.IsZero() || !canvas.Clicked(decl.item.ID) {
			continue
		}
		p.shareMenu = shareMenuState{}
		if decl.action != nil {
			decl.action()
		}
		return
	}
	if canvas.Clicked(p.ids.shareMenuDismiss) {
		p.shareMenu = shareMenuState{}
	}
}

func (p *Panels) shareMenuItems(canvas *ui.Canvas, submit func(domain.Command)) []shareMenuDecl {
	var decls []shareMenuDecl
	separator := func() {
		if len(decls) > 0 && !decls[len(decls)-1].separator {
			decls = append(decls, shareMenuDecl{separator: true})
		}
	}

	switch p.shareMenu.target {
	case shareMenuLocal:
		entry := p.shareMenu.local
		shared, hasShared := p.shareByPath[sharePathKey(entry.path)]
		if entry.dir {
			decls = append(decls, shareMenuDecl{
				item: components.ContextMenuItem{ID: p.ids.shareMenuFolder, Label: "打开此文件夹"},
				action: func() {
					p.openShareDirectory(entry.path)
				},
			})
			return decls
		}
		decls = append(decls,
			shareMenuDecl{
				item: components.ContextMenuItem{ID: p.ids.shareMenuOpen, Label: "打开"},
				action: func() {
					panelOpenLocal(canvas, p, entry.path, false)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{ID: p.ids.shareMenuFolder, Label: "在文件夹中显示"},
				action: func() {
					panelOpenLocal(canvas, p, entry.path, true)
				},
			},
		)
		separator()
		count := p.shareSelectionCount()
		shareLabel := "分享此文件…"
		if count > 1 {
			shareLabel = fmt.Sprintf("分享所选 %d 个文件…", count)
		}
		decls = append(decls,
			shareMenuDecl{
				item: components.ContextMenuItem{ID: p.ids.shareMenuShare, Label: shareLabel, Disabled: count == 0},
				action: func() {
					p.openShareSendDialog()
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuCopy, Label: "复制分享链接",
					Disabled: !hasShared || count != 1 || shared.file.ShareURL == "",
				},
				action: func() {
					p.copyShareLink(canvas, shared.file.ShareURL)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuQR, Label: "显示二维码",
					Disabled: !hasShared || count != 1 || len(shared.file.ShareQR) == 0,
				},
				action: func() {
					p.openShareQR(shared.file)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuPassword, Label: sharePasswordLabel(hasShared, shared.file.ShareProtected),
					Disabled: !hasShared || count != 1,
				},
				action: func() {
					p.openSharePassword(shared.file)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuClearPass, Label: "清除访问密码",
					Disabled: !hasShared || count != 1 || !shared.file.ShareProtected,
				},
				action: func() {
					submit(domain.Command{Kind: domain.SetSharePassword, FileID: shared.file.ID})
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuUnshare, Label: "取消分享",
					Variant:  components.ContextMenuItemDestructive,
					Disabled: !hasShared || count != 1,
				},
				action: func() {
					submit(domain.Command{Kind: domain.RemoveShare, FileID: shared.file.ID})
				},
			},
		)
	case shareMenuShared:
		entry := p.shareMenu.share
		if entry.path != "" {
			decls = append(decls,
				shareMenuDecl{
					item: components.ContextMenuItem{
						ID: p.ids.shareMenuOpen, Label: "打开", Disabled: !entry.available,
					},
					action: func() {
						if entry.available {
							panelOpenLocal(canvas, p, entry.path, false)
						}
					},
				},
				shareMenuDecl{
					item: components.ContextMenuItem{
						ID: p.ids.shareMenuFolder, Label: "在文件夹中显示", Disabled: !entry.available,
					},
					action: func() {
						if entry.available {
							panelOpenLocal(canvas, p, entry.path, true)
						}
					},
				},
			)
		}
		if !entry.own {
			decls = append(decls, shareMenuDecl{
				item: components.ContextMenuItem{ID: p.ids.shareMenuGet, Label: "获取文件"},
				action: func() {
					submit(domain.Command{Kind: domain.GetShared, FileID: entry.file.ID})
				},
			})
		}
		separator()
		decls = append(decls,
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuCopy, Label: "复制分享链接",
					Disabled: entry.file.ShareURL == "",
				},
				action: func() {
					p.copyShareLink(canvas, entry.file.ShareURL)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuQR, Label: "显示二维码",
					Disabled: len(entry.file.ShareQR) == 0,
				},
				action: func() {
					p.openShareQR(entry.file)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuPassword, Label: sharePasswordLabel(entry.own, entry.file.ShareProtected),
					Disabled: !entry.own,
				},
				action: func() {
					p.openSharePassword(entry.file)
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuClearPass, Label: "清除访问密码",
					Disabled: !entry.own || !entry.file.ShareProtected,
				},
				action: func() {
					submit(domain.Command{Kind: domain.SetSharePassword, FileID: entry.file.ID})
				},
			},
			shareMenuDecl{
				item: components.ContextMenuItem{
					ID: p.ids.shareMenuUnshare, Label: "取消分享",
					Variant:  components.ContextMenuItemDestructive,
					Disabled: !entry.own,
				},
				action: func() {
					submit(domain.Command{Kind: domain.RemoveShare, FileID: entry.file.ID})
				},
			},
		)
	}
	return decls
}

func sharePasswordLabel(shared, protected bool) string {
	if !shared {
		return "设置访问密码…"
	}
	if protected {
		return "更改访问密码…"
	}
	return "设置访问密码…"
}

func (p *Panels) copyShareLink(canvas *ui.Canvas, link string) {
	if strings.TrimSpace(link) == "" {
		p.localError = "此分享暂无可用链接"
		return
	}
	canvas.AfterPaint(func() {
		if err := app.SetClipboardText(link); err != nil {
			p.localError = "复制失败：" + err.Error()
			p.localNotice = ""
			return
		}
		p.localError = ""
		p.localNotice = "分享链接已复制"
	})
}

func (p *Panels) openShareSendDialog() {
	paths := p.selectedSharePaths()
	if len(paths) == 0 {
		return
	}
	p.closeShareInteractions()
	p.shareDialog.open = true
	p.shareDialog.paths = paths
	p.shareDialog.password.Secure = true
	p.shareDialog.password.SetText("")
}

func (p *Panels) closeShareSendDialog() {
	p.shareDialog.open = false
	p.shareDialog.paths = nil
	p.shareDialog.password.SetText("")
}

func (p *Panels) submitShareSendDialog(submit func(domain.Command)) {
	if !p.shareDialog.open || len(p.shareDialog.paths) == 0 {
		return
	}
	paths := append([]string(nil), p.shareDialog.paths...)
	password := p.shareDialog.password.Text()
	p.closeShareSendDialog()
	p.clearShareSelection()
	p.localError = ""
	submit(domain.Command{Kind: domain.ShareFiles, Paths: paths, Text: password})
}

func (p *Panels) openSharePassword(file domain.File) {
	if strings.TrimSpace(file.ID) == "" {
		return
	}
	p.closeShareInteractions()
	p.sharePassword.open = true
	p.sharePassword.fileID = file.ID
	p.sharePassword.protected = file.ShareProtected
	p.sharePassword.field.Secure = true
	p.sharePassword.field.SetText("")
}

func (p *Panels) closeSharePassword() {
	p.sharePassword = sharePasswordDialogState{}
}

func (p *Panels) submitSharePassword(submit func(domain.Command)) {
	if !p.sharePassword.open || p.sharePassword.fileID == "" {
		return
	}
	fileID := p.sharePassword.fileID
	password := p.sharePassword.field.Text()
	p.closeSharePassword()
	p.localError = ""
	submit(domain.Command{Kind: domain.SetSharePassword, FileID: fileID, Text: password})
}

func (p *Panels) openShareQR(file domain.File) {
	p.closeShareInteractions()
	p.shareQR = shareQRDialogState{
		open: true, fileID: file.ID, name: panelShareName(file), url: file.ShareURL,
	}
	key, ok := shareQRKeyFor(file)
	if !ok {
		p.shareQR.err = "此分享未提供二维码"
		return
	}
	p.shareQR.key = key
	p.shareQR.hasKey = true
	if cached, exists := p.shareQRCache[key]; exists {
		p.shareQR.bitmap = cached.bitmap
		if !cached.ok {
			p.shareQR.err = "二维码图片无法读取"
		}
		return
	}
	p.shareQR.loading = true
	p.shareBrowser.startQR(key, append([]byte(nil), file.ShareQR...))
}

func (p *Panels) closeShareQR() {
	p.shareQR = shareQRDialogState{}
}

func (p *Panels) drawShareDialogs(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	if p.shareDialog.open {
		p.drawShareSendDialog(f, theme, submit)
	}
	if p.sharePassword.open {
		p.drawSharePasswordDialog(f, theme, submit)
	}
	if p.shareQR.open {
		p.drawShareQRDialog(f, theme)
	}
}

func (p *Panels) drawShareSendDialog(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	count := len(p.shareDialog.paths)
	description := fmt.Sprintf("将通过 GoIM 共享 %d 个文件；访问者使用链接或二维码下载。", count)
	dialog := f.Dialog(flex.DialogSpec{
		Title: "分享所选文件", Description: description, Width: 460,
		Scope: p.ids.shareDialogScope, DismissID: p.ids.shareDialogDismiss,
		CloseID: p.ids.shareDialogClose, InitialFocusID: p.ids.shareDialogPassword,
	})
	var submitted bool
	dialog.Body(func() {
		f.Column().Fill().Gap(8).Children(func() {
			names := make([]string, 0, min(count, 3))
			for _, path := range p.shareDialog.paths {
				if len(names) == 3 {
					break
				}
				names = append(names, filepath.Base(path))
			}
			if count > 3 {
				names = append(names, fmt.Sprintf("等 %d 个文件", count))
			}
			f.Text(strings.Join(names, "、")).FontSize(theme.Metrics.FontSizeS + 1).
				Weight(ui.WeightMedium).Ellipsis()
			field := f.Field(&p.shareDialog.password, p.ids.shareDialogPassword,
				"访问密码（可选）", "留空表示无需密码").FillWidth()
			submitted = field.Submitted()
			f.Text("不设密码时，持有链接的同网用户可下载。").Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
	dialog.Footer(func() {
		if f.Button(p.ids.shareDialogCancel, "取消").Outline().Clicked() {
			p.closeShareSendDialog()
		}
		if f.Button(p.ids.shareDialogSubmit, "创建分享").Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		p.closeShareSendDialog()
		return
	}
	if submitted {
		p.submitShareSendDialog(submit)
	}
}

func (p *Panels) drawSharePasswordDialog(f *flex.Frame, theme ui.Theme, submit func(domain.Command)) {
	title := "设置访问密码"
	if p.sharePassword.protected {
		title = "更改访问密码"
	}
	dialog := f.Dialog(flex.DialogSpec{
		Title: title, Description: "密码只影响这项分享；留空并保存会清除密码。", Width: 440,
		Scope: p.ids.sharePasswordScope, DismissID: p.ids.sharePasswordDismiss,
		CloseID: p.ids.sharePasswordClose, InitialFocusID: p.ids.sharePasswordField,
	})
	var submitted bool
	dialog.Body(func() {
		f.Column().Fill().Gap(8).Children(func() {
			field := f.Field(&p.sharePassword.field, p.ids.sharePasswordField,
				"访问密码", "留空清除密码").FillWidth()
			submitted = field.Submitted()
			f.Text("访问者打开分享链接或扫描二维码后需要输入此密码。").Muted().
				FontSize(theme.Metrics.FontSizeS)
		})
	})
	dialog.Footer(func() {
		if f.Button(p.ids.sharePasswordCancel, "取消").Outline().Clicked() {
			p.closeSharePassword()
		}
		if f.Button(p.ids.sharePasswordSubmit, "保存").Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		p.closeSharePassword()
		return
	}
	if submitted {
		p.submitSharePassword(submit)
	}
}

func (p *Panels) drawShareQRDialog(f *flex.Frame, theme ui.Theme) {
	dialog := f.Dialog(flex.DialogSpec{
		Title: "分享二维码", Description: p.shareQR.name, Width: 380,
		Scope: p.ids.shareQRScope, DismissID: p.ids.shareQRDismiss,
		CloseID: p.ids.shareQRClose,
	})
	dialog.Body(func() {
		f.Column().Fill().Gap(10).Items(flex.Center).Children(func() {
			switch {
			case p.shareQR.err != "":
				f.Column().FillWidth().PadY(28).Items(flex.Center).Children(func() {
					f.Paragraph(p.shareQR.err, 3).Muted().TextCenter().
						FontSize(theme.Metrics.FontSizeS + 1).MaxWidth(280)
				})
			case p.shareQR.loading:
				f.Row().Size(224, 224).Radius(10).Background(theme.Muted).
					Items(flex.Center).Justify(flex.Center).Children(func() {
					f.Spinner().Size(20, 20)
				})
			default:
				f.Row().Size(224, 224).Radius(10).Background(ui.RGB(255, 255, 255)).
					Items(flex.Center).Justify(flex.Center).Children(func() {
					f.Image(p.shareQR.bitmap).Size(208, 208)
				})
			}
			if p.shareQR.url != "" {
				f.Text(p.shareQR.url).Muted().FontSize(theme.Metrics.FontSizeS).
					Ellipsis().MaxWidth(300)
			}
			f.Text("手机与电脑连接同一局域网后，即可扫码下载。").Muted().
				FontSize(theme.Metrics.FontSizeS).TextCenter()
		})
	})
	dialog.Footer(func() {
		if p.shareQR.url != "" {
			if f.Button(p.ids.shareQRCopy, "复制链接").Outline().Clicked() {
				p.copyShareLink(f.Canvas(), p.shareQR.url)
			}
		}
		if f.Button(p.ids.shareQRDismissButton, "关闭").Clicked() {
			p.closeShareQR()
		}
	})
	if dialog.Dismissed() {
		p.closeShareQR()
	}
}

package desktop

import (
	"cmp"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type ownedShareFolder struct {
	projectID string
	title     string
	location  string
	directory domain.ShareFolderEntry
	contents  *domain.ShareFolderContents
	stats     bool
	rows      []ownedShareRow
	sort      panelTableSort
	list      ui.ListState
}

type ownedShareRow struct {
	entry             domain.ShareFolderEntry
	name, size, count string
	icon              *gfx.Icon
}

func (p *Panels) openOwnedShare(entry panelShareEntry, stats bool) {
	v := &ownedShareFolder{projectID: entry.file.ID}
	v.list.Key = func(i int) int { return v.rows[i].entry.ID }
	v.list.Label = func(i int) string { return v.rows[i].name }
	v.update(entry, stats)
	p.ownedShare = v
	p.shareSelected = entry.file.ID
	p.shareMenu = shareMenuState{}
	if p.invalidate != nil {
		p.invalidate()
	}
}

func (p *Panels) backOwnedShare() {
	v := p.ownedShare
	if v.directory.ID == 0 {
		p.ownedShare = nil
	} else {
		var parent domain.ShareFolderEntry
		for item := range v.contents.All() {
			if item.ID == v.directory.Parent {
				parent = item
				break
			}
		}
		v.navigate(parent)
	}
	if p.invalidate != nil {
		p.invalidate()
	}
}

func (v *ownedShareFolder) update(project panelShareEntry, stats bool) {
	if v.contents == project.file.ShareContents && v.stats == stats && v.title == project.title {
		return
	}
	v.contents, v.stats, v.title = project.file.ShareContents, stats, project.title
	if !stats && v.sort.column == "下载次数" {
		v.sort = panelTableSort{}
	}
	if v.directory.ID != 0 {
		// If the open folder disappeared, keep the closest surviving parent.
		var parent domain.ShareFolderEntry
		for entry := range v.contents.All() {
			if !entry.Folder {
				continue
			}
			if entry.Path == v.directory.Path {
				parent = entry
				break
			}
			if len(entry.Path) > len(parent.Path) && strings.HasPrefix(v.directory.Path, entry.Path+"/") {
				parent = entry
			}
		}
		if parent.ID != v.directory.ID {
			v.navigate(parent)
			return
		}
		v.directory = parent
	}
	v.rebuildRows()
}

func (v *ownedShareFolder) navigate(directory domain.ShareFolderEntry) {
	v.directory = directory
	v.list.Select(-1)
	v.list.ScrollTo(0, ui.ListStart)
	v.rebuildRows()
}

func (v *ownedShareFolder) rebuildRows() {
	v.location = v.title
	if v.directory.ID != 0 {
		v.location += " / " + v.directory.Path
	}
	clear(v.rows)
	v.rows = v.rows[:0]
	for entry := range v.contents.All() {
		if entry.Parent != v.directory.ID {
			continue
		}
		name := path.Base(entry.Path)
		row := ownedShareRow{entry: entry, name: name, icon: fileTypeIcon(domain.File{Name: name, Folder: entry.Folder})}
		if entry.Folder {
			row.count = "—"
		} else {
			row.size = panelFormatBytes(entry.Size)
			if v.stats {
				row.count = strconv.FormatUint(entry.Downloads, 10)
			}
		}
		v.rows = append(v.rows, row)
	}
	v.sortRows()
}

func (v *ownedShareFolder) sortRows() {
	slices.SortStableFunc(v.rows, func(a, b ownedShareRow) int {
		if a.entry.Folder != b.entry.Folder {
			if a.entry.Folder {
				return -1
			}
			return 1
		}
		switch v.sort.column {
		case "大小":
			if order := cmp.Compare(a.entry.Size, b.entry.Size); order != 0 {
				return v.sort.order(order)
			}
		case "下载次数":
			if order := cmp.Compare(a.entry.Downloads, b.entry.Downloads); order != 0 {
				return v.sort.order(order)
			}
		case "名称":
			return v.sort.order(cmp.Compare(a.name, b.name))
		}
		return cmp.Compare(a.name, b.name)
	})
}

func shareDownloadColumnWidth(theme ui.Theme) float32 {
	return theme.Metrics.ScaledText(106)
}

func drawShareFolderHeading(f ui.C, id gfx.ID, location string, disabled bool, back func()) {
	if ui.Button(f.Part("back-projects"), i18n.T("返回")).Icon(viewerBackIcon).Small().Outline().Disabled(disabled).Clicked() {
		back()
	}
	ui.Row(f).Width(1).Height(f.Theme().Metrics.ScaledText(18)).Background(f.Theme().Border)
	ui.Text(f, location).MinWidth(0).Grow(1).Ellipsis().Weight(gfx.WeightMedium).
		HitWith(id, location, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).Tooltip(location)
}

func (p *Panels) drawOwnedShare(f ui.C, theme ui.Theme, project panelShareEntry, submit func(domain.Command)) {
	v := p.ownedShare
	canvas := f.Canvas()
	sizeWidth, actionWidth := theme.Metrics.ScaledText(84), theme.Metrics.ScaledText(56)
	var countWidth float32
	if v.stats {
		countWidth = shareDownloadColumnWidth(theme)
	}
	ui.Column(f.Part("owned-folder")).FillWidth().Grow(1).MinHeight(0).PadSides(8, 0, 0, 0).Gap(8).Children(func() {
		ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			drawShareFolderHeading(f, p.ids.shareLocation, v.location, false, p.backOwnedShare)
			if ui.Button(f.Part("reload-directory"), i18n.T("刷新")).Small().Outline().Clicked() {
				submit(domain.Command{Kind: domain.RefreshShares})
			}
		})
		if project.file.ShareUnavailable {
			ui.Text(f, i18n.T("分享不可用")).Muted()
			return
		}
		ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
			v.sort.header(f, "名称", 0, v.sortRows)
			v.sort.header(f, "大小", sizeWidth, v.sortRows)
			if countWidth > 0 {
				v.sort.header(f, "下载次数", countWidth, v.sortRows)
			}
			ui.Row(f).Width(actionWidth)
		})
		if len(v.rows) == 0 {
			ui.Text(f, i18n.T("这个文件夹是空的")).Muted().PadX(15)
			return
		}
		open := func(entry domain.ShareFolderEntry) {
			canvas.AfterPaint(func() {
				if p.ownedShare != v {
					return
				}
				if entry.Folder {
					v.navigate(entry)
					canvas.RequestFrame()
				} else {
					p.requestLocalAction(filepath.Join(project.file.SourcePath, filepath.FromSlash(entry.Path)), false, false)
				}
			})
		}
		ui.List(f, &v.list, len(v.rows), func(row ui.ListRow) {
			item := v.rows[row.Index]
			cell := row.C.KeyInt(row.Key)
			row.Element.Height(max(44, theme.Metrics.ScaledText(36))).Tooltip(item.name).Children(func() {
				ui.Column(cell).Fill().Children(func() {
					ui.Row(cell).FillWidth().Grow(1).PadX(15).Gap(12).Items(ui.Center).Children(func() {
						ui.Row(cell).MinWidth(0).Grow(1).Gap(10).Items(ui.Center).Children(func() {
							drawFileIcon(cell, theme, item.icon, 28, 20)
							ui.Text(cell, item.name).MinWidth(0).Grow(1).Ellipsis()
						})
						ui.Text(cell, item.size).Width(sizeWidth).Muted().FontSize(textSize(cell, 13)).Ellipsis()
						if countWidth > 0 {
							ui.Text(cell, item.count).Width(countWidth).FontSize(textSize(cell, 13)).Ellipsis()
						}
						ui.Row(cell).Width(actionWidth).Justify(ui.End).Children(func() {
							if ui.Button(cell.Part("open"), i18n.T("打开")).Small().Ghost().
								Disabled(!item.entry.Folder && project.file.SourcePath == "").Clicked() {
								open(item.entry)
							}
						})
					})
					ui.Divider(cell).FillWidth()
				})
			})
		}).FillWidth().Grow(1).Label(i18n.T("文件列表"))
		if index, ok := v.list.Submitted(); ok && index >= 0 && index < len(v.rows) {
			if entry := v.rows[index].entry; entry.Folder || project.file.SourcePath != "" {
				open(entry)
			}
		}
	})
}

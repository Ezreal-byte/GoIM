package desktop

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type shareStatsView struct {
	projectID string
	rows      []domain.ShareFileDownloads
	sort      panelTableSort
	list      ui.ListState
}

func (v *shareStatsView) update(files []domain.ShareFileDownloads) {
	v.rows = append(v.rows[:0], files...)
	v.sortRows()
}

func (v *shareStatsView) sortRows() {
	slices.SortStableFunc(v.rows, func(a, b domain.ShareFileDownloads) int {
		if v.sort.column == "下载次数" && a.Count != b.Count {
			return v.sort.order(cmp.Compare(a.Count, b.Count))
		}
		names := cmp.Compare(a.Path, b.Path)
		if v.sort.column == "文件路径" {
			return v.sort.order(names)
		}
		return names
	})
}

func shareDownloadColumnWidth(theme ui.Theme) float32 {
	return theme.Metrics.ScaledText(106)
}

func (p *Panels) drawShareStats(f ui.C, theme ui.Theme, entry panelShareEntry, submit func(domain.Command)) {
	v := &p.shareStats
	downloads := shareDownloadColumnWidth(theme)
	ui.Column(f.Part("share-stats")).FillWidth().Grow(1).MinHeight(0).PadSides(8, 0, 0, 0).Gap(6).Children(func() {
		ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.Button(f.Part("back"), i18n.T("返回")).Icon(viewerBackIcon).Small().Outline().Clicked() {
				p.shareStats = shareStatsView{}
			}
			ui.Text(f, entry.title).Grow(1).MinWidth(0).Ellipsis().Weight(gfx.WeightMedium)
			if ui.Button(f.Part("refresh"), i18n.T("刷新")).Icon(RefreshCw).Small().Outline().Clicked() {
				submit(domain.Command{Kind: domain.RefreshShares})
			}
		})
		if entry.file.ShareUnavailable {
			ui.Text(f, i18n.T("分享不可用")).Muted()
			return
		}
		if len(v.rows) == 0 {
			ui.Text(f, i18n.T("暂无可统计的文件")).Muted()
			return
		}
		ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(12).Gap(10).Items(ui.Center).Children(func() {
			v.sort.header(f, "文件路径", 0, v.sortRows)
			v.sort.header(f, "下载次数", downloads, v.sortRows)
		})
		ui.Divider(f).FillWidth()
		ui.List(f, &v.list, len(v.rows), func(row ui.ListRow) {
			stat := v.rows[row.Index]
			row.Element.Height(max(38, textSize(row.C, 17)+12)).PadX(12).Gap(10).Items(ui.Center).Tooltip(stat.Path).Children(func() {
				ui.Text(row.C, stat.Path).Grow(1).MinWidth(0).Ellipsis()
				ui.Text(row.C, strconv.FormatUint(stat.Count, 10)).Width(downloads).Ellipsis()
			})
		}).FillWidth().Grow(1).Label(i18n.T("共享文件下载统计"))
	})
}

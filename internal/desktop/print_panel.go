package desktop

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type printTab uint8

const (
	printTabLAN printTab = iota
	printTabMine
	printTabRecords
)

var printTabs = [...]printTab{printTabLAN, printTabMine, printTabRecords}

var panelPrintIcon = gfx.Icon{
	ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(6, 8), gfx.LineTo(6, 4), gfx.LineTo(18, 4), gfx.LineTo(18, 8),
		gfx.MoveTo(6, 17), gfx.LineTo(4, 17), gfx.QuadTo(2.8, 17, 2.8, 15.8),
		gfx.LineTo(2.8, 11.2), gfx.QuadTo(2.8, 10, 4, 10), gfx.LineTo(20, 10),
		gfx.QuadTo(21.2, 10, 21.2, 11.2), gfx.LineTo(21.2, 15.8),
		gfx.QuadTo(21.2, 17, 20, 17), gfx.LineTo(18, 17),
		gfx.MoveTo(6, 14), gfx.LineTo(18, 14), gfx.LineTo(18, 21),
		gfx.LineTo(6, 21), gfx.ClosePath(),
		gfx.MoveTo(17, 12.6), gfx.LineTo(17, 13.1),
	},
}

type printPanelIDs struct {
	feature          gfx.Scope
	refresh          gfx.ID
	openShare        gfx.ID
	shareDialogScope gfx.ID
	shareDismiss     gfx.ID
	shareClose       gfx.ID
	shareName        gfx.ID
	shareCancel      gfx.ID
	shareSubmit      gfx.ID
	cards            gfx.Scope
}

type printQueueOption struct {
	id        string
	name      string
	available bool
	status    string
}

type panelPrintCard struct {
	printer       domain.PrinterShare
	ownerName     string
	ownerAvatar   string
	ownerOnline   bool
	available     bool
	own           bool
	capability    string
	printActionID gfx.ID
	unshareID     gfx.ID
}

type panelPrintRecord struct {
	job        domain.PrintJob
	name       string
	direction  string
	source     string
	date       string
	status     string
	statusKind string
	options    string
	error      string
	sent       bool
}

type printPanel struct {
	tree *gfx.Tree
	ids  printPanelIDs

	tab        printTab
	queues     []printQueueOption
	lanCards   []panelPrintCard
	mineCards  []panelPrintCard
	allCards   []panelPrintCard
	records    []panelPrintRecord
	shareOpen  bool
	shareQueue string
	shareName  ui.Field

	shareDefault    string
	shareNameManual bool
}

func (t *printPanel) init(tree *gfx.Tree) {
	if tree == nil || t.tree == tree {
		return
	}
	t.tree = tree
	t.ids.feature = tree.Feature("print-panel")
	t.ids.refresh = tree.ID(t.ids.feature, "refresh")
	t.ids.openShare = tree.ID(t.ids.feature, "share-printer")
	t.ids.shareDialogScope = tree.ID(t.ids.feature, "share-dialog-scope")
	t.ids.shareDismiss = tree.ID(t.ids.feature, "share-dialog-dismiss")
	t.ids.shareClose = tree.ID(t.ids.feature, "share-dialog-close")
	t.ids.shareName = tree.ID(t.ids.feature, "share-dialog-name")
	t.ids.shareCancel = tree.ID(t.ids.feature, "share-dialog-cancel")
	t.ids.shareSubmit = tree.ID(t.ids.feature, "share-dialog-submit")
	t.ids.cards = tree.Sub(t.ids.feature, tree.Name("cards"))
}

func (t *printPanel) resetInteractions() {
	t.closeShareDialog()
}

func (t *printPanel) openShareDialog(state domain.State) {
	t.shareOpen = true
	t.shareNameManual = false
	t.shareDefault = ""
	t.shareQueue = ""
	t.selectDefaultQueue()
	t.applyShareDefault(state)
}

func (t *printPanel) closeShareDialog() {
	t.shareOpen = false
	t.shareQueue = ""
	t.shareDefault = ""
	t.shareNameManual = false
	t.shareName.SetText("")
}

func (t *printPanel) selectDefaultQueue() {
	if len(t.queues) == 0 {
		t.shareQueue = ""
		return
	}
	for _, queue := range t.queues {
		if queue.id == t.shareQueue {
			return
		}
	}
	t.shareQueue = t.queues[0].id
	for _, queue := range t.queues {
		if queue.available {
			t.shareQueue = queue.id
			break
		}
	}
}

func (t *printPanel) queueOption(id string) (printQueueOption, bool) {
	for _, queue := range t.queues {
		if queue.id == id {
			return queue, true
		}
	}
	return printQueueOption{}, false
}

func (t *printPanel) queueLabel(id string) string {
	queue, ok := t.queueOption(id)
	if !ok {
		return id
	}
	if queue.available {
		return queue.name
	}
	return queue.name + " · " + i18n.T("不可用")
}

func (t *printPanel) defaultShareName(state domain.State) string {
	name := strings.TrimSpace(state.Settings.Nickname)
	if name == "" {
		name = strings.TrimSpace(state.Self.Name)
	}
	if name == "" {
		name = i18n.T("我")
	}
	if queue, ok := t.queueOption(t.shareQueue); ok {
		return name + "-" + queue.name
	}
	return name + "-"
}

func (t *printPanel) applyShareDefault(state domain.State) {
	if !t.shareOpen || t.shareNameManual {
		return
	}
	t.shareDefault = t.defaultShareName(state)
	t.shareName.SetText(t.shareDefault)
}

func (t *printPanel) rebuild(state domain.State) {
	t.queues = t.queues[:0]
	for _, printer := range state.LocalPrinters {
		status := strings.TrimSpace(printer.Status)
		t.queues = append(t.queues, printQueueOption{
			id: printer.ID, name: printer.Name, available: printer.Available, status: status,
		})
	}
	sort.SliceStable(t.queues, func(i, j int) bool {
		if t.queues[i].available != t.queues[j].available {
			return t.queues[i].available
		}
		return t.queues[i].name < t.queues[j].name
	})
	if !t.shareOpen {
		t.shareQueue = ""
	} else {
		t.selectDefaultQueue()
	}

	t.allCards = t.allCards[:0]
	t.lanCards = t.lanCards[:0]
	t.mineCards = t.mineCards[:0]
	selfName := strings.TrimSpace(state.Settings.Nickname)
	if selfName == "" {
		selfName = state.Self.Name
	}
	if selfName == "" {
		selfName = i18n.T("我")
	}
	selfAvatar := strings.TrimSpace(state.Self.Avatar)
	if selfAvatar == "" {
		selfAvatar = strings.TrimSpace(state.Settings.Avatar)
	}
	for _, share := range state.Printers {
		own := share.OwnerID == state.Self.ID
		owner := state.Peer(share.OwnerID)
		ownerName := strings.TrimSpace(share.OwnerName)
		if ownerName == "" {
			ownerName = owner.Name
		}
		ownerOnline := own || owner.Online
		if own {
			ownerName = selfName
		}
		if ownerName == "" {
			ownerName = i18n.T("未知联系人")
		}
		card := panelPrintCard{
			printer: share, ownerName: ownerName, ownerAvatar: owner.Avatar,
			ownerOnline: ownerOnline, own: own,
			available:  share.Available && ownerOnline || own && share.Available,
			capability: printCapabilitySummary(share.Capabilities),
		}
		if own {
			card.ownerAvatar = selfAvatar
		}
		scope := t.tree.Sub(t.ids.cards, t.tree.Name(share.ID))
		card.printActionID = t.tree.ID(scope, "print")
		card.unshareID = t.tree.ID(scope, "unshare")
		t.allCards = append(t.allCards, card)
		if own {
			t.mineCards = append(t.mineCards, card)
		} else {
			t.lanCards = append(t.lanCards, card)
		}
	}
	sortPrintCards(t.allCards)
	sortPrintCards(t.lanCards)
	sortPrintCards(t.mineCards)

	t.records = t.records[:0]
	for _, job := range state.PrintJobs {
		sent := job.SenderID == state.Self.ID
		source := job.PrinterName
		if sent {
			source = job.PrinterName
		} else if strings.TrimSpace(job.SenderName) != "" {
			source = job.SenderName
		}
		direction := i18n.T("收到")
		if sent {
			direction = i18n.T("发出")
		}
		label, kind := printStatusPresentation(job.Status)
		record := panelPrintRecord{
			job: job, name: job.FileName, direction: direction, source: source,
			status: label, statusKind: kind, options: printOptionSummary(job.Options),
			error: job.Error, sent: sent,
		}
		record.date = printRecordTime(job.CreatedAt)
		t.records = append(t.records, record)
	}
	sort.SliceStable(t.records, func(i, j int) bool {
		return t.records[i].job.CreatedAt.After(t.records[j].job.CreatedAt)
	})
}

func sortPrintCards(cards []panelPrintCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].available != cards[j].available {
			return cards[i].available
		}
		if cards[i].printer.Name != cards[j].printer.Name {
			return cards[i].printer.Name < cards[j].printer.Name
		}
		return cards[i].ownerName < cards[j].ownerName
	})
}

func printCapabilitySummary(capabilities domain.PrintCapabilities) string {
	parts := make([]string, 0, 3)
	if len(capabilities.Papers) > 0 {
		papers := append([]string(nil), capabilities.Papers...)
		if len(papers) > 2 {
			papers = papers[:2]
		}
		parts = append(parts, strings.Join(papers, " / "))
	}
	duplex := false
	for _, mode := range capabilities.DuplexModes {
		if mode != domain.PrintDuplexSimplex {
			duplex = true
			break
		}
	}
	if duplex {
		parts = append(parts, i18n.T("双面"))
	} else {
		parts = append(parts, i18n.T("单面"))
	}
	for _, mode := range capabilities.ColorModes {
		if mode == domain.PrintColorColor {
			parts = append(parts, i18n.T("彩色"))
			break
		}
	}
	if len(parts) == 0 {
		return i18n.T("A4") + " · " + i18n.T("单面")
	}
	return strings.Join(parts, " · ")
}

func printOptionSummary(options domain.PrintOptions) string {
	parts := []string{options.Paper}
	if options.Copies > 1 {
		parts = append(parts, fmt.Sprintf(i18n.T("%d 份"), options.Copies))
	}
	if options.Duplex != "" && options.Duplex != domain.PrintDuplexSimplex {
		parts = append(parts, i18n.T("双面"))
	}
	if options.PageRange != "" {
		parts = append(parts, i18n.T("页码 ")+options.PageRange)
	}
	return strings.Join(parts, " · ")
}

func printStatusPresentation(status string) (string, string) {
	switch status {
	case domain.PrintStatusSending:
		return i18n.T("发送中"), "active"
	case domain.PrintStatusReceiving:
		return i18n.T("接收中"), "active"
	case domain.PrintStatusSubmitting:
		return i18n.T("提交队列中"), "active"
	case domain.PrintStatusQueued:
		return i18n.T("已提交队列"), "queued"
	case domain.PrintStatusCompleted:
		return i18n.T("已完成"), "completed"
	case domain.PrintStatusFailed:
		return i18n.T("失败"), "failed"
	default:
		return status, "active"
	}
}

func printRecordTime(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	now := time.Now()
	if at.Local().Format("2006-01-02") == now.Format("2006-01-02") {
		return at.Local().Format("15:04")
	}
	if at.Year() == now.Year() {
		return at.Local().Format("01-02 15:04")
	}
	return at.Local().Format("2006-01-02 15:04")
}

func printPanelMinCell(width float32) float32 {
	switch {
	case width >= 1180:
		return 320
	case width >= 760:
		return 290
	default:
		return 240
	}
}

func (t *printPanel) tabLabel(tab printTab) string {
	switch tab {
	case printTabMine:
		return i18n.T("我的共享")
	case printTabRecords:
		return i18n.T("打印记录")
	default:
		return i18n.T("局域网打印机")
	}
}

func (u *UI) paintPrintAvatar(f ui.C, peerID, name, path string, size float32, online bool) {
	_ = peerID
	u.avatarStatus(f, name, path, size, online)
}

func (p *Panels) drawPrint(f ui.C, theme ui.Theme, bounds ui.Rect, state domain.State, submit func(domain.Command)) {
	pad := panelPageInset(bounds.Width)
	ui.Column(f).Fill().PadSides(panelPageTop, pad, 0, pad).Children(func() {
		p.drawPrintToolbar(f, theme, bounds.Width, state, submit)
		ui.Row(f).FillWidth().PadSides(18, 0, 0, 0).Children(func() {
			if ui.Tabs(f.Part("print-filter"), &p.print.tab, printTabs[:], p.print.tabLabel).
				Line().FontSize(panelTabFontSize).Changed() {
				p.print.shareOpen = false
			}
		})
		ui.Divider(f).FillWidth()
		ui.Scroll(f.KeyInt(700 + int(p.print.tab))).FillWidth().Grow(1).Children(func() {
			ui.Column(f).FillWidth().PadY(18).Gap(18).Children(func() {
				p.drawPrintTab(f, theme, bounds.Width, state, submit)
			})
		})
		p.drawPrinterShareDialog(f, theme, state, submit)
	})
}

func (p *Panels) drawPrintToolbar(f ui.C, theme ui.Theme, width float32, state domain.State, submit func(domain.Command)) {
	height, labelSize := float32(45), float32(15)
	if width < 760 {
		height, labelSize = 39, 13
	}
	ui.Row(f).FillWidth().Gap(10).Items(ui.Center).Children(func() {
		drawPanelPageHeading(f, width, i18n.T("打印共享"), i18n.T("选择局域网打印机并发送文件。"))
		refresh := panelActionIcon(f, theme, p.print.ids.refresh, RefreshCw, i18n.T("刷新打印机"), false, state.PrintersLoading)
		if refresh.Clicked() {
			submit(domain.Command{Kind: domain.RefreshPrinters})
		}
		button := ui.Row(f).Height(height).Gap(8).PadX(14).Items(ui.Center).Radius(8).
			Background(theme.Primary).Hover(ui.Look{}.Background(theme.Primary.WithAlpha(.87))).
			HitWith(p.print.ids.openShare, i18n.T("分享打印机"), gfx.HitOptions{Role: gfx.RoleButton})
		button.Children(func() {
			ui.Icon(f, Plus).Size(17, 17).Color(theme.PrimaryForeground)
			ui.Text(f, i18n.T("分享打印机")).Color(theme.PrimaryForeground).
				FontSize(labelSize).Weight(gfx.WeightSemibold)
		})
		if button.Clicked() {
			p.print.openShareDialog(state)
			submit(domain.Command{Kind: domain.RefreshPrinters})
		}
	})
	ui.Row(f).Height(15)
}

func (p *Panels) drawPrintTab(f ui.C, theme ui.Theme, width float32, state domain.State, submit func(domain.Command)) {
	if state.PrintError != "" {
		panelBanner(f, theme, state.PrintError, true, nil)
	}
	switch p.print.tab {
	case printTabMine:
		if len(p.print.mineCards) == 0 {
			panelEmpty(f, theme, i18n.T("尚未共享打印机"),
				i18n.T("点击右上角「分享打印机」，选择本机已安装的打印队列。"))
			return
		}
		p.drawPrintCardGrid(f, theme, width, p.print.mineCards, true, submit)
	case printTabRecords:
		if len(p.print.records) == 0 {
			panelEmpty(f, theme, i18n.T("暂无打印记录"),
				i18n.T("发出的任务和通过 GoIM 收到的任务都会保留在这里。"))
			return
		}
		p.drawPrintRecords(f, theme, width, p.print.records, true)
	default:
		if len(p.print.lanCards) == 0 {
			title, description := i18n.T("暂无共享打印机"), i18n.T("同一局域网内使用新版 GoIM 的成员共享打印机后会显示在这里。")
			if state.PrintersLoading {
				title, description = i18n.T("正在读取打印机"), i18n.T("正在从本机和局域网成员同步打印机信息。")
			}
			panelEmpty(f, theme, title, description)
			return
		}
		p.drawPrintCardGrid(f, theme, width, p.print.lanCards, false, submit)
		if len(p.print.records) > 0 {
			ui.Column(f).FillWidth().Gap(8).Children(func() {
				ui.Text(f, i18n.T("最近打印")).FontSize(22).Weight(gfx.WeightSemibold)
				recent := p.print.records
				if len(recent) > 5 {
					recent = recent[:5]
				}
				p.drawPrintRecords(f, theme, width, recent, false)
			})
		}
	}
}

func (p *Panels) drawPrintCardGrid(f ui.C, theme ui.Theme, width float32, cards []panelPrintCard,
	mine bool, submit func(domain.Command)) {
	ui.Grid(f, printPanelMinCell(width)).FillWidth().Gap(14).Children(func() {
		for _, card := range cards {
			p.drawPrintCard(f, theme, card, mine, submit)
		}
	})
}

func (p *Panels) drawPrintCard(f ui.C, theme ui.Theme, card panelPrintCard,
	mine bool, submit func(domain.Command)) {
	ui.Row(f).Fill().MinHeight(112).PadSides(18, 16, 18, 16).Gap(14).Radius(10).
		Background(theme.Background).Border(1, theme.Border).Children(func() {
		p.drawPrintAvatar(f, card, theme, 54)
		ui.Column(f).Grow(1).MinWidth(0).Gap(5).Justify(ui.Center).Children(func() {
			ui.Text(f, card.printer.Name).FontSize(16).Weight(gfx.WeightSemibold).Ellipsis()
			ui.Text(f, i18n.T("共享人 ")+card.ownerName).Muted().FontSize(12.5).Ellipsis()
			ui.Row(f).Items(ui.Center).Gap(8).Children(func() {
				tone := theme.Success
				label := i18n.T("可用")
				if !card.available {
					tone = theme.MutedForeground
					label = i18n.T("离线")
				}
				ui.Row(f).Size(7, 7).Radius(4).Background(tone)
				ui.Text(f, label).FontSize(12).Color(tone)
				ui.Row(f).Width(1).Height(15).Background(theme.Border)
				ui.Text(f, card.capability).Muted().FontSize(12).Ellipsis()
			})
		})
		ui.Column(f).Gap(8).Items(ui.End).Justify(ui.Center).Children(func() {
			if mine {
				if namedButton(f, card.unshareID, i18n.T("停止共享")).Small().Outline().Clicked() {
					submit(domain.Command{Kind: domain.UnsharePrinter, PrinterID: card.printer.ID})
				}
				return
			}
			disabled := card.own || !card.available
			hint := i18n.T("打印")
			if card.own {
				hint = i18n.T("本机打印机请使用系统打印")
			} else if !card.available {
				hint = i18n.T("共享打印机当前不可用")
			}
			box := ui.Row(f).Height(34).PadSides(13, 11, 13, 11).Gap(7).Radius(7).
				Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
				HitWith(card.printActionID, hint, gfx.HitOptions{
					Role: gfx.RoleButton, Hint: hint, Disabled: disabled,
				})
			color := theme.Foreground
			if disabled {
				color = theme.MutedForeground.WithAlpha(.55)
			}
			box.Children(func() {
				ui.Icon(f, panelPrintIcon).Size(16, 16).Color(color)
				ui.Text(f, i18n.T("打印")).FontSize(13).Weight(gfx.WeightMedium).Color(color)
			})
			if box.Clicked() && !disabled {
				if p.presentPrint == nil || !p.presentPrint(card.printer.ID) {
					if p.toast != nil {
						p.toast(i18n.T("无法打开打印窗口"), i18n.T("请稍后重试。"), components.ToastWarning)
					}
				}
			}
		})
	})
}

func (p *Panels) drawPrintAvatar(f ui.C, card panelPrintCard, theme ui.Theme, size float32) {
	if p.printAvatar != nil {
		p.printAvatar(f, card.printer.OwnerID, card.ownerName, card.ownerAvatar, size, card.ownerOnline)
		return
	}
	ui.Row(f).Size(size, size).Radius(size / 2).Background(theme.Muted).
		Items(ui.Center).Justify(ui.Center).Children(func() {
		ui.Text(f, initialOf(card.ownerName)).FontSize(18).Weight(gfx.WeightSemibold)
	})
}

func (p *Panels) drawPrintRecords(f ui.C, theme ui.Theme, width float32, records []panelPrintRecord, direction bool) {
	compact := width < 760
	directionWidth := float32(88)
	sourceWidth := float32(190)
	timeWidth := float32(92)
	statusWidth := float32(128)
	if compact {
		directionWidth, sourceWidth, timeWidth, statusWidth = 0, 130, 0, 108
	}
	ui.Row(f).FillWidth().Height(40).PadX(14).Gap(12).Items(ui.Center).Children(func() {
		ui.Text(f, i18n.T("文件名称")).Muted().FontSize(13).Grow(1)
		if direction {
			ui.Text(f, i18n.T("类型")).Muted().FontSize(13).Width(directionWidth)
		}
		ui.Text(f, i18n.T("打印机 / 来源")).Muted().FontSize(13).Width(sourceWidth)
		if !compact {
			ui.Text(f, i18n.T("时间")).Muted().FontSize(13).Width(timeWidth)
		}
		ui.Text(f, i18n.T("状态")).Muted().FontSize(13).Width(statusWidth)
	})
	ui.Divider(f).FillWidth()
	for _, record := range records {
		row := ui.Row(f).FillWidth().MinHeight(54).PadX(14).Gap(12).Items(ui.Center).
			Hover(ui.Look{}.Background(theme.Hover))
		row.Children(func() {
			ui.Row(f).Grow(1).MinWidth(0).Gap(10).Items(ui.Center).Children(func() {
				ui.Row(f).Size(30, 30).Radius(7).Background(panelTint(theme.Primary, .10, theme.Background)).
					Items(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(f, printFileIcon(record.job)).Size(17, 17).Color(theme.Primary)
				})
				ui.Column(f).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(f, record.name).FontSize(14).Weight(gfx.WeightMedium).Ellipsis()
					meta := record.options
					if compact {
						meta = record.direction + " · " + record.source
						if record.options != "" {
							meta += " · " + record.options
						}
					}
					if record.error != "" && record.statusKind == "failed" {
						meta += " · " + record.error
					}
					if meta != "" {
						ui.Text(f, meta).Muted().FontSize(11).Ellipsis()
					}
				})
			})
			if direction {
				ui.Text(f, record.direction).FontSize(13).Width(directionWidth).Ellipsis()
			}
			ui.Text(f, record.source).FontSize(13).Width(sourceWidth).Ellipsis()
			if !compact {
				ui.Text(f, record.date).Muted().FontSize(12).Width(timeWidth).Ellipsis()
			}
			ui.Row(f).Width(statusWidth).Gap(7).Items(ui.Center).Children(func() {
				icon, color := printStatusIcon(record.statusKind, theme)
				ui.Icon(f, icon).Size(16, 16).Color(color)
				ui.Text(f, record.status).FontSize(12.5).Color(color).Ellipsis()
			})
		})
		ui.Divider(f).FillWidth()
	}
}

func printStatusIcon(kind string, theme ui.Theme) (gfx.Icon, ui.Color) {
	switch kind {
	case "completed":
		return CircleCheck, theme.Success
	case "failed":
		return CircleAlert, theme.Danger
	case "queued":
		return Clock, theme.Foreground
	default:
		return Clock, theme.MutedForeground
	}
}

func printFileIcon(job domain.PrintJob) gfx.Icon {
	mime := strings.ToLower(job.MIME)
	ext := strings.ToLower(job.FileName)
	switch {
	case strings.Contains(mime, "image"), strings.HasSuffix(ext, ".png"),
		strings.HasSuffix(ext, ".jpg"), strings.HasSuffix(ext, ".jpeg"):
		return Image
	case strings.Contains(mime, "pdf"), strings.HasSuffix(ext, ".pdf"),
		strings.Contains(mime, "text"), strings.HasSuffix(ext, ".txt"):
		return FileText
	default:
		return File
	}
}

func (p *Panels) drawPrinterShareDialog(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	if !p.print.shareOpen {
		return
	}
	previousQueue := p.print.shareQueue
	p.print.selectDefaultQueue()
	if p.print.shareQueue != previousQueue {
		p.print.shareNameManual = false
		p.print.applyShareDefault(state)
	}
	dialog := ui.Dialog(f, ui.DialogSpec{
		Title: i18n.T("分享打印机"), Description: i18n.T("选择本机已安装的打印机，并为局域网成员设置共享名称。"),
		Width: 480, Scope: p.print.ids.shareDialogScope, DismissID: p.print.ids.shareDismiss,
		CloseID: p.print.ids.shareClose, InitialFocusID: p.print.ids.shareName,
	})
	var submitted bool
	queueIDs := make([]string, 0, len(p.print.queues))
	for _, queue := range p.print.queues {
		queueIDs = append(queueIDs, queue.id)
	}
	dialog.Body(func() {
		ui.Column(f).Fill().Gap(9).Children(func() {
			ui.Text(f, i18n.T("本机打印机")).FontSize(12.5).Weight(gfx.WeightMedium)
			if state.PrintersLoading && len(queueIDs) == 0 {
				ui.Row(f).Height(38).Gap(8).Items(ui.Center).Children(func() {
					ui.Spinner(f).Size(16, 16)
					ui.Text(f, i18n.T("正在读取本机打印机…")).Muted().FontSize(12.5)
				})
			} else {
				previous := p.print.shareQueue
				ui.Select(f.Part("print-printer"), &p.print.shareQueue, queueIDs, p.print.queueLabel).
					Placeholder(i18n.T("选择本机打印机")).FillWidth().Disabled(len(queueIDs) == 0)
				if p.print.shareQueue != previous {
					p.print.shareNameManual = false
					p.print.applyShareDefault(state)
				}
			}
			ui.Text(f, i18n.T("共享名称")).FontSize(12.5).Weight(gfx.WeightMedium)
			field := ui.TextFieldID(f, p.print.ids.shareName, &p.print.shareName,
				i18n.T("共享名称"), i18n.T("例如：陈晨-前台 HP")).FillWidth()
			if field.Changed() {
				p.print.shareNameManual = true
			}
			submitted = field.Submitted()
			ui.Text(f, i18n.T("同一账号可共享多台打印机，局域网成员无需密码即可选择并使用。")).
				Muted().FontSize(theme.Metrics.FontSizeS)
			if state.PrintError != "" {
				ui.Text(f, state.PrintError).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger)
			}
		})
	})
	dialog.Footer(func() {
		if dialogActionButton(f, p.print.ids.shareCancel, i18n.T("取消")).Outline().Clicked() {
			p.print.closeShareDialog()
		}
		disabled := p.print.shareQueue == ""
		if dialogActionButton(f, p.print.ids.shareSubmit, i18n.T("分享")).Disabled(disabled).Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		p.print.closeShareDialog()
		return
	}
	if !submitted || p.print.shareQueue == "" {
		return
	}
	queue := p.print.shareQueue
	name := strings.TrimSpace(p.print.shareName.Text())
	if name == "" {
		name = p.print.defaultShareName(state)
	}
	p.print.closeShareDialog()
	submit(domain.Command{Kind: domain.SharePrinter, PrinterID: queue, Name: name})
}

package desktop

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	feature              gfx.Scope
	refresh              gfx.ID
	openShare            gfx.ID
	shareDialogScope     gfx.ID
	shareDismiss         gfx.ID
	shareClose           gfx.ID
	shareName            gfx.ID
	sharePassword        gfx.ID
	shareRequirePassword gfx.ID
	shareCancel          gfx.ID
	shareSubmit          gfx.ID
	cards                gfx.Scope
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
	editID        gfx.ID
	pinID         gfx.ID
	pinned        bool
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

	tab                                printTab
	queues                             []printQueueOption
	lanCards                           []panelPrintCard
	mineCards                          []panelPrintCard
	records                            []panelPrintRecord
	recent                             []panelPrintRecord
	deviceSort, recordSort, recentSort panelTableSort
	shareOpen                          bool
	shareQueue                         string
	shareName                          ui.Field
	sharePassword                      ui.Field
	shareProtected                     bool
	shareWasProtected                  bool
	shareEditing                       bool
	shareError                         string

	shareDefault    string
	shareNameManual bool
}

func (t *printPanel) init(tree *gfx.Tree) {
	if tree == nil || t.tree == tree {
		return
	}
	t.tree = tree
	t.recordSort = panelTableSort{column: "时间", descending: true}
	t.recentSort = t.recordSort
	t.ids.feature = tree.Feature("print-panel")
	t.ids.refresh = tree.ID(t.ids.feature, "refresh")
	t.ids.openShare = tree.ID(t.ids.feature, "share-printer")
	t.ids.shareDialogScope = tree.ID(t.ids.feature, "share-dialog-scope")
	t.ids.shareDismiss = tree.ID(t.ids.feature, "share-dialog-dismiss")
	t.ids.shareClose = tree.ID(t.ids.feature, "share-dialog-close")
	t.ids.shareName = tree.ID(t.ids.feature, "share-dialog-name")
	t.ids.sharePassword = tree.ID(t.ids.feature, "share-dialog-password")
	t.ids.shareRequirePassword = tree.ID(t.ids.feature, "share-dialog-require-password")
	t.sharePassword.Secure = true
	t.ids.shareCancel = tree.ID(t.ids.feature, "share-dialog-cancel")
	t.ids.shareSubmit = tree.ID(t.ids.feature, "share-dialog-submit")
	t.ids.cards = tree.Sub(t.ids.feature, tree.Name("cards"))
}

func (t *printPanel) resetInteractions() {
	t.closeShareDialog()
}

func (t *printPanel) openShareDialog(state domain.State) {
	t.closeShareDialog()
	t.shareOpen = true
	t.shareNameManual = false
	t.shareDefault = ""
	t.shareQueue = ""
	t.selectDefaultQueue()
	t.applyShareDefault(state)
	t.loadExistingShare(state)
}

func (t *printPanel) loadExistingShare(state domain.State) {
	t.sharePassword.SetText("")
	t.shareProtected, t.shareWasProtected = false, false
	for _, share := range state.Printers {
		if share.OwnerID == state.Self.ID && share.DeviceName == t.shareQueue {
			t.shareName.SetText(share.Name)
			t.shareNameManual = true
			t.shareProtected, t.shareWasProtected = share.PasswordRequired, share.PasswordRequired
			return
		}
	}
}

func (t *printPanel) editShareDialog(share domain.PrinterShare) {
	t.closeShareDialog()
	t.shareOpen, t.shareEditing, t.shareNameManual = true, true, true
	t.shareQueue = share.DeviceName
	t.shareName.SetText(share.Name)
	t.shareProtected, t.shareWasProtected = share.PasswordRequired, share.PasswordRequired
}

func (t *printPanel) closeShareDialog() {
	t.shareOpen = false
	t.shareQueue = ""
	t.shareDefault = ""
	t.shareNameManual = false
	t.shareName.SetText("")
	t.sharePassword.SetText("")
	t.shareProtected, t.shareWasProtected, t.shareEditing = false, false, false
	t.shareError = ""
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
	} else if !t.shareEditing {
		t.selectDefaultQueue()
	}

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
		card.editID = t.tree.ID(scope, "edit")
		card.pinID = t.tree.ID(scope, "pin")
		card.pinned = slices.Contains(state.Settings.PinnedPrinters, share.ID)
		if own {
			t.mineCards = append(t.mineCards, card)
		} else {
			t.lanCards = append(t.lanCards, card)
		}
	}
	t.sortDevices()

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
	newest := panelTableSort{column: "时间", descending: true}
	sortPrintRecords(t.records, newest)
	t.recent = append(t.recent[:0], t.records[:min(5, len(t.records))]...)
	if t.recordSort != newest {
		sortPrintRecords(t.records, t.recordSort)
	}
	if t.recentSort != newest {
		sortPrintRecords(t.recent, t.recentSort)
	}
}

func (t *printPanel) sortDevices() {
	sort := t.deviceSort
	compare := func(a, b panelPrintCard) int {
		if pin := trueFirst(a.pinned, b.pinned); pin != 0 {
			return pin
		}
		availability := trueFirst(a.available, b.available)
		var order int
		switch sort.column {
		case "打印机":
			order = cmp.Compare(a.printer.Name, b.printer.Name)
		case "共享人":
			order = cmp.Compare(a.ownerName, b.ownerName)
		case "打印能力":
			order = cmp.Compare(a.capability, b.capability)
		case "状态":
			order = availability
		}
		return cmp.Or(sort.order(order), availability, cmp.Compare(a.printer.Name, b.printer.Name), cmp.Compare(a.ownerName, b.ownerName), cmp.Compare(a.printer.ID, b.printer.ID))
	}
	slices.SortStableFunc(t.lanCards, compare)
	slices.SortStableFunc(t.mineCards, compare)
}

func trueFirst(a, b bool) int {
	if a == b {
		return 0
	}
	if a {
		return -1
	}
	return 1
}

func sortPrintRecords(records []panelPrintRecord, sort panelTableSort) {
	slices.SortStableFunc(records, func(a, b panelPrintRecord) int {
		var order int
		switch sort.column {
		case "文件名称":
			order = cmp.Compare(a.name, b.name)
		case "类型":
			order = cmp.Compare(a.direction, b.direction)
		case "打印机 / 来源":
			order = cmp.Compare(a.source, b.source)
		case "时间":
			order = a.job.CreatedAt.Compare(b.job.CreatedAt)
		case "状态":
			order = cmp.Compare(a.status, b.status)
		}
		return cmp.Or(sort.order(order), b.job.CreatedAt.Compare(a.job.CreatedAt), cmp.Compare(a.job.ID, b.job.ID))
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

func printControlHeight(theme ui.Theme) float32 {
	return 36 + 20*max(theme.Metrics.EffectiveTextScale()-1, 0)
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
		p.drawPrintToolbar(f, theme, bounds.Width-2*pad, state, submit)
		ui.Row(f).FillWidth().PadSides(18, 0, 0, 0).Children(func() {
			if ui.Tabs(f.Part("print-filter"), &p.print.tab, printTabs[:], p.print.tabLabel).
				Line().FontSize(panelTabFontSize).Changed() {
				p.print.shareOpen = false
			}
		})
		ui.Divider(f).FillWidth()
		if state.PrintError != "" {
			panelBanner(f, theme, state.PrintError, true, nil)
		}
		if p.print.tab == printTabRecords && len(p.print.records) > 0 {
			p.drawPrintRecords(f, theme, bounds.Width-2*pad, p.print.records, true)
		} else {
			ui.Scroll(f.KeyInt(700 + int(p.print.tab))).FillWidth().Grow(1).MinHeight(0).AlwaysScrollbars().Children(func() {
				ui.Column(f).FillWidth().PadY(4).Children(func() {
					p.drawPrintTab(f, theme, bounds.Width-2*pad, state, submit)
				})
			})
		}
		p.drawPrinterShareDialog(f, theme, state, submit)
	})
}

func (p *Panels) drawPrintToolbar(f ui.C, theme ui.Theme, width float32, state domain.State, submit func(domain.Command)) {
	compact := width < 620*theme.Metrics.EffectiveTextScale()
	var container ui.Box
	if compact {
		container = ui.Column(f).FillWidth().Gap(14)
	} else {
		container = ui.Row(f).FillWidth().Gap(12).Items(ui.Center)
	}
	container.Children(func() {
		drawPanelPageHeading(f, width, i18n.T("打印共享"), i18n.T("选择局域网打印机并发送文件。"))
		ui.Row(f).Gap(10).Items(ui.Center).Children(func() {
			refresh := panelActionIcon(f, theme, p.print.ids.refresh, RefreshCw, i18n.T("刷新打印机"), false, state.PrintersLoading)
			if refresh.Clicked() {
				submit(domain.Command{Kind: domain.RefreshPrinters})
			}
			button := ui.Row(f).MinHeight(40).Gap(8).Pad(12).Items(ui.Center).Radius(radiusControl).
				Background(theme.Primary).Hover(ui.Look{}.Background(theme.Primary.WithAlpha(.87))).
				HitWith(p.print.ids.openShare, i18n.T("分享打印机"), gfx.HitOptions{Role: gfx.RoleButton})
			button.Children(func() {
				ui.Icon(f, Plus).Size(17, 17).Color(theme.PrimaryForeground)
				ui.Text(f, i18n.T("分享打印机")).Color(theme.PrimaryForeground).
					FontSize(14).Weight(gfx.WeightSemibold)
			})
			if button.Clicked() {
				p.print.openShareDialog(state)
				submit(domain.Command{Kind: domain.RefreshPrinters})
			}
		})
	})
}

func (p *Panels) drawPrintTab(f ui.C, theme ui.Theme, width float32, state domain.State, submit func(domain.Command)) {
	switch p.print.tab {
	case printTabMine:
		if len(p.print.mineCards) == 0 {
			panelEmpty(f, theme, i18n.T("尚未共享打印机"),
				i18n.T("点击右上角「分享打印机」，选择本机已安装的打印队列。"))
			return
		}
		p.drawPrinterTable(f, theme, width, p.print.mineCards, true, submit)
	case printTabRecords:
		panelEmpty(f, theme, i18n.T("暂无打印记录"),
			i18n.T("发出的任务和通过 GoIM 收到的任务都会保留在这里。"))
	default:
		if len(p.print.lanCards) == 0 {
			title, description := i18n.T("暂无共享打印机"), i18n.T("同一局域网内使用新版 GoIM 的成员共享打印机后会显示在这里。")
			if state.PrintersLoading {
				title, description = i18n.T("正在读取打印机"), i18n.T("正在从本机和局域网成员同步打印机信息。")
			}
			panelEmpty(f, theme, title, description)
			return
		}
		p.drawPrinterTable(f, theme, width, p.print.lanCards, false, submit)
		if len(p.print.records) > 0 {
			ui.Column(f).FillWidth().PadSides(14, 0, 0, 0).Children(func() {
				ui.Text(f, i18n.T("最近打印")).FontSize(17).Weight(gfx.WeightSemibold)
				p.drawPrintRecords(f, theme, width, p.print.recent, false)
			})
		}
	}
}

func (p *Panels) drawPrinterTable(f ui.C, theme ui.Theme, width float32, cards []panelPrintCard,
	mine bool, submit func(domain.Command)) {
	scale := theme.Metrics.EffectiveTextScale()
	wide := width >= 780*scale
	owner, capability, status := 145*scale, 160*scale, 72*scale
	actions := 116 * scale
	if mine {
		actions = 3*36 + 2*6
		if wide {
			actions = max(actions, 252*scale)
		}
	}
	ui.Column(f).FillWidth().Children(func() {
		ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
			p.print.deviceSort.header(f, "打印机", 0, p.print.sortDevices)
			if wide {
				p.print.deviceSort.header(f, "共享人", owner, p.print.sortDevices)
				p.print.deviceSort.header(f, "打印能力", capability, p.print.sortDevices)
			}
			p.print.deviceSort.header(f, "状态", status, p.print.sortDevices)
			ui.Row(f).Width(actions).Shrink(0)
		})
		for _, card := range cards {
			ui.Row(f.Key(card.printer.ID)).FillWidth().MinHeight(max(panelTableRowHeight, 42*scale)).PadX(15).Gap(12).Items(ui.Center).
				Hover(ui.Look{}.Background(theme.Hover)).Children(func() {
				ui.Row(f).Grow(1).MinWidth(0).Gap(10).Items(ui.Center).Children(func() {
					p.drawPrintAvatar(f, card, theme, 32)
					ui.Column(f).Grow(1).MinWidth(0).Children(func() {
						ui.Text(f, card.printer.Name).FontSize(15).Weight(gfx.WeightMedium).Ellipsis()
						if !wide {
							ui.Text(f, i18n.T("共享人 ")+card.ownerName+" · "+card.capability).FontSize(12).Muted().Ellipsis()
						}
					})
					if card.printer.PasswordRequired {
						ui.Icon(f, Lock).Size(14, 14).Color(theme.MutedForeground)
					}
				})
				if wide {
					ui.Text(f, card.ownerName).Width(owner).FontSize(13).Ellipsis()
					ui.Text(f, card.capability).Width(capability).FontSize(13).Ellipsis()
				}
				tone, label := theme.Success, i18n.T("可用")
				if !card.available {
					tone, label = theme.MutedForeground, i18n.T("离线")
				}
				ui.Row(f).Width(status).Gap(6).Items(ui.Center).Children(func() {
					ui.Row(f).Size(7, 7).Radius(4).Background(tone)
					ui.Text(f, label).FontSize(13).Color(tone).Ellipsis()
				})
				ui.Row(f).Width(actions).Gap(6).Items(ui.Center).Justify(ui.End).Children(func() {
					p.drawPrinterActions(f, theme, card, !wide, submit)
				})
			})
			ui.Divider(f).FillWidth()
		}
	})
}

func (p *Panels) drawPrinterActions(f ui.C, theme ui.Theme, card panelPrintCard, compact bool, submit func(domain.Command)) {
	label := i18n.T("置顶打印机")
	if card.pinned {
		label = i18n.T("取消置顶")
	}
	if panelActionIcon(f, theme, card.pinID, Pin, label, card.pinned, false).Clicked() {
		submit(domain.Command{Kind: domain.SetPrinterPinned, PrinterID: card.printer.ID, Bool: !card.pinned})
	}
	if card.own {
		action := func(id gfx.ID, label string, icon gfx.Icon) ui.Box {
			if compact {
				return panelActionIcon(f, theme, id, icon, label, false, false).Tooltip(label)
			}
			return namedButton(f, id, label).Small().MinHeight(printControlHeight(theme)).Outline()
		}
		if action(card.editID, i18n.T("编辑共享"), Pencil).Clicked() {
			p.print.editShareDialog(card.printer)
		}
		if action(card.unshareID, i18n.T("停止共享"), X).Clicked() {
			submit(domain.Command{Kind: domain.UnsharePrinter, PrinterID: card.printer.ID})
		}
		return
	}
	disabled := !card.available
	hint := i18n.T("打印")
	if disabled {
		hint = i18n.T("共享打印机当前不可用")
	}
	box := ui.Row(f).MinHeight(34).PadSides(9, 12, 9, 12).Gap(7).Radius(radiusControl).
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
}

func (p *Panels) drawPrintAvatar(f ui.C, card panelPrintCard, theme ui.Theme, size float32) {
	if p.printAvatar != nil {
		p.printAvatar(f, card.printer.OwnerID, card.ownerName, card.ownerAvatar, size, card.ownerOnline)
		return
	}
	ui.Row(f).Size(size, size).Shrink(0).Radius(size / 2).Background(theme.Muted).
		Items(ui.Center).Justify(ui.Center).Children(func() {
		ui.Text(f, initialOf(card.ownerName)).FontSize(fontToken(theme, min(18*theme.Metrics.EffectiveTextScale(), size*.42))).Weight(gfx.WeightSemibold)
	})
}

func (p *Panels) drawPrintRecords(f ui.C, theme ui.Theme, width float32, records []panelPrintRecord, fullHistory bool) {
	compact := width < 900*theme.Metrics.EffectiveTextScale()
	directionWidth := float32(88)
	sourceWidth := float32(190)
	timeWidth := float32(92)
	statusWidth := float32(128)
	if compact {
		directionWidth, sourceWidth, timeWidth, statusWidth = 0, 0, 0, 100*theme.Metrics.EffectiveTextScale()
	}
	sort := &p.print.recentSort
	if fullHistory {
		sort = &p.print.recordSort
	}
	apply := func() { sortPrintRecords(records, *sort) }
	ui.Row(f).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
		sort.header(f, "文件名称", 0, apply)
		if fullHistory && !compact {
			sort.header(f, "类型", directionWidth, apply)
		}
		if !compact {
			sort.header(f, "打印机 / 来源", sourceWidth, apply)
			sort.header(f, "时间", timeWidth, apply)
		}
		sort.header(f, "状态", statusWidth, apply)
	})
	drawRows := func() {
		for _, record := range records {
			row := ui.Row(f).FillWidth().MinHeight(max(panelTableRowHeight, 42*theme.Metrics.EffectiveTextScale())).PadX(15).Gap(12).Items(ui.Center).
				Hover(ui.Look{}.Background(theme.Hover))
			row.Children(func() {
				ui.Row(f).Grow(1).MinWidth(0).Gap(10).Items(ui.Center).Children(func() {
					ui.Row(f).Size(30, 30).Radius(radiusControl).Background(panelTint(theme.Primary, .10, theme.Background)).
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
				if fullHistory && !compact {
					ui.Text(f, record.direction).FontSize(13).Width(directionWidth).Ellipsis()
				}
				if !compact {
					ui.Text(f, record.source).FontSize(13).Width(sourceWidth).Ellipsis()
				}
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
	if fullHistory {
		ui.Scroll(f.Part("print-records")).FillWidth().Grow(1).MinHeight(0).AlwaysScrollbars().Children(drawRows)
	} else {
		drawRows()
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
	if !p.print.shareEditing {
		p.print.selectDefaultQueue()
	}
	if p.print.shareQueue != previousQueue {
		p.print.shareNameManual = false
		p.print.applyShareDefault(state)
	}
	title := i18n.T("分享打印机")
	if p.print.shareEditing {
		title = i18n.T("编辑共享打印机")
	}
	dialog := p.dialog(f, ui.DialogSpec{
		Title: title, Description: i18n.T("设置局域网共享名称和可选的打印密码。"),
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
					Placeholder(i18n.T("选择本机打印机")).FillWidth().MinHeight(printControlHeight(theme)).Disabled(len(queueIDs) == 0 || p.print.shareEditing)
				if p.print.shareQueue != previous {
					p.print.shareNameManual = false
					p.print.applyShareDefault(state)
					p.print.loadExistingShare(state)
				}
			}
			ui.Text(f, i18n.T("共享名称")).FontSize(12.5).Weight(gfx.WeightMedium)
			field := ui.TextFieldID(f, p.print.ids.shareName, &p.print.shareName,
				i18n.T("共享名称"), i18n.T("例如：陈晨-前台 HP")).FillWidth().MinHeight(printControlHeight(theme))
			if field.Changed() {
				p.print.shareNameManual = true
			}
			submitted = field.Submitted()
			ui.Row(f).FillWidth().Gap(12).Items(ui.Center).Children(func() {
				ui.Text(f, i18n.T("启用打印密码")).Grow(1).FontSize(13)
				ui.Switch(f, &p.print.shareProtected, "").Hit(p.print.ids.shareRequirePassword, i18n.T("启用打印密码"))
			})
			if p.print.shareProtected {
				hint := i18n.T("至少 6 个字符")
				if p.print.shareWasProtected {
					hint = i18n.T("留空保留当前密码")
				}
				ui.TextFieldID(f, p.print.ids.sharePassword, &p.print.sharePassword, i18n.T("打印密码"), hint).FillWidth().MinHeight(printControlHeight(theme))
			}
			ui.Paragraph(f, i18n.T("首次使用时验证一次；修改密码后，成员需要重新验证。"), 3).FillWidth().Muted().FontSize(12)
			if p.print.shareError != "" {
				ui.Text(f, p.print.shareError).FontSize(12).Color(theme.Danger)
			}
			if state.PrintError != "" {
				ui.Text(f, state.PrintError).FontSize(fontToken(theme, theme.Metrics.FontSizeS)).Color(theme.Danger)
			}
		})
	})
	dialog.Footer(func() {
		if dialogActionButton(f, p.print.ids.shareCancel, i18n.T("取消")).MinHeight(printControlHeight(theme)).Outline().Clicked() {
			p.print.closeShareDialog()
		}
		disabled := p.print.shareQueue == ""
		label := i18n.T("分享")
		if p.print.shareEditing {
			label = i18n.T("保存")
		}
		if dialogActionButton(f, p.print.ids.shareSubmit, label).MinHeight(printControlHeight(theme)).Disabled(disabled).Clicked() {
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
	password := p.print.sharePassword.Text()
	setting := "password"
	if p.print.shareProtected {
		if password == "" && p.print.shareWasProtected {
			setting = ""
		} else if utf8.RuneCountInString(password) < 6 || len(password) > 256 {
			p.print.shareError = i18n.T("密码至少 6 个字符，且不能超过 256 字节")
			return
		}
	} else {
		password = ""
	}
	p.print.closeShareDialog()
	submit(domain.Command{Kind: domain.SharePrinter, PrinterID: queue, Name: name, Password: password, Setting: setting})
}

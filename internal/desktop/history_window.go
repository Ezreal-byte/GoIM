package desktop

import (
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type historyResult struct {
	message                               domain.Message
	name, username, day, time, text, meta string
	icon                                  gfx.Icon
	key                                   int
}

type historyWindowView struct {
	backend            domain.Backend
	conversationID     string
	state              domain.State
	tree               gfx.Tree
	page, titlePage    *ui.Page
	theme              ui.Theme
	palette            palette
	wake               app.Invalidator
	search             ui.Field
	input              app.TextInput
	editInput          *app.TextInput
	searchID, locateID gfx.ID
	tab, date, sender  string
	senders            []string
	dates              []string
	rows               []historyResult
	all                []historyResult
	keys               map[string]int
	list               ui.ListState
	title, count       string
	filterKey          string
	ready              bool
	refreshDay         int
	focus              bool
	close              func()
	locate             func(string, string)
	avatar             func(ui.C, string, string, string, float32, bool)
	now                func() time.Time
}

func newHistoryWindowView(backend domain.Backend, conversationID string) *historyWindowView {
	v := &historyWindowView{backend: backend, conversationID: conversationID, tab: "all", date: "all", sender: "", focus: true, now: time.Now, keys: make(map[string]int)}
	v.theme = ui.LightTheme()
	v.palette = paletteFor(v.theme, v.state.Settings.ColorScheme)
	scope := v.tree.Feature("chat-history-window")
	v.searchID = v.tree.ID(scope, "search")
	v.locateID = v.tree.ID(scope, "locate-message")
	v.page = ui.NewPage(ui.PageConfig{Root: "history-content", Tree: &v.tree, Theme: &v.theme})
	v.titlePage = ui.NewPage(ui.PageConfig{Root: "history-title", Tree: &v.tree, Theme: &v.theme})
	v.list.Key = func(i int) int { return v.rows[i].key }
	v.list.Label = func(i int) string { return v.rows[i].text }
	v.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if event.Kind == app.TextPasteNoText || event.Kind == app.TextCommandEvent && (event.Command == app.TextInsertNewline || event.Command == app.TextInsertLineBreak) {
			return app.TextStateOf(v.editInput.Editor), nil
		}
		event.Apply(v.editInput.Editor)
		state := app.TextStateOf(v.editInput.Editor)
		v.filter()
		state.Redraw = true
		return state, nil
	}
	v.refresh()
	return v
}

func (v *historyWindowView) Config() app.Config {
	return app.Config{Title: v.title, Width: 820, Height: 880, MinWidth: 640, MinHeight: 560,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: 44, Tree: &v.tree, Invalidator: &v.wake,
		Paint: v.paint, OnWake: v.refresh, TextInput: v.textInput, OnPointer: v.page.Pointer, OnScroll: v.page.Wheel,
		OnKey: v.key, OnShortcut: v.key, WindowControls: func() app.WindowControlsStyle { return windowControlsFor(v.palette) },
	}
}

func (v *historyWindowView) Close() { v.search.SetText(""); v.page.Close(); v.titlePage.Close() }

func (v *historyWindowView) textInput(context app.TextInputContext) *app.TextInput {
	if v.state.Locked {
		return nil
	}
	input := v.page.TextInput(context)
	if input == nil || context.ID != v.searchID {
		return input
	}
	v.editInput = input
	handler := v.input.OnEvent
	v.input = *input
	v.input.Editor = nil
	v.input.OnEvent = handler
	v.input.State = app.TextStateOf(input.Editor)
	return &v.input
}

func (v *historyWindowView) key(event gfx.KeyEvent) gfx.EventResult {
	if v.state.Locked {
		return gfx.EventResult{}
	}
	if !event.Release && event.Key == gfx.KeyEscape && v.close != nil {
		v.close()
		return gfx.EventResult{Handled: true}
	}
	return v.page.Key(event)
}

func (v *historyWindowView) refresh() {
	if v.backend == nil {
		return
	}
	next := v.backend.Snapshot()
	now := v.now()
	day := now.Year()*1000 + now.YearDay()
	unchanged := v.ready && next.Revision == v.state.Revision && next.Locked == v.state.Locked && day == v.refreshDay
	v.state = next
	v.theme = themeForSettings(v.state.Settings)
	v.palette = paletteFor(v.theme, v.state.Settings.ColorScheme)
	if unchanged {
		return
	}
	v.ready = true
	v.refreshDay = day
	if v.state.Locked {
		v.title = "GoIM"
		v.all = v.all[:0]
		v.rows = v.rows[:0]
		return
	}
	v.all = v.all[:0]
	v.senders = append(v.senders[:0], "")
	conversation := v.state.Conversation(v.conversationID)
	name := ""
	if conversation.Kind == "direct" {
		name = v.state.Peer(conversation.PeerID).DisplayName()
	}
	if conversation.Kind == "group" {
		name = v.state.Group(conversation.GroupID).Name
	}
	if name == "" {
		name = i18n.T("会话")
	}
	for _, m := range v.state.Messages {
		if m.ConversationID != v.conversationID || m.Deleted {
			continue
		}
		key, ok := v.keys[m.ID]
		if !ok {
			key = len(v.keys) + 1
			v.keys[m.ID] = key
		}
		r := historyResult{message: m, name: v.senderLabel(m.SenderID), day: friendlyDay(m.CreatedAt), time: shortTime(m.CreatedAt), text: m.Text, key: key}
		r.username = remarkUsername(v.state.Peer(m.SenderID))
		if m.Nickname != "" && conversation.Kind == "group" {
			r.name = m.Nickname
			r.username = ""
		}
		file := v.state.File(m.FileID)
		switch {
		case m.Recalled:
			r.text = i18n.T("此消息已撤回")
		case isVoiceRecord(m):
			r.text = voiceRecordLabel(m)
			r.icon = Phone
		case m.Kind == "print":
			job := v.state.PrintJob(m.PrintJobID)
			r.text = job.FileName
			r.meta, _ = printStatusPresentation(job.Status)
			r.meta += " · " + job.PrinterName
			r.icon = panelPrintIcon
		case m.FileID != "":
			r.text = displayFileName(file, m)
			r.meta = fileMeta(file, m)
			file.Name = r.text
			r.icon = *fileTypeIcon(file)
		}
		v.all = append(v.all, r)
		if !containsString(v.senders, m.SenderID) {
			v.senders = append(v.senders, m.SenderID)
		}
	}
	sort.SliceStable(v.all, func(i, j int) bool { return v.all[i].message.CreatedAt.After(v.all[j].message.CreatedAt) })
	v.dates = append(v.dates[:0], historyDates...)
	lastDate := ""
	for _, r := range v.all {
		if r.message.CreatedAt.IsZero() {
			continue
		}
		day := r.message.CreatedAt.Local().Format("2006-01-02")
		if day != lastDate {
			v.dates = append(v.dates, day)
			lastDate = day
		}
	}
	v.title = "“" + name + "”" + i18n.T("的聊天记录") + " (" + intString(len(v.all)) + ")"
	if !containsString(v.senders, v.sender) {
		v.sender = ""
	}
	v.filter()
}

func (v *historyWindowView) senderLabel(id string) string {
	if id == "" {
		return i18n.T("全部成员")
	}
	if id == v.state.Self.ID {
		return i18n.T("我")
	}
	if p := v.state.Peer(id); p.DisplayName() != "" {
		return p.DisplayName()
	}
	return i18n.T("未知成员")
}

func (v *historyWindowView) filter() {
	selected := ""
	if n := v.list.Selected(); n >= 0 && n < len(v.rows) {
		selected = v.rows[n].message.ID
	}
	v.rows = v.rows[:0]
	query := strings.TrimSpace(v.search.Text())
	now := v.now()
	var since time.Time
	switch v.date {
	case "today":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		since = now.AddDate(0, 0, -7)
	case "month":
		since = now.AddDate(0, -1, 0)
	}
	for _, r := range v.all {
		m := r.message
		if v.sender != "" && m.SenderID != v.sender {
			continue
		}
		if !since.IsZero() && m.CreatedAt.Before(since) {
			continue
		}
		if len(v.date) == 10 && m.CreatedAt.Local().Format("2006-01-02") != v.date {
			continue
		}
		if query != "" && !containsFold(r.text, query) && !containsFold(r.meta, query) {
			continue
		}
		match := v.tab == "all"
		if !m.Recalled {
			switch v.tab {
			case "files":
				match = m.FileID != "" && !fileIsImage(v.state.File(m.FileID))
			case "images":
				match = m.FileID != "" && fileIsImage(v.state.File(m.FileID))
			case "links":
				match = m.Kind == "text" && (strings.Contains(m.Text, "https://") || strings.Contains(m.Text, "http://"))
			case "calls":
				match = isVoiceRecord(m)
			case "prints":
				match = m.Kind == "print"
			}
		}
		if match {
			v.rows = append(v.rows, r)
		}
	}
	v.list.Select(-1)
	for i, r := range v.rows {
		if r.message.ID == selected {
			v.list.Select(i)
			break
		}
	}
	v.count = intString(len(v.rows)) + i18n.T(" 条记录")
	key := query + "\x00" + v.tab + "\x00" + v.date + "\x00" + v.sender
	if v.filterKey != key {
		v.list.ScrollTo(0, ui.ListStart)
		v.filterKey = key
	}
}

var historyTabs = []struct{ value, label string }{{"all", "全部"}, {"files", "文件"}, {"images", "图片"}, {"links", "链接"}, {"calls", "通话"}, {"prints", "打印"}}
var historyDates = []string{"all", "today", "week", "month"}

func historyDateLabel(value string) string {
	if len(value) == 10 {
		return value
	}
	switch value {
	case "today":
		return i18n.T("今天")
	case "week":
		return i18n.T("最近 7 天")
	case "month":
		return i18n.T("最近一个月")
	}
	return i18n.T("全部日期")
}

func (v *historyWindowView) paint(canvas *gfx.Canvas, size ui.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, v.palette.sidebar)
	if v.state.Locked {
		return
	}
	title := ui.Rect{Width: size.Width, Height: 44}
	leading, trailing := app.TitlebarInsets()
	v.titlePage.Draw(canvas, title, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().PadSides(8, trailing+16, 8, leading+20).Items(ui.Center).Children(func() {
			ui.Text(f, v.title).FontSize(14).Weight(gfx.WeightMedium).Grow(1).Ellipsis()
		})
	})
	v.page.Draw(canvas, ui.Rect{Y: 44, Width: size.Width, Height: max(size.Height-44, 0)}, func(f ui.C) {
		ui.Column(f).Fill().Children(func() {
			ui.Column(f).FillWidth().PadSides(16, 24, 12, 24).Gap(14).Children(func() {
				ui.TextFieldID(f, v.searchID, &v.search, i18n.T("搜索聊天记录"), i18n.T("搜索消息、文件名或打印记录")).FillWidth().Height(44).FontSize(14)
				ui.Flow(f).FillWidth().Gap(8).Children(func() {
					for _, tab := range historyTabs {
						button := ui.Button(f.Part(tab.value), i18n.T(tab.label)).Ghost().Height(36).FontSize(13).Radius(8)
						if v.tab == tab.value {
							button.Background(v.palette.selected).Color(v.palette.selectedText)
						}
						if button.Clicked() {
							v.tab = tab.value
							v.filter()
						}
					}
				})
				ui.Flow(f).FillWidth().Items(ui.Center).Gap(10).Children(func() {
					if ui.Select(f.Part("date"), &v.date, v.dates, historyDateLabel).MinWidth(126).Height(36).Changed() {
						v.filter()
					}
					if ui.Select(f.Part("sender"), &v.sender, v.senders, v.senderLabel).MinWidth(126).Height(36).Changed() {
						v.filter()
					}
					ui.Text(f, v.count).FontSize(12).Muted()
				})
			})
			ui.Divider(f)
			if len(v.rows) == 0 {
				ui.Column(f).Grow(1).FillWidth().Items(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
					ui.Icon(f, Search).Size(32, 32).Color(v.palette.secondaryText)
					ui.Text(f, i18n.T("没有找到聊天记录")).FontSize(16).Weight(gfx.WeightMedium)
					ui.Text(f, i18n.T("试试其他关键词、日期或成员。")).FontSize(13).Muted()
				})
			} else {
				ui.List(f.Part("results"), &v.list, len(v.rows), func(row ui.ListRow) { v.drawResult(row) }).Grow(1).FillWidth().Scrollbars()
				if index, submitted := v.list.Submitted(); submitted && index >= 0 && index < len(v.rows) && v.locate != nil {
					id := v.rows[index].message.ID
					canvas.AfterPaint(func() { v.locate(v.conversationID, id) })
				}
			}
			ui.Divider(f)
			ui.Row(f).FillWidth().MinHeight(60).PadX(24).Items(ui.Center).Gap(12).Children(func() {
				ui.Text(f, i18n.T("选择记录，查看聊天上下文")).FontSize(12).Muted().Grow(1).Ellipsis()
				selected := v.list.Selected()
				button := namedButton(f, v.locateID, i18n.T("定位到聊天")).Height(36).Disabled(selected < 0 || selected >= len(v.rows))
				if button.Clicked() && selected >= 0 && selected < len(v.rows) && v.locate != nil {
					messageID := v.rows[selected].message.ID
					canvas.AfterPaint(func() { v.locate(v.conversationID, messageID) })
				}
			})
		})
	})
	if v.focus {
		canvas.RequestFocus(v.searchID)
		v.focus = false
	}
}

func (v *historyWindowView) drawResult(row ui.ListRow) {
	f := row.C
	r := v.rows[row.Index]
	row.Element.FillWidth().PadSides(12, 24, 12, 24)
	if row.Selected() {
		row.Element.Background(v.palette.selected)
	} else {
		row.Element.Hover(ui.Look{}.Background(v.palette.hover))
	}
	ui.Column(f).FillWidth().Gap(12).Children(func() {
		if row.Index == 0 || v.rows[row.Index-1].day != r.day {
			ui.Text(f, r.day).FontSize(12).Weight(gfx.WeightMedium).Muted().PadY(4)
		}
		ui.Row(f).FillWidth().Gap(12).Items(ui.Start).Children(func() {
			if v.avatar != nil {
				p := v.state.Peer(r.message.SenderID)
				v.avatar(f, r.message.SenderID, r.name, p.Avatar, 36, false)
			}
			ui.Column(f).Grow(1).MinWidth(0).Gap(8).Children(func() {
				ui.Row(f).FillWidth().Gap(12).Children(func() {
					if r.username == "" {
						ui.Text(f, r.name).FontSize(12).Muted().Grow(1).Ellipsis()
					} else {
						ui.Row(f).Grow(1).MinWidth(0).Gap(6).Children(func() {
							ui.Text(f, r.name).FontSize(12).Weight(gfx.WeightSemibold).Ellipsis()
							ui.Text(f, r.username).FontSize(11).Muted().MaxWidth(140).Ellipsis()
						})
					}
					ui.Text(f, r.time).FontSize(11).Muted()
				})
				if r.message.FileID != "" || r.message.Kind == "print" || isVoiceRecord(r.message) {
					ui.Row(f).FillWidth().Pad(12).Gap(12).Radius(10).Background(v.palette.incoming).Items(ui.Center).Children(func() {
						ui.Icon(f, r.icon).Size(28, 28).Color(v.palette.selectedText)
						ui.Column(f).Grow(1).MinWidth(0).Gap(5).Children(func() {
							ui.Text(f, r.text).FontSize(14).Weight(gfx.WeightMedium).MaxLines(2).Highlight(strings.TrimSpace(v.search.Text()), v.theme.Primary.WithAlpha(.18))
							if r.meta != "" {
								ui.Text(f, r.meta).FontSize(12).Muted().Ellipsis()
							}
						})
					})
				} else {
					ui.Paragraph(f, r.text, 5).FontSize(14).LineHeight(22).FillWidth().Highlight(strings.TrimSpace(v.search.Text()), v.theme.Primary.WithAlpha(.18))
				}
			})
		})
	})
}

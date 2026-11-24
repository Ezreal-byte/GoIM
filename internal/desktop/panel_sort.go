package desktop

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/i18n"
	"goim/internal/shareclient"
)

// column is the untranslated column label, so changing language preserves the order.
type panelTableSort struct {
	column     string
	descending bool
}

func (s *panelTableSort) header(f ui.C, label string, width float32, apply func()) {
	id := ui.ID(f.Key(label))
	theme := f.Theme()
	active := s.column == label
	hint := i18n.T("点击升序排列")
	if active && !s.descending {
		hint = i18n.T("点击降序排列")
	}
	title := i18n.T(label)
	box := ui.Row(f).Height(panelTableHeadHeight-4).Gap(3).MinWidth(0).Radius(radiusControl).
		Items(ui.Center).Hover(ui.Look{}.Background(theme.Hover)).
		HitWith(id, title, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint}).Tooltip(hint)
	if width == 0 {
		box.Grow(1)
	} else {
		box.Width(width)
	}
	if f.Canvas().FocusVisible() == id {
		box.Stroke(1, theme.Ring)
	}
	box.Children(func() {
		ui.Text(f, title).FontSize(textSize(f, 13)).Muted().MinWidth(0).Ellipsis()
		if active {
			arrow := "↑"
			if s.descending {
				arrow = "↓"
			}
			ui.Text(f, arrow).FontSize(textSize(f, 12)).Color(theme.Primary)
		} else {
			ui.Text(f, "↕").FontSize(textSize(f, 11)).Color(theme.MutedForeground)
		}
	})
	if box.Clicked() {
		s.descending = active && !s.descending
		s.column = label
		apply()
	}
}

func (s panelTableSort) order(value int) int {
	if s.descending {
		return -value
	}
	return value
}

func sortTransferEntries(entries []panelTransferEntry, sort panelTableSort) {
	compare := func(a, b panelTransferEntry) int {
		var order int
		switch sort.column {
		case "文件名":
			order = cmp.Compare(a.name, b.name)
		case "对方":
			order = cmp.Compare(a.peer, b.peer)
		case "来源":
			order = cmp.Compare(a.source, b.source)
		case "方向":
			order = cmp.Compare(a.transfer.Direction, b.transfer.Direction)
		case "时间":
			order = a.updated.Compare(b.updated)
		case "大小":
			order = cmp.Compare(a.transfer.Size, b.transfer.Size)
		case "状态":
			order = cmp.Compare(a.status, b.status)
		}
		return cmp.Or(sort.order(order), b.updated.Compare(a.updated), cmp.Compare(a.transfer.ID, b.transfer.ID))
	}
	slices.SortStableFunc(entries, compare)
}

func (p *Panels) sortTransfers() {
	sortTransferEntries(p.transferRows, p.transferSort)
	p.transferIncoming = p.transferIncoming[:0]
	p.transferReceived = p.transferReceived[:0]
	p.transferPending = p.transferPending[:0]
	p.transferFinished = p.transferFinished[:0]
	for _, entry := range p.transferRows {
		switch {
		case entry.accept:
			p.transferIncoming = append(p.transferIncoming, entry)
		case panelTransferIsFinished(entry.transfer.Status):
			p.transferFinished = append(p.transferFinished, entry)
		default:
			p.transferPending = append(p.transferPending, entry)
		}
		if entry.transfer.Direction == "receive" {
			p.transferReceived = append(p.transferReceived, entry)
		}
	}
}

func (p *Panels) sortShares() {
	sort := p.shareSort
	size := func(e panelShareEntry) (int64, int) {
		if !e.own && e.file.ShareProtected {
			return -1, 0
		}
		return e.file.Size, e.file.ShareEntryCount
	}
	compare := func(a, b panelShareEntry) int {
		var order int
		switch sort.column {
		case "名称":
			order = cmp.Compare(a.title, b.title)
		case "大小 · 项目数":
			as, an := size(a)
			bs, bn := size(b)
			order = cmp.Or(cmp.Compare(as, bs), cmp.Compare(an, bn))
		case "访问方式":
			order = cmp.Compare(a.access, b.access)
		case "分享时间":
			order = a.file.CreatedAt.Compare(b.file.CreatedAt)
		}
		return cmp.Or(sort.order(order), b.file.CreatedAt.Compare(a.file.CreatedAt), cmp.Compare(a.file.ID, b.file.ID))
	}
	slices.SortStableFunc(p.sharesMine, compare)
	slices.SortStableFunc(p.sharesRemote, compare)
}

func memberEntrySize(e shareclient.Entry) int64 {
	if e.Folder {
		return -1
	}
	if e.Bytes != nil {
		return *e.Bytes
	}
	// Older peers return rounded display sizes in the HTML catalog.
	value, unit, _ := strings.Cut(strings.TrimSpace(e.Size), " ")
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return -1
	}
	switch unit {
	case "GB":
		amount *= 1 << 30
	case "MB":
		amount *= 1 << 20
	case "KB":
		amount *= 1 << 10
	case "B", "字节":
	default:
		return -1
	}
	return int64(amount)
}

func (v *memberProjectView) sortItems() {
	if v.sort.column == "" {
		return
	}
	slices.SortStableFunc(v.page.Items, func(a, b shareclient.Entry) int {
		if v.sort.column == "大小" {
			if order := cmp.Compare(memberEntrySize(a), memberEntrySize(b)); order != 0 {
				return v.sort.order(order)
			}
		}
		return v.sort.order(cmp.Compare(a.Name, b.Name))
	})
}

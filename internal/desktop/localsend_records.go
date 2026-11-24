package desktop

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

type localSendRecordFilter uint8

const (
	localSendAllRecords localSendRecordFilter = iota
	localSendSentRecords
	localSendReceivedRecords
)

var localSendRecordFilters = [...]localSendRecordFilter{localSendAllRecords, localSendSentRecords, localSendReceivedRecords}

func localSendRecordFilterLabel(filter localSendRecordFilter) string {
	switch filter {
	case localSendSentRecords:
		return i18n.T("发出")
	case localSendReceivedRecords:
		return i18n.T("接收")
	default:
		return i18n.T("全部")
	}
}

func (v *localSendPanel) rebuildRecords(transfers []panelTransferEntry) {
	v.records = v.records[:0]
	for _, entry := range transfers {
		if entry.transfer.LegacyTransport != domain.TransportLocalSend {
			continue
		}
		entry.source = localSendRecordFilterLabel(localSendSentRecords)
		if entry.transfer.Direction == "receive" {
			entry.source = localSendRecordFilterLabel(localSendReceivedRecords)
		}
		if !entry.updated.IsZero() {
			entry.timeLabel = entry.updated.Local().Format("01-02 15:04")
		}
		v.records = append(v.records, entry)
	}
	sortTransferEntries(v.records, v.recordSort)
}

func (p *Panels) drawLocalSendRecords(f ui.C, theme ui.Theme, width float32, submit func(domain.Command)) {
	v := &p.localSend
	columns := measureTransferColumns(width)
	columns.source, columns.actions = 50, 150
	if width >= 850 {
		columns.date = 105
	}
	if width < 670 {
		columns.peer, columns.status, columns.size = 64, 100, 64
	}
	ui.Row(f).FillWidth().Items(ui.Center).Gap(10).Children(func() {
		drawTableSearch(f, p.tree.ID(v.scope, "record-search"), &v.recordSearch, i18n.T("搜索文件"))
		ui.Tabs(f.Part("record-filter"), &v.recordFilter, localSendRecordFilters[:], localSendRecordFilterLabel).Width(222)
	})
	ui.Column(f).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Row(f.Part("record-header")).FillWidth().Height(panelTableHeadHeight).PadX(15).Gap(12).Items(ui.Center).Children(func() {
			apply := func() {
				p.transferMenu = ""
				sortTransferEntries(v.records, v.recordSort)
			}
			v.recordSort.header(f, "文件名", 0, apply)
			v.recordSort.header(f, "对方", columns.peer, apply)
			v.recordSort.header(f, "方向", columns.source, apply)
			v.recordSort.header(f, "大小", columns.size, apply)
			if columns.date > 0 {
				v.recordSort.header(f, "时间", columns.date, apply)
			}
			v.recordSort.header(f, "状态", columns.status, apply)
			ui.Row(f).Width(columns.actions).Justify(ui.End).Children(func() {
				ui.Text(f, i18n.T("操作")).FontSize(textSize(f, 13)).Muted()
			})
		})
		ui.Divider(f).FillWidth()
		ui.Scroll(f.KeyInt(int(v.recordFilter)).Part("record-list")).Grow(1).MinHeight(0).FillWidth().Scrollbars().Children(func() {
			ui.Column(f).FillWidth().Children(func() {
				shown := false
				query := strings.ToLower(strings.TrimSpace(v.recordSearch.Text()))
				for i := range v.records {
					entry := &v.records[i]
					isReceive := entry.transfer.Direction == "receive"
					if v.recordFilter == localSendSentRecords && isReceive || v.recordFilter == localSendReceivedRecords && !isReceive {
						continue
					}
					if query != "" && !strings.Contains(strings.ToLower(entry.name), query) && !strings.Contains(strings.ToLower(entry.peer), query) {
						continue
					}
					shown = true
					p.drawTransferRow(f.Key(entry.transfer.ID), theme, columns, entry, submit)
				}
				if !shown {
					ui.Column(f).FillWidth().PadY(22).Children(func() {
						if len(v.records) == 0 {
							panelEmpty(f, theme, i18n.T("暂无 LocalSend 记录"), i18n.T("发送和接收进度会显示在这里。"))
						} else {
							panelEmpty(f, theme, i18n.T("暂无匹配的传输"), i18n.T("换个筛选或关键词试试。"))
						}
					})
				}
			})
		})
	})
}

func (p *Panels) drawLocalSendRecordActions(f ui.C, theme ui.Theme, entry *panelTransferEntry) {
	v := &p.localSend
	if len(entry.transfer.LocalSendItems) > 0 && panelActionIcon(f, theme, p.tree.ID(v.scope, "details:"+entry.transfer.ID), FileText, i18n.T("文件清单"), false, false).Clicked() {
		p.transferMenu = ""
		v.modal, v.recordID = localSendFilesModal, entry.transfer.ID
	}
	if entry.transfer.LocalSendNeedsPIN && panelActionIcon(f, theme, p.tree.ID(v.scope, "request-pin:"+entry.transfer.ID), Lock, i18n.T("输入对方 PIN"), false, false).Clicked() {
		p.transferMenu = ""
		v.modal, v.recordID = localSendPINModal, entry.transfer.ID
		v.sendPIN.SetText("")
	}
}

func (p *Panels) drawLocalSendFilesDialog(f ui.C, theme ui.Theme, entry *panelTransferEntry) {
	v := &p.localSend
	dialog, scope := p.localSendDialog(f, "files-dialog", i18n.T("文件清单"), entry.peer, 625, gfx.ID{})
	dialog.Body(func() {
		maxHeight := max(145, min(400, p.dialogViewport.Height-220))
		height := min(maxHeight, float32(36+len(entry.transfer.LocalSendItems)*44))
		ui.Scroll(f.Part("file-list")).FillWidth().Height(height).Scrollbars().Children(func() {
			p.localSendTable(f, theme, "状态", func() {
				for _, item := range entry.transfer.LocalSendItems {
					key := f.Key(item.ID)
					p.localSendFileRow(key, theme, item.Name, panelFormatBytes(item.Size), false, func() {
						ui.Text(key, panelTransferStatus(item.Status, entry.transfer.Direction)).FontSize(textSize(key, 12)).
							Color(panelStatusColor(theme, item.Status)).Ellipsis()
					})
				}
			})
		})
	})
	dialog.Footer(func() {
		if dialogActionButton(f, p.tree.ID(scope, "done"), i18n.T("关闭")).Outline().Clicked() {
			v.closeModal()
		}
	})
	if dialog.Dismissed() {
		v.closeModal()
	}
}

func (p *Panels) drawLocalSendPINDialog(f ui.C, entry *panelTransferEntry, submit func(domain.Command)) {
	v := &p.localSend
	fieldID := p.tree.ID(v.scope, "send-pin")
	dialog, scope := p.localSendDialog(f, "pin-dialog", i18n.T("输入对方 PIN"), entry.peer, 410, fieldID)
	var submitted bool
	dialog.Body(func() {
		field := ui.TextFieldID(f, fieldID, &v.sendPIN, i18n.T("对方 PIN"), "PIN").FillWidth()
		submitted = field.Submitted()
	})
	pin := strings.TrimSpace(v.sendPIN.Text())
	dialog.Footer(func() {
		if dialogActionButton(f, p.tree.ID(scope, "cancel"), i18n.T("取消")).Outline().Clicked() {
			v.closeModal()
		}
		if dialogActionButton(f, p.tree.ID(scope, "send"), i18n.T("继续发送")).Disabled(pin == "").Clicked() {
			submitted = true
		}
	})
	if dialog.Dismissed() {
		v.closeModal()
	} else if v.modal == localSendPINModal && submitted && pin != "" {
		submit(domain.Command{Kind: domain.RetryTransfer, TransferID: entry.transfer.ID, Password: pin})
		v.closeModal()
	}
}

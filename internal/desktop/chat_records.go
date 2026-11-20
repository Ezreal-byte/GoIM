package desktop

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

// Older stores only contain a system message. Keep those records readable,
// without inventing a direction that was never persisted.
func isVoiceRecord(m domain.Message) bool {
	return m.Voice != nil || m.Kind == "system" && strings.HasPrefix(m.ID, "voice:")
}

func voiceRecordLabel(m domain.Message) string {
	detail := strings.TrimPrefix(m.Text, "语音通话 · ")
	if m.Voice == nil {
		return i18n.T("语音通话") + " · " + detail
	}
	direction := i18n.T("呼出通话")
	if m.Voice.Direction == domain.VoiceCallIncoming {
		direction = i18n.T("呼入通话")
	}
	return direction + " · " + detail
}

func (u *UI) drawVoiceRecord(f ui.C, message domain.Message) {
	color := u.palette.secondaryText
	icon := u.iconPhone
	if message.Voice != nil && message.Voice.Connected {
		color = u.palette.success
	} else if strings.Contains(message.Text, "拒绝") || strings.Contains(message.Text, "无人接听") {
		icon = u.iconPhoneOff
	}
	ui.Row(f).FillWidth().Justify(ui.Center).Children(func() {
		card := ui.Row(f).MaxWidthPercent(90).PadSides(10, 14, 10, 14).Radius(10).Gap(10).Items(ui.Center).
			HitWith(u.messageIDs[message.ID], i18n.T("查看通话记录"), gfx.HitOptions{Role: gfx.RoleButton}).
			Background(u.palette.incoming).Children(func() {
			ui.Icon(f, icon).Size(18, 18).Color(color)
			ui.Wrap(f, voiceRecordLabel(message)).FontSize(13).Color(color)
			ui.Text(f, shortTime(message.CreatedAt)).FontSize(11).Muted()
			ui.Icon(f, u.iconChevronRight).Size(14, 14).Color(u.palette.secondaryText)
		})
		if card.Clicked() {
			u.closeDrawer()
			u.activeVoiceRecord = message
			u.drawer = "files"
		}
	})
}

func (u *UI) drawVoiceRecordDetail(f ui.C, message domain.Message) {
	ui.Scroll(f.Part("call-record-detail")).Fill().Scrollbars().Gap(16).Children(func() {
		ui.Row(f).Gap(12).Items(ui.Center).Children(func() {
			ui.Icon(f, u.iconPhone).Size(32, 32).Color(u.theme.Primary)
			ui.Text(f, i18n.T("语音通话")).FontSize(17).Weight(gfx.WeightSemibold)
		})
		ui.Divider(f)
		u.fileInfoRow(f, i18n.T("所在会话"), u.conversationTitle(u.state.Conversation(message.ConversationID)))
		u.fileInfoRow(f, i18n.T("记录时间"), longDate(message.CreatedAt))
		u.fileInfoRow(f, i18n.T("通话结果"), strings.TrimPrefix(message.Text, "语音通话 · "))
		direction := i18n.T("旧记录未保存通话方向")
		if message.Voice != nil {
			direction = i18n.T("呼出通话")
			if message.Voice.Direction == domain.VoiceCallIncoming {
				direction = i18n.T("呼入通话")
			}
		}
		u.fileInfoRow(f, i18n.T("通话方向"), direction)
	})
}

func (u *UI) drawPrintDetail(f ui.C, job domain.PrintJob) {
	ui.Scroll(f.Part("print-record-detail")).Fill().Scrollbars().Gap(16).Children(func() {
		ui.Row(f).Gap(12).Items(ui.Center).Children(func() {
			ui.Icon(f, panelPrintIcon).Size(36, 36).Color(u.theme.Primary)
			ui.Wrap(f, job.FileName).Grow(1).FontSize(17).Weight(gfx.WeightSemibold)
		})
		ui.Divider(f)
		status, _ := printStatusPresentation(job.Status)
		u.fileInfoRow(f, i18n.T("打印状态"), status)
		u.fileInfoRow(f, i18n.T("打印机"), job.PrinterName)
		u.fileInfoRow(f, i18n.T("打印设置"), printOptionSummary(job.Options))
		if job.Error != "" {
			ui.Wrap(f, job.Error).FontSize(13).Color(u.palette.danger)
		}
		ui.Wrap(f, i18n.T("这是历史打印记录。打印任务的最新状态可在打印共享中查看。")).FontSize(12).Muted()
	})
}

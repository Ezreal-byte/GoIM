package desktop

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) presentConnectionFeedback() {
	feedback := u.state.ConnectionFeedback
	if feedback.Sequence == 0 || feedback.Sequence == u.connectionSequence {
		return
	}
	u.connectionSequence = feedback.Sequence
	u.connectPending = feedback.Pending
	if feedback.Pending || u.detachedWindow {
		return
	}
	if feedback.Problem != "" {
		u.showToast(i18n.T("连接失败"), i18n.T(feedback.Problem), components.ToastError)
		return
	}
	if strings.TrimSpace(u.connectField.Text()) == feedback.Address {
		u.connectOpen = false
	}
	u.showToast(i18n.T("连接成功"), feedback.Address, components.ToastSuccess)
}

func connectionFeedbackText(f ui.C, theme ui.Theme, state domain.State) {
	feedback := state.ConnectionFeedback
	if feedback.Pending {
		ui.Text(f, i18n.T("正在连接，请稍候…")).Muted()
	} else if feedback.Problem != "" {
		ui.Paragraph(f, i18n.T(feedback.Problem), 4).Color(theme.Danger)
	}
}

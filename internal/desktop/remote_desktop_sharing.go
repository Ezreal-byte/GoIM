package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

func (v *remoteDesktopView) sharingConfig() app.Config {
	return v.notificationConfig(i18n.T("正在共享屏幕"), "GoIM.RemoteDesktopSharing", v.paintSharing)
}

func (v *remoteDesktopView) paintSharing(canvas *gfx.Canvas, size gfx.Size) {
	status := i18n.T("正在查看你的屏幕")
	if v.session.Control {
		status = i18n.T("正在控制你的电脑")
	}
	detail := ""
	if v.session.Audio {
		detail = i18n.T("正在共享系统声音")
	}
	v.paintNotification(canvas, size, status, detail, func(f ui.C) {
		stop := i18n.T("停止共享")
		if invitationAction(f, v.stopID, stop, ui.RGB(211, 55, 65)).Tooltip(stop).Children(func() {
			ui.Row(f).Size(16, 16).Radius(2).Background(ui.RGB(255, 255, 255))
		}).Clicked() {
			f.After(v.stop)
		}
	})
}

package desktop

import (
	"fmt"
	"math"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

func sharingCollapsedSize(theme ui.Theme) (int, int) {
	scale := max(float32(1), theme.Metrics.ScaledText(1))
	return int(math.Ceil(float64(58 * scale))), int(math.Ceil(float64(46 * scale)))
}

func (v *remoteDesktopView) sharingConfig() app.Config {
	cfg := v.notificationConfig(i18n.T("正在共享屏幕"), "GoIM.RemoteDesktopSharing", v.paintSharing)
	minW, minH := sharingCollapsedSize(v.theme)
	cfg.MinWidth, cfg.MinHeight = minW, minH
	return cfg
}

func (v *remoteDesktopView) toggleSharingCollapsed() {
	v.collapsed = !v.collapsed
	w, h := invitationSize(v.theme)
	if v.collapsed {
		w, h = sharingCollapsedSize(v.theme)
	}
	if v.resize != nil {
		v.resize(w, h)
	} else if v.handle != nil {
		_ = v.handle.SetSize(w, h)
	}
	v.wake.Invalidate()
}

func (v *remoteDesktopView) paintSharing(canvas *gfx.Canvas, size gfx.Size) {
	if v.collapsed {
		v.paintSharingCollapsed(canvas, size)
		return
	}
	status := i18n.T("正在查看你的屏幕")
	if v.session.Control {
		status = i18n.T("正在控制你的电脑")
	}
	detail := ""
	if v.session.Audio {
		detail = i18n.T("正在共享系统声音")
	}
	v.paintNotification(canvas, size, status, detail, func(f ui.C) {
		collapse := i18n.T("折叠浮窗")
		neutral := v.theme.Muted
		if invitationAction(f, v.collapseID, collapse, neutral).Tooltip(collapse).Children(func() {
			ui.Icon(f, ChevronRight).Size(20, 20).Color(v.theme.Foreground)
		}).Clicked() {
			f.After(v.toggleSharingCollapsed)
		}
		stop := i18n.T("停止共享")
		if invitationAction(f, v.stopID, stop, ui.RGB(211, 55, 65)).Tooltip(stop).Children(func() {
			ui.Row(f).Size(16, 16).Radius(2).Background(ui.RGB(255, 255, 255))
		}).Clicked() {
			f.After(v.stop)
		}
	})
}

func (v *remoteDesktopView) paintSharingCollapsed(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.RoundedBorder(bounds, app.PanelCornerRadius(), 1, v.theme.Border, v.theme.Background)
	tip := fmt.Sprintf(i18n.T("%s 正在控制你的电脑 · 点击展开"), v.peerName)
	if !v.session.Control {
		tip = fmt.Sprintf(i18n.T("%s 正在查看你的屏幕 · 点击展开"), v.peerName)
	}
	expand := i18n.T("展开远程协助状态")
	v.page.Draw(canvas, bounds, func(f ui.C) {
		canvas.Block(bounds)
		btn := ui.Row(f).Fill().PadSides(8, 10, 8, 10).Gap(6).Items(ui.Center).Justify(ui.Center).
			Radius(app.PanelCornerRadius()).
			Hover(ui.Look{}.Background(v.theme.Muted)).
			Focus(ui.Look{}.Ring(2, v.theme.Ring)).
			HitWith(v.collapseID, expand, gfx.HitOptions{Role: gfx.RoleButton}).
			Tooltip(tip).
			Children(func() {
				ui.Icon(f, chevronLeftIcon).Size(16, 16).Color(v.theme.Foreground)
				ui.Row(f).Size(10, 10).Radius(5).Background(ui.RGB(211, 55, 65))
			})
		if btn.Clicked() {
			f.After(v.toggleSharingCollapsed)
		}
	})
}

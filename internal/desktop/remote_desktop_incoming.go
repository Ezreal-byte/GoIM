package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

func (v *remoteDesktopView) incomingConfig() app.Config {
	width, height := invitationSize(v.theme)
	return app.Config{
		Title: i18n.T("远程协助请求") + " · GoIM", Class: "GoIM.IncomingRemoteDesktop", Icon: windowIcon(),
		Width: width, Height: height, MinWidth: width, MinHeight: height,
		WindowMode: app.WindowNotification, Tree: &v.tree, Invalidator: &v.wake,
		ClearColor: colorToApp(v.theme.Background),
		OnPointer:  v.page.Pointer, OnKey: v.page.Key, Paint: v.paintIncoming,
		OnWake: func() {
			if v.avatarShell != nil {
				v.avatarShell.applyMediaResults()
			}
		},
	}
}

func (v *remoteDesktopView) paintIncoming(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.RoundedBorder(bounds, app.PanelCornerRadius(), 1, v.theme.Border, v.theme.Background)
	v.page.Draw(canvas, bounds, func(f ui.C) {
		canvas.Block(bounds)
		ui.Row(f).Fill().Pad(16).Gap(12).Items(ui.Center).MovesWindow().Children(func() {
			if v.avatarShell != nil {
				v.avatarShell.avatarWithPresence(f, v.peerName, v.peerAvatar, 48, false, false, v.theme.Background)
			}
			ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(f, v.peerName).FillWidth().Weight(gfx.WeightSemibold).FontSize(textSize(f, 16)).Ellipsis()
				ui.Paragraph(f, i18n.T("请求控制你的电脑"), 2).FillWidth().Muted().FontSize(textSize(f, 13))
			})
			reject := i18n.T("拒绝")
			if invitationAction(f, v.stopID, reject, ui.RGB(211, 55, 65)).Tooltip(reject).
				Children(func() {
					ui.Icon(f, X).Size(22, 22).Color(ui.RGB(255, 255, 255))
				}).Clicked() {
				f.After(v.stop)
			}
			accept := i18n.T("允许控制")
			if invitationAction(f, v.acceptID, accept, ui.RGB(20, 144, 83)).Tooltip(accept).
				Disabled(v.decisionSent).Children(func() {
				ui.Icon(f, Check).Size(22, 22).Color(ui.RGB(255, 255, 255))
			}).Clicked() && !v.decisionSent && v.accept != nil {
				v.decisionSent = true
				f.After(v.accept)
			}
		})
	})
}

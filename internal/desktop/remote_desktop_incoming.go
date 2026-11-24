package desktop

import (
	"math"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

func (v *remoteDesktopView) incomingConfig() app.Config {
	scale := max(float32(1), v.theme.Metrics.ScaledText(1))
	width := int(math.Ceil(float64(408 * scale)))
	height := int(math.Ceil(float64(202 * scale)))
	return app.Config{
		Title: i18n.T("远程协助请求") + " · GoIM", Class: "GoIM.IncomingRemoteDesktop", Icon: windowIcon(),
		Width: width, Height: height, MinWidth: width, MinHeight: height,
		WindowMode: app.WindowNotification, Tree: &v.tree, Invalidator: &v.wake,
		ClearColor: colorToApp(v.theme.Card),
		OnPointer:  v.page.Pointer, OnKey: v.page.Key, Paint: v.paintIncoming,
	}
}

func (v *remoteDesktopView) paintIncoming(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.RoundedBorder(bounds, app.PanelCornerRadius(), 1, v.theme.Border, v.theme.Card)
	v.page.Draw(canvas, bounds, func(f ui.C) {
		canvas.Block(bounds)
		ui.Column(f).Fill().Pad(16).Gap(8).Children(func() {
			ui.Row(f).FillWidth().Gap(10).Items(ui.Center).MovesWindow().Children(func() {
				ui.Row(f).Size(42, 42).Shrink(0).Radius(11).Background(v.theme.Primary.Mix(v.theme.Card, .88)).
					Items(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(f, remoteDesktopIcon).Size(23, 23).Color(v.theme.Primary)
				})
				ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(f, v.peerName).FillWidth().Weight(gfx.WeightSemibold).FontSize(textSize(f, 16)).Ellipsis()
					ui.Text(f, i18n.T("远程协助请求")).FillWidth().Muted().FontSize(textSize(f, 13)).Ellipsis()
				})
			})
			ui.Paragraph(f, i18n.T("仅查看会共享屏幕。允许控制还会让对方操作本机的键盘和鼠标。"), 3).Muted()
			ui.Checkbox(f.Part("share-audio"), &v.consentAudio, i18n.T("共享本机系统声音")).Disabled(v.decisionSent)
			ui.Spacer(f)
			v.drawConsentActions(f)
		})
	})
}

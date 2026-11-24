package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/style"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func centerRemoteDesktopWaiting(config *app.Config, parent *app.Window) {
	if parent == nil || parent.Hidden() || parent.Minimized() {
		return
	}
	if frame, ok := parent.Frame(); ok && frame.Width > 0 && frame.Height > 0 {
		// Use Viva's logical window coordinates so the request follows the
		// main window when it is on another monitor.
		config.Origin = &gfx.Point{
			X: frame.X + (frame.Width-float32(config.Width))/2,
			Y: frame.Y + (frame.Height-float32(config.Height))/2,
		}
	}
}

func (v *remoteDesktopView) paintWaiting(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, v.theme.Background)
	status := i18n.T("等待对方确认远程协助…")
	if v.session.Status == domain.RemoteDesktopConnecting {
		status = i18n.T("正在建立远程协助…")
	}
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Pad(24).Gap(16).Items(ui.Center).Justify(ui.Center).Children(func() {
			ui.Row(f).Size(54, 54).Radius(16).Background(v.theme.Muted).Center().Children(func() {
				ui.Spinner(f.Part("progress")).Size(26, 26)
			})
			ui.Column(f).FillWidth().Items(ui.Center).Gap(4).Children(func() {
				ui.Paragraph(f, status, 2).FillWidth().TextCenter().
					FontSize(textSize(f, 18)).LineHeight(textSize(f, 24)).Weight(gfx.WeightSemibold)
				ui.Text(f, v.peerName).FillWidth().FontSize(textSize(f, 13)).Muted().TextCenter().Ellipsis()
			})
			if namedButton(f, v.stopID, i18n.T("取消")).ButtonRecipe(remoteDesktopCancelLook).
				Width(236).Height(max(42, textSize(f, 14)*2.5)).Clicked() {
				f.After(v.stop)
			}
		})
		canvas.SetInitialFocus(v.stopID)
	})
}

func remoteDesktopCancelLook(theme gfx.Theme, variant style.Variant, state gfx.State) style.ButtonLook {
	theme.Primary = ui.RGB(201, 47, 56)
	theme.PrimaryForeground = ui.RGB(255, 255, 255)
	return components.ButtonRecipe(theme, variant, state)
}

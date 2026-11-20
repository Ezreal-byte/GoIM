package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (r *desktopRuntime) syncVoiceWindow(state domain.State, locked bool) {
	if state.VoiceCall.ID == "" {
		if r.voiceWindow != nil {
			r.voiceWindow.Close()
		}
		r.voiceDismissedID = ""
		return
	}
	if state.VoiceCall.Status == domain.VoiceCallEnded {
		r.voiceDismissedID = state.VoiceCall.ID
		if r.voiceWindow != nil {
			r.voiceWindow.Close()
		}
		return
	}
	if r.voiceWindow != nil {
		r.voiceView.invalidator.Invalidate()
		if locked {
			r.voiceWindow.Hide()
		} else if r.voiceWindow.Hidden() {
			r.voiceWindow.Show()
		}
		return
	}
	if locked || r.application == nil || r.voiceDismissedID == state.VoiceCall.ID {
		return
	}
	view := newUIShell(r.backend, false)
	view.voiceWindow = true
	config := view.voiceConfig()
	config.OnCloseRequest = func(allow func()) {
		r.voiceDismissedID = view.state.VoiceCall.ID
		if view.state.VoiceCall.Live() {
			r.backend.Submit(domain.Command{Kind: domain.EndVoiceCall, CallID: view.state.VoiceCall.ID})
		}
		allow()
	}
	config.OnClosed = func() { r.voiceWindow, r.voiceView = nil, nil; view.Close() }
	var canvas gfx.Canvas
	window, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		r.voiceDismissedID = state.VoiceCall.ID
		view.Close()
		r.backend.Submit(domain.Command{Kind: domain.EndVoiceCall, CallID: state.VoiceCall.ID})
		r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法打开语音通话窗口：") + err.Error()})
		return
	}
	r.voiceWindow, r.voiceView = window, view
	window.Show()
}

func (view *UI) voiceConfig() app.Config {
	view.voiceWindow = true
	return app.Config{
		Title: i18n.T("语音通话") + " · GoIM", Class: "GoIM.Voice", Icon: windowIcon(),
		Width: 430, Height: 540, MinWidth: 400, MinHeight: 500,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: titlebarHeight,
		WindowControls: view.windowControls,
		Tree:           &view.tree, Invalidator: &view.invalidator,
		ClearColor: colorToApp(view.theme.Background),
		OnWake:     view.refresh,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, view.theme.Background)
			title := ui.Rect{Width: size.Width, Height: titlebarHeight}
			view.titlePage.Draw(canvas, title, func(f ui.C) {
				leading, trailing := app.TitlebarInsets()
				ui.Row(f).Fill().MovesWindow().PadSides(8, trailing+12, 8, leading+12).Items(ui.Center).Gap(8).Children(func() {
					ui.Icon(f, Phone).Size(18, 18).Color(view.palette.icon)
					ui.Text(f, i18n.T("语音通话")+" · GoIM").FontSize(13).Ellipsis()
				})
			})
			view.drawVoiceCall(canvas, ui.Rect{Y: title.Height, Width: size.Width, Height: max(size.Height-title.Height, 0)})
		},
		OnClosed: view.Close,
	}
}

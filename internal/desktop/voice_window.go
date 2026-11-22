package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const voiceTitlebarHeight = 48

func incomingVoiceInvitation(call domain.VoiceCall) bool {
	return call.ID != "" && call.Direction == domain.VoiceCallIncoming && call.Status == domain.VoiceCallRinging
}

func (r *desktopRuntime) syncVoiceWindow(state domain.State, locked bool) {
	call := state.VoiceCall
	invitation := incomingVoiceInvitation(call)
	if r.ui.inlineVoiceID != call.ID || !invitation {
		r.ui.inlineVoiceID = ""
	}
	// A close is asynchronous on native desktops. Release the old binding before
	// opening the next surface so its close callback cannot erase a newer call.
	if r.voiceView != nil && (!call.Live() || r.voiceView.voiceBoundID != call.ID || r.voiceView.voiceInvitation != invitation || (invitation && !state.Settings.DesktopNotifications)) {
		old := r.voiceWindow
		r.voiceWindow, r.voiceView = nil, nil
		old.Close()
	}
	if call.ID == "" || !call.Live() {
		r.cancelSystemInvitation()
		r.voiceDismissedID, r.voicePopupFailed = "", ""
		return
	}
	if r.syncSystemInvitation(state, locked) {
		if r.voiceView != nil && r.voiceView.voiceInvitation {
			old := r.voiceWindow
			r.voiceView, r.voiceWindow = nil, nil
			old.Close()
		}
		return
	}
	if r.voiceWindow != nil {
		if invitation {
			r.ui.inlineVoiceID = ""
		}
		r.voiceView.voiceConcealed = locked
		r.voiceView.invalidator.Invalidate()
		if !invitation {
			if locked {
				r.voiceWindow.Hide()
			} else if r.voiceWindow.Hidden() {
				r.voiceWindow.Show()
			}
		}
		return
	}
	if r.application == nil || r.voiceDismissedID == call.ID || (locked && !invitation) {
		return
	}
	if invitation && (!state.Settings.DesktopNotifications || r.voicePopupFailed == call.ID) {
		r.ui.inlineVoiceID = call.ID
		return
	}
	view := newUIShell(nil, false)
	view.backend = r.backend
	view.voiceBoundID, view.voiceInvitation, view.voiceConcealed = call.ID, invitation, locked
	view.refreshVoice()
	view.unlockForVoice = func() {
		r.window.Show()
		r.ui.SetWindowVisible(true)
	}
	config := view.voiceConfig()
	if invitation {
		config = view.invitationConfig()
	}
	config.OnCloseRequest = func(allow func()) {
		current := r.backend.Snapshot().VoiceCall
		if current.ID == call.ID && current.Live() && incomingVoiceInvitation(current) == invitation {
			r.voiceDismissedID = call.ID
			kind := domain.EndVoiceCall
			if invitation {
				kind = domain.RejectVoiceCall
			}
			r.backend.Submit(domain.Command{Kind: kind, CallID: call.ID})
		}
		allow()
	}
	config.OnClosed = func() {
		if r.voiceView == view {
			r.voiceWindow, r.voiceView = nil, nil
		}
		view.Close()
	}
	if invitation {
		width, height := config.Width, config.Height
		r.geometry.remember("voice-invitation", &config)
		config.Width, config.Height = width, height
	} else {
		r.geometry.remember("voice", &config)
	}
	var canvas gfx.Canvas
	window, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		view.Close()
		if invitation {
			r.voicePopupFailed = call.ID
			r.ui.inlineVoiceID = call.ID
			r.window.Show()
			r.ui.SetWindowVisible(true)
			r.ui.invalidator.Invalidate()
			return
		}
		r.voiceDismissedID = call.ID
		r.backend.Submit(domain.Command{Kind: domain.EndVoiceCall, CallID: call.ID})
		r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法打开语音通话窗口：") + err.Error()})
		return
	}
	r.voiceWindow, r.voiceView = window, view
	if !invitation {
		window.Show()
	} else {
		r.ui.inlineVoiceID = ""
	}
	view.invalidator.Invalidate()
}

func (view *UI) refreshVoice() {
	previous := view.state
	view.state = view.backend.Snapshot()
	view.theme = themeForSettings(view.state.Settings)
	view.palette = paletteFor(view.theme, view.state.Settings.ColorScheme)
	_, path := voicePeerPresentation(view.state)
	if view.voiceInvitation && (view.voiceConcealed || view.state.Locked) {
		path = ""
	}
	if path != "" && (!view.mediaDesiredImages[path] || previous.Revision != view.state.Revision) {
		view.mediaDesiredImages = make(map[string]bool)
		view.markAvatarImage(path)
		view.queueImage(path)
	}
	view.applyMediaResults()
	view.voicePicker.collect()
}

func voicePeerPresentation(state domain.State) (name, avatar string) {
	call := state.VoiceCall
	name, avatar = call.PeerName, call.PeerAvatar
	if peer, ok := state.LookupPeer(call.PeerID); ok {
		name, avatar = defaultIfEmpty(peer.DisplayName(), name), peer.Avatar
	}
	return defaultIfEmpty(name, i18n.T("未知联系人")), avatar
}

func (view *UI) voiceConfig() app.Config {
	view.voiceWindow = true
	return app.Config{
		Title: i18n.T("语音通话") + " · GoIM", Class: "GoIM.Voice", Icon: windowIcon(),
		Width: 430, Height: 540, MinWidth: 400, MinHeight: 500,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: voiceTitlebarHeight,
		WindowControls: view.windowControls,
		Tree:           &view.tree, Invalidator: &view.invalidator,
		ClearColor: colorToApp(view.theme.Background),
		OnWake:     view.refreshVoice,
		OnPointer:  view.dialogPage.Pointer, OnScroll: view.dialogPage.Wheel,
		OnKey: view.voiceKey, OnShortcut: view.voiceKey,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, view.theme.Background)
			title := ui.Rect{Width: size.Width, Height: voiceTitlebarHeight}
			view.titlePage.Draw(canvas, title, func(f ui.C) {
				leading, trailing := canvas.TitlebarInsets()
				ui.Row(f).Fill().MovesWindow().PadSides(8, trailing+12, 8, leading+12).Items(ui.Center).Gap(8).Children(func() {
					ui.Icon(f, Phone).Size(18, 18).Color(view.palette.icon)
					ui.Text(f, i18n.T("语音通话")+" · GoIM").FontSize(textSize(f, 13)).Ellipsis()
				})
			})
			view.drawVoiceCall(canvas, ui.Rect{Y: title.Height, Width: size.Width, Height: max(size.Height-title.Height, 0)})
		},
		OnClosed: view.Close,
	}
}

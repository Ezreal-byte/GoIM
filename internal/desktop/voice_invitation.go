package desktop

import (
	"math"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) voiceInvitationSize() (int, int) {
	scale := max(float32(1), u.theme.Metrics.EffectiveTextScale())
	return int(math.Ceil(float64(336 * scale))), int(math.Ceil(float64(136 * scale)))
}

func (u *UI) invitationConfig() app.Config {
	u.voiceInvitation = true
	width, height := u.voiceInvitationSize()
	return app.Config{
		Title: i18n.T("语音来电") + " · GoIM", Class: "GoIM.IncomingCall",
		Width: width, Height: height, Icon: windowIcon(), WindowMode: app.WindowNotification,
		Tree: &u.tree, Invalidator: &u.invalidator, ClearColor: colorToApp(u.theme.Background),
		OnWake: u.refreshVoice, OnPointer: u.voiceFallbackPage.Pointer, OnKey: u.voiceFallbackPage.Key,
		Paint: func(c *gfx.Canvas, size gfx.Size) {
			u.drawVoiceInvitation(c, ui.Rect{Width: size.Width, Height: size.Height}, u.voiceBoundID)
		}, OnClosed: u.Close,
	}
}

func (u *UI) drawVoiceInvitation(canvas *gfx.Canvas, card ui.Rect, id string) {
	call := u.state.VoiceCall
	if id != call.ID || !incomingVoiceInvitation(call) {
		return
	}
	locked := u.voiceConcealed || u.lockVisible()
	name, path := voicePeerPresentation(u.state)
	body := i18n.T("邀请你进行语音通话")
	if locked || u.state.Settings.HideNotificationText {
		name, body, path = "GoIM", i18n.T("收到一个语音通话邀请"), ""
	}
	u.voiceFallbackPage.Draw(canvas, card, func(f ui.C) {
		canvas.Block(card)
		ui.Column(f).Fill().Background(u.theme.Background).Border(1, u.palette.border).Pad(14).Gap(10).Children(func() {
			ui.Row(f).FillWidth().Gap(12).Items(ui.Center).Grow(1).Children(func() {
				u.avatarWithPresence(f, name, path, 44, false, false, u.theme.Background)
				ui.Column(f).Grow(1).Gap(3).Children(func() {
					ui.Text(f, name).FillWidth().Weight(gfx.WeightSemibold).FontSize(16).Ellipsis()
					ui.Text(f, body).FillWidth().Muted().FontSize(13).Ellipsis()
				})
			})
			ui.Row(f).FillWidth().Gap(10).Justify(ui.End).Children(func() {
				if namedButton(f, u.ids.voiceReject, i18n.T("拒绝")).Outline().Clicked() {
					u.respondToInvitation(id, false)
				}
				label := i18n.T("接听")
				if locked {
					label = i18n.T("解锁后接听")
				}
				if namedButton(f, u.ids.voiceAccept, label).Clicked() {
					u.respondToInvitation(id, true)
				}
			})
		})
	})
}

// The tray gives keyboard users the same guarded actions as the non-focusable
// notification, including while the main window is minimized.
func (r *desktopRuntime) voiceTrayItems() []app.MenuItem {
	state := r.backend.Snapshot()
	if !incomingVoiceInvitation(state.VoiceCall) {
		return nil
	}
	id := state.VoiceCall.ID
	label := i18n.T("接听")
	if state.Locked || r.ui.lockVisible() {
		label = i18n.T("解锁后接听")
	}
	return []app.MenuItem{
		{ID: "call-answer-" + id, Title: label, OnSelect: func() { r.ui.respondToInvitation(id, true) }},
		{ID: "call-reject-" + id, Title: i18n.T("拒绝"), OnSelect: func() { r.ui.respondToInvitation(id, false) }},
		{Separator: true},
	}
}

func (u *UI) respondToInvitation(id string, accept bool) {
	state := u.backend.Snapshot()
	if state.VoiceCall.ID != id || !incomingVoiceInvitation(state.VoiceCall) {
		return
	}
	kind := domain.RejectVoiceCall
	if accept {
		if state.Locked || u.voiceConcealed || u.lockVisible() {
			if u.unlockForVoice != nil {
				u.unlockForVoice()
			}
			return
		}
		kind = domain.AcceptVoiceCall
	}
	u.submit(domain.Command{Kind: kind, CallID: id})
}

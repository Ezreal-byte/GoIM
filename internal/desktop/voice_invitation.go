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
	return int(math.Ceil(float64(368 * scale))), int(math.Ceil(float64(92 * scale)))
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
	name, path, body := invitationCaller(u.state, locked)
	u.voiceFallbackPage.Draw(canvas, card, func(f ui.C) {
		canvas.Block(card)
		ui.Row(f).Fill().Background(u.theme.Background).Border(1, u.palette.border).
			Pad(16).Gap(12).Items(ui.Center).Children(func() {
			u.avatarWithPresence(f, name, path, 48, false, false, u.theme.Background)
			ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(f, name).FillWidth().Weight(gfx.WeightSemibold).FontSize(16).Ellipsis()
				ui.Text(f, body).FillWidth().Muted().FontSize(13).Ellipsis()
			})
			if invitationButton(f, u.ids.voiceReject, i18n.T("拒绝"), ui.RGB(211, 55, 65), 3*math.Pi/4).Clicked() {
				u.respondToInvitation(id, false)
			}
			label := i18n.T("接听")
			if locked {
				label = i18n.T("解锁后接听")
			}
			if invitationButton(f, u.ids.voiceAccept, label, ui.RGB(20, 144, 83), 0).Clicked() {
				u.respondToInvitation(id, true)
			}
		})
	})
}

func invitationCaller(state domain.State, locked bool) (name, avatar, body string) {
	if locked {
		return "GoIM", "", i18n.T("收到一个语音通话邀请")
	}
	name, avatar = voicePeerPresentation(state)
	return name, avatar, i18n.T("邀请你进行语音通话")
}

func invitationButton(f ui.C, id gfx.ID, label string, color ui.Color, rotation float32) ui.Box {
	const diameter = 44
	ink := ui.RGB(0, 0, 0)
	return ui.Row(f).Size(diameter, diameter).Shrink(0).Radius(diameter/2).Background(color).
		Items(ui.Center).Justify(ui.Center).
		Hover(ui.Look{}.Background(color.Mix(ink, .10))).Pressed(ui.Look{}.Background(color.Mix(ink, .20))).
		Focus(ui.Look{}.Ring(2, f.Theme().Ring)).HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton}).
		Children(func() {
			ui.Icon(f, Phone).Size(22, 22).Color(ui.RGB(255, 255, 255)).IconTransform(gfx.IconTransform{Rotate: rotation})
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

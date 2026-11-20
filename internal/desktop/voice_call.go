package desktop

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

func (u *UI) voiceVisible() bool {
	return u.voiceWindow && u.state.VoiceCall.ID != ""
}

func (u *UI) startVoiceCall(peerID string) {
	peer := u.state.Peer(peerID)
	call := u.state.VoiceCall
	switch {
	case call.ID != "" && call.Live():
		u.showToast(i18n.T("正在通话"), i18n.T("请先结束当前语音通话。"), components.ToastWarning)
		return
	case peer.ID == "" || peer.ID == u.state.Self.ID:
		return
	case !peer.VisibleOnline():
		u.showToast(i18n.T("对方当前不在线"), i18n.T("联系人上线后即可发起语音通话。"), components.ToastWarning)
		return
	case peer.Transport != domain.TransportGoIM || !peer.VoiceSupported:
		u.showToast(i18n.T("无法发起语音通话"), i18n.T("对方客户端版本不支持语音通话"), components.ToastWarning)
		return
	}
	u.submit(domain.Command{Kind: domain.StartVoiceCall, PeerID: peerID})
}

func (u *UI) drawVoiceCall(canvas *gfx.Canvas, viewport ui.Rect) {
	call := u.state.VoiceCall
	if !u.voiceVisible() || u.lockVisible() {
		u.voiceFocusedFor = ""
		return
	}
	if !u.voiceWindow {
		canvas.Fill(viewport, ui.RGBA(6, 8, 12, 0.68))
	}
	width := min(max(viewport.Width-64, min(viewport.Width-24, 320)), 430)
	height := min(max(viewport.Height-72, min(viewport.Height-24, 460)), 600)
	card := ui.Rect{
		X:     viewport.X + (viewport.Width-width)/2,
		Y:     viewport.Y + (viewport.Height-height)/2,
		Width: width, Height: height,
	}
	if u.voiceWindow {
		card, width, height = viewport, viewport.Width, viewport.Height
	}
	accent := u.theme.Primary
	if call.Status == domain.VoiceCallActive {
		accent = u.palette.success
	}
	canvas.Rounded(ui.Rect{
		X: card.X + card.Width/2 - 170, Y: card.Y + 30,
		Width: 340, Height: 340,
	}, 170, accent.WithAlpha(0.075))

	surface := u.theme.Background
	if u.isDark() {
		surface = u.palette.sidebar.Mix(ui.RGB(255, 255, 255), 0.035)
	}
	subtle := u.theme.Muted
	if u.isDark() {
		subtle = u.palette.hover
	}
	status := u.voiceCallStatus(call)
	avatarSize := float32(128)
	if u.voiceWindow {
		avatarSize = min(128, max(64, height-220-100*u.theme.Metrics.EffectiveTextScale()))
	}
	var muteButton, speakerButton, acceptButton, rejectButton, hangupButton ui.Box

	u.dialogPage.Draw(canvas, viewport, func(f ui.C) {
		f.Canvas().Block(viewport)
		ui.Row(f).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
			ui.Column(f).Width(width).Height(height).PadSides(30, 32, 26, 32).Gap(0).Radius(radiusCard).
				Background(surface).Border(1, u.palette.border).Shadow(gfx.ShadowLG).
				Children(func() {
					if !u.voiceWindow {
						ui.Row(f).FillWidth().Justify(ui.Center).Gap(7).Items(ui.Center).Children(func() {
							ui.Stack(f).Size(7, 7).Radius(4).Background(accent)
							ui.Text(f, i18n.T("语音通话")).FontSize(11.5).Weight(gfx.WeightMedium).Muted()
						})
						ui.Slot(f).Height(28)
					}
					ui.Row(f).FillWidth().Justify(ui.Center).Children(func() {
						ui.Stack(f).Size(avatarSize, avatarSize).Pad(8).Radius(avatarSize / 2).Background(subtle).
							Children(func() {
								u.avatarWithPresence(f, call.PeerName, call.PeerAvatar, avatarSize-16, false, false, surface)
							})
					})
					ui.Slot(f).Height(20)
					ui.Text(f, defaultIfEmpty(call.PeerName, i18n.T("未知联系人"))).FillWidth().
						FontSize(24).Weight(gfx.WeightSemibold).TextCenter().Ellipsis()
					ui.Slot(f).Height(8)
					ui.Text(f, status).FillWidth().FontSize(13.5).Muted().TextCenter()
					if call.Status == domain.VoiceCallActive {
						ui.Slot(f).Height(5)
						ui.Text(f, voiceCallElapsed(call.ConnectedAt)).FillWidth().FontSize(12).
							Muted().TextCenter()
					}
					ui.Spacer(f)
					if call.Status == domain.VoiceCallEnded {
						ui.Text(f, u.voiceEndedDetail(call.Reason)).FillWidth().FontSize(12).
							Muted().TextCenter()
						return
					}
					ui.Row(f).FillWidth().Justify(ui.Center).Gap(20).Children(func() {
						if call.Direction == domain.VoiceCallIncoming && call.Status == domain.VoiceCallRinging {
							rejectButton = u.voiceControl(f, u.ids.voiceReject, u.iconPhoneOff,
								i18n.T("拒绝"), ui.RGB(230, 63, 72), ui.RGB(255, 255, 255), false, true)
							acceptButton = u.voiceControl(f, u.ids.voiceAccept, u.iconPhone,
								i18n.T("接听"), ui.RGB(45, 178, 111), ui.RGB(255, 255, 255), false, true)
							return
						}
						muteLabel := i18n.T("麦克风已开")
						muteIcon := u.iconMic
						if call.Muted {
							muteLabel, muteIcon = i18n.T("麦克风已关闭"), u.iconMicOff
						}
						muteButton = u.voiceControl(f, u.ids.voiceMute, muteIcon, muteLabel,
							subtle, surfaceContrast(surface), call.Muted, false)
						hangupLabel := i18n.T("挂断")
						if call.Status == domain.VoiceCallRinging {
							hangupLabel = i18n.T("取消")
						}
						hangupButton = u.voiceControl(f, u.ids.voiceHangup, u.iconPhoneOff, hangupLabel,
							ui.RGB(230, 63, 72), ui.RGB(255, 255, 255), false, true)
						speakerLabel := i18n.T("扬声器已开")
						speakerIcon := u.iconVolume
						if call.SpeakerMuted {
							speakerLabel, speakerIcon = i18n.T("扬声器已关闭"), u.iconVolumeX
						}
						speakerButton = u.voiceControl(f, u.ids.voiceSpeaker, speakerIcon, speakerLabel,
							subtle, surfaceContrast(surface), call.SpeakerMuted, false)
					})
				})
		})
	})

	focus := u.ids.voiceHangup
	if call.Direction == domain.VoiceCallIncoming && call.Status == domain.VoiceCallRinging {
		focus = u.ids.voiceAccept
	}
	focusKey := call.ID + ":" + string(call.Direction)
	if call.Status != domain.VoiceCallEnded && u.voiceFocusedFor != focusKey {
		u.voiceFocusedFor = focusKey
		canvas.RequestFocus(focus)
	}
	if call.Status == domain.VoiceCallActive {
		// Only the live call clock asks for frames; the shell remains idle when
		// no call is on screen.
		elapsed := time.Since(call.ConnectedAt)
		delay := time.Second - elapsed%time.Second
		if delay < 16*time.Millisecond {
			delay = 16 * time.Millisecond
		}
		canvas.RequestFrameIn(delay)
	}

	switch {
	case voiceClicked(rejectButton):
		u.submit(domain.Command{Kind: domain.RejectVoiceCall, CallID: call.ID})
	case voiceClicked(acceptButton):
		u.submit(domain.Command{Kind: domain.AcceptVoiceCall, CallID: call.ID})
	case voiceClicked(hangupButton):
		u.submit(domain.Command{Kind: domain.EndVoiceCall, CallID: call.ID})
	case voiceClicked(muteButton):
		u.submit(domain.Command{Kind: domain.SetVoiceMuted, CallID: call.ID, Bool: !call.Muted})
	case voiceClicked(speakerButton):
		u.submit(domain.Command{Kind: domain.SetVoiceSpeakerMuted, CallID: call.ID, Bool: !call.SpeakerMuted})
	}
}

func voiceClicked(box ui.Box) bool {
	return box.Valid() && box.Clicked()
}

func (u *UI) voiceControl(f ui.C, id gfx.ID, icon gfx.Icon, label string, background, foreground ui.Color, selected, primary bool) ui.Box {
	diameter := float32(62)
	if primary {
		diameter = 68
	}
	button := ui.Column(f).Width(88).Gap(8).Items(ui.Center).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected})
	button.Children(func() {
		circle := ui.Row(f).Size(diameter, diameter).Radius(diameter / 2).Background(background).
			Shadow(gfx.ShadowMD).Items(ui.Center).Justify(ui.Center)
		if f.Canvas().FocusVisible() == id || f.Canvas().Hovered() == id {
			circle = circle.Stroke(2, u.theme.Ring)
		}
		circle.Children(func() {
			ui.Icon(f, icon).Size(24, 24).Color(foreground)
		})
		ui.Text(f, label).FillWidth().FontSize(11.5).Weight(gfx.WeightMedium).Muted().TextCenter().MaxLines(2)
	})
	return button
}

func (u *UI) voiceCallStatus(call domain.VoiceCall) string {
	switch call.Status {
	case domain.VoiceCallActive:
		return i18n.T("通话中")
	case domain.VoiceCallEnded:
		return i18n.T("通话已结束")
	case domain.VoiceCallRinging:
		if call.Direction == domain.VoiceCallIncoming {
			return i18n.T("邀请你进行语音通话")
		}
		return i18n.T("等待对方接听…")
	default:
		return i18n.T("语音通话")
	}
}

func (u *UI) voiceEndedDetail(reason string) string {
	switch reason {
	case "declined":
		return i18n.T("对方已拒绝")
	case "busy":
		return i18n.T("对方正在通话中")
	case "no_answer":
		return i18n.T("无人接听")
	case "cancelled":
		return i18n.T("通话已取消")
	case "disconnected":
		return i18n.T("连接已断开")
	case "unavailable":
		return i18n.T("对方当前不可用")
	case "incompatible":
		return i18n.T("双方客户端不兼容")
	default:
		return i18n.T("通话已结束")
	}
}

func voiceCallElapsed(connected time.Time) string {
	if connected.IsZero() {
		return "00:00"
	}
	seconds := int(math.Max(0, time.Since(connected).Seconds()))
	hours, minutes, seconds := seconds/3600, seconds/60%60, seconds%60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func surfaceContrast(surface ui.Color) ui.Color {
	if surface.Red*0.299+surface.Green*0.587+surface.Blue*0.114 > 0.55 {
		return ui.RGB(28, 31, 38)
	}
	return ui.RGB(246, 247, 249)
}

package desktop

import (
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

var (
	videoCallIcon = gfx.Icon{
		ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
		Commands: []gfx.PathCommand{
			gfx.MoveTo(16, 13), gfx.LineTo(21.223, 16.482),
			gfx.CubicTo(21.555, 16.703, 22, 16.465, 22, 16.066),
			gfx.LineTo(22, 7.934),
			gfx.CubicTo(22, 7.535, 21.555, 7.297, 21.223, 7.518),
			gfx.LineTo(16, 11),
			gfx.MoveTo(4, 6), gfx.LineTo(14, 6),
			gfx.CubicTo(15.1046, 6, 16, 6.89543, 16, 8),
			gfx.LineTo(16, 16),
			gfx.CubicTo(16, 17.1046, 15.1046, 18, 14, 18),
			gfx.LineTo(4, 18),
			gfx.CubicTo(2.89543, 18, 2, 17.1046, 2, 16),
			gfx.LineTo(2, 8),
			gfx.CubicTo(2, 6.89543, 2.89543, 6, 4, 6),
			gfx.ClosePath(),
		},
	}

	videoCallOffIcon = gfx.Icon{
		ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
		Commands: []gfx.PathCommand{
			gfx.MoveTo(10.66, 6), gfx.LineTo(14, 6),
			gfx.CubicTo(15.1046, 6, 16, 6.89543, 16, 8),
			gfx.LineTo(16, 10.34),
			gfx.LineTo(17, 11.34),
			gfx.LineTo(21.223, 7.518),
			gfx.CubicTo(21.555, 7.297, 22, 7.535, 22, 7.934),
			gfx.LineTo(22, 16.066),
			gfx.MoveTo(16, 16),
			gfx.CubicTo(16, 17.1046, 15.1046, 18, 14, 18),
			gfx.LineTo(4, 18),
			gfx.CubicTo(2.89543, 18, 2, 17.1046, 2, 16),
			gfx.LineTo(2, 8),
			gfx.CubicTo(2, 6.89543, 2.89543, 6, 4, 6),
			gfx.LineTo(6, 6),
			gfx.MoveTo(2, 2), gfx.LineTo(22, 22),
		},
	}

	chevronLeftIcon = gfx.Icon{
		ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
		Commands: []gfx.PathCommand{
			gfx.MoveTo(15, 18), gfx.LineTo(9, 12), gfx.LineTo(15, 6),
		},
	}
)

func (u *UI) startVideoCall(peerID string) {
	peer := u.state.Peer(peerID)
	call := u.state.VoiceCall
	switch {
	case call.ID != "" && call.Live():
		u.showToast(i18n.T("正在通话"), i18n.T("请先结束当前通话。"), components.ToastWarning)
		return
	case peer.ID == "" || peer.ID == u.state.Self.ID:
		return
	case !peer.VisibleOnline():
		u.showToast(i18n.T("对方当前不在线"), i18n.T("联系人上线后即可发起视频通话。"), components.ToastWarning)
		return
	case peer.Transport != domain.TransportGoIM || !peer.VideoSupported:
		u.showToast(i18n.T("无法发起视频通话"), i18n.T("对方客户端版本不支持视频通话"), components.ToastWarning)
		return
	}
	u.submit(domain.Command{Kind: domain.StartVideoCall, PeerID: peerID})
}

func (view *UI) videoConfig() app.Config {
	view.voiceWindow = true
	return app.Config{
		Title: i18n.T("视频通话") + " · GoIM", Class: "GoIM.Video", Icon: windowIcon(),
		Width: 760, Height: 540, MinWidth: 560, MinHeight: 420,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: voiceTitlebarHeight,
		WindowControls: func() app.WindowControlsStyle {
			return windowControlsFor(palette{
				icon:     ui.RGB(218, 224, 236),
				hover:    ui.RGBA(255, 255, 255, 0.12),
				selected: ui.RGBA(255, 255, 255, 0.20),
			})
		},
		Tree: &view.tree, Invalidator: &view.invalidator,
		ClearColor: colorToApp(ui.RGB(18, 22, 30)),
		OnWake:     view.refreshVoice,
		OnPointer:  view.dialogPage.Pointer, OnScroll: view.dialogPage.Wheel,
		OnKey: view.voiceKey, OnShortcut: view.voiceKey,
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			stageBg := ui.RGB(18, 22, 30)
			canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, stageBg)
			title := ui.Rect{Width: size.Width, Height: voiceTitlebarHeight}
			view.titlePage.Draw(canvas, title, func(f ui.C) {
				leading, trailing := canvas.TitlebarInsets()
				ui.Row(f).Fill().MovesWindow().PadSides(8, trailing+12, 8, leading+12).Items(ui.Center).Gap(8).Children(func() {
					ui.Icon(f, videoCallIcon).Size(18, 18).Color(ui.RGB(235, 240, 248))
					ui.Text(f, i18n.T("视频通话")+" · GoIM").FontSize(textSize(f, 13)).Color(ui.RGB(235, 240, 248)).Ellipsis()
				})
			})
			view.drawVideoCall(canvas, ui.Rect{Y: title.Height, Width: size.Width, Height: max(size.Height-title.Height, 0)})
		},
		OnClosed: view.Close,
	}
}

func (u *UI) drawVideoCall(canvas *gfx.Canvas, viewport ui.Rect) {
	call := u.state.VoiceCall
	if !u.voiceVisible() || u.voiceConcealed || u.lockVisible() {
		u.voiceFocusedFor = ""
		return
	}
	if u.voicePicker.kind != "" {
		u.drawCallDevicePicker(canvas, viewport)
		return
	}

	remoteBmp, localBmp := u.videoMedia.pullFrames()
	name, path := voicePeerPresentation(u.state)
	stageBg := ui.RGB(18, 22, 30)
	canvas.Fill(viewport, stageBg)

	// Main video stage: remote video when active, or local preview while waiting.
	mainBmp := remoteBmp
	if call.Status != domain.VoiceCallActive {
		mainBmp = localBmp
	}
	showMainVideo := bitmapReady(mainBmp) && (call.Status != domain.VoiceCallActive && !call.CameraOff || call.Status == domain.VoiceCallActive && !call.PeerCameraOff)
	if showMainVideo {
		fit := fitBitmapRect(viewport, mainBmp.Width, mainBmp.Height)
		canvas.DrawBitmap(mainBmp, fit)
	}

	// Local PiP card when active.
	pipWidth := min(float32(180), max(float32(128), viewport.Width*0.24))
	pipHeight := pipWidth * 9 / 16
	controlBarHeight := float32(114)
	pipRect := ui.Rect{
		X:      viewport.X + viewport.Width - pipWidth - 20,
		Y:      viewport.Y + viewport.Height - controlBarHeight - pipHeight - 16,
		Width:  pipWidth,
		Height: pipHeight,
	}
	showPiP := call.Status == domain.VoiceCallActive && pipRect.Y > viewport.Y+54
	if showPiP {
		canvas.RoundedBorder(pipRect, 12, 1.5, ui.RGBA(255, 255, 255, 0.24), ui.RGB(28, 34, 46))
		if !call.CameraOff && bitmapReady(localBmp) {
			inner := ui.Rect{X: pipRect.X + 2, Y: pipRect.Y + 2, Width: pipRect.Width - 4, Height: pipRect.Height - 4}
			canvas.Clip(inner)
			canvas.DrawBitmap(localBmp, fitBitmapRect(inner, localBmp.Width, localBmp.Height))
			canvas.Unclip()
		}
	}

	lightText := ui.RGB(246, 248, 252)
	mutedText := ui.RGB(186, 194, 208)
	pillBg := ui.RGBA(12, 16, 24, 0.62)
	ctrlBg := ui.RGBA(255, 255, 255, 0.16)
	ctrlActiveBg := ui.RGBA(255, 255, 255, 0.92)
	ctrlActiveFg := ui.RGB(24, 28, 36)

	var muteButton, cameraButton, speakerButton, hangupButton ui.Box
	cameraProblem := u.videoMedia.cameraProblem()

	u.dialogPage.Draw(canvas, viewport, func(f ui.C) {
		f.Canvas().Block(viewport)
		ui.Column(f).Fill().PadSides(12, 20, 20, 20).Children(func() {
			// Top peer & timer pill
			ui.Column(f).FillWidth().Items(ui.Center).Gap(6).Children(func() {
				ui.Row(f).PadSides(7, 14, 7, 14).Radius(18).Background(pillBg).Items(ui.Center).Gap(8).Children(func() {
					ui.Text(f, name).FontSize(textSize(f, 13.5)).Weight(gfx.WeightSemibold).Color(lightText).Ellipsis()
					if call.Status == domain.VoiceCallActive {
						ui.Text(f, "· "+voiceCallElapsed(call.ConnectedAt)).FontSize(textSize(f, 12.5)).Color(mutedText)
					} else {
						ui.Text(f, "· "+i18n.T("等待对方接听…")).FontSize(textSize(f, 12.5)).Color(mutedText)
					}
				})
				if showMainVideo && cameraProblem != "" && !call.CameraOff {
					ui.Row(f).PadSides(5, 12, 5, 12).Radius(14).Background(pillBg).Children(func() {
						ui.Text(f, cameraProblem).FontSize(textSize(f, 11.5)).Color(ui.RGB(255, 210, 130))
					})
				}
			})

			// Center placeholder when no video stream is visible
			ui.Column(f).FillWidth().Grow(1).Items(ui.Center).Justify(ui.Center).Gap(12).Children(func() {
				if !showMainVideo {
					avatarSize := min(float32(116), max(float32(72), viewport.Height*0.24))
					ui.Stack(f).Size(avatarSize, avatarSize).Pad(6).Radius(avatarSize / 2).Background(ui.RGBA(255, 255, 255, 0.10)).Children(func() {
						u.avatarWithPresence(f, name, path, avatarSize-12, false, false, stageBg)
					})
					ui.Text(f, name).FontSize(textSize(f, 22)).Weight(gfx.WeightSemibold).Color(lightText).TextCenter().Ellipsis()
					placeholderStatus := u.videoCallStatus(call)
					ui.Text(f, placeholderStatus).FontSize(textSize(f, 13.5)).Color(mutedText).TextCenter()
					if cameraProblem != "" && !call.CameraOff {
						ui.Row(f).PadSides(6, 12, 6, 12).Radius(14).Background(pillBg).Children(func() {
							ui.Text(f, cameraProblem).FontSize(textSize(f, 12)).Color(ui.RGB(255, 210, 130))
						})
					}
				}
			})

			// Bottom control dock
			ui.Row(f).FillWidth().Justify(ui.Center).Children(func() {
				ui.Row(f).PadSides(12, 20, 10, 20).Radius(24).Background(pillBg).Items(ui.Center).Gap(16).Children(func() {
					muteLabel, muteIcon := i18n.T("麦克风已开"), u.iconMic
					muteBg, muteFg := ctrlBg, lightText
					if call.Muted {
						muteLabel, muteIcon = i18n.T("麦克风已关闭"), u.iconMicOff
						muteBg, muteFg = ctrlActiveBg, ctrlActiveFg
					}
					muteButton = u.videoControl(f, u.ids.voiceMute, muteIcon, muteLabel, muteBg, muteFg, lightText, call.Muted, false)

					camLabel, camIcon := i18n.T("摄像头已开"), videoCallIcon
					camBg, camFg := ctrlBg, lightText
					if call.CameraOff {
						camLabel, camIcon = i18n.T("摄像头已关闭"), videoCallOffIcon
						camBg, camFg = ctrlActiveBg, ctrlActiveFg
					}
					cameraButton = u.videoControl(f, u.ids.videoCamera, camIcon, camLabel, camBg, camFg, lightText, call.CameraOff, false)

					hangupLabel := i18n.T("挂断")
					if call.Status == domain.VoiceCallRinging {
						hangupLabel = i18n.T("取消")
					}
					hangupButton = u.videoControl(f, u.ids.voiceHangup, u.iconPhoneOff, hangupLabel,
						ui.RGB(230, 63, 72), ui.RGB(255, 255, 255), lightText, false, true)

					speakerLabel, speakerIcon := i18n.T("扬声器已开"), u.iconVolume
					speakerBg, speakerFg := ctrlBg, lightText
					if call.SpeakerMuted {
						speakerLabel, speakerIcon = i18n.T("扬声器已关闭"), u.iconVolumeX
						speakerBg, speakerFg = ctrlActiveBg, ctrlActiveFg
					}
					speakerButton = u.videoControl(f, u.ids.voiceSpeaker, speakerIcon, speakerLabel, speakerBg, speakerFg, lightText, call.SpeakerMuted, false)
				})
			})
		})
	})

	for _, control := range []struct {
		id   gfx.ID
		kind string
	}{{u.ids.voiceMute, "麦克风"}, {u.ids.voiceSpeaker, "扬声器"}} {
		if event, ok := canvas.PointerOn(control.id); ok && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
			u.openCallDevicePicker(control.kind)
			return
		}
	}
	u.hover.Paint(canvas, u.theme, viewport, components.TooltipOptions{}, 0)

	focusKey := call.ID + ":" + string(call.Direction)
	if call.Status != domain.VoiceCallEnded && u.voiceFocusedFor != focusKey {
		u.voiceFocusedFor = focusKey
		canvas.RequestFocus(u.ids.voiceHangup)
	}
	if call.Status == domain.VoiceCallActive {
		elapsed := time.Since(call.ConnectedAt)
		delay := time.Second - elapsed%time.Second
		if delay < 16*time.Millisecond {
			delay = 16 * time.Millisecond
		}
		canvas.RequestFrameIn(delay)
	}

	switch {
	case voiceClicked(hangupButton):
		u.submit(domain.Command{Kind: domain.EndVoiceCall, CallID: call.ID})
	case voiceClicked(muteButton):
		u.submit(domain.Command{Kind: domain.SetVoiceMuted, CallID: call.ID, Bool: !call.Muted})
	case voiceClicked(cameraButton):
		u.submit(domain.Command{Kind: domain.SetVideoCameraOff, CallID: call.ID, Bool: !call.CameraOff})
	case voiceClicked(speakerButton):
		u.submit(domain.Command{Kind: domain.SetVoiceSpeakerMuted, CallID: call.ID, Bool: !call.SpeakerMuted})
	}
}

func (u *UI) videoControl(f ui.C, id gfx.ID, icon gfx.Icon, label string, background, foreground, labelColor ui.Color, selected, primary bool) ui.Box {
	diameter := float32(54)
	if primary {
		diameter = 60
	}
	options := gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}
	if id == u.ids.voiceMute || id == u.ids.voiceSpeaker {
		options.Hint = i18n.T("右键或按 Shift+F10 调整设备和音量")
	}
	button := ui.Column(f).Width(84).Gap(6).Items(ui.Center).HitWith(id, label, options)
	button.Children(func() {
		circle := ui.Row(f).Size(diameter, diameter).Radius(diameter / 2).Background(background).
			Items(ui.Center).Justify(ui.Center)
		if f.Canvas().FocusVisible() == id || f.Canvas().Hovered() == id {
			circle = circle.Stroke(2, u.theme.Ring)
		}
		circle.Children(func() {
			ui.Icon(f, icon).Size(22, 22).Color(foreground)
		})
		ui.Text(f, label).FillWidth().FontSize(textSize(f, 11.5)).Weight(gfx.WeightMedium).Color(labelColor).TextCenter().MaxLines(2)
	})
	return button
}

func (u *UI) videoCallStatus(call domain.VoiceCall) string {
	switch call.Status {
	case domain.VoiceCallActive:
		if call.PeerCameraOff {
			return i18n.T("对方已关闭摄像头")
		}
		return i18n.T("正在等待对方画面…")
	case domain.VoiceCallEnded:
		return i18n.T("通话已结束")
	case domain.VoiceCallRinging:
		if call.Direction == domain.VoiceCallIncoming {
			return i18n.T("邀请你进行视频通话")
		}
		return i18n.T("等待对方接听…")
	default:
		return i18n.T("视频通话")
	}
}

func fitBitmapRect(bounds ui.Rect, width, height int) ui.Rect {
	if width <= 0 || height <= 0 || bounds.Width <= 0 || bounds.Height <= 0 {
		return bounds
	}
	scale := min(bounds.Width/float32(width), bounds.Height/float32(height))
	w, h := float32(width)*scale, float32(height)*scale
	return ui.Rect{
		X:      bounds.X + (bounds.Width-w)/2,
		Y:      bounds.Y + (bounds.Height-h)/2,
		Width:  w,
		Height: h,
	}
}

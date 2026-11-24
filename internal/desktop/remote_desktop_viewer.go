package desktop

import (
	"math"
	"slices"
	"strconv"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/style"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type remoteDesktopViewerPanel uint8

const (
	remoteDesktopViewerPanelNone remoteDesktopViewerPanel = iota
	remoteDesktopViewerPanelKeys
	remoteDesktopViewerPanelAudio
)

const remoteDesktopViewerMinTitlebarHeight = 50

var (
	remoteDesktopKeyboardIcon = viewerLineIcon(
		gfx.MoveTo(3, 5), gfx.LineTo(21, 5), gfx.QuadTo(23, 5, 23, 7), gfx.LineTo(23, 17),
		gfx.QuadTo(23, 19, 21, 19), gfx.LineTo(3, 19), gfx.QuadTo(1, 19, 1, 17), gfx.LineTo(1, 7),
		gfx.QuadTo(1, 5, 3, 5), gfx.ClosePath(),
		gfx.MoveTo(5, 9), gfx.LineTo(5, 9.2), gfx.MoveTo(9.5, 9), gfx.LineTo(9.5, 9.2),
		gfx.MoveTo(14, 9), gfx.LineTo(14, 9.2), gfx.MoveTo(18.5, 9), gfx.LineTo(18.5, 9.2),
		gfx.MoveTo(5, 13), gfx.LineTo(5, 13.2), gfx.MoveTo(18.5, 13), gfx.LineTo(18.5, 13.2),
		gfx.MoveTo(9, 15), gfx.LineTo(15, 15))
	remoteDesktopExitFullscreenIcon = viewerLineIcon(
		gfx.MoveTo(8, 3), gfx.LineTo(8, 8), gfx.LineTo(3, 8), gfx.MoveTo(16, 3), gfx.LineTo(16, 8), gfx.LineTo(21, 8),
		gfx.MoveTo(3, 16), gfx.LineTo(8, 16), gfx.LineTo(8, 21), gfx.MoveTo(21, 16), gfx.LineTo(16, 16), gfx.LineTo(16, 21))
	remoteDesktopStopIcon = viewerLineIcon(
		gfx.MoveTo(6, 5), gfx.LineTo(18, 5), gfx.QuadTo(19, 5, 19, 6), gfx.LineTo(19, 18),
		gfx.QuadTo(19, 19, 18, 19), gfx.LineTo(6, 19), gfx.QuadTo(5, 19, 5, 18), gfx.LineTo(5, 6),
		gfx.QuadTo(5, 5, 6, 5), gfx.ClosePath())
)

type remoteDesktopViewer struct {
	view    *remoteDesktopView
	backend domain.RemoteDesktopBackend
	frameID gfx.ID
	stop    chan struct{}
	pending atomic.Pointer[domain.RemoteDesktopFrame]
	bitmap  gfx.Bitmap

	input          app.TextInput
	controls       remoteDesktopViewerPanel
	titlebarHeight float32
	captured       bool
	buttons        uint8
	fullscreen     bool
	fullscreenFail bool
	inputFail      bool
	volume         ui.SliderState
	volumeChanged  bool
	muted          bool
	device         string
	devices        []string
}

func newRemoteDesktopViewer(view *remoteDesktopView, backend domain.RemoteDesktopBackend) *remoteDesktopViewer {
	v := &remoteDesktopViewer{
		view: view, backend: backend,
		frameID: view.tree.ID(view.tree.Feature("remote-desktop-viewer"), "screen"),
		stop:    make(chan struct{}),
		volume:  ui.SliderState{Value: 100},
	}
	v.setDevices(view.session.OutputDevices)
	v.input.KeyReleases, v.input.DisableIME, v.input.HideNativeOverlays = true, true, true
	// Text composition stays on the shared computer. Never convert local
	// clipboard paste or a text-service commit into remote keystrokes.
	v.input.OnEvent = func(app.TextEvent) (app.TextState, error) { return app.TextState{}, nil }
	return v
}

func (v *remoteDesktopViewer) configure(config *app.Config) {
	config.Title = v.view.viewerTitle()
	config.Width, config.Height = 1050, 680
	config.MinWidth, config.MinHeight = 640, 420
	config.WindowMode = app.WindowCustomTitlebar
	v.titlebarHeight = max(remoteDesktopViewerMinTitlebarHeight, v.view.theme.Metrics.ScaledText(26)+20)
	config.TitlebarHeight = v.titlebarHeight
	config.ClearColor = colorToApp(v.view.theme.Card)
	config.WindowBorderColor = func() app.Color { return colorToApp(v.view.theme.Card) }
	config.WindowControls = func() app.WindowControlsStyle {
		theme := v.view.theme
		return windowControlsFor(palette{icon: theme.MutedForeground, hover: theme.Hover, selected: theme.Accent})
	}
	config.OnWake = v.nextFrame
	config.Paint = v.paint
	config.OnPointer, config.OnScroll, config.OnKey = v.pointer, v.scroll, v.key
	config.OnDeactivate = func() gfx.EventResult {
		return gfx.EventResult{Repaint: v.releaseInput()}
	}
	config.OnMinimize = func(minimized bool) {
		if minimized && v.releaseInput() {
			v.view.wake.Invalidate()
		}
	}
	config.OnFullscreen = func(active bool) {
		v.fullscreen, v.fullscreenFail = active, false
	}
	config.TextInput = func(context app.TextInputContext) *app.TextInput {
		if context.ID == v.frameID && v.captured && v.view.session.Control {
			v.input.Bounds = context.Bounds
			return &v.input
		}
		return v.view.page.TextInput(context)
	}
}

func (v *remoteDesktopViewer) start() {
	frames := v.backend.RemoteDesktopFrames(v.view.session.ID)
	if frames == nil {
		return
	}
	go func() {
		for {
			select {
			case <-v.stop:
				return
			case frame, ok := <-frames:
				if !ok {
					return
				}
				if validRemoteDesktopFrame(frame) {
					v.pending.Store(&frame)
					v.view.wake.Invalidate()
				}
			}
		}
	}()
}

func (v *remoteDesktopViewer) close() {
	close(v.stop)
	v.pending.Store(nil)
	v.bitmap = gfx.Bitmap{}
}

func validRemoteDesktopFrame(frame domain.RemoteDesktopFrame) bool {
	return frame.Width > 0 && frame.Width <= 3840 &&
		frame.Height > 0 && frame.Height <= 2160 &&
		len(frame.BGRA) == frame.Width*frame.Height*4
}

func (v *remoteDesktopViewer) nextFrame() {
	frame := v.pending.Swap(nil)
	if frame == nil {
		return
	}
	if frame.Width != v.bitmap.Width || frame.Height != v.bitmap.Height {
		v.releaseInput()
	}
	v.bitmap = gfx.Bitmap{
		ID: 1, Version: v.bitmap.Version + 1, Pix: frame.BGRA,
		Width: frame.Width, Height: frame.Height, Stride: frame.Width * 4,
	}
}

func (v *remoteDesktopViewer) toolPanelHeight() float32 {
	return 46 + max(0, v.view.theme.Metrics.ScaledText(14)-14)
}

func (v *remoteDesktopViewer) toolbarHeight() float32 {
	if v.controls == remoteDesktopViewerPanelNone {
		return v.titlebarHeight
	}
	return v.titlebarHeight + v.toolPanelHeight()
}

func (v *remoteDesktopViewer) stage(size gfx.Size) ui.Rect {
	height := v.toolbarHeight()
	return ui.Rect{Y: height, Width: size.Width, Height: max(0, size.Height-height)}
}

func (v *remoteDesktopViewer) imageRect(size gfx.Size) ui.Rect {
	area := v.stage(size)
	if !bitmapReady(v.bitmap) || area.Width <= 0 || area.Height <= 0 {
		return ui.Rect{}
	}
	scale := min(area.Width/float32(v.bitmap.Width), area.Height/float32(v.bitmap.Height))
	width, height := float32(v.bitmap.Width)*scale, float32(v.bitmap.Height)*scale
	return ui.Rect{X: area.X + (area.Width-width)/2, Y: area.Y + (area.Height-height)/2, Width: width, Height: height}
}

func (v *remoteDesktopViewer) paint(canvas *gfx.Canvas, size gfx.Size) {
	area := v.stage(size)
	canvas.Fill(area, ui.RGB(22, 24, 29))
	if image := v.imageRect(size); image.Width > 0 {
		canvas.DrawBitmap(v.bitmap, image)
		if v.view.session.Control {
			canvas.HitWithOptions(v.frameID, image, i18n.T("远程屏幕"), gfx.HitOptions{
				Role: gfx.RoleTerminal, Hint: i18n.T("点击画面以控制。按 Ctrl+Shift+F12 释放键鼠。"),
			})
		}
	} else {
		canvas.LabelCenter(i18n.T("正在等待远程画面…"), area, gfx.TextStyle{Size: 14, Color: ui.RGB(221, 225, 230)})
	}
	toolbar := ui.Rect{Width: size.Width, Height: v.toolbarHeight()}
	canvas.Fill(toolbar, v.view.theme.Card)
	if v.controls != remoteDesktopViewerPanelNone {
		canvas.Fill(ui.Rect{Y: v.titlebarHeight, Width: toolbar.Width, Height: 1}, v.view.theme.Border)
	}
	canvas.Fill(ui.Rect{Y: toolbar.Bottom() - 1, Width: toolbar.Width, Height: 1}, v.view.theme.Border)
	leading, trailing := canvas.TitlebarInsets()
	if v.fullscreen {
		leading, trailing = 0, 0
	}
	available := size.Width - leading - trailing - 24
	compactWidth := 610 + 10*max(0, v.view.theme.Metrics.ScaledText(14)-14)
	v.view.page.Draw(canvas, toolbar, func(f ui.C) {
		ui.Column(f).Fill().Children(func() {
			v.drawTitlebar(f, leading, trailing, available, available < compactWidth)
			switch v.controls {
			case remoteDesktopViewerPanelKeys:
				v.drawShortcuts(f, size.Width < compactWidth)
			case remoteDesktopViewerPanelAudio:
				v.drawAudio(f)
			}
		})
	})
}

func (v *remoteDesktopViewer) drawTitlebar(f ui.C, leading, trailing, available float32, compact bool) {
	theme := v.view.theme
	accent := theme.Primary
	if theme.Card.Red < .5 {
		accent = accent.Mix(theme.Foreground, .5)
	}
	ui.Row(f).FillWidth().Height(v.titlebarHeight).MovesWindow().
		PadSides(0, trailing+12, 0, leading+12).Gap(10).Items(ui.Center).Children(func() {
		if available >= 345 {
			ui.Row(f).Size(32, 32).Shrink(0).Radius(9).Background(accent.Mix(theme.Card, .86)).
				Center().Children(func() {
				ui.Icon(f, remoteDesktopIcon).Size(19, 19).Color(accent)
			})
		}
		ui.Column(f).Grow(1).MinWidth(0).Gap(2).Children(func() {
			// The native caption height stays fixed while this window is open.
			// Fit both lines if the font setting changes after it opens.
			titleSize := min(textSize(f, 14), v.titlebarHeight*.4)
			statusSize := min(textSize(f, 11), v.titlebarHeight*.3)
			ui.Text(f, v.view.peerName).FillWidth().Weight(gfx.WeightSemibold).
				FontSize(titleSize).Ellipsis()
			label, statusColor := v.status()
			ui.Row(f).FillWidth().Gap(6).Items(ui.Center).Children(func() {
				ui.Row(f).Size(6, 6).Radius(3).Shrink(0).Background(statusColor)
				ui.Text(f, label).Grow(1).MinWidth(0).FontSize(statusSize).Muted().Ellipsis()
			})
		})
		ui.Row(f).Gap(5).Shrink(0).Items(ui.Center).Children(func() {
			v.drawTitlebarActions(f, compact)
		})
		if trailing > 0 && available >= 345 {
			ui.Row(f).Size(1, 22).Shrink(0).Background(theme.Border)
		}
	})
}

func (v *remoteDesktopViewer) status() (string, ui.Color) {
	theme := v.view.theme
	switch {
	case v.fullscreenFail:
		return i18n.T("当前窗口无法切换全屏"), theme.Danger
	case v.inputFail:
		return i18n.T("未能发送远程控制指令"), theme.Danger
	case v.captured:
		return i18n.T("正在控制"), paletteFor(theme, "").success
	case v.view.session.Control:
		return i18n.T("点击画面以控制"), theme.Primary
	default:
		return i18n.T("仅查看"), theme.MutedForeground
	}
}

func (v *remoteDesktopViewer) drawTitlebarActions(f ui.C, compact bool) {
	if v.view.session.Control {
		button := remoteDesktopToolButton(f.Part("shortcuts"), i18n.T("快捷键"), remoteDesktopKeyboardIcon, compact)
		if v.controls == remoteDesktopViewerPanelKeys {
			button = button.Secondary()
		}
		if button.Clicked() {
			v.togglePanel(remoteDesktopViewerPanelKeys)
		}
	}
	if v.view.session.Audio {
		icon := Volume2
		if v.muted {
			icon = VolumeX
		}
		button := remoteDesktopToolButton(f.Part("audio"), i18n.T("声音"), icon, compact)
		if v.controls == remoteDesktopViewerPanelAudio {
			button = button.Secondary()
		}
		if button.Clicked() {
			v.togglePanel(remoteDesktopViewerPanelAudio)
		}
	}
	fullscreen, icon := i18n.T("全屏"), viewerFullscreenIcon
	if v.fullscreen {
		fullscreen, icon = i18n.T("退出全屏"), remoteDesktopExitFullscreenIcon
	}
	if remoteDesktopToolButton(f.Part("fullscreen"), fullscreen, icon, compact).Clicked() && v.view.handle != nil {
		f.After(func() {
			v.fullscreenFail = v.view.handle.SetFullscreen(!v.fullscreen) != nil
			v.view.wake.Invalidate()
		})
	}
	end := i18n.T("结束协助")
	if remoteDesktopToolButton(f, end, remoteDesktopStopIcon, compact).
		Hit(v.view.stopID, end).Variant(style.VariantDefault).ButtonRecipe(remoteDesktopCancelLook).Clicked() {
		f.After(v.view.stop)
	}
}

func remoteDesktopToolButton(f ui.C, label string, icon gfx.Icon, compact bool) ui.Box {
	button := ui.Button(f, label).Icon(icon).Small().Height(34).Ghost().Tooltip(label)
	if compact {
		return button.IconOnly().Size(36, 34)
	}
	return button
}

func (v *remoteDesktopViewer) togglePanel(panel remoteDesktopViewerPanel) {
	if v.controls == panel {
		v.controls = remoteDesktopViewerPanelNone
	} else {
		v.controls = panel
	}
}

func (v *remoteDesktopViewer) drawShortcuts(f ui.C, compact bool) {
	ui.Row(f).FillWidth().Height(v.toolPanelHeight()).PadX(12).Gap(8).Items(ui.Center).Children(func() {
		if remoteDesktopToolButton(f.Part("switch-app"), i18n.T("切换远端应用"), ArrowRightLeft, compact).Outline().Clicked() {
			f.After(func() { v.send(domain.RemoteDesktopInput{Action: "shortcut", Key: "switch-app"}) })
		}
		if remoteDesktopToolButton(f.Part("close-window"), i18n.T("关闭远端窗口"), X, compact).Outline().Clicked() {
			f.After(func() { v.send(domain.RemoteDesktopInput{Action: "shortcut", Key: "close-window"}) })
		}
		if remoteDesktopToolButton(f.Part("release-input"), i18n.T("释放键鼠"), Lock, compact).Outline().Clicked() {
			f.After(func() { v.releaseInput(); v.view.wake.Invalidate() })
		}
		ui.Text(f, "Ctrl+Shift+F12").Grow(1).MinWidth(0).Muted().Ellipsis()
	})
}

func (v *remoteDesktopViewer) drawAudio(f ui.C) {
	ui.Row(f).FillWidth().Height(v.toolPanelHeight()).PadX(12).Gap(10).Items(ui.Center).Children(func() {
		label := i18n.T("静音")
		if v.muted {
			label = i18n.T("取消静音")
		}
		if ui.Button(f.Part("mute"), label).Small().Outline().Clicked() {
			v.muted = !v.muted
			f.After(v.updateAudio)
		}
		if ui.Slider(f.Part("volume"), &v.volume).Range(0, 100).Step(1).Grow(1).Label(i18n.T("扬声器音量")).Changed() {
			v.volumeChanged = true
		}
		if v.volumeChanged && !v.volume.Dragging() {
			v.volumeChanged = false
			f.After(v.updateAudio)
		}
		ui.Text(f, strconv.Itoa(int(v.volume.Value))+"%").Width(max(42, textSize(f, 34))).Muted()
		if len(v.devices) > 1 {
			width := max(180, textSize(f, 124))
			if ui.Select(f.Part("audio-device"), &v.device, v.devices, remoteDesktopSpeakerLabel).
				Width(width).Label(i18n.T("扬声器")).Changed() {
				f.After(v.updateAudio)
			}
		} else {
			ui.Text(f, i18n.T("系统默认")).Muted()
		}
	})
}

func remoteDesktopSpeakerLabel(device string) string {
	if device == "" {
		return i18n.T("系统默认")
	}
	return device
}

func (v *remoteDesktopViewer) setDevices(names []string) {
	v.devices = append([]string{""}, names...)
	if !slices.Contains(v.devices, v.device) {
		v.device = ""
		v.updateAudio()
	}
}

func (v *remoteDesktopViewer) updateAudio() {
	if v.view.session.Audio {
		v.send(domain.RemoteDesktopInput{
			Action: "audio", Muted: v.muted, Volume: int(v.volume.Value), Device: v.device,
		})
	}
}

func (v *remoteDesktopViewer) send(input domain.RemoteDesktopInput) bool {
	err := v.backend.SendRemoteDesktopInput(v.view.session.ID, input)
	if failed := err != nil; v.inputFail != failed {
		v.inputFail = failed
		v.view.wake.Invalidate()
	}
	return err == nil
}

func (v *remoteDesktopViewer) releaseInput() bool {
	if !v.captured {
		return false
	}
	v.captured, v.buttons = false, 0
	v.send(domain.RemoteDesktopInput{Action: "release"})
	return true
}

func (v *remoteDesktopViewer) pointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind == gfx.PointerDown && event.Target != v.frameID {
		released := v.releaseInput()
		result := v.view.page.Pointer(event)
		result.Repaint = result.Repaint || released
		return result
	}
	if !v.view.session.Control || event.Target != v.frameID && v.buttons == 0 {
		return v.view.page.Pointer(event)
	}

	image := v.imageRect(event.Viewport)
	if image.Width == 0 {
		return gfx.EventResult{}
	}
	input := domain.RemoteDesktopInput{
		X: remoteDesktopCoordinate(event.X-image.X, image.Width),
		Y: remoteDesktopCoordinate(event.Y-image.Y, image.Height),
	}
	switch event.Kind {
	case gfx.PointerDown:
		input.Button = remoteDesktopButton(event.Button)
		if input.Button == 0 || !pointInside(event.X, event.Y, image) {
			return gfx.EventResult{}
		}
		input.Action, input.Down = "button", true
		if !v.send(input) {
			return gfx.EventResult{Handled: true}
		}
		v.buttons |= 1 << input.Button
		v.captured = true
		return gfx.EventResult{Handled: true, Focus: v.frameID, Repaint: true}
	case gfx.PointerUp:
		input.Button = remoteDesktopButton(event.Button)
		if input.Button == 0 || v.buttons&(1<<input.Button) == 0 {
			return gfx.EventResult{}
		}
		input.Action = "button"
		v.buttons &^= 1 << input.Button
		v.send(input)
	case gfx.PointerMove:
		if !v.captured {
			return gfx.EventResult{}
		}
		input.Action = "move"
		v.send(input)
	}
	return gfx.EventResult{Handled: true}
}

func remoteDesktopButton(button gfx.PointerButton) int {
	switch button {
	case gfx.PointerButtonPrimary:
		return 1
	case gfx.PointerButtonSecondary:
		return 2
	case gfx.PointerButtonMiddle:
		return 3
	default:
		return 0
	}
}

func remoteDesktopCoordinate(offset, extent float32) int {
	return int(math.Round(float64(min(1, max(0, offset/extent))) * 65535))
}

func (v *remoteDesktopViewer) scroll(event gfx.ScrollEvent) gfx.EventResult {
	if !v.view.session.Control || !v.captured || event.Target != v.frameID {
		return v.view.page.Wheel(event)
	}
	x, y := remoteDesktopWheel(event.DeltaX), remoteDesktopWheel(event.DeltaY)
	if x != 0 || y != 0 {
		image := v.imageRect(event.Viewport)
		if image.Width > 0 {
			v.send(domain.RemoteDesktopInput{Action: "wheel", DeltaX: x, DeltaY: y,
				X: remoteDesktopCoordinate(event.X-image.X, image.Width), Y: remoteDesktopCoordinate(event.Y-image.Y, image.Height),
			})
		}
	}
	return gfx.EventResult{Handled: true}
}

func remoteDesktopWheel(delta float32) int {
	return int(math.Round(float64(min(120, max(-120, delta)))))
}

func (v *remoteDesktopViewer) key(event gfx.KeyEvent) gfx.EventResult {
	if !v.captured || !v.view.session.Control || event.Target != v.frameID {
		released := v.captured && event.Target != v.frameID && v.releaseInput()
		result := v.view.page.Key(event)
		result.Repaint = result.Repaint || released
		return result
	}
	if event.Key == gfx.KeyF12 && event.Modifiers == gfx.ModifierControl|gfx.ModifierShift && !event.Release {
		v.releaseInput()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: v.view.stopID}
	}
	if key := remoteDesktopKey(event); key != "" {
		v.send(domain.RemoteDesktopInput{Action: "key", Key: key, Modifiers: int(event.Modifiers), Down: !event.Release})
	}
	return gfx.EventResult{Handled: true}
}

var remoteDesktopNamedKeys = [...]string{
	gfx.KeyTab: "tab", gfx.KeyEnter: "enter", gfx.KeySpace: "space", gfx.KeyEscape: "escape",
	gfx.KeyLeft: "left", gfx.KeyRight: "right", gfx.KeyUp: "up", gfx.KeyDown: "down",
	gfx.KeyHome: "home", gfx.KeyEnd: "end", gfx.KeyBackspace: "backspace", gfx.KeyDelete: "delete",
	gfx.KeyPageUp: "pageup", gfx.KeyPageDown: "pagedown", gfx.KeyInsert: "insert",
	gfx.KeyF1: "f1", gfx.KeyF2: "f2", gfx.KeyF3: "f3", gfx.KeyF4: "f4",
	gfx.KeyF5: "f5", gfx.KeyF6: "f6", gfx.KeyF7: "f7", gfx.KeyF8: "f8",
	gfx.KeyF9: "f9", gfx.KeyF10: "f10", gfx.KeyF11: "f11", gfx.KeyF12: "f12",
	gfx.KeyShiftLeft: "shift-left", gfx.KeyShiftRight: "shift-right",
	gfx.KeyControlLeft: "control-left", gfx.KeyControlRight: "control-right",
	gfx.KeyAltLeft: "alt-left", gfx.KeyAltRight: "alt-right",
	gfx.KeyMetaLeft: "meta-left", gfx.KeyMetaRight: "meta-right",
}

func remoteDesktopKey(event gfx.KeyEvent) string {
	if event.Key > gfx.KeyUnknown && int(event.Key) < len(remoteDesktopNamedKeys) {
		return remoteDesktopNamedKeys[event.Key]
	}
	// Use the unmodified character for both the press and its matching release.
	r, width := utf8.DecodeRuneInString(event.Text)
	if width == len(event.Text) && width > 0 && r != utf8.RuneError && unicode.IsPrint(r) {
		return event.Text
	}
	return ""
}

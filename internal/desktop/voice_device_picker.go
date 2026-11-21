package desktop

import (
	"fmt"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
	voiceaudio "goim/internal/voice"
)

// Each opening re-enumerates the devices, including headsets attached mid-call.
// A slow driver never blocks the window or holds the device selection open.
type callDevicePicker struct {
	kind      string
	devices   voiceaudio.Devices
	loading   <-chan voiceaudio.Devices
	enumerate func() voiceaudio.Devices
	choices   []panelVoiceChoice
	selected  int
	device    string
	level     panelVoiceLevelState
}

func (u *UI) openCallDevicePicker(kind string) {
	p := &u.voicePicker
	p.kind, p.choices, p.level = kind, nil, panelVoiceLevelState{}
	results := make(chan voiceaudio.Devices, 1)
	p.loading = results
	enumerate := p.enumerate
	if enumerate == nil {
		enumerate = voiceaudio.Enumerate
	}
	go func() {
		devices := enumerate()
		select {
		case <-u.done:
		case results <- devices:
			u.invalidator.Invalidate()
		}
	}()
	u.invalidator.Invalidate()
}

func (p *callDevicePicker) collect() {
	select {
	case devices := <-p.loading:
		p.devices, p.loading, p.choices = devices, nil, nil
	default:
	}
}

func (u *UI) voiceKey(event gfx.KeyEvent) gfx.EventResult {
	if result := u.dialogPage.Key(event); result.Handled {
		return result
	}
	if event.Release || !u.voiceVisible() || u.voiceConcealed || u.state.Locked {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyF10 && event.Modifiers == gfx.ModifierShift {
		switch event.Target {
		case u.ids.voiceMute:
			u.openCallDevicePicker("麦克风")
		case u.ids.voiceSpeaker:
			u.openCallDevicePicker("扬声器")
		default:
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Key == gfx.KeyEscape && u.voicePicker.kind != "" {
		return u.closeCallDevicePicker()
	}
	return gfx.EventResult{}
}

func (u *UI) closeCallDevicePicker() gfx.EventResult {
	focus := u.ids.voiceMute
	if u.voicePicker.kind == "扬声器" {
		focus = u.ids.voiceSpeaker
	}
	u.voicePicker.kind = ""
	return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
}

func (u *UI) drawCallDevicePicker(canvas *gfx.Canvas, viewport ui.Rect) {
	p := &u.voicePicker
	call := u.state.VoiceCall
	settings := call.AudioSettings(u.state.Settings)
	device, volume := settings.VoiceMicrophone, settings.VoiceMicrophoneVolume
	devices := p.devices.Microphones
	setDevice, setVolume := domain.SetCallMicrophone, domain.SetCallMicrophoneVolume
	if p.kind == "扬声器" {
		device, volume = settings.VoiceSpeaker, settings.VoiceSpeakerVolume
		devices = p.devices.Speakers
		setDevice, setVolume = domain.SetCallSpeaker, domain.SetCallSpeakerVolume
	}
	if p.choices == nil || p.device != device {
		p.choices, p.selected = u.panels.voiceDeviceChoices(device, devices, p.kind)
		p.device = device
	}
	level := &p.level
	if level.pendingSet && level.pending == volume {
		level.pendingSet = false
	}
	if !level.pendingSet && !level.dirty && !level.slider.Dragging() {
		level.slider.Value = float32(volume)
	}
	u.dialogPage.Draw(canvas, viewport, func(f ui.C) {
		ui.Column(f).Fill().Pad(24).Gap(14).Children(func() {
			ui.Text(f, i18n.T(p.kind)).FontSize(textSize(f, 20)).Weight(gfx.WeightSemibold)
			ui.Text(f, i18n.T("仅用于本次通话")).Muted()
			choice := p.choices[p.selected]
			if ui.Select(f.Part("call-device"), &choice, p.choices, panelVoiceChoice.title).
				FillWidth().Label(i18n.T(p.kind)).Changed() {
				u.submit(domain.Command{Kind: setDevice, CallID: call.ID, Text: choice.value})
			}
			if p.loading != nil {
				ui.Text(f, i18n.T("正在读取音频设备…")).Muted()
			} else if p.devices.Problem != "" {
				ui.Text(f, i18n.T(p.devices.Problem)).Muted()
			}
			title := i18n.T(p.kind + "音量")
			ui.Text(f, title)
			ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				slider := ui.Slider(f.Part("call-volume"), &level.slider).Range(0, domain.VoiceVolumeMaximum).Step(1).Grow(1).Label(title)
				if slider.Changed() {
					level.dirty = true
				}
				if level.dirty && !level.slider.Dragging() {
					value := int(level.slider.Value)
					level.pending, level.pendingSet, level.dirty = value, true, false
					u.submit(domain.Command{Kind: setVolume, CallID: call.ID, Text: strconv.Itoa(value)})
				}
				ui.Text(f, fmt.Sprintf("%d%%", int(level.slider.Value))).Width(42).Muted()
			})
			ui.Spacer(f)
			if ui.Button(f, i18n.T("返回通话")).FillWidth().Clicked() {
				result := u.closeCallDevicePicker()
				canvas.RequestFocus(result.Focus)
				u.invalidator.Invalidate()
			}
		})
	})
}

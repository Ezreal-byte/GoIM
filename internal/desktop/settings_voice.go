package desktop

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
	voiceaudio "goim/internal/voice"
)

type panelVoiceDeviceState struct {
	known      bool
	refreshing bool
	devices    voiceaudio.Devices
	enumerate  func() voiceaudio.Devices
}

type panelVoiceChoice struct {
	value string
	label string
}

type panelVoiceLevelState struct {
	slider     ui.SliderState
	dirty      bool
	pending    int
	pendingSet bool
}

func (choice panelVoiceChoice) title() string { return choice.label }

func (p *Panels) drawSettingsVoice(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	devices := &p.settingsExtra.voiceDevices
	panelSection(f, i18n.T("通话设备"), i18n.T("设备在通话接通后打开；设备修改从下一次通话生效，音量会立即生效。"), func() {
		p.settingsRow(f, theme, i18n.T("麦克风"), p.voiceDeviceSummary(devices.devices.Microphones, "麦克风"), func() {
			p.settingsControl(f, 340, func() {
				choices, selected := p.voiceDeviceChoices(settings.VoiceMicrophone, devices.devices.Microphones, "麦克风")
				choice := choices[selected]
				if ui.Select(f.Part("voice-microphone"), &choice, choices, panelVoiceChoice.title).
					FillWidth().Label(i18n.T("麦克风")).
					Hint(i18n.T("系统默认会跟随操作系统的默认输入设备。")).
					Changed() {
					p.submitString(submit, state, "voice_microphone", choice.value)
				}
			})
		})
		p.drawVoiceVolume(f, theme, state, submit,
			i18n.T("麦克风音量"), i18n.T("调整送入通话的本机声音大小"),
			"voice_microphone_volume", settings.VoiceMicrophoneVolume, &p.settingsExtra.voiceMicrophoneLevel)
		p.settingsRow(f, theme, i18n.T("扬声器"), p.voiceDeviceSummary(devices.devices.Speakers, "扬声器"), func() {
			p.settingsControl(f, 340, func() {
				choices, selected := p.voiceDeviceChoices(settings.VoiceSpeaker, devices.devices.Speakers, "扬声器")
				choice := choices[selected]
				if ui.Select(f.Part("voice-speaker"), &choice, choices, panelVoiceChoice.title).
					FillWidth().Label(i18n.T("扬声器")).
					Hint(i18n.T("系统默认会跟随操作系统的默认输出设备。")).
					Changed() {
					p.submitString(submit, state, "voice_speaker", choice.value)
				}
			})
		})
		p.drawVoiceVolume(f, theme, state, submit,
			i18n.T("扬声器音量"), i18n.T("调整对方声音在本机的播放大小"),
			"voice_speaker_volume", settings.VoiceSpeakerVolume, &p.settingsExtra.voiceSpeakerLevel)
		p.settingsRow(f, theme, i18n.T("设备列表"), p.voiceDevicesStatus(), func() {
			p.settingsControl(f, 160, func() {
				label := i18n.T("刷新设备")
				if devices.refreshing {
					label = i18n.T("正在刷新…")
				}
				if namedButton(f, p.ids.settingsRefreshVoice, label).Small().Outline().
					Disabled(devices.refreshing).Clicked() {
					devices.refreshing = true
					f.After(p.refreshVoiceDevices)
				}
			})
		})
	})
	if state.VoiceCall.Live() {
		ui.Row(f).FillWidth().PadSides(10, 12, 10, 12).Gap(10).Items(ui.Center).
			Background(panelTint(theme.Primary, 0.06, theme.Background)).
			Border(1, panelTint(theme.Primary, 0.20, theme.Background)).Radius(8).Children(func() {
			ui.Paragraph(f, i18n.T("在通话窗口右键点击麦克风或扬声器，可以临时调整本次通话。"), 2).
				FontSize(theme.Metrics.FontSizeS + 1).Grow(1)
		})
	}
}

func (p *Panels) drawVoiceVolume(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command), title, description, setting string, current int, level *panelVoiceLevelState) {
	p.settingsRow(f, theme, title, description, func() {
		p.settingsControl(f, 340, func() {
			current = domain.ClampVoiceVolume(current)
			if !level.slider.Dragging() && !level.dirty {
				if level.pendingSet {
					if current == level.pending {
						level.pendingSet = false
					}
				} else {
					level.slider.Value = float32(current)
				}
			}
			ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				slider := ui.Slider(f, &level.slider).Range(0, domain.VoiceVolumeMaximum).Step(1).Grow(1).
					Label(title)
				if slider.Changed() {
					level.dirty = true
				}
				if level.dirty && !level.slider.Dragging() {
					value := int(level.slider.Value)
					level.pending, level.pendingSet, level.dirty = value, true, false
					p.submitString(submit, state, setting, fmt.Sprintf("%d", value))
				}
				ui.Text(f, fmt.Sprintf("%d%%", int(level.slider.Value))).FontSize(textSize(f, 12)).Muted().Width(40)
			})
		})
	})
}

func (p *Panels) refreshVoiceDevices() {
	devices := &p.settingsExtra.voiceDevices
	devices.refreshing = true
	enumerate := devices.enumerate
	if enumerate == nil {
		enumerate = voiceaudio.Enumerate
	}
	devices.devices = enumerate()
	devices.known = true
	devices.refreshing = false
	p.invalidateAfterVoiceDevices()
}

func (p *Panels) invalidateAfterVoiceDevices() {
	if p.invalidate != nil {
		p.invalidate()
	}
}

func (p *Panels) voiceDeviceChoices(current string, devices []voiceaudio.Device, kind string) ([]panelVoiceChoice, int) {
	current = strings.TrimSpace(current)
	choices := make([]panelVoiceChoice, 1, len(devices)+2)
	choices[0] = panelVoiceChoice{label: i18n.T("系统默认")}
	if device, ok := voiceDefaultDevice(devices); ok {
		choices[0].label += " · " + voiceDeviceLabel(device.Name)
	}
	found := current == ""
	if !found {
		for _, device := range devices {
			if device.ID == current {
				found = true
				break
			}
		}
	}
	for _, device := range devices {
		choices = append(choices, panelVoiceChoice{
			value: device.ID,
			label: voiceDeviceLabel(device.Name),
		})
	}
	if !found {
		choices = append(choices, panelVoiceChoice{
			value: current,
			label: fmt.Sprintf(i18n.T("已保存的%s（当前未检测到） · %s"), i18n.T(kind), voiceDeviceLabel(current)),
		})
	}
	for index, choice := range choices {
		if choice.value == current {
			return choices, index
		}
	}
	return choices, 0
}

func voiceDefaultDevice(devices []voiceaudio.Device) (voiceaudio.Device, bool) {
	for _, device := range devices {
		if device.Default {
			return device, true
		}
	}
	return voiceaudio.Device{}, false
}

func voiceDeviceLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return i18n.T("未命名设备")
	}
	const maxRunes = 64
	runes := []rune(name)
	if len(runes) <= maxRunes {
		return name
	}
	return string(runes[:maxRunes-1]) + "…"
}

func (p *Panels) voiceDeviceSummary(devices []voiceaudio.Device, kind string) string {
	state := &p.settingsExtra.voiceDevices
	switch {
	case !state.known || state.refreshing:
		return i18n.T("正在读取音频设备…")
	case state.devices.Problem != "":
		return i18n.T(state.devices.Problem)
	case len(devices) == 0:
		return fmt.Sprintf(i18n.T("未检测到可用的%s；系统默认仍可能可用"), i18n.T(kind))
	default:
		return fmt.Sprintf(i18n.T("检测到 %d 个%s；系统默认会跟随系统设备切换"), len(devices), i18n.T(kind))
	}
}

func (p *Panels) voiceDevicesStatus() string {
	state := &p.settingsExtra.voiceDevices
	if !state.known || state.refreshing {
		return i18n.T("正在读取系统音频设备…")
	}
	if state.devices.Problem != "" {
		return i18n.T(state.devices.Problem)
	}
	return i18n.T("刷新后重新读取当前连接的麦克风和扬声器")
}

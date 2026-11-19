package desktop

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
	"goim/internal/i18n"
	voiceaudio "goim/internal/voice"
)

func TestVoiceDeviceSettingsSelectMicrophoneAndSpeaker(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.LastSection = "settings"
	h := panelTestNewHarness(t, panelModuleSettings, state)
	h.panels.settingsExtra.voiceDevices.enumerate = func() voiceaudio.Devices {
		return voiceaudio.Devices{
			Microphones: []voiceaudio.Device{
				{ID: "mic:builtin", Name: "Built-in Microphone", Default: true},
				{ID: "mic:studio", Name: "Studio Microphone"},
			},
			Speakers: []voiceaudio.Device{
				{ID: "speaker:display", Name: "Display Speakers"},
				{ID: "speaker:studio", Name: "Studio Monitors"},
			},
		}
	}
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsVoice)))
	deadline := time.Now().Add(2 * time.Second)
	for !h.panels.settingsExtra.voiceDevices.known && time.Now().Before(deadline) {
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			h.t.Fatal(err)
		}
	}
	if !h.panels.settingsExtra.voiceDevices.known {
		t.Fatal("voice device enumeration did not run")
	}
	if !h.hasText("Built-in Microphone") {
		t.Fatal("system-default microphone label does not name the detected default")
	}

	h.chooseSelect("麦克风", "Studio Microphone")
	command := h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "voice_microphone" || command.Value != "mic:studio" {
		t.Fatalf("microphone command = %+v", command)
	}

	h.chooseSelect("扬声器", "Studio Monitors")
	command = h.requireLastCommand()
	if command.Kind != domain.SetSetting || command.Setting != "voice_speaker" || command.Value != "speaker:studio" {
		t.Fatalf("speaker command = %+v", command)
	}
	h.requireLayout()
}

func TestVoiceDeviceSettingsRefreshIsExplicitAndCached(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleSettings, state)
	calls := 0
	h.panels.settingsExtra.voiceDevices.enumerate = func() voiceaudio.Devices {
		calls++
		return voiceaudio.Devices{
			Microphones: []voiceaudio.Device{{ID: "mic:one", Name: "Microphone One", Default: true}},
			Speakers:    []voiceaudio.Device{{ID: "speaker:one", Name: "Speaker One"}},
		}
	}
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsVoice)))
	deadline := time.Now().Add(2 * time.Second)
	for calls == 0 && time.Now().Before(deadline) {
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			h.t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("initial device enumerations = %d, want 1", calls)
	}
	for range 3 {
		if err := h.driver.Pump(); err != nil {
			h.t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("idle repaint enumerated devices again: %d calls", calls)
	}

	hit, ok := h.findHit(h.panels.ids.settingsRefreshVoice)
	if !ok {
		t.Fatal("refresh devices button is not visible")
	}
	h.clickHit(hit)
	deadline = time.Now().Add(2 * time.Second)
	for calls == 1 && time.Now().Before(deadline) {
		h.wake.Invalidate()
		if err := h.driver.Pump(); err != nil {
			h.t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("device enumerations after refresh = %d, want 2", calls)
	}
}

func TestVoiceSettingsAdjustMicrophoneAndSpeakerVolume(t *testing.T) {
	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleSettings, state)
	h.panels.settingsExtra.voiceDevices = panelVoiceDeviceState{known: true}
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsVoice)))

	clickFraction := func(label string, fraction float32) {
		t.Helper()
		hit, ok := h.findHitLabelRole(label, gfx.RoleSlider)
		if !ok {
			t.Fatalf("%s slider is not visible", label)
		}
		x := hit.Bounds.X + 8 + (hit.Bounds.Width-16)*fraction
		if err := h.driver.Click(x, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}

	clickFraction("麦克风音量", 0.25)
	command := h.requireLastCommand()
	microphone := h.panels.settingsExtra.voiceMicrophoneLevel.slider.Value
	if command.Kind != domain.SetSetting || command.Setting != "voice_microphone_volume" ||
		command.Value != fmt.Sprintf("%d", int(microphone)) || microphone < 20 || microphone > 30 {
		t.Fatalf("microphone volume command = %+v, slider = %.1f", command, microphone)
	}

	h.clearCommands()
	clickFraction("扬声器音量", 0.75)
	command = h.requireLastCommand()
	speaker := h.panels.settingsExtra.voiceSpeakerLevel.slider.Value
	if command.Kind != domain.SetSetting || command.Setting != "voice_speaker_volume" ||
		command.Value != fmt.Sprintf("%d", int(speaker)) || speaker < 70 || speaker > 80 {
		t.Fatalf("speaker volume command = %+v, slider = %.1f", command, speaker)
	}
	h.requireLayout()
}

func TestVoiceSettingsVolumeLabelsFitEnglishLayout(t *testing.T) {
	previous := i18n.Language()
	i18n.SetLanguage(i18n.English)
	t.Cleanup(func() { i18n.SetLanguage(previous) })

	state := panelTestDefaultState(t)
	h := panelTestNewHarness(t, panelModuleSettings, state)
	h.panels.settingsExtra.voiceDevices = panelVoiceDeviceState{known: true}
	h.click(h.panels.ids.settingsCategories.At(int(panelSettingsVoice)))
	for _, label := range []string{"Microphone volume", "Speaker volume"} {
		if _, ok := h.findHitLabelRole(label, gfx.RoleSlider); !ok {
			t.Fatalf("%s slider is not visible", label)
		}
	}
	h.requireLayout()
}

func TestVoiceDeviceSettingsShowsSavedMissingDevice(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Settings.VoiceMicrophone = "mic:removed"
	state.Settings.VoiceSpeaker = "speaker:removed"
	h := panelTestNewHarness(t, panelModuleSettings, state)
	h.panels.settingsExtra.voiceDevices = panelVoiceDeviceState{
		known: true,
		devices: voiceaudio.Devices{
			Microphones: []voiceaudio.Device{{ID: "mic:builtin", Name: "Built-in Microphone"}},
			Speakers:    []voiceaudio.Device{{ID: "speaker:display", Name: "Display Speakers"}},
		},
	}
	h.panels.settingsTab = panelSettingsVoice
	h.wake.Invalidate()
	if err := h.driver.Pump(); err != nil {
		h.t.Fatal(err)
	}

	microphone, ok := h.findHitLabelRole("麦克风", gfx.RoleCombobox)
	if !ok {
		t.Fatal("microphone selector is not visible")
	}
	h.clickHit(microphone)
	if !h.hasText("已保存的麦克风") {
		t.Fatal("missing saved microphone is not explained")
	}
}

func TestVoiceDeviceSummarySeparatesMicrophoneAndSpeakerAvailability(t *testing.T) {
	p := &Panels{}
	p.settingsExtra.voiceDevices = panelVoiceDeviceState{
		known: true,
		devices: voiceaudio.Devices{
			Speakers: []voiceaudio.Device{{ID: "speaker:display", Name: "Display Speakers"}},
		},
	}
	microphone := p.voiceDeviceSummary(nil, "麦克风")
	if want := fmt.Sprintf(i18n.T("未检测到可用的%s；系统默认仍可能可用"), i18n.T("麦克风")); microphone != want {
		t.Fatalf("microphone summary = %q, want %q", microphone, want)
	}
	speaker := p.voiceDeviceSummary(p.settingsExtra.voiceDevices.devices.Speakers, "扬声器")
	if want := fmt.Sprintf(i18n.T("检测到 %d 个%s；系统默认会跟随系统设备切换"), 1, i18n.T("扬声器")); speaker != want {
		t.Fatalf("speaker summary = %q, want %q", speaker, want)
	}
}

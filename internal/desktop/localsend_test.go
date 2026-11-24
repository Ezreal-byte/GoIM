package desktop

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	vivaheadless "github.com/vivaui/viva/headless"
	"goim/internal/domain"
)

func TestLocalSendModuleDisabledAndSettingsEntry(t *testing.T) {
	state := shellMinimalState(t)
	h := newShellTestHarness(t, state)
	h.clickID(h.ui.ids.navLocalSend)
	if h.ui.section != sectionLocalSend || !h.hasText("启用 LocalSend 兼容功能") {
		t.Fatal("disabled LocalSend view missing")
	}
	h.clickID(h.ui.tree.ID(h.ui.panels.localSend.scope, "open-settings"))
	h.settle()
	if h.ui.section != sectionSettings || h.ui.panels.settingsTab != panelSettingsNetwork {
		t.Fatal("network settings jump failed")
	}
	id := h.ui.tree.ID(h.ui.panels.localSend.scope, "enabled")
	h.clickID(id)
	if cmd := h.lastCommand(); cmd.Kind != domain.LocalSendConfigure || !cmd.LocalSend.Config.Enabled {
		t.Fatalf("invalid enable command: %+v", cmd)
	}
}

func TestLocalSendReceiveSelectionInClassicThemes(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			state := shellMinimalState(t)
			state.Settings.Theme = theme
			state.Settings.LastSection = sectionLocalSend
			state.Settings.LocalSend.Enabled = true
			state.LocalSend = domain.LocalSendStatus{Active: true, Alias: "Work PC", Offer: domain.LocalSendOffer{
				ID: "ls-request", From: "林晚的 iPhone", Files: []domain.LocalSendFile{{ID: "dir", Name: "项目资料/报价.pdf", Size: 558230}, {ID: "photo", Name: "会议现场.png", Size: 183991}},
			}}
			h := newShellTestHarnessAtSize(t, state, 760, 650)
			h.settle()
			if !h.hasText("林晚的 iPhone") || !h.hasText("会议现场.png") {
				t.Fatal("incoming file list incomplete")
			}
			for _, problem := range vivaheadless.CheckLayout(h.snapshot()) {
				if strings.Contains(fmt.Sprint(problem), "LocalSend") || strings.Contains(fmt.Sprint(problem), "接收所选") || strings.Contains(fmt.Sprint(problem), "会议现场") {
					t.Error(problem)
				}
			}
			h.clickLabel("选择 会议现场.png")
			h.clickID(h.ui.tree.ID(h.ui.panels.localSend.scope, "accept"))
			c := h.lastCommand()
			if c.Kind != domain.LocalSendAnswer || !c.Bool || c.TransferID != "ls-request" || !slices.Equal(c.IDs, []string{"dir"}) {
				t.Fatalf("unexpected approval: %+v", c)
			}
		})
	}
}

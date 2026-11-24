package desktop

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	vivaheadless "github.com/vivaui/viva/headless"
	"goim/internal/domain"
)

func TestLocalSendModuleDisabledAndSettingsEntry(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.LastSection = sectionLocalSend
	h := newShellTestHarness(t, state)
	if h.hasHit(h.ui.ids.navLocalSend) || h.ui.section == sectionLocalSend {
		t.Fatal("disabled module is visible or restored")
	}
	h.clickID(h.ui.ids.navSettings)
	h.clickID(h.ui.panels.ids.settingsCategories.At(int(panelSettingsNetwork)))
	id := h.ui.tree.ID(h.ui.panels.localSend.scope, "enabled")
	h.clickID(id)
	if cmd := h.lastCommand(); cmd.Kind != domain.LocalSendConfigure || !cmd.LocalSend.Config.Enabled {
		t.Fatalf("invalid enable command: %+v", cmd)
	}
	state.Revision++
	state.Settings.LocalSend.Enabled = true
	state.LocalSend = domain.LocalSendStatus{Active: true, Alias: "GoIM QA", Port: 53317}
	h.publish(state)
	if h.hasHit(h.ui.tree.ID(h.ui.panels.localSend.scope, "port")) {
		t.Fatal("module port still shown in network settings")
	}
	h.clickID(h.ui.ids.navLocalSend)
	settings := &h.ui.panels.localSend
	tree := &h.ui.tree
	h.clickID(tree.ID(settings.scope, "settings"))
	if settings.modal != localSendSettingsModal || h.ui.section != sectionLocalSend {
		t.Fatal("module settings did not open in place")
	}
	h.clickID(tree.ID(settings.scope, "receive-http"))
	h.typeInto(tree.ID(settings.scope, "receive-pin"), "4571")
	scope := tree.Sub(settings.scope, tree.Name("settings-dialog"))
	h.clickID(tree.ID(scope, "save"))
	cmd := h.lastCommand()
	if cmd.Kind != domain.LocalSendConfigure || !cmd.LocalSend.Config.HTTP || cmd.LocalSend.Config.Port != 53317 || !cmd.LocalSend.SetPIN || cmd.Password != "4571" {
		t.Fatalf("invalid save: %+v", cmd.LocalSend)
	}
	state.Revision++
	state.Settings.LocalSend.Enabled = false
	state.LocalSend = domain.LocalSendStatus{}
	h.publish(state)
	if h.hasHit(h.ui.ids.navLocalSend) || h.ui.section == sectionLocalSend {
		t.Fatal("module remained visible after disabling")
	}
}

func TestLocalSendRecordSortingAndFileDialog(t *testing.T) {
	state := shellMinimalState(t)
	state.Settings.LocalSend.Enabled = true
	state.Settings.LastSection = sectionLocalSend
	state.LocalSend = domain.LocalSendStatus{Active: true, Alias: "GoIM QA", Port: 53317}
	now := time.Now()
	state.Transfers = []domain.Transfer{
		{ID: "ls-a", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: "Android", Name: "A 项目资料", Direction: "send", Status: "completed", Size: 300, UpdatedAt: now.Add(-time.Minute),
			LocalSendItems: []domain.LocalSendTransferItem{{ID: "f-1", Name: "完整交付清单.pdf", Status: "completed", Size: 300}}},
		{ID: "ls-z", LegacyTransport: domain.TransportLocalSend, LocalSendPeer: "iPhone", Name: "Z 现场照片", Direction: "receive", Status: "completed", Size: 200, UpdatedAt: now},
	}
	h := newShellTestHarnessAtSize(t, state, 760, 650)
	h.clickLabel("记录")
	y := func(label string) float32 {
		t.Helper()
		hit, ok := h.findHitLabel(label)
		if !ok {
			t.Fatalf("record %q missing", label)
		}
		return hit.Bounds.Y
	}
	if y("Z 现场照片") >= y("A 项目资料") {
		t.Fatal("recent transfers should be first")
	}
	h.clickLabel("文件名")
	if y("A 项目资料") >= y("Z 现场照片") {
		t.Fatal("ascending filename sort failed")
	}
	h.clickLabel("文件名")
	if y("Z 现场照片") >= y("A 项目资料") {
		t.Fatal("descending filename sort failed")
	}
	panel := &h.ui.panels.localSend
	tree := &h.ui.tree
	h.clickID(tree.ID(panel.scope, "details:ls-a"))
	if panel.modal != localSendFilesModal || !h.hasText("完整交付清单.pdf") {
		t.Fatal("file list should be visible in a modal")
	}
	scope := tree.Sub(panel.scope, tree.Name("files-dialog"))
	h.clickID(tree.ID(scope, "done"))
	if panel.modal != localSendNoModal || h.hasText("完整交付清单.pdf") {
		t.Fatal("file list remained inline after closing")
	}
	h.clickLabel("发出")
	if h.hasText("Z 现场照片") || !h.hasText("A 项目资料") {
		t.Fatal("outgoing filter failed")
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

package desktop

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"goim/internal/domain"
)

func TestTransferRateAndPartialCleanupInTable(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Transfers = []domain.Transfer{
		{ID: "live", Name: "active.bin", Direction: "receive", Status: "transferring", Size: 10000, Done: 1000, Speed: 2048},
		{ID: "stopped", Name: "interrupted.bin", Direction: "receive", Status: "failed", Size: 10000, Done: 1000},
	}
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	if !h.hasText(transferRateLabel(state.Transfers[0])) {
		t.Fatal("active transfer rate is missing from the table")
	}
	h.clickLabelRole(h.panels.transferLabels[panelTransferFinished], gfx.RoleTab)
	entry := h.panels.transferFinished[0]
	h.click(entry.ids.more)
	h.click(entry.ids.clearPartial)
	if cmd := h.requireLastCommand(); cmd.Kind != domain.ClearTransferPartial || cmd.TransferID != "stopped" {
		t.Fatalf("cleanup dispatched %+v", cmd)
	}
}

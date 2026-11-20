package desktop

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"goim/internal/desktop/fileglyph"
	"goim/internal/domain"
)

func TestTransferListUsesAttachmentMetadataForIcons(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Files = []domain.File{
		{ID: "folder", Name: "存档.zip", Folder: true},
		{ID: "sound", Name: "录音", MIME: "audio/ogg"},
	}
	state.Transfers = []domain.Transfer{
		{ID: "directory-transfer", FileID: "folder", Name: "存档.zip"},
		{ID: "audio-transfer", FileID: "sound", Name: "录音"},
		{ID: "old-transfer", FileID: "missing", Name: "历史文档.pdf"},
	}
	h := panelTestNewHarness(t, panelModuleTransfers, state)
	want := map[string]*gfx.Icon{"directory-transfer": &fileglyph.Folder, "audio-transfer": &fileglyph.Audio, "old-transfer": &fileglyph.Pdf}
	for _, row := range h.panels.transferRows {
		if row.icon != want[row.transfer.ID] {
			t.Errorf("incorrect transfer icon for %q", row.transfer.ID)
		}
		delete(want, row.transfer.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing transfer rows: %v", want)
	}
}

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

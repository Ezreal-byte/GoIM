package desktop

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/shareclient"
)

func TestTransferScrollbarAndSorting(t *testing.T) {
	state := panelTestDefaultState(t)
	for i := range 40 {
		id := fmt.Sprintf("report-%02d", i)
		state.Transfers = append(state.Transfers, domain.Transfer{ID: id, Name: id, Size: int64(i+1) * 1024, Direction: "receive", Status: "completed", UpdatedAt: time.Unix(int64(i), 0)})
	}
	h := panelTestNewHarnessBounds(t, panelModuleTransfers, state, ui.Rect{Width: 1000, Height: 540})
	bar, ok := h.findHitLabelRole("Vertical scrollbar", gfx.RoleSeparator)
	if !ok {
		t.Fatal("overflowing transfer history has no visible scrollbar")
	}
	header, _ := h.findHitLabelRole("大小", gfx.RoleButton)
	lastID := h.panels.transferRows[len(h.panels.transferRows)-1].ids.more
	if _, visible := h.findHit(lastID); visible {
		t.Fatal("last transfer initially visible")
	}
	x, y := bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+2
	if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(x, bar.Bounds.Y+bar.Bounds.Height-1); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Release(x, bar.Bounds.Y+bar.Bounds.Height-1, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	_, lastVisible := h.findHit(lastID)
	after, _ := h.findHitLabelRole("大小", gfx.RoleButton)
	if !lastVisible || after.Bounds != header.Bounds {
		t.Fatalf("scroll did not reach last transfer or moved header: visible=%v header=%v to %v", lastVisible, header.Bounds, after.Bounds)
	}
	h.clickLabelRole("大小", gfx.RoleButton)
	if h.panels.transferRows[0].name != "report-00" {
		t.Fatal("size header did not sort ascending")
	}
	h.clickLabelRole("大小", gfx.RoleButton)
	h.clickLabelRole(h.panels.transferLabels[panelTransferReceived], gfx.RoleTab)
	state.Transfers[0].Size = 1 << 30
	state.Revision++
	h.publish(state)
	if got := h.panels.transferEntries()[0].name; got != "report-00" {
		t.Fatalf("descending size after refresh = %s", got)
	}
	h.typeInto(h.panels.ids.transferSearch, "report-03")
	if _, ok := h.findHitLabelRole("Vertical scrollbar", gfx.RoleSeparator); ok {
		t.Fatal("a single filtered row should not overflow")
	}
}

func TestShareSortPreservesSelectionAndNumericSize(t *testing.T) {
	state := panelTestDefaultState(t)
	state.Peers = []domain.Peer{{ID: "peer", Online: true}}
	state.Files = []domain.File{
		{ID: "big", Name: "a.zip", Size: 1 << 20, Shared: true, SenderID: "peer"},
		{ID: "small", Name: "z.zip", Size: 9 << 10, Shared: true, SenderID: "peer"},
	}
	h := panelTestNewHarness(t, panelModuleShares, state)
	h.clickLabelRole("选择 a.zip", gfx.RoleCheckbox)
	h.clickLabelRole("大小 · 项目数", gfx.RoleButton)
	if e := h.panels.sharesRemote[0]; e.file.ID != "small" {
		t.Fatalf("first by size = %s", e.title)
	}
	h.clickLabelRole(downloadSelectedLabel(1), gfx.RoleButton)
	if cmd := h.requireLastCommand(); cmd.FileID != "big" {
		t.Fatalf("selection after sorting: %+v", cmd)
	}
}

func TestDirectorySortHandlesLegacySizes(t *testing.T) {
	exact := int64(8500)
	v := memberProjectView{sort: panelTableSort{column: "大小"}, page: shareclient.Page{Items: []shareclient.Entry{
		{Name: "MB", Size: "2.0 MB"}, {Name: "bytes", Size: "910 字节"}, {Name: "KB", Size: "9.1 KB"}, {Name: "exact", Bytes: &exact},
	}}}
	v.sortItems()
	for i, want := range []string{"bytes", "exact", "KB", "MB"} {
		if got := v.page.Items[i].Name; got != want {
			t.Fatalf("row %d: %s", i, got)
		}
	}
}

func TestPrintSortPreservesPinnedDevicesAndRecentJobs(t *testing.T) {
	state := printTestState(t)
	for i := range 7 {
		id := fmt.Sprintf("job-%d", i)
		state.PrintJobs = append(state.PrintJobs, domain.PrintJob{ID: id, FileName: id, CreatedAt: time.Unix(int64(i), 0)})
	}
	copy := state.Printers[0]
	copy.ID, copy.Name = "pinned", "ZZZ"
	state.Printers = append(state.Printers, copy)
	state.Settings.PinnedPrinters = []string{copy.ID}
	h := panelTestNewHarness(t, panelModulePrint, state)
	h.clickLabelRole("打印机", gfx.RoleButton)
	h.clickLabelRole("打印机", gfx.RoleButton)
	if h.panels.print.lanCards[0].printer.ID != "pinned" {
		t.Fatal("sort moved the pinned printer")
	}
	h.clickLabelRole("文件名称", gfx.RoleButton)
	if got := h.panels.print.recent[0].name; got != "job-3" {
		t.Fatalf("recent records include older jobs: %s", got)
	}
	h.clickLabelRole("打印记录", gfx.RoleTab)
	h.clickLabelRole("文件名称", gfx.RoleButton)
	h.clickLabelRole("文件名称", gfx.RoleButton)
	state.Revision++
	h.publish(state)
	if got := h.panels.print.records[0].name; got != state.PrintJobs[0].FileName {
		t.Fatalf("record sort lost on reload: %s", got)
	}
	if got := h.panels.print.recent[0].name; got != "job-3" {
		t.Fatalf("sorting full history changed recent records: %s", got)
	}
}

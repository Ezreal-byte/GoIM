package desktop

import (
	"testing"

	"goim/internal/domain"
	"goim/internal/selfupdate"
)

func TestManualUpdateOpensTheMatchingPackage(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	url := selfupdate.AssetURL("0.1.5", "windows", "amd64", ".msi")
	h.ui.updater = &updateController{view: updateView{phase: updateOffered, offer: selfupdate.Offer{
		Version: "0.1.5", NotesURL: selfupdate.ReleaseURL("0.1.5"), DownloadURL: url,
	}}}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if !h.hasHit(h.ui.updateIDs.chrome) || !h.hasText("发现新版本") {
		t.Fatal("update availability is not surfaced")
	}
	for _, label := range h.driver.Snapshot().Labels {
		if label.Value == "有更新" {
			t.Fatal("wide titlebar still renders update text instead of the compact green control")
		}
	}
	h.clickID(h.ui.updateIDs.chrome)
	h.clickID(h.ui.updateIDs.action)
	if opened, ok := h.driver.LastOpenedURL(); !ok || opened != url {
		t.Fatalf("manual update opened %q, want %q", opened, url)
	}
}

func TestUpdateDialogShowsReleaseNotes(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	h.ui.updater = &updateController{view: updateView{
		phase: updateOffered,
		offer: selfupdate.Offer{
			Version: "0.1.6", Automatic: true,
			NotesURL: selfupdate.ReleaseURL("0.1.6"),
		},
		notes: "- 修复独立会话窗口\n- 新增自动更新开关",
	}}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.updateIDs.chrome)
	h.settle()
	requireShellLayout(t, h.snapshot())
	if !h.hasText("更新说明") || !h.hasText("修复独立会话窗口") || !h.hasText("新增自动更新开关") {
		t.Fatal("update dialog did not render release notes")
	}
}

func TestUpdateRestartWaitsForAnActiveTransfer(t *testing.T) {
	state := shellMinimalState(t)
	state.Transfers = []domain.Transfer{{ID: "running", Status: "transferring"}}
	h := newShellTestHarness(t, state)
	h.ui.updater = &updateController{view: updateView{phase: updateReady, offer: selfupdate.Offer{
		Version: "0.1.5", Automatic: true, NotesURL: selfupdate.ReleaseURL("0.1.5"),
	}}}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.updateIDs.chrome)
	if hit, ok := h.findHit(h.ui.updateIDs.action); !ok || !hit.Disabled {
		t.Fatal("install was available during a transfer")
	}
	state.Transfers[0].Status = "completed"
	state.Revision++
	h.publish(state)
	if hit, ok := h.findHit(h.ui.updateIDs.action); !ok || hit.Disabled {
		t.Fatal("install did not resume availability after transfer settled")
	}
}

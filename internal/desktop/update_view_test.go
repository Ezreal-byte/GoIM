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
	h.clickID(h.ui.updateIDs.chrome)
	h.clickID(h.ui.updateIDs.action)
	if opened, ok := h.driver.LastOpenedURL(); !ok || opened != url {
		t.Fatalf("manual update opened %q, want %q", opened, url)
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

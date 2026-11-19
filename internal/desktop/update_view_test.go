package desktop

import (
	"strings"
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
		notes: "## 更新内容\n\n- **修复**独立会话窗口\n- 新增 `自动更新` 开关\n\n" +
			"[完整说明](https://example.com/releases/0.1.6)",
	}}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.updateIDs.chrome)
	h.settle()
	requireShellLayout(t, h.snapshot())
	for _, want := range []string{
		"更新说明", "更新内容", "修复", "独立会话窗口",
		"新增", "自动更新", "开关", "完整说明",
	} {
		if h.hasText(want) {
			continue
		}
		var texts []string
		for _, text := range h.snapshot().Texts {
			texts = append(texts, text.Value)
		}
		t.Fatalf("update dialog did not render %q: %q", want, texts)
	}
	for _, text := range h.snapshot().Texts {
		if strings.ContainsAny(text.Value, "#`") || strings.Contains(text.Value, "**") ||
			strings.HasPrefix(text.Value, "- ") {
			t.Fatalf("update dialog displayed raw Markdown: %q", text.Value)
		}
	}
	clicked := false
	for _, hit := range h.snapshot().Hits {
		if hit.Label == "完整说明" {
			h.clickID(hit.ID)
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatal("Markdown link did not register a clickable region")
	}
	h.settle()
	if opened, ok := h.driver.LastOpenedURL(); !ok || opened != "https://example.com/releases/0.1.6" {
		t.Fatalf("Markdown link opened %q, want release link", opened)
	}
}

func TestUpdateWithoutSignedFeedOpensReleasePage(t *testing.T) {
	h := newShellTestHarness(t, shellMinimalState(t))
	page := selfupdate.ReleaseURL("0.1.6")
	h.ui.updater = &updateController{view: updateView{phase: updateOffered, offer: selfupdate.Offer{
		Version: "0.1.6", NotesURL: page, DownloadURL: page, FeedUnavailable: true,
	}}}
	if err := h.driver.Paint(); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.updateIDs.chrome)
	h.settle()
	requireShellLayout(t, h.snapshot())
	if hit, ok := h.findHit(h.ui.updateIDs.action); !ok || hit.Label != "打开发布页" {
		t.Fatalf("fallback action = %+v", hit)
	}
	h.clickID(h.ui.updateIDs.action)
	if opened, ok := h.driver.LastOpenedURL(); !ok || opened != page {
		t.Fatalf("fallback opened %q, want %q", opened, page)
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

package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"goim/internal/domain"
)

type interruptedShareResponse struct{ *httptest.ResponseRecorder }

func (w interruptedShareResponse) Write([]byte) (int, error) {
	return 0, errors.New("download interrupted")
}

func TestShareDownloadStatsToggleAndPersistence(t *testing.T) {
	n := testNode(t, "owner")
	source := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(source, []byte("share stats"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(source, ""); err != nil {
		t.Fatal(err)
	}
	file := n.Snapshot().Files[0]
	get := func(method string, headers map[string]string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, file.ShareURL+"/download", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		n.handleShare(w, r)
		if w.Code != want {
			t.Fatalf("%s: status = %d, want %d", method, w.Code, want)
		}
	}
	toggle := func(enabled bool) {
		t.Helper()
		if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "share_download_stats", Bool: enabled}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want uint64) {
		t.Helper()
		if got := n.Snapshot().File(file.ID).ShareDownloadCount; got != want {
			t.Fatalf("download count = %d, want %d", got, want)
		}
	}
	get(http.MethodGet, nil, http.StatusOK)
	toggle(true)
	get(http.MethodHead, nil, http.StatusOK)
	get(http.MethodGet, map[string]string{"Range": "bytes=0-3"}, http.StatusPartialContent)
	get(http.MethodGet, map[string]string{"If-None-Match": `"` + file.Hash + `"`}, http.StatusNotModified)
	n.handleShare(interruptedShareResponse{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, file.ShareURL+"/download", nil))
	check(0)
	get(http.MethodGet, nil, http.StatusOK)
	check(1)
	toggle(false)
	get(http.MethodGet, nil, http.StatusOK)
	check(0)
	toggle(true)
	check(1)
	dir := n.DataDir()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "owner", dir)
	if !n.Snapshot().Settings.ShareDownloadStats {
		t.Fatal("statistics setting was lost after restarting")
	}
	check(1)
}

func TestOwnedFolderContentsStayAvailableWithoutStatistics(t *testing.T) {
	n := testNode(t, "owner")
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(source, "notes.txt")
	if err := os.WriteFile(filePath, []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(source, "folder-password"); err != nil {
		t.Fatal(err)
	}
	f := n.Snapshot().Files[0]
	if f.ShareContents == nil || f.ShareEntryCount != 2 || f.Size != 5 || !f.ShareProtected {
		t.Fatalf("unexpected owned folder summary: %+v", f)
	}
	entries := slices.Collect(f.ShareContents.All())
	if len(entries) != 2 || !entries[0].Folder || entries[0].Path != "empty" ||
		entries[1].Folder || entries[1].Path != "notes.txt" || entries[1].Size != 5 || entries[1].Downloads != 0 {
		t.Fatalf("unexpected folder entries with statistics disabled: %+v", entries)
	}
	if next := n.Snapshot().File(f.ID).ShareContents; next != f.ShareContents {
		t.Fatal("unchanged snapshot rebuilt the folder contents")
	}
	if n.snapshot(false).File(f.ID).ShareContents != nil {
		t.Fatal("internal snapshot exposed local catalog contents")
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "share_download_stats", Bool: true}); err != nil {
		t.Fatal(err)
	}
	n.recordShareDownloads(map[string][]int{f.ID: {entries[1].ID}})
	counted := slices.Collect(n.Snapshot().File(f.ID).ShareContents.All())
	if len(counted) != 2 || counted[1].Downloads != 1 || slices.Collect(f.ShareContents.All())[1].Downloads != 0 {
		t.Fatalf("the new count did not reach a fresh snapshot: %+v", counted)
	}
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "report.txt"), []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}
	n.refreshShareSources([]string{f.ID}, checkShareTree)
	updated := slices.Collect(n.Snapshot().File(f.ID).ShareContents.All())
	if len(updated) != 2 || updated[1].Path != "report.txt" || updated[1].Downloads != 0 {
		t.Fatalf("the live catalog did not change: %+v", updated)
	}
}

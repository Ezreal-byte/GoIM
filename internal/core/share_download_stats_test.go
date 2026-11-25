package core

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

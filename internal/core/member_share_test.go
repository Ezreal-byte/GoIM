package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestMemberCatalogDownloadBrowseUnlockAndRevoke(t *testing.T) {
	a, b := testNode(t, "publisher"), testNode(t, "member")
	connectNodes(t, a, b)
	base := t.TempDir()
	folder := filepath.Join(base, "shared folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	public, private := filepath.Join(base, "public.txt"), filepath.Join(base, "confidential.txt")
	for _, path := range []string{public, private, filepath.Join(folder, "inside.txt")} {
		if err := os.WriteFile(path, []byte("shared bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []domain.Command{
		{Kind: domain.ShareFiles, Paths: []string{public, folder}},
		{Kind: domain.ShareFiles, Paths: []string{private}, Text: "member-secret"},
	} {
		if err := a.execute(cmd); err != nil {
			t.Fatal(err)
		}
	}
	even := func() []domain.File {
		var files []domain.File
		for _, f := range b.Snapshot().Files {
			if f.Shared {
				files = append(files, f)
			}
		}
		return files
	}
	eventually(t, "all three projects appear on the other peer", func() bool { return len(even()) == 3 })
	a.mu.Lock()
	encoded, err := json.Marshal(a.shareCatalog())
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"confidential.txt", "member-secret", "inside.txt"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("catalog disclosed %q", secret)
		}
	}
	for _, f := range a.Snapshot().Files {
		_, token, _ := strings.Cut(f.ShareURL, "/s/")
		if token == "" || strings.Contains(string(encoded), token) {
			t.Fatal("catalog included a private browser token")
		}
	}
	var native, locked, dir domain.File
	for _, f := range even() {
		switch {
		case f.ShareProtected:
			locked = f
		case f.ShareFolder:
			dir = f
		default:
			native = f
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	var resolved string
	get := func(link string, status int) string {
		t.Helper()
		resp, err := client.Get(link)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != status {
			t.Fatalf("HTTP %d, expected %d (%v)", resp.StatusCode, status, err)
		}
		resolved = resp.Request.URL.String()
		return string(result)
	}
	if native.Name != "public.txt" || native.MemberURL != "" || dir.Name != "shared folder" || dir.ShareEntryCount != 1 || locked.Size != 0 {
		t.Fatal("unexpected member metadata")
	}
	if body := get(locked.MemberURL, 200); strings.Contains(body, "confidential.txt") || !strings.Contains(body, "分享密码") {
		t.Fatal("password gate did not protect file metadata")
	}
	login := resolved
	get(login+"/download", http.StatusUnauthorized)
	resp, err := client.PostForm(login, url.Values{"password": {"member-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || get(login+"/download", 200) != "shared bytes" {
		t.Fatal("member could not unlock and download private file")
	}
	if body := get(dir.MemberURL, 200); !strings.Contains(body, "inside.txt") {
		t.Fatal("directory missing shared child")
	}
	if get(resolved+"/download?file=1", 200) != "shared bytes" {
		t.Fatal("member could not download folder entry")
	}
	if err := b.execute(domain.Command{Kind: domain.GetShared, FileID: native.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "native member offer", func() bool { return len(b.Snapshot().Transfers) == 1 })
	task := b.Snapshot().Transfers[0]
	if err := b.execute(domain.Command{Kind: domain.AcceptTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "native member file downloaded", func() bool { return b.Snapshot().Transfers[0].Status == "completed" })
	content, err := os.ReadFile(b.Snapshot().Transfers[0].Path)
	if err != nil || string(content) != "shared bytes" {
		t.Fatal("downloaded file differs", err)
	}
	for _, f := range a.Snapshot().Files {
		if f.ShareFolder {
			if err := a.execute(domain.Command{Kind: domain.RemoveShare, FileID: f.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	eventually(t, "revoked directory disappears", func() bool { return len(even()) == 2 })
	get(dir.MemberURL, 404)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "offline project link disabled", func() bool {
		for _, f := range even() {
			if f.ShareProtected {
				return f.MemberURL == ""
			}
		}
		return false
	})
}

package core

import (
	"fmt"
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
	"goim/internal/shareclient"
)

func TestLiveShareEventsWithdrawOpenPages(t *testing.T) {
	owner := testNode(t, "owner")
	if err := owner.shareFile(t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := owner.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	s := owner.Snapshot()
	f := s.Files[0]
	owner.mu.Lock()
	projectStamp := projectVersion(f.ID, owner.data.BrowserShares[f.ID])
	owner.mu.Unlock()
	for _, tc := range []struct {
		name, link, stamp string
		change            func() error
	}{
		{"project locks", f.ShareURL, projectStamp, func() error { return owner.updateSharePassword(f.ID, "private") }},
		{"homepage closes", s.DeviceShare.URL, "", func() error { return owner.setDeviceShare(false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stamp == "" {
				owner.mu.Lock()
				tc.stamp = fmt.Sprint(owner.webCatalogVersion)
				owner.mu.Unlock()
			}
			client := http.Client{Timeout: 8 * time.Second}
			resp, err := client.Get(tc.link + "/events?v=" + url.QueryEscape(tc.stamp))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("event stream: %s %v", resp.Status, resp.Header)
			}
			if err := tc.change(); err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != ": connected\n\nevent: change\ndata: reload\n\n" {
				t.Fatalf("event stream after access change = %q, %v", body, err)
			}
		})
	}
}

func TestSharedFolderListingTracksDisk(t *testing.T) {
	n := testNode(t, "owner")
	folder := t.TempDir()
	old := filepath.Join(folder, "old-report.txt")
	if err := os.WriteFile(old, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(folder, ""); err != nil {
		t.Fatal(err)
	}
	link := n.Snapshot().Files[0].ShareURL
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "new-report.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	stale, added := strings.Contains(string(page), "old-report.txt"), strings.Contains(string(page), "new-report.txt")
	if stale || !added {
		t.Fatalf("deleted entry still listed = %t, new entry listed = %t", stale, added)
	}
}

func TestOfflineShareSourceHiddenAndRepairKeepsAccess(t *testing.T) {
	owner, member := testNode(t, "owner"), testNode(t, "member")
	connectNodes(t, owner, member)
	base := t.TempDir()
	dir := filepath.Join(base, "shared-dir")
	plain := filepath.Join(base, "original.txt")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested.txt"), []byte("nested"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owner.shareFile(dir, "password"); err != nil {
		t.Fatal(err)
	}
	if err := owner.shareFile(plain, ""); err != nil {
		t.Fatal(err)
	}
	if err := owner.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	owner.broadcastCatalog()
	before := owner.Snapshot()
	var folder, file domain.File
	for _, f := range before.Files {
		if f.ShareFolder {
			folder = f
		} else {
			file = f
		}
	}
	var oldMember domain.File
	eventually(t, "both projects visible", func() bool {
		for _, f := range member.Snapshot().Files {
			if f.ShareProtected {
				oldMember = f
			}
		}
		return oldMember.MemberURL != "" && member.Snapshot().File(file.ID).Shared
	})
	if err := owner.replaceShareSource(file.ID, dir); err == nil {
		t.Fatal("replaced a live source")
	}
	if err := os.Remove(plain); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	// No browser request or manual refresh drives this check.
	eventually(t, "owner sees unavailable and member loses both projects", func() bool {
		s := owner.Snapshot()
		r := member.Snapshot()
		return s.File(folder.ID).ShareUnavailable && s.File(file.ID).ShareUnavailable && !r.File(file.ID).Shared && r.File(oldMember.ID).ID == ""
	})
	get := func(client *http.Client, raw string, code int) string {
		t.Helper()
		resp, err := client.Get(raw)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != code {
			t.Fatalf("HTTP %d want %d: %s, %v", resp.StatusCode, code, body, err)
		}
		return string(body)
	}
	home := get(http.DefaultClient, before.DeviceShare.URL, 200)
	if strings.Contains(home, folder.Name) || strings.Contains(home, file.Name) {
		t.Fatal("missing sources advertised on device homepage")
	}
	get(http.DefaultClient, folder.ShareURL, 410)
	get(http.DefaultClient, file.ShareURL, 410)
	if err := owner.replaceShareSource(file.ID, filepath.Join(base, "missing")); err == nil {
		t.Fatal("accepted missing replacement")
	}
	replacement := filepath.Join(base, "replacement.txt")
	if err := os.WriteFile(replacement, []byte("repaired source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owner.replaceShareSource(folder.ID, replacement); err != nil {
		t.Fatal(err)
	}
	if err := owner.replaceShareSource(file.ID, replacement); err == nil {
		t.Fatal("accepted duplicate source")
	}
	if err := owner.replaceShareSource(file.ID, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "repaired root projects advertised", func() bool {
		private, public := false, false
		for _, f := range member.Snapshot().Files {
			if f.Shared && f.MemberURL != "" {
				private = private || f.ShareProtected && f.ID != oldMember.ID
				public = public || f.Name == "moved" && f.ShareFolder
			}
		}
		return private && public
	})
	if err := owner.offerShared(member.Snapshot().Self.ID, file.ID, ""); err == nil {
		t.Fatal("old file identity still grants access")
	}
	get(http.DefaultClient, oldMember.MemberURL, 404)
	get(http.DefaultClient, folder.ShareURL+"/download", 401)
	if body := get(http.DefaultClient, folder.ShareURL, 200); strings.Contains(body, "replacement.txt") {
		t.Fatal("locked replacement disclosed its new name")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.PostForm(folder.ShareURL, url.Values{"password": {"password"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := get(client, folder.ShareURL+"/download", 200); got != "repaired source" {
		t.Fatal(got)
	}
	get(client, folder.ShareURL+"/download?file=1", 404)
	if body := get(http.DefaultClient, file.ShareURL, 200); !strings.Contains(body, "nested.txt") {
		t.Fatal("original file URL did not open its replacement folder")
	}
}

func TestLiveFolderPreservesExistingIDsAcrossRescanAndRestart(t *testing.T) {
	data := t.TempDir()
	n := testNodeAt(t, "owner", data)
	folder := t.TempDir()
	for _, name := range []string{"b.txt", "z.txt"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.shareFile(folder, ""); err != nil {
		t.Fatal(err)
	}
	share := n.Snapshot().Files[0]
	n.mu.Lock()
	r := n.data.BrowserShares[share.ID]
	n.mu.Unlock()
	// Save a manifest in the pre-live format (implicit IDs).
	if err := n.durable(func(p *persisted) []pending {
		legacy := p.BrowserShares[share.ID]
		legacy.Entries = append([]shareFolderEntry(nil), legacy.Entries...)
		for i := range legacy.Entries {
			legacy.Entries[i].ID = 0
		}
		legacy.LastID = 0
		p.BrowserShares[share.ID] = legacy
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(folder, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "owner", data)
	url := n.Snapshot().File(share.ID).ShareURL
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for i, e := range r.Entries {
		resp, err = http.Get(fmt.Sprintf("%s/download?file=%d", url, i+1))
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if e.Path == "b.txt" && resp.StatusCode != 404 || e.Path == "z.txt" && (resp.StatusCode != 200 || string(b) != "z.txt") {
			t.Fatalf("old entry %s: HTTP %d", e.Path, resp.StatusCode)
		}
	}
}

// The open member page gets a new version even for a rename that leaves the
// count and total size unchanged. Fetch the page through its member URL.
func TestNestedChangeUpdatesMemberCatalogVersion(t *testing.T) {
	owner, member := testNode(t, "owner"), testNode(t, "member")
	connectNodes(t, owner, member)
	folder := t.TempDir()
	sub := filepath.Join(folder, "child")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "old.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := owner.shareFile(folder, ""); err != nil {
		t.Fatal(err)
	}
	owner.broadcastCatalog()
	var f domain.File
	eventually(t, "folder published", func() bool {
		for _, v := range member.Snapshot().Files {
			if v.ShareFolder && v.MemberURL != "" {
				f = v
				return true
			}
		}
		return false
	})
	client, err := shareclient.New(f.MemberURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	page, err := client.List(t.Context(), "", "")
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if err = os.Rename(filepath.Join(sub, "old.txt"), filepath.Join(sub, "new.txt")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "catalog version advanced after rename", func() bool { return member.Snapshot().File(f.ID).ShareVersion > f.ShareVersion })
	updated, err := client.List(t.Context(), page.Items[0].URL, "")
	if err != nil || len(updated.Items) != 1 || updated.Items[0].Name != "new.txt" {
		t.Fatal(updated, err)
	}
}

func TestFolderSweepDetectsAnEditedFile(t *testing.T) {
	n := testNode(t, "owner")
	root := t.TempDir()
	sub := filepath.Join(root, "reports")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "report.txt")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(root, ""); err != nil {
		t.Fatal(err)
	}
	f := n.Snapshot().Files[0]
	n.mu.Lock()
	old := n.data.BrowserShares[f.ID]
	n.mu.Unlock()
	if len(old.Entries) != 2 {
		t.Fatal("missing shared folder or file")
	}
	dir, report := old.Entries[0], old.Entries[1]
	if err := os.WriteFile(path, []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	mtime := report.Modified.Add(2 * time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// In-place edits need not change any directory timestamps.
	if err := os.Chtimes(sub, dir.Modified, dir.Modified); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(root, old.Modified, old.Modified); err != nil {
		t.Fatal(err)
	}
	n.refreshShareSources([]string{f.ID}, checkShareTree)
	n.mu.Lock()
	current := n.data.BrowserShares[f.ID]
	n.mu.Unlock()
	if _, ok := current.entry(report.ID); ok || current.Version <= old.Version {
		t.Fatal("open viewers still have the previous file revision")
	}
}

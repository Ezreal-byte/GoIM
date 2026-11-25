package core

import (
	"archive/zip"
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestArchiveStopsWithinRevokedFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "large")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	const size = 10 << 20
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	checks := 0
	active := func() bool { checks++; return checks == 1 }
	err = writeArchiveFile(zip.NewWriter(io.Discard), "large", time.Time{}, file, size, active)
	offset, seekErr := file.Seek(0, io.SeekCurrent)
	if err != os.ErrPermission || seekErr != nil || offset <= 0 || offset >= size {
		t.Fatalf("revoked file offset %d of %d, error %v, seek %v", offset, size, err, seekErr)
	}
}

func TestBrowserBatchDownload(t *testing.T) {
	files := t.TempDir()
	n, err := New(Options{DataDir: t.TempDir(), DownloadDir: t.TempDir(), Listen: "127.0.0.1:0", DisableDiscovery: true, DisableCompatibility: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "share_download_stats", Bool: true}); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"资料/说明.txt": "notes", "资料/图片/图.png": "image", "资料/.hidden": "private",
		"a/同名.txt": "first", "b/同名.txt": "second",
	} {
		path := filepath.Join(files, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(files, "资料/空目录"), 0700); err != nil {
		t.Fatal(err)
	}
	share := func(name, password string) domain.File {
		t.Helper()
		if err := n.shareFile(filepath.Join(files, name), password); err != nil {
			t.Fatal(err)
		}
		list := n.Snapshot().Files
		return list[len(list)-1]
	}
	folder := share("资料", "folder-pass")
	first := share("a/同名.txt", "")
	second := share("b/同名.txt", "file-pass")
	if err := n.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	device := n.Snapshot().DeviceShare.URL
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	post := func(target string, form url.Values, status int, want map[string]string) {
		t.Helper()
		resp, err := client.PostForm(target, form)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != status || (resp.Header.Get("Content-Type") == "application/zip") != (want != nil) {
			t.Fatalf("archive response = %d %s, want %d, err %v", resp.StatusCode, resp.Header.Get("Content-Type"), status, err)
		}
		if want == nil {
			return
		}
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, entry := range z.File {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			got[entry.Name] = string(b)
		}
		if len(z.File) != len(want) || !maps.Equal(got, want) {
			t.Fatalf("archive entries = %v, want %v", got, want)
		}
	}
	form := url.Values{"item": {first.ID, folder.ID, second.ID, first.ID}, "password-" + folder.ID: {"folder-pass"}}
	post(device+"/archive", form, 401, nil)
	form.Set("password-"+second.ID, "file-pass")
	post(device+"/archive", form, 200, map[string]string{"同名.txt": "first", "同名 (2).txt": "second", "资料/": "", "资料/说明.txt": "notes", "资料/图片/": "", "资料/图片/图.png": "image", "资料/空目录/": ""})

	_, record, _ := n.lookupShare(folder.ShareURL[strings.LastIndex(folder.ShareURL, "/")+1:])
	ids := map[string]string{}
	for i, e := range record.Entries {
		ids[e.Path] = strconv.Itoa(record.entryID(i))
	}
	selection := url.Values{"dir": {"0"}, "item": {ids["说明.txt"], ids["图片"]}}
	post(folder.ShareURL+"/archive", selection, 403, nil)
	post(folder.ShareURL, url.Values{"password": {"folder-pass"}}, 200, nil)
	post(folder.ShareURL+"/archive", selection, 200, map[string]string{"说明.txt": "notes", "图片/": "", "图片/图.png": "image"})
	selection = url.Values{"dir": {ids["图片"]}, "item": {ids["图片/图.png"]}}
	post(folder.ShareURL+"/archive", selection, 200, map[string]string{"图.png": "image"})
	selection.Set("dir", "0")
	post(folder.ShareURL+"/archive", selection, 410, nil)
	post(folder.ShareURL+"/archive", url.Values{}, 400, nil)
	eventually(t, "archives counted each included file once", func() bool {
		state := n.Snapshot()
		f := state.File(folder.ID)
		counts := make(map[string]uint64)
		for _, stat := range f.ShareDownloadFiles {
			counts[stat.Path] = stat.Count
		}
		return state.File(first.ID).ShareDownloadCount == 1 && state.File(second.ID).ShareDownloadCount == 1 &&
			f.ShareDownloadCount == 5 && maps.Equal(counts, map[string]uint64{"说明.txt": 2, "图片/图.png": 3})
	})
	if err := n.setDeviceShare(false); err != nil {
		t.Fatal(err)
	}
	post(device+"/archive", form, 403, nil)
}

package core

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
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

func TestDeviceHomeRoutesAndUpload(t *testing.T) {
	dir := t.TempDir()
	openNode := func() *Node {
		node, err := New(Options{DataDir: dir, DownloadDir: filepath.Join(dir, "downloads"), Listen: "127.0.0.1:0", DisableDiscovery: true, DisableIPMSG: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = node.Close() })
		return node
	}
	n := openNode()
	source := t.TempDir()
	open := filepath.Join(source, "公开说明.txt")
	locked := filepath.Join(source, "设计归档")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{open, filepath.Join(locked, "归档说明.txt")} {
		if err := os.WriteFile(path, []byte("published contents"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.shareFile(open, ""); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(locked, "project-password"); err != nil {
		t.Fatal(err)
	}
	unlocked, protected := n.Snapshot().Files[0], n.Snapshot().Files[1]
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}
	check := func(r *http.Request, expected int) string {
		t.Helper()
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != expected {
			t.Fatalf("%s %s = %d, want %d: %v", r.Method, r.URL.Path, resp.StatusCode, expected, err)
		}
		return string(body)
	}
	get := func(target string, code int) string {
		t.Helper()
		r, _ := http.NewRequest(http.MethodGet, target, nil)
		return check(r, code)
	}
	upload := func(target, origin string, expected int, names ...string) string {
		t.Helper()
		var body bytes.Buffer
		mp := multipart.NewWriter(&body)
		for _, name := range names {
			part, err := mp.CreateFormFile("file", name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, "sent from the browser"); err != nil {
				t.Fatal(err)
			}
		}
		if err := mp.Close(); err != nil {
			t.Fatal(err)
		}
		r, _ := http.NewRequest(http.MethodPost, target, &body)
		r.Header.Set("Content-Type", mp.FormDataContentType())
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return check(r, expected)
	}
	if n.Snapshot().DeviceShare.URL != "" || strings.Contains(get(unlocked.ShareURL, 200), "设备主页") {
		t.Fatal("an upgrade exposed the device home before it was enabled")
	}
	root, _ := url.Parse(unlocked.ShareURL)
	root.Path = "/"
	get(root.String(), 404)
	if err := n.execute(domain.Command{Kind: domain.SetDeviceShare, Bool: true}); err != nil {
		t.Fatal(err)
	}
	device := n.Snapshot().DeviceShare
	if device.URL == "" || len(device.QR) == 0 || device.ReceiveDir == "" {
		t.Fatal("missing device share link, QR or inbox")
	}
	saved, _ := json.Marshal(n.Snapshot())
	if bytes.Contains(saved, []byte(device.URL)) {
		t.Fatal("device link entered public state JSON")
	}
	home := get(device.URL, 200)
	if !strings.Contains(home, "公开说明.txt") || !strings.Contains(home, "设计归档，需密码") ||
		!strings.Contains(home, "上传文件") || strings.Contains(home, "归档说明.txt") || strings.Contains(home, device.ReceiveDir) {
		t.Fatal("device catalog did not reflect the published shares")
	}
	if html := get(unlocked.ShareURL, 200); !strings.Contains(html, "设备主页") || strings.Contains(html, "上传文件") || strings.Contains(html, "设计归档") {
		t.Fatal("a direct link did not open only the requested project")
	}
	if got := get(unlocked.ShareURL+"/download", 200); got != "published contents" {
		t.Fatalf("unprotected file bytes = %q", got)
	}
	if html := get(protected.ShareURL, 200); !strings.Contains(html, "输入分享密码") || strings.Contains(html, "归档说明.txt") {
		t.Fatal("a protected project leaked contents before authentication")
	}
	get(protected.ShareURL+"/download?file=1", 401)
	r, _ := http.NewRequest(http.MethodPost, protected.ShareURL, strings.NewReader(url.Values{"password": {"project-password"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if html := check(r, 200); !strings.Contains(html, "归档说明.txt") {
		t.Fatal("the correct password did not open the project")
	}
	upload(unlocked.ShareURL+"/upload", "", 404, "unexpected.txt")
	upload(device.URL+"/upload", "https://another-origin.example", 403, "blocked.txt")
	upload(device.URL+"/upload", "", 400, "../escape.txt")
	upload(device.URL+"/upload", "", 400, "first.txt", "second.txt")
	if html := upload(device.URL+"/upload", "", 200, "补充资料.txt"); !strings.Contains(html, "上传完成") {
		t.Fatal("successful upload did not show feedback")
	}
	upload(device.URL+"/upload", "", 200, "补充资料.txt")
	for _, filename := range []string{"补充资料.txt", "补充资料 (2).txt"} {
		got, err := os.ReadFile(filepath.Join(device.ReceiveDir, filename))
		if err != nil || string(got) != "sent from the browser" {
			t.Fatalf("inbox file %q: %q, %v", filename, got, err)
		}
	}
	entries, err := os.ReadDir(device.ReceiveDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("invalid, interrupted or temporary files entered inbox: %v, %v", entries, err)
	}
	if strings.Contains(get(device.URL, 200), "补充资料.txt") {
		t.Fatal("an upload was published as a share")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = openNode()
	if n.Snapshot().DeviceShare.URL != device.URL {
		t.Fatal("device link did not survive restart")
	}
	if err := n.execute(domain.Command{Kind: domain.SetDeviceShare}); err != nil {
		t.Fatal(err)
	}
	get(device.URL, 404)
	upload(device.URL+"/upload", "", 404, "late.txt")
	get(unlocked.ShareURL+"/download", 200)
	if err := n.execute(domain.Command{Kind: domain.SetDeviceShare, Bool: true}); err != nil {
		t.Fatal(err)
	}
	if n.Snapshot().DeviceShare.URL == device.URL {
		t.Fatal("reenabling revived an old device link")
	}
}

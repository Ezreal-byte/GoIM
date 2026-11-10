package core

import (
	"html"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestBrowserFolderShareNavigationAndDownload(t *testing.T) {
	nodeDir := t.TempDir()
	n := testOptimizationNode(t, nodeDir)
	root := filepath.Join(t.TempDir(), "交付资料")
	folder := filepath.Join(root, "设计稿")
	empty := filepath.Join(root, "空文件夹")
	for _, dir := range []string{folder, empty} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const name = "交付 + 说明.txt"
	const content = "Browser folder file bytes"
	file := filepath.Join(folder, name)
	write := func(path, payload string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(file, content)
	write(filepath.Join(root, ".private"), "private")
	if err := n.shareFile(root, "test-share-password"); err != nil {
		t.Fatal(err)
	}
	shared := n.Snapshot().Files[0]
	if !shared.ShareFolder || !shared.ShareProtected || shared.ShareURL == "" {
		t.Fatalf("invalid folder share: %#v", shared)
	}
	if err := n.offerShared("peer", shared.ID); err == nil {
		t.Fatal("a browser-only directory could be offered as a chat file")
	}
	write(filepath.Join(folder, "新增文件.txt"), "not shared")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	request := func(r *http.Request, expected int) (string, http.Header) {
		t.Helper()
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != expected {
			t.Fatalf("%s %s = %d, want %d (%v)", r.Method, r.URL, response.StatusCode, expected, err)
		}
		return string(body), response.Header
	}
	get := func(target string, expected int) string {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := request(r, expected)
		return body
	}
	link := func(markup, label string) string {
		t.Helper()
		match := regexp.MustCompile(`href="([^"]+)" aria-label="` + regexp.QuoteMeta(label) + `"`).FindStringSubmatch(html.UnescapeString(markup))
		if len(match) < 2 {
			t.Fatalf("missing browser action %q", label)
		}
		base, _ := url.Parse(shared.ShareURL)
		next, err := base.Parse(match[1])
		if err != nil {
			t.Fatal(err)
		}
		return next.String()
	}
	locked := get(shared.ShareURL, 200)
	if strings.Contains(locked, "交付资料") || strings.Contains(locked, "设计稿") || !strings.Contains(locked, "分享密码") {
		t.Fatal("locked page exposed the folder contents or omitted the password prompt")
	}
	get(shared.ShareURL+"/download?file=2", 401)
	form := url.Values{"password": {"test-share-password"}}
	authenticate := func(target string) string {
		t.Helper()
		r, _ := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		page, _ := request(r, 200)
		return page
	}
	page := authenticate(shared.ShareURL)
	if strings.Contains(page, ".private") || strings.Contains(page, "ZgotmplZ") || !strings.Contains(page, "data:image/svg+xml;base64,") {
		t.Fatal("root page exposed a private name or failed to render icons")
	}
	nestedURL := link(page, "打开文件夹：设计稿")
	nested := get(nestedURL, 200)
	if strings.Contains(nested, "新增文件.txt") || !strings.Contains(nested, "返回上一级") || !strings.Contains(nested, "交付资料") {
		t.Fatal("nested page did not preserve the share scope and parent navigation")
	}
	download := link(nested, "下载文件："+name)
	rangeRequest, _ := http.NewRequest(http.MethodGet, download, nil)
	rangeRequest.Header.Set("Range", "bytes=0-6")
	partial, headers := request(rangeRequest, 206)
	kind, parameters, err := mime.ParseMediaType(headers.Get("Content-Disposition"))
	if err != nil || kind != "attachment" || parameters["filename"] != name || partial != content[:7] {
		t.Fatalf("unexpected download: %q %q %v", partial, headers.Get("Content-Disposition"), err)
	}
	if got := get(download, 200); got != content {
		t.Fatalf("wrong file contents: %q", got)
	}
	if !strings.Contains(get(link(page, "打开文件夹：空文件夹"), 200), "这个文件夹是空的") {
		t.Fatal("the empty folder does not have an empty state")
	}
	get(shared.ShareURL+"?dir=..%2F..", 404)
	get(shared.ShareURL+"/download?file=..%5C..", 404)
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testOptimizationNode(t, nodeDir)
	if got := n.Snapshot().File(shared.ID).ShareURL; got != shared.ShareURL {
		t.Fatalf("folder link changed on restart: %q", got)
	}
	if afterLogin := authenticate(nestedURL); !strings.Contains(html.UnescapeString(afterLogin), name) {
		t.Fatal("re-authenticating a bookmarked folder did not return to it")
	}
	write(file, "Changed since it was shared")
	get(download, 410)
	if err := n.execute(domain.Command{Kind: domain.RemoveShare, FileID: shared.ID}); err != nil {
		t.Fatal(err)
	}
	get(nestedURL, 404)
}

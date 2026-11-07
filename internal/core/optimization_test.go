package core

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand/v2"
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

func testOptimizationNode(t *testing.T, dir string) *Node {
	t.Helper()
	n, err := New(Options{DataDir: dir, Listen: "127.0.0.1:0", DisableDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func TestBrowserSharePasswordRevocationAndRanges(t *testing.T) {
	dir := t.TempDir()
	n := testOptimizationNode(t, dir)
	path := filepath.Join(dir, "局域网分享 & QA.txt")
	const payload = "downloaded directly by a browser"
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n.shareFile(path, "review-123"); err != nil {
		t.Fatal(err)
	}
	state := n.Snapshot()
	f := state.Files[0]
	if !f.Shared || !f.ShareProtected || f.ShareURL == "" {
		t.Fatalf("incomplete link: %#v", f)
	}
	if err := n.offerShared("peer", f.ID); err == nil {
		t.Fatal("a password-protected browser share was offered to a native peer")
	}
	if err := n.shareFile(path, ""); err == nil {
		t.Fatal("an unprotected duplicate of a password-protected share was created")
	}
	if qr, err := png.Decode(bytes.NewReader(f.ShareQR)); err != nil || qr.Bounds().Dx() != 224 {
		t.Fatalf("invalid QR bitmap: %v", err)
	}
	wire, err := json.Marshal(publicFile(f))
	if err != nil || bytes.Contains(wire, []byte(f.ShareURL)) || bytes.Contains(wire, []byte(path)) {
		t.Fatalf("private data in peer offer: %s, %v", wire, err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	get := func(path string, want int, rangeHeader string) string {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, f.ShareURL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rangeHeader != "" {
			r.Header.Set("Range", rangeHeader)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("GET %q = %d, want %d; %v: %s", path, resp.StatusCode, want, err, body)
		}
		return string(body)
	}
	login := func(password string, want int) {
		t.Helper()
		resp, err := client.PostForm(f.ShareURL, url.Values{"password": {password}})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("unlock status = %d, want %d", resp.StatusCode, want)
		}
	}
	if html := get("", 200, ""); strings.Contains(html, f.Name) || !strings.Contains(html, "分享密码") {
		t.Fatal("metadata must remain hidden until successful login")
	}
	get("/download", 401, "")
	login("wrong-password", 401)
	login("review-123", 200)
	if html := get("", 200, ""); !strings.Contains(html, "局域网分享 &amp; QA.txt") || !strings.Contains(html, "下载文件") {
		t.Fatal("file page does not contain an escaped filename and download action")
	}
	if body := get("/download", 206, "bytes=0-9"); body != payload[:10] {
		t.Fatalf("unexpected range contents %q", body)
	}
	if err := n.updateSharePassword(f.ID, "review-456"); err != nil {
		t.Fatal(err)
	}
	get("/download", 401, "")
	login("review-456", 200)
	if body := get("/download", 200, ""); body != payload {
		t.Fatal("wrong downloaded file")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testOptimizationNode(t, dir)
	restarted := n.Snapshot().File(f.ID)
	if restarted.ShareURL != f.ShareURL || !restarted.ShareProtected {
		t.Fatal("the browser link and its access policy must survive a restart")
	}
	get("/download", 401, "")
	login("review-456", 200)
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(payload)), 0600); err != nil {
		t.Fatal(err)
	}
	get("/download", 410, "")
	if err := n.execute(domain.Command{Kind: domain.RemoveShare, FileID: f.ID}); err != nil {
		t.Fatal(err)
	}
	get("", 404, "")
	if n.Snapshot().File(f.ID).ShareURL != "" {
		t.Fatal("revoked link still present in snapshot")
	}
}

func TestAppLockSurvivesRestartAndRequiresCurrentPassword(t *testing.T) {
	dir := t.TempDir()
	n := testOptimizationNode(t, dir)
	set := func(n *Node, previous, password string, lock bool) error {
		return n.execute(domain.Command{Kind: domain.SetLockPassword, Password: previous, Text: password, Bool: lock})
	}
	unlock := func(n *Node, password string) error {
		return n.execute(domain.Command{Kind: domain.UnlockApp, Password: password})
	}
	if err := set(n, "", "secret-0123", true); err != nil {
		t.Fatal(err)
	}
	if s := n.Snapshot(); !s.Locked || !s.Settings.HasLockPassword {
		t.Fatal("first-use lock failed")
	}
	if err := n.execute(domain.Command{Kind: domain.SetSetting, Setting: "theme", Value: "dark"}); err != nil || n.Snapshot().Settings.Theme != "light" {
		t.Fatal("the locked instance accepted a normal UI action")
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testOptimizationNode(t, dir)
	if !n.Snapshot().Locked {
		t.Fatal("relaunch bypassed the lock")
	}
	if err := unlock(n, "not-the-password"); err == nil {
		t.Fatal("accepted incorrect password")
	}
	if err := unlock(n, "secret-0123"); err == nil {
		t.Fatal("failed attempts did not throttle")
	}
	n.mu.Lock()
	n.unlockAfter = time.Time{}
	n.mu.Unlock()
	if err := unlock(n, "secret-0123"); err != nil || n.Snapshot().Locked {
		t.Fatalf("cannot unlock: %v", err)
	}
	if err := set(n, "secret-0123", "replacement-456", false); err != nil {
		t.Fatal(err)
	}
	if err := n.execute(domain.Command{Kind: domain.LockApp}); err != nil {
		t.Fatal(err)
	}
	if err := unlock(n, "replacement-456"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "goim.db"))
	if err != nil || bytes.Contains(raw, []byte("secret-0123")) || bytes.Contains(raw, []byte("replacement-456")) {
		t.Fatal("plaintext password persisted")
	}
}

func TestLargeLocalAvatarIsCanonicalizedBeforePublication(t *testing.T) {
	n := testOptimizationNode(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "camera.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2600, 1800))
	rng := rand.New(rand.NewPCG(41, 42))
	for i := 0; i < len(img.Pix); i += 4 {
		color := rng.Uint32()
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(color), byte(color>>8), byte(color>>16), 255
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() < 3<<20 {
		t.Fatal("fixture must be a photo of at least 3 MiB")
	}
	if err := n.setProfile("测试相机头像", path); err != nil {
		t.Fatal(err)
	}
	avatar := n.Snapshot().Self.Avatar
	if !strings.HasPrefix(avatar, filepath.Join(n.DataDir(), "avatars")+string(filepath.Separator)) {
		t.Fatalf("raw source path in profile: %s", avatar)
	}
	data, err := os.ReadFile(avatar)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != 192 || config.Height != 192 || len(data) > 192*192*4 {
		t.Fatalf("bad canonical avatar (%+v, %d bytes): %v", config, len(data), err)
	}
}

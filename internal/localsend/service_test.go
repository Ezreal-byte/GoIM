package localsend

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goim/internal/domain"
)

type fixture struct {
	*Service
	destination string
	mu          sync.Mutex
	history     map[string]domain.Transfer
}

func newFixture(t *testing.T, alias string, http, pin bool) *fixture {
	t.Helper()
	f := &fixture{destination: t.TempDir(), history: make(map[string]domain.Transfer)}
	cfg := domain.LocalSendConfig{HTTP: http}
	if pin {
		cfg.PIN = "0123"
	}
	s, err := Start(Options{Config: cfg, IdentityDir: t.TempDir(), Alias: alias, NoDiscovery: true,
		DownloadDir: func() string { return f.destination }, Persist: func(row domain.Transfer) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.history[row.ID] = row
		}})
	if err != nil {
		t.Fatal(err)
	}
	f.Service = s
	t.Cleanup(s.Close)
	return f
}

func (f *fixture) newest(status string) domain.Transfer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result domain.Transfer
	for _, row := range f.history {
		if row.Status == status && row.UpdatedAt.After(result.UpdatedAt) {
			result = row
		}
	}
	return result
}

func (f *fixture) address() string { return net.JoinHostPort("127.0.0.1", portString(f.port)) }

func waitFor[T any](t *testing.T, fn func() (T, bool)) T {
	t.Helper()
	timeout := time.After(8 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if value, ok := fn(); ok {
			return value
		}
		select {
		case <-ticker.C:
		case <-timeout:
			t.Fatal("timed out waiting for LocalSend state")
		}
	}
}

func (f *fixture) waitOffer(t *testing.T) domain.LocalSendOffer {
	return waitFor(t, func() (domain.LocalSendOffer, bool) { state, _ := f.View(); return state.Offer, state.Offer.ID != "" })
}

func addPeer(t *testing.T, a, b *fixture, plain bool) string {
	t.Helper()
	if err := a.AddDevice(b.address(), plain); err != nil {
		t.Fatal(err)
	}
	view, _ := a.View()
	for _, peer := range view.Devices {
		if peer.Address == b.address() {
			return peer.ID
		}
	}
	t.Fatalf("peer not found: %+v", view)
	return ""
}

func TestMutualTLSGroupPartialPINAndText(t *testing.T) {
	a := newFixture(t, "Windows GoIM", false, false)
	phone := newFixture(t, "Android", false, true)
	second := newFixture(t, "iPhone", false, false)
	id := addPeer(t, a, phone, false)
	other := addPeer(t, a, second, false)
	// A stock server requires the client to prove ownership of its announced fingerprint.
	view, _ := phone.View()
	if len(view.Devices) != 1 || view.Devices[0].ID != "https:"+a.fingerprint {
		t.Fatalf("registration did not authenticate sender: %+v", view)
	}
	folder := filepath.Join(t.TempDir(), "发给大家")
	if err := os.MkdirAll(filepath.Join(folder, "子文件夹"), 0700); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{"归档.txt": "file A", filepath.Join("子文件夹", "现场照片.jpg"): "file B"} {
		if err := os.WriteFile(filepath.Join(folder, file), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Send([]string{id, other}, []string{folder}, "", "", false); err != nil {
		t.Fatal(err)
	}
	rejected := second.waitOffer(t)
	if err := second.Answer(rejected.ID, nil, false); err != nil {
		t.Fatal(err)
	}
	failure := waitFor(t, func() (domain.Transfer, bool) { row := a.newest("failed"); return row, row.LocalSendNeedsPIN })
	if err := a.Retry(failure.ID, "0123"); err != nil {
		t.Fatal(err)
	}
	offer := phone.waitOffer(t)
	if len(offer.Files) != 2 || offer.From != "Windows GoIM" {
		t.Fatalf("unexpected offer: %+v", offer)
	}
	var accepted string
	for _, f := range offer.Files {
		if strings.Contains(f.Name, "现场照片") {
			accepted = f.ID
		}
	}
	if err := phone.Answer(offer.ID, []string{accepted}, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() (domain.Transfer, bool) {
		row := a.newest("partial")
		return row, row.Done == 6 && row.ID == failure.ID
	})
	real := filepath.Join(phone.destination, "发给大家", "子文件夹", "现场照片.jpg")
	if got, err := os.ReadFile(real); err != nil || string(got) != "file B" {
		t.Fatalf("saved data %q: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(phone.destination, "发给大家", "归档.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected unselected file: %v", err)
	}

	back := addPeer(t, phone, a, false)
	if err := phone.Send([]string{back}, nil, "你好，GoIM", "", false); err != nil {
		t.Fatal(err)
	}
	msg := a.waitOffer(t)
	if !msg.IsText || msg.Text != "你好，GoIM" {
		t.Fatalf("bad message: %+v", msg)
	}
	if err := a.Answer(msg.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() (domain.Transfer, bool) {
		row := phone.newest("completed")
		return row, row.Direction == "send" && row.Name == "文字消息"
	})
}

func TestExplicitPlainAndPendingCancel(t *testing.T) {
	a := newFixture(t, "HTTPS sender", false, false)
	b := newFixture(t, "HTTP receiver", true, false)
	id := addPeer(t, a, b, true)
	if err := a.Send([]string{id}, nil, "test", "", false); err == nil {
		t.Fatal("HTTP should require explicit permission")
	}
	if err := a.Send([]string{id}, nil, "test", "", true); err != nil {
		t.Fatal(err)
	}
	_ = b.waitOffer(t)
	active := a.newest("offered")
	if active.ID == "" || !a.Cancel(active.ID) {
		t.Fatal("could not cancel pending send")
	}
	waitFor(t, func() (domain.LocalSendStatus, bool) { v, _ := b.View(); return v, v.Offer.ID == "" })
}

func TestCertificatePinRejectedWhenEndpointChanges(t *testing.T) {
	a := newFixture(t, "sender", false, false)
	b := newFixture(t, "receiver", false, false)
	addPeer(t, a, b, false)
	wrong := strings.Repeat("0", 64)
	err := a.probe(context.Background(), "127.0.0.1", info{Port: b.port, Protocol: "https", Fingerprint: wrong}, false)
	if err == nil || !strings.Contains(err.Error(), "证书已变更") {
		t.Fatalf("unexpected result: %v", err)
	}
}

func TestPublishAvoidsOverwriteAndRejectsBadPayload(t *testing.T) {
	dir := t.TempDir()
	f := fileInfo{Name: "照片/文档.txt", Size: 4, Type: "text"}
	p := newProgress(func(int64) {})
	a, err := publishFile(context.Background(), dir, f, strings.NewReader("one1"), p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := publishFile(context.Background(), dir, f, strings.NewReader("two2"), p)
	if err != nil || a == b {
		t.Fatalf("second copy %q: %v", b, err)
	}
	data, err := os.ReadFile(a)
	if err != nil || string(data) != "one1" {
		t.Fatalf("first copy was changed: %q, %v", data, err)
	}
	f.SHA = strings.Repeat("0", 64)
	if _, err := publishFile(context.Background(), dir, f, strings.NewReader("two2"), p); !errors.Is(err, errHash) {
		t.Fatalf("bad SHA: %v", err)
	}
	if names, err := os.ReadDir(filepath.Dir(a)); err != nil || len(names) != 2 {
		t.Fatalf("partial file remains: %v, %v", names, err)
	}
	for _, name := range []string{"../out", "目录/../out", "C:/out", "aa\\out", "x//y", "x:stream", "/绝对路径"} {
		if validName(name) {
			t.Errorf("accepted unsafe name %q", name)
		}
	}
}

func TestLocalSendNativeEndpointsOnly(t *testing.T) {
	s := newFixture(t, "GoIM", true, false)
	base := "http://" + s.address()
	reqs := []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodPost, prefix + "prepare-download"},
		{http.MethodGet, prefix + "download"},
	}
	for _, request := range reqs {
		r, err := http.NewRequest(request.method, base+request.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s = %d", request.method, request.path, response.StatusCode)
		}
	}
	payload := `{"info":{"alias":"Browser","version":"2.2","deviceType":"web","fingerprint":"browser","port":53317,"protocol":"http"},"files":{"id":{"id":"id","fileName":"test.txt","size":5,"fileType":"text"}}}`
	resp, err := http.Post(base+prefix+"prepare-upload", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("browser sender accepted: %s", resp.Status)
	}
}

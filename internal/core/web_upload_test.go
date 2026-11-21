package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestBrowserUploadApprovalAndHistory(t *testing.T) {
	n := testNode(t, "owner")
	if err := n.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	device := n.Snapshot().DeviceShare
	if !device.AutoAcceptUntil.IsZero() {
		t.Fatal("approval must default to on")
	}
	post := func(name string, size int64) webTicket {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"name": name, "size": size})
		resp, err := http.Post(device.URL+"/upload/request", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var ticket webTicket
		if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&ticket) != nil {
			t.Fatalf("offer HTTP %d", resp.StatusCode)
		}
		return ticket
	}
	put := func(id, body string, status int) {
		t.Helper()
		req, _ := http.NewRequest("PUT", device.URL+"/upload/"+id, strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			msg, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload HTTP %d, want %d: %s", resp.StatusCode, status, msg)
		}
	}
	row := func(id string) domain.Transfer {
		t.Helper()
		for _, r := range n.Snapshot().Transfers {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("missing transfer %s", id)
		return domain.Transfer{}
	}
	first := post("批准.txt", 4)
	if first.Status != "offered" {
		t.Fatal(first)
	}
	put(first.ID, "data", 403)
	if entries, e := os.ReadDir(device.ReceiveDir); e != nil || len(entries) != 0 {
		t.Fatal("unapproved bytes on disk", entries, e)
	}
	if err := n.execute(domain.Command{Kind: domain.AcceptTransfer, TransferID: first.ID}); err != nil {
		t.Fatal(err)
	}
	put(first.ID, "xx", 400)
	put(first.ID, "data", 204)
	got := row(first.ID)
	if got.Status != "completed" || got.WebSender != "127.0.0.1" || got.Done != 4 || got.Size != 4 || filepath.Dir(got.Path) != device.ReceiveDir {
		t.Fatalf("history: %#v", got)
	}
	if data, err := os.ReadFile(got.Path); err != nil || string(data) != "data" {
		t.Fatal("incorrect file", err)
	}
	put(first.ID, "more", 410)
	declined := post("拒绝.txt", 0)
	if err := n.execute(domain.Command{Kind: domain.RejectTransfer, TransferID: declined.ID}); err != nil {
		t.Fatal(err)
	}
	put(declined.ID, "", 410)
	if row(declined.ID).Status != "rejected" {
		t.Fatal("missing rejection")
	}
	if err := n.setWebApproval(false); err != nil {
		t.Fatal(err)
	}
	until := n.Snapshot().DeviceShare.AutoAcceptUntil
	if d := time.Until(until); d <= 0 || d > 10*time.Minute {
		t.Fatalf("automatic window %v", d)
	}
	if err := n.setWebApproval(false); err != nil || !until.Equal(n.Snapshot().DeviceShare.AutoAcceptUntil) {
		t.Fatal("repeated command extended the window")
	}
	auto := post("取消.txt", 0)
	if auto.Status != "waiting" {
		t.Fatal(auto)
	}
	if err := n.setWebApproval(true); err != nil {
		t.Fatal(err)
	}
	put(auto.ID, "", 403)
	if err := n.setWebApproval(false); err != nil {
		t.Fatal(err)
	}
	expired := post("超时.txt", 0)
	n.mu.Lock()
	n.data.WebAutoUntil = time.Now().Add(-time.Second)
	n.mu.Unlock()
	// A previously issued permit cannot outlive the window, even between heartbeats.
	put(expired.ID, "", 403)
	if row(expired.ID).Status != "offered" || !n.Snapshot().DeviceShare.AutoAcceptUntil.IsZero() {
		t.Fatal("unattended mode survived its deadline")
	}
	n.expireWebUploads(time.Now().Add(11 * time.Minute))
	if row(expired.ID).Status != "failed" {
		t.Fatal("abandoned request did not expire")
	}
	if err := n.setWebApproval(false); err != nil {
		t.Fatal(err)
	}
	interrupted := post("重启.txt", 0)
	dir := n.DataDir()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	n = testNodeAt(t, "owner", dir)
	if row(first.ID).Status != "completed" || row(interrupted.ID).Status != "failed" || !n.Snapshot().DeviceShare.AutoAcceptUntil.IsZero() {
		t.Fatal("restart lost history or revived a permit")
	}
}

func TestWebUploadRecoveryRecognizesOnlyPublishedStage(t *testing.T) {
	inbox := filepath.Join(t.TempDir(), domain.WebReceiveFolder)
	if err := os.Mkdir(inbox, 0700); err != nil {
		t.Fatal(err)
	}
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpublished", true: "linked"}[published], func(t *testing.T) {
			id := newID()
			stage := ".goim-upload-" + id
			row := domain.Transfer{ID: id, WebSender: "127.0.0.1", WebInbox: inbox, ShareStage: stage, Status: "transferring", Path: filepath.Join(inbox, id+".txt")}
			tmp := filepath.Join(inbox, stage)
			if err := os.WriteFile(tmp, []byte("body"), 0600); err != nil {
				t.Fatal(err)
			}
			if published {
				if err := os.Link(tmp, row.Path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(row.Path, []byte("other"), 0600); err != nil {
				t.Fatal(err)
			}
			recoverWebUpload(&row)
			if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
				t.Fatal("stage left behind", err)
			}
			if published && (row.Status != "completed" || row.Done != 4) || !published && row.Status != "failed" {
				t.Fatalf("recovery: %#v", row)
			}
		})
	}
}

func TestWebUploadProgressAndCancellation(t *testing.T) {
	n := testNode(t, "owner")
	if err := n.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	if err := n.setWebApproval(false); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	token := n.data.BrowserHomeToken
	n.mu.Unlock()
	device := n.Snapshot().DeviceShare
	task, err := n.offerWebUpload(token, "127.0.0.1", "partial.bin", 1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	in, out := io.Pipe()
	defer out.Close()
	req, err := http.NewRequestWithContext(t.Context(), "PUT", device.URL+"/upload/"+task.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = task.Size
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := http.DefaultClient.Do(req)
		if err == nil {
			response.Body.Close()
		}
	}()
	if _, err = out.Write(make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "record received bytes before completion", func() bool {
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.ID == task.ID {
				return transfer.Status == "transferring" && transfer.Done > 0 && transfer.Done < transfer.Size
			}
		}
		return false
	})
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	out.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the upload")
	}
	eventually(t, "cancellation cleans the partial and preserves history", func() bool {
		files, err := os.ReadDir(device.ReceiveDir)
		for _, transfer := range n.Snapshot().Transfers {
			if transfer.ID == task.ID {
				return err == nil && len(files) == 0 && transfer.Status == "cancelled"
			}
		}
		return false
	})
}

func TestWebUploadPendingLimitAndCrossOrigin(t *testing.T) {
	n := testNode(t, "owner")
	if err := n.setDeviceShare(true); err != nil {
		t.Fatal(err)
	}
	device := n.Snapshot().DeviceShare
	req, _ := http.NewRequest("POST", device.URL+"/upload/request", strings.NewReader(`{"name":"test","size":0}`))
	req.Header.Set("Origin", "https://another-origin.example")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || len(n.Snapshot().Transfers) != 0 {
		t.Fatal("unexpected cross-origin request result", resp.Status)
	}
	n.mu.Lock()
	token := n.data.BrowserHomeToken
	n.mu.Unlock()
	for range 8 {
		if _, err := n.offerWebUpload(token, "127.0.0.1", "test", 0, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := n.offerWebUpload(token, "127.0.0.1", "extra", 0, false); err != errUploadBusy {
		t.Fatal("per-sender cap", err)
	}
	if _, err := n.offerWebUpload(token, "127.0.0.2", "other", 0, false); err != nil {
		t.Fatal("other sender blocked", err)
	}
}

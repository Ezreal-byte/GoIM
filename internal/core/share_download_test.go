package core

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"goim/internal/domain"
	"goim/internal/shareclient"
)

func TestMemberBatchDownloadKeepsFoldersAndTransferHistory(t *testing.T) {
	owner, member := testNode(t, "owner"), testNode(t, "member")
	connectNodes(t, owner, member)
	if err := owner.execute(domain.Command{Kind: domain.SetSetting, Setting: "share_download_stats", Bool: true}); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	folder := filepath.Join(base, "资料")
	if err := os.MkdirAll(filepath.Join(folder, "子目录", "空目录"), 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("shared content")
	for _, file := range []string{"公开.txt", "加密.txt", "资料/子目录/明细.txt"} {
		if err := os.WriteFile(filepath.Join(base, file), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, cmd := range []domain.Command{
		{Kind: domain.ShareFiles, Paths: []string{folder, filepath.Join(base, "公开.txt")}},
		{Kind: domain.ShareFiles, Paths: []string{filepath.Join(base, "加密.txt")}, Text: "local password"},
	} {
		if err := owner.execute(cmd); err != nil {
			t.Fatal(err)
		}
	}
	var project, protected, ordinary domain.File
	eventually(t, "catalog ready", func() bool {
		for _, f := range member.Snapshot().Files {
			switch {
			case f.ShareFolder:
				project = f
			case f.ShareProtected:
				protected = f
			case f.Shared:
				ordinary = f
			}
		}
		return project.MemberURL != "" && protected.MemberURL != "" && ordinary.ID != ""
	})
	client, err := shareclient.New(protected.MemberURL)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := client.List(t.Context(), "", "")
	if err != nil || !gate.Locked {
		t.Fatal(gate, err)
	}
	unlocked, err := client.List(t.Context(), gate.Path, "local password")
	if err != nil {
		t.Fatal(err)
	}
	secret := unlocked.Whole()
	batch := []domain.ShareDownload{
		{ProjectID: ordinary.ID},
		{ProjectID: project.ID, Name: project.Name, Folder: true},
		{ProjectID: protected.ID, Name: secret.Name, Session: client.Session()},
	}
	client.Close() // leaving the detail page must not cancel the submitted batch
	browser, err := shareclient.New(project.MemberURL)
	if err != nil {
		t.Fatal(err)
	}
	root, err := browser.List(t.Context(), "", "")
	if err != nil || len(root.Items) != 1 {
		t.Fatal(root, err)
	}
	sub, err := browser.List(t.Context(), root.Items[0].URL, "")
	if err != nil || len(sub.Items) != 2 {
		t.Fatal(sub, err)
	}
	for _, e := range sub.Items {
		batch = append(batch, domain.ShareDownload{ProjectID: project.ID, Name: e.Name, Index: e.Index(), Folder: e.Folder})
	}
	browser.Close()
	dest := member.Snapshot().Settings.DownloadDir
	if err := os.MkdirAll(dest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, secret.Name), []byte("keep original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := member.execute(domain.Command{Kind: domain.GetShared, ShareDownloads: batch}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "all five selected downloads completed", func() bool {
		rows := member.Snapshot().Transfers
		for _, row := range rows {
			if row.Status == "failed" || row.Status == "rejected" {
				t.Fatalf("transfer failed: %+v", row)
			}
			if row.Status != "completed" {
				return false
			}
		}
		return len(rows) == len(batch)
	})
	eventually(t, "owner counted every delivered file", func() bool {
		state := owner.Snapshot()
		counts := make(map[string]uint64)
		for _, f := range state.Files {
			counts[f.Name] = f.ShareDownloadCount
			if f.ShareFolder && (len(f.ShareDownloadFiles) != 1 ||
				f.ShareDownloadFiles[0] != (domain.ShareFileDownloads{Path: "子目录/明细.txt", Count: 2})) {
				return false
			}
		}
		return counts["公开.txt"] == 1 && counts["加密.txt"] == 1 && counts["资料"] == 2
	})
	for _, file := range []string{"公开.txt", "加密 (1).txt", "资料/子目录/明细.txt", "明细.txt"} {
		got, err := os.ReadFile(filepath.Join(dest, file))
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("contents of %s: %q, %v", file, got, err)
		}
	}
	for _, folder := range []string{"空目录", "资料/子目录/空目录"} {
		if children, err := os.ReadDir(filepath.Join(dest, folder)); err != nil || len(children) != 0 {
			t.Fatalf("empty folder %s: %v %v", folder, children, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dest, secret.Name)); err != nil || string(got) != "keep original" {
		t.Fatal("original replaced", err)
	}
	stateDir := member.DataDir()
	if err := member.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := testNodeAt(t, "member", stateDir)
	history := restarted.Snapshot().Transfers
	if len(history) != len(batch) {
		t.Fatal("missing persisted rows", history)
	}
	for _, task := range history {
		if !task.FromShare || task.Direction != "receive" || task.Status != "completed" || task.Done != task.Size || task.ShareStage != "" {
			t.Fatalf("incorrect transfer history: %+v", task)
		}
	}
}

func TestSameNamedCatalogDownloadsGetDistinctDestinations(t *testing.T) {
	owner, member := testNode(t, "owner"), testNode(t, "member")
	connectNodes(t, owner, member)
	want := map[string]bool{"first": true, "second": true}
	for value := range want {
		path := filepath.Join(t.TempDir(), "notes.txt")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := owner.execute(domain.Command{Kind: domain.ShareFiles, Paths: []string{path}}); err != nil {
			t.Fatal(err)
		}
	}
	var batch []domain.ShareDownload
	eventually(t, "both notes in member catalog", func() bool {
		batch = nil
		for _, f := range member.Snapshot().Files {
			if f.Shared {
				batch = append(batch, domain.ShareDownload{ProjectID: f.ID})
			}
		}
		return len(batch) == 2
	})
	if err := member.execute(domain.Command{Kind: domain.GetShared, ShareDownloads: batch}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "both notes received", func() bool {
		rows := member.Snapshot().Transfers
		return len(rows) == 2 && rows[0].Status == "completed" && rows[1].Status == "completed"
	})
	for _, name := range []string{"notes.txt", "notes (1).txt"} {
		content, err := os.ReadFile(filepath.Join(member.Snapshot().Settings.DownloadDir, name))
		if err != nil || !want[string(content)] {
			t.Fatalf("%s: %q, %v", name, content, err)
		}
		delete(want, string(content))
	}
}

func TestMemberDownloadProgressCancelAndRetry(t *testing.T) {
	owner, member := testNode(t, "owner"), testNode(t, "member")
	connectNodes(t, owner, member)
	project := "/s/" + newID()
	key := newID()
	part := strings.Repeat("a", 130000)
	resume := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/m/" + key:
			http.Redirect(w, r, project, http.StatusFound)
		case project:
			w.Header().Set("Content-Type", shareclient.MediaType)
			size := int64(2 * len(part))
			_ = json.NewEncoder(w).Encode(shareclient.Page{Title: "Folder", Items: []shareclient.Entry{{Name: "file.txt", URL: project + "/download?file=1", Bytes: &size}}})
		case project + "/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(2*len(part)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write([]byte(part))
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-resume:
				_, _ = w.Write([]byte(part))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	peerID := owner.Snapshot().Self.ID
	port := uint16(server.Listener.Addr().(*net.TCPAddr).Port)
	member.mu.Lock()
	pc := member.connections[peerID]
	member.mu.Unlock()
	if err := member.receiveFrom(peerID, envelope{Type: "catalog", CatalogRevision: 999, MemberCatalog: &memberCatalog{Port: port, Projects: []memberProject{{Key: key, Name: "Folder"}}}}, pc); err != nil {
		t.Fatal(err)
	}
	err := member.execute(domain.Command{Kind: domain.GetShared, ShareDownloads: []domain.ShareDownload{{ProjectID: "member:" + peerID + ":" + key, Name: "Folder", Folder: true}}})
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Transfer
	eventually(t, "positive progress while source still blocked", func() bool {
		task = member.Snapshot().Transfers[0]
		return task.Status == "transferring" && task.Size == int64(len(part)*2) && task.Done > 0 && task.Done < task.Size
	})
	stage := task.ShareStage
	if stage == "" {
		t.Fatal("missing stage")
	}
	if err := member.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "canceled staging cleaned", func() bool {
		_, err := os.Stat(stage)
		return os.IsNotExist(err) && member.Snapshot().Transfers[0].Status == "cancelled"
	})
	if _, err := os.Stat(task.Path); !os.IsNotExist(err) {
		t.Fatal("partial folder published", err)
	}
	close(resume)
	if err := member.execute(domain.Command{Kind: domain.RetryTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "retry completed", func() bool { return member.Snapshot().Transfers[0].Status == "completed" })
	file, err := os.ReadFile(filepath.Join(task.Path, "file.txt"))
	if err != nil || string(file) != part+part {
		t.Fatalf("retried download: %d bytes, %v", len(file), err)
	}
}

func TestCancelledCatalogRequestIgnoresLateOffer(t *testing.T) {
	n := testNode(t, "member")
	peer := newID()
	f := domain.File{ID: newID(), SenderID: peer, Name: "pending.txt", Hash: strings.Repeat("a", 64), Shared: true}
	if err := n.change(func(s *domain.State) { s.Files = append(s.Files, f) }); err != nil {
		t.Fatal(err)
	}
	if err := n.queueShareDownloads([]domain.ShareDownload{{ProjectID: f.ID}}); err != nil {
		t.Fatal(err)
	}
	task := n.Snapshot().Transfers[0]
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: task.ID}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{task.ID, ""} {
		offer := envelope{Type: "file_offer", TransferID: newID(), ShareRequestID: request, File: f}
		if err := n.receiveOffer(peer, offer); err != nil {
			t.Fatal(err)
		}
	}
	s := n.Snapshot()
	if len(s.Transfers) != 1 || s.Transfers[0].Status != "cancelled" || len(s.Messages) != 0 {
		t.Fatal("late offer revived a canceled download")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, pending := range n.data.Outbox {
		if pending.Envelope.Type == "get_shared" {
			t.Fatal("canceled request in outbox")
		}
	}
}

func TestShareRequestCancelFollowsOfferID(t *testing.T) {
	n := testNode(t, "member")
	peer := newID()
	f := domain.File{ID: newID(), SenderID: peer, Name: "test.txt", Hash: strings.Repeat("a", 64), Shared: true}
	if err := n.change(func(s *domain.State) { s.Files = append(s.Files, f) }); err != nil {
		t.Fatal(err)
	}
	if err := n.queueShareDownloads([]domain.ShareDownload{{ProjectID: f.ID}}); err != nil {
		t.Fatal(err)
	}
	requestID := n.Snapshot().Transfers[0].ID
	offer := envelope{Type: "file_offer", File: f, TransferID: newID(), ShareRequestID: requestID}
	if err := n.receiveOffer(peer, offer); err != nil {
		t.Fatal(err)
	}
	if err := n.execute(domain.Command{Kind: domain.CancelTransfer, TransferID: requestID}); err != nil {
		t.Fatal(err)
	}
	rows := n.Snapshot().Transfers
	if len(rows) != 1 || rows[0].ID != offer.TransferID || rows[0].Status != "cancelled" {
		t.Fatal("cancellation missed the incoming offer", rows)
	}
}

func TestInterruptedShareRemovesOnlyItsOwnStage(t *testing.T) {
	parent := t.TempDir()
	id := newID()
	stage, err := os.MkdirTemp(parent, directoryStagePrefix+id+"-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "incomplete.txt"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	task := domain.Transfer{ID: id, ShareFolder: true, Path: filepath.Join(parent, "final"), ShareStage: stage, Status: "transferring"}
	resetShareDownload(&task)
	if _, err := os.Stat(stage); !os.IsNotExist(err) || task.Status != "failed" || task.ShareStage != "" {
		t.Fatal(task, err)
	}
	original := filepath.Join(parent, "original.txt")
	if err := os.WriteFile(original, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	task.ShareStage = original
	resetShareDownload(&task)
	if data, err := os.ReadFile(original); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file changed", err)
	}
}

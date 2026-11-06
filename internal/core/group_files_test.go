package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"goim/internal/domain"
)

func TestGroupFileConfirmationAndRetryArePerRecipient(t *testing.T) {
	a, b, c := testNode(t, "群主"), testNode(t, "乙"), testNode(t, "丙")
	id := createTestGroup(t, a, b, c)
	source := filepath.Join(t.TempDir(), "群文件.txt")
	content := bytes.Repeat([]byte("群文件逐成员确认与校验\n"), 4096)
	_ = os.WriteFile(source, content, 0600)
	if err := a.sendFile("group:"+id, source, false, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "per-member offers", func() bool { return len(b.Snapshot().Transfers) == 1 && len(c.Snapshot().Transfers) == 1 })
	b.startReceive(b.Snapshot().Transfers[0].ID, "")
	if err := c.execute(domain.Command{Kind: domain.RejectTransfer, TransferID: c.Snapshot().Transfers[0].ID}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "independent recipient outcomes", func() bool {
		completed, rejected := 0, 0
		for _, v := range a.Snapshot().Transfers {
			if v.PeerID == b.Snapshot().Self.ID && v.Status == "completed" {
				completed++
			}
			if v.PeerID == c.Snapshot().Self.ID && v.Status == "rejected" {
				rejected++
			}
		}
		return completed == 1 && rejected == 1
	})
	received, err := os.ReadFile(b.Snapshot().Transfers[0].Path)
	if err != nil || !bytes.Equal(received, content) {
		t.Fatalf("recipient bytes differ: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Snapshot().Settings.DownloadDir, "群文件.txt")); !os.IsNotExist(err) {
		t.Fatal("rejecting member received data")
	}
	if err := c.retryTransfer(c.Snapshot().Transfers[0]); err != nil {
		t.Fatal(err)
	}
	eventually(t, "only rejected member retries", func() bool { s := c.Snapshot(); return len(s.Transfers) == 2 && s.Transfers[1].Status == "completed" })
	if len(b.Snapshot().Transfers) != 1 || b.Snapshot().Transfers[0].Status != "completed" {
		t.Fatal("one member's retry changed another member's task")
	}
	if hash, err := fileHash(c.Snapshot().Transfers[1].Path); err != nil || hash != a.Snapshot().Files[0].Hash {
		t.Fatalf("retried group file hash: %s %v", hash, err)
	}
}

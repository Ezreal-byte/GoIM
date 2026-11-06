package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGroupWaitsForFirstAcceptedRecipient(t *testing.T) {
	a, b, c := testNode(t, "甲"), testNode(t, "乙"), testNode(t, "丙")
	connectNodes(t, a, b)
	connectNodes(t, a, c)
	if err := a.createGroup("等待确认", []string{b.Snapshot().Self.ID, c.Snapshot().Self.ID}); err != nil {
		t.Fatal(err)
	}
	conv := "group:" + a.Snapshot().Groups[0].ID
	if err := a.sendText(conv, "不能假装发出"); err == nil {
		t.Fatal("queued a message with no recipient")
	}
	path := filepath.Join(t.TempDir(), "pending.txt")
	_ = os.WriteFile(path, []byte("file"), 0600)
	if err := a.sendFile(conv, path, false, ""); err == nil {
		t.Fatal("created a file with no recipient")
	}
	if len(a.Snapshot().Messages) != 0 || len(a.Snapshot().Files) != 0 {
		t.Fatal("unaddressed payload was recorded as sent")
	}
}

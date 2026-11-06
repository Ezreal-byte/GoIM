package core

import (
	"strings"
	"testing"

	"goim/internal/domain"
)

func TestFileNamesAndIDsCannotEscapePrivatePartialDirectory(t *testing.T) {
	base := domain.File{ID: strings.Repeat("a", 32), Name: "正常.txt", Hash: strings.Repeat("b", 64), Size: 12}
	if !validFile(base) {
		t.Fatal("valid file rejected")
	}
	for _, name := range []string{"../escape", `..\escape`, ".", "..", `C:\absolute`, "a\x00b", "dir/file"} {
		f := base
		f.Name = name
		if validFile(f) {
			t.Fatalf("unsafe file accepted: %q", name)
		}
	}
	for _, id := range []string{"../escape", `..\escape`, "a", strings.Repeat("z", 32)} {
		f := base
		f.ID = id
		if validFile(f) {
			t.Fatalf("unsafe id accepted: %q", id)
		}
	}
}
func TestTerminalTransferStateIsNotOverwrittenByLateProgress(t *testing.T) {
	n := testNode(t, "terminal")
	n.change(func(s *domain.State) {
		s.Transfers = append(s.Transfers, domain.Transfer{ID: "test", Status: "completed", Done: 100, Size: 100})
	})
	n.setTransfer("test", "verifying", 100, 0, "")
	n.setTransfer("test", "transferring", 20, 12, "")
	if got := n.Snapshot().Transfers[0]; got.Status != "completed" || got.Done != 100 {
		t.Fatalf("completion overwritten: %+v", got)
	}
	n.change(func(s *domain.State) { s.Transfers[0].Status = "cancelled" })
	n.setTransfer("test", "waiting", 20, 0, "")
	if n.Snapshot().Transfers[0].Status != "cancelled" {
		t.Fatal("cancelled task revived")
	}
}

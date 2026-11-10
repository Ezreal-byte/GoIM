package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
)

func TestMessageMenuCopyRecallDeleteAndSave(t *testing.T) {
	state := shellTestState(t)
	data := []byte("original attachment")
	source := filepath.Join(t.TempDir(), "设计素材.zip")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	state.Files[0].SourcePath = source
	state.Notice = "已保存"
	h := newShellTestHarness(t, state)
	h.waitFor(func() bool { return h.ui.fileAvailable["file-1"] })
	if h.hasText("已保存") {
		t.Fatal("generic saved notice leaked into chat")
	}
	right := func(id ui.ID) {
		t.Helper()
		hit, ok := h.findHit(id)
		if !ok {
			t.Fatalf("missing message hit %v", id)
		}
		if err := h.driver.ClickButton(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2, ui.PointerButtonSecondary); err != nil {
			t.Fatal(err)
		}
		if h.ui.shellMenu.kind != shellMenuMessage {
			t.Fatal("right-click did not open the message menu")
		}
	}
	right(h.ui.messageIDs["message-1"])
	if h.hasHit(h.ui.ids.messageMenuRecall) {
		t.Fatal("recall exposed for another sender")
	}
	h.clickID(h.ui.ids.messageMenuCopy)
	if got := h.driver.Clipboard(); got != state.Messages[0].Text {
		t.Fatalf("copied %q", got)
	}
	right(h.ui.messageIDs["message-2"])
	h.clickID(h.ui.ids.messageMenuRecall)
	if cmd := h.lastCommand(); cmd.Kind != domain.RecallMessage || cmd.MessageID != "message-2" {
		t.Fatalf("recall command = %+v", cmd)
	}
	right(h.ui.messageIDs["message-1"])
	h.clickID(h.ui.ids.messageMenuDelete)
	h.clickID(h.ui.ids.confirmRun)
	if cmd := h.lastCommand(); cmd.Kind != domain.DeleteMessage || cmd.MessageID != "message-1" {
		t.Fatalf("delete command = %+v", cmd)
	}

	right(h.ui.fileButtonIDs["file-1"])
	destination := filepath.Join(t.TempDir(), "saved.zip")
	if err := h.driver.QueueSaveFile(destination, nil); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.ids.messageMenuSave)
	h.waitFor(func() bool { saved, err := os.ReadFile(destination); return err == nil && bytes.Equal(saved, data) })
}

package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type conversationTestWindow struct {
	*app.Driver
}

func (window conversationTestWindow) Frame() (gfx.Rect, bool) {
	return window.Driver.Frame(), true
}

func (window conversationTestWindow) SetSize(width, height int) error {
	return window.Resize(width, height)
}

func TestConversationRowDraggedOutsideOpensDetachedWindow(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.attachNativeWindow(conversationTestWindow{h.driver})
	var detachedID string
	h.ui.presentDetached = func(id string) bool {
		detachedID = id
		return true
	}
	hit, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("conversation row is not on screen")
	}
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(-20, y); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Release(-20, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	h.settle()

	if detachedID != "conv-group" || !h.ui.detached["conv-group"] {
		t.Fatalf("detached conversation = %q, detached map = %+v", detachedID, h.ui.detached)
	}
	if h.ui.selected == "conv-group" {
		t.Fatal("main window kept a detached conversation selected")
	}
	if h.hasHit(h.ui.convRowIDs["conv-group"]) {
		t.Fatal("detached conversation remained in the main conversation list")
	}

	h.ui.restoreDetachedConversation("conv-group")
	h.settle()
	if !h.hasHit(h.ui.convRowIDs["conv-group"]) {
		t.Fatal("closing the detached window did not return the conversation row")
	}
}

func TestConversationDragInsideMainWindowDoesNotDetach(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.attachNativeWindow(conversationTestWindow{h.driver})
	called := false
	h.ui.presentDetached = func(string) bool {
		called = true
		return true
	}
	hit, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("conversation row is not on screen")
	}
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(x+80, y+40); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Release(x+80, y+40, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	h.settle()

	if called || h.ui.detached["conv-group"] || h.ui.selected != "conv-group" {
		t.Fatalf("inside-window drag detached=%v selected=%q called=%v",
			h.ui.detached["conv-group"], h.ui.selected, called)
	}
}

func TestDetachedConversationViewRendersOnlyItsConversation(t *testing.T) {
	backend := newShellTestBackend(shellTestState(t))
	view := newDetachedConversationView(backend, "conv-alice")
	t.Cleanup(view.Close)
	config := view.DetachedConfig()
	if config.Title != "Alice · GoIM" || config.WindowMode != app.WindowTitled {
		t.Fatalf("detached config title=%q mode=%v", config.Title, config.WindowMode)
	}
	closed := false
	baseClosed := config.OnClosed
	config.OnClosed = func() {
		closed = true
		baseClosed()
	}
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	snapshot := driver.Snapshot()
	foundHeader, foundMessage := false, false
	for _, text := range snapshot.Texts {
		foundHeader = foundHeader || text.Value == "Alice"
		foundMessage = foundMessage || text.Value == "晚点确认。"
	}
	if !foundHeader || !foundMessage {
		t.Fatalf("detached view did not render its conversation: header=%v message=%v",
			foundHeader, foundMessage)
	}
	if !view.hasDetachedHit(driver.Snapshot(), view.ids.chatSend) {
		t.Fatal("detached view did not render its composer")
	}
	driver.Close()
	if !closed {
		t.Fatal("detached window close did not run its return lifecycle")
	}
}

func (u *UI) hasDetachedHit(snapshot app.Snapshot, id gfx.ID) bool {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

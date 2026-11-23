package desktop

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

type conversationTestWindow struct {
	*app.Driver
	topmost bool
}

func (window *conversationTestWindow) Frame() (gfx.Rect, bool) {
	return window.Driver.Frame(), true
}

func (window *conversationTestWindow) SetSize(width, height int) error {
	return window.Resize(width, height)
}

func (window *conversationTestWindow) SetAlwaysOnTop(enabled bool) error {
	window.topmost = enabled
	return nil
}

func (window *conversationTestWindow) AlwaysOnTop() bool {
	return window.topmost
}

func TestConversationRowDraggedOutsideOpensDetachedWindow(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.attachNativeWindow(&conversationTestWindow{Driver: h.driver})
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
	if h.ui.selected != "" {
		t.Fatalf("main window did not leave detached content blank: %q", h.ui.selected)
	}
	if !h.hasHit(h.ui.convRowIDs["conv-group"]) {
		t.Fatal("detached conversation disappeared from the main conversation list")
	}

	h.ui.restoreDetachedConversation("conv-group")
	h.settle()
	if !h.hasHit(h.ui.convRowIDs["conv-group"]) || h.ui.detached["conv-group"] {
		t.Fatal("closing the detached window did not restore the row state")
	}
}

func TestConversationRowDoubleClickOpensDetachedWindow(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	var detachedID string
	h.ui.presentDetached = func(id string) bool {
		detachedID = id
		return true
	}
	hit, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("conversation row is not on screen")
	}
	if err := h.driver.DoubleClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	h.settle()

	if detachedID != "conv-group" || !h.ui.detached["conv-group"] {
		t.Fatalf("double-click detached conversation = %q, detached map = %+v", detachedID, h.ui.detached)
	}
	if h.ui.selected != "" {
		t.Fatalf("main window did not leave double-clicked content blank: %q", h.ui.selected)
	}
	if !h.hasHit(h.ui.convRowIDs["conv-group"]) {
		t.Fatal("double-clicked conversation disappeared from the main conversation list")
	}
}

func TestConversationDragInsideMainWindowDoesNotDetach(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.attachNativeWindow(&conversationTestWindow{Driver: h.driver})
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
	if config.Title != "Alice · GoIM" {
		t.Fatalf("detached config title=%q", config.Title)
	}
	if config.WindowMode != app.WindowCustomTitlebar || config.TitlebarHeight != detachedTitlebarHeight {
		t.Fatalf("detached custom titlebar config = mode %v height %v", config.WindowMode, config.TitlebarHeight)
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
	if snapshot.Focused != view.ids.chatEditorField {
		t.Fatal("new detached conversation did not focus its composer")
	}
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
	if !view.hasDetachedHit(driver.Snapshot(), view.ids.detachedPin) {
		t.Fatal("detached view did not render its titlebar pin control")
	}
	movesWindow := false
	for _, hit := range driver.Snapshot().Hits {
		if hit.MovesWindow {
			movesWindow = true
			break
		}
	}
	if !movesWindow {
		t.Fatal("detached custom titlebar has no window move region")
	}
	driver.Close()
	if !closed {
		t.Fatal("detached window close did not run its return lifecycle")
	}
	requireShellLayout(t, snapshot)
}

func TestDetachedTitlebarPinUsesNativeAlwaysOnTop(t *testing.T) {
	state := shellTestState(t)
	backend := newShellTestBackend(state)
	view := newDetachedConversationView(backend, "conv-alice")
	t.Cleanup(view.Close)
	driver, err := app.NewDriver(view.DetachedConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	window := &conversationTestWindow{Driver: driver}
	view.attachNativeWindow(window)
	hit, ok := findDetachedHit(driver.Snapshot(), view.ids.detachedPin)
	if !ok {
		t.Fatal("detached pin control is missing")
	}
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !window.topmost || !view.detachedPinned {
		t.Fatalf("titlebar pin did not enable native always-on-top: window=%v state=%v", window.topmost, view.detachedPinned)
	}
}

func TestLockedDetachedConversationConcealsContentAndClearsOverlays(t *testing.T) {
	backend := newShellTestBackend(shellTestState(t))
	view := newDetachedConversationView(backend, "conv-alice")
	t.Cleanup(view.Close)
	driver, err := app.NewDriver(view.DetachedConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	view.surface.viewer.open, view.emojiOpen, view.detachedPinned = true, true, true
	view.concealConversation(true)
	if err := driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if view.detachedTitle() != "GoIM" || view.surface.viewer.open || view.emojiOpen {
		t.Fatal("the locked window retained its conversation title or overlays")
	}
	locked := driver.Snapshot()
	for _, text := range locked.Texts {
		if text.Value == "Alice" || text.Value == "晚点确认。" {
			t.Fatal("conversation text remained visible while locked")
		}
	}
	if view.hasDetachedHit(locked, view.ids.chatSend) || view.hasDetachedHit(locked, view.ids.detachedPin) {
		t.Fatal("conversation controls remained available while locked")
	}
	view.concealConversation(false)
	if err := driver.Paint(); err != nil {
		t.Fatal(err)
	}
	if view.detachedTitle() != "Alice · GoIM" || !view.hasDetachedHit(driver.Snapshot(), view.ids.chatSend) || !view.detachedPinned {
		t.Fatal("unlock did not restore the detached conversation and its pin state")
	}
}

func TestDetachedConversationRowKeepsPrimaryContentEmpty(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.detached["conv-alice"] = true
	h.ui.selected = ""
	h.ui.rebuildPresentation("")
	h.settle()
	if !h.hasHit(h.ui.convRowIDs["conv-alice"]) {
		t.Fatal("detached row is missing from the primary conversation list")
	}
	h.clickID(h.ui.convRowIDs["conv-alice"])
	if h.ui.selected != "" {
		t.Fatalf("clicking a detached row selected it in the primary window: %q", h.ui.selected)
	}
}

type conversationPreviewStub struct {
	moves  []gfx.Point
	closed bool
}

func (preview *conversationPreviewStub) MoveTo(point gfx.Point) {
	preview.moves = append(preview.moves, point)
}

func (preview *conversationPreviewStub) Close() {
	preview.closed = true
}

func TestConversationDragPreviewFollowsPointerAndCloses(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.attachNativeWindow(&conversationTestWindow{Driver: h.driver})
	preview := &conversationPreviewStub{}
	opened := 0
	var previewConfig app.DragPreviewConfig
	h.ui.openDragPreview = func(config app.DragPreviewConfig) (conversationDragPreview, error) {
		opened++
		previewConfig = config
		if config.Width != conversationPreviewWidth || config.Height != conversationPreviewHeight {
			t.Fatalf("drag preview size = %dx%d", config.Width, config.Height)
		}
		return preview, nil
	}
	hit, ok := h.findHit(h.ui.convRowIDs["conv-group"])
	if !ok {
		t.Fatal("conversation row is missing")
	}
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	if err := h.driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(x+20, y+5); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Move(x+40, y+15); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || len(preview.moves) < 2 {
		t.Fatalf("drag preview opened=%d moves=%v", opened, preview.moves)
	}
	canvas := &gfx.Canvas{}
	canvas.BeginFrame()
	previewConfig.Paint(canvas, gfx.Size{
		Width: float32(previewConfig.Width), Height: float32(previewConfig.Height),
	})
	canvas.EndFrame()
	texts := make(map[string]bool)
	for _, text := range canvas.Texts() {
		texts[text.Value] = true
	}
	for _, want := range []string{"产品讨论组", "Alice：看一下最新预览"} {
		if !texts[want] {
			t.Fatalf("drag preview did not show %q; texts=%v", want, texts)
		}
	}
	if err := h.driver.Release(x+40, y+15, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !preview.closed {
		t.Fatal("drag preview stayed open after pointer release")
	}
}

func TestUnavailableDragPreviewIsProbedOncePerGesture(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	opened := 0
	h.ui.openDragPreview = func(app.DragPreviewConfig) (conversationDragPreview, error) {
		opened++
		return nil, app.ErrNoDragPreview
	}
	drag := conversationDragState{}
	for range 10 {
		h.ui.openConversationDragPreview(&drag, gfx.PointerEvent{})
	}
	if opened != 1 || drag.preview != nil {
		t.Fatalf("attempts=%d preview=%v", opened, drag.preview)
	}
	drag = conversationDragState{}
	h.ui.openConversationDragPreview(&drag, gfx.PointerEvent{})
	if opened != 2 {
		t.Fatal("next gesture did not retry preview support")
	}
}

func findDetachedHit(snapshot app.Snapshot, id gfx.ID) (gfx.Hit, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

func (u *UI) hasDetachedHit(snapshot app.Snapshot, id gfx.ID) bool {
	for _, hit := range snapshot.Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

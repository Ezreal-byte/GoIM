package desktop

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func addViewerImage(t *testing.T, state *domain.State, id, name string, at time.Time, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	source := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			source.SetNRGBA(x, y, color.NRGBA{
				R: uint8(30 + x%180), G: uint8(60 + y%150), B: 210, A: 255,
			})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, source); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	state.Files = append(state.Files, domain.File{
		ID: id, Name: name, Size: int64(size * size), MIME: "image/png",
		SenderID: "peer-a", ConversationID: "conv-group", SourcePath: path, CreatedAt: at,
	})
	state.Messages = append(state.Messages, domain.Message{
		ID: "message-" + id, ConversationID: "conv-group", SenderID: "peer-a",
		Kind: "image", FileID: id, CreatedAt: at, Status: "delivered",
	})
	return path
}

func TestImageViewerNavigatesWithKeyboardAndButtons(t *testing.T) {
	state := shellTestState(t)
	now := time.Now()
	firstPath := addViewerImage(t, &state, "file-image-1", "第一张.png", now.Add(-2*time.Minute), 24)
	secondPath := addViewerImage(t, &state, "file-image-2", "第二张.png", now.Add(-time.Minute), 32)
	harness := newChatSurfaceHarness(t, state, 1440, 960)
	harness.waitFor(func() bool {
		return bitmapReady(harness.ui.fileBitmap(firstPath)) &&
			bitmapReady(harness.ui.fileBitmap(secondPath))
	})

	harness.clickID(harness.ui.fileButtonIDs["file-image-1"])
	harness.settle()
	surface := harness.ui.chatSurface()
	if !harness.ui.imageViewerOpen() || surface.viewer.fileID != "file-image-1" {
		t.Fatal("first image did not open in the viewer")
	}
	if !harness.hasText("1 / 2") || !harness.hasText("第一张.png") {
		t.Fatal("viewer did not expose the first counter and filename")
	}
	if focused := harness.snapshot().Focused; focused != surface.ids.viewerClose {
		for _, hit := range harness.snapshot().Hits {
			if hit.ID == surface.ids.viewerClose {
				t.Fatalf("viewer took focus %v on open, want %v (scope %v, focusable %v)",
					focused, surface.ids.viewerClose, hit.Scope, hit.Focusable)
			}
		}
		t.Fatalf("viewer took focus %v on open, want missing close control %v",
			focused, surface.ids.viewerClose)
	}
	if err := harness.driver.Key(ui.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	focused := harness.snapshot().Focused
	viewerControls := []ui.ID{
		surface.ids.viewerClose, surface.ids.viewerPrevious, surface.ids.viewerNext,
		surface.ids.viewerFit, surface.ids.viewerActual, surface.ids.viewerZoomOut,
		surface.ids.viewerZoomIn, surface.ids.viewerRotate,
	}
	if !slices.Contains(viewerControls, focused) {
		t.Fatalf("Tab escaped the image viewer focus scope to %v", focused)
	}
	previous, _ := harness.findHit(surface.ids.viewerPrevious)
	next, _ := harness.findHit(surface.ids.viewerNext)
	if !previous.Disabled || next.Disabled {
		t.Fatalf("first image navigation state = previous %v, next %v", previous.Disabled, next.Disabled)
	}

	if err := harness.driver.Key(ui.KeyRight, 0); err != nil {
		t.Fatal(err)
	}
	if surface.viewer.fileID != "file-image-2" || !harness.hasText("2 / 2") {
		t.Fatal("Right did not advance to the second image")
	}
	previous, _ = harness.findHit(surface.ids.viewerPrevious)
	next, _ = harness.findHit(surface.ids.viewerNext)
	if previous.Disabled || !next.Disabled {
		t.Fatalf("last image navigation state = previous %v, next %v", previous.Disabled, next.Disabled)
	}

	if err := harness.driver.Key(ui.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if surface.viewer.fileID != "file-image-1" {
		t.Fatal("Home did not return to the first image")
	}
	if err := harness.driver.Key(ui.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if surface.viewer.fileID != "file-image-2" {
		t.Fatal("End did not reach the last image")
	}

	harness.clickID(surface.ids.viewerPrevious)
	harness.settle()
	if surface.viewer.fileID != "file-image-1" {
		t.Fatal("previous button did not navigate")
	}
	harness.clickID(surface.ids.viewerNext)
	harness.settle()
	if surface.viewer.fileID != "file-image-2" {
		t.Fatal("next button did not navigate")
	}

	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.imageViewerOpen() {
		t.Fatal("Escape did not close the image viewer")
	}
	if harness.hasHit(surface.ids.viewerClose) {
		t.Fatal("closed viewer still exposed its controls")
	}

	harness.clickID(harness.ui.fileButtonIDs["file-image-1"])
	harness.settle()
	if err := harness.driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.imageViewerOpen() {
		t.Fatal("Enter did not activate the viewer's focused close control")
	}
	requireShellLayout(t, harness.snapshot())
}

func TestImageViewerZoomFitActualRotateAndWheel(t *testing.T) {
	state := shellTestState(t)
	path := addViewerImage(t, &state, "file-image-zoom", "细节.png", time.Now(), 256)
	harness := newChatSurfaceHarness(t, state, 1440, 960)
	harness.waitFor(func() bool { return bitmapReady(harness.ui.fileBitmap(path)) })
	harness.clickID(harness.ui.fileButtonIDs["file-image-zoom"])
	harness.settle()
	surface := harness.ui.chatSurface()
	stateViewer := &surface.viewer
	if !stateViewer.fit || stateViewer.fitScale <= 0 {
		t.Fatal("viewer did not start fitted to the stage")
	}

	if err := harness.driver.KeyDown(ui.KeyUnknown, "1"); err != nil {
		t.Fatal(err)
	}
	if stateViewer.fit || stateViewer.zoom != 1 {
		t.Fatalf("1:1 did not leave fit and set 100%%: fit %v zoom %.3f", stateViewer.fit, stateViewer.zoom)
	}
	if err := harness.driver.KeyDown(ui.KeyUnknown, "0"); err != nil {
		t.Fatal(err)
	}
	if !stateViewer.fit {
		t.Fatal("0 did not restore fit")
	}

	if err := harness.driver.KeyDown(ui.KeyUnknown, "+"); err != nil {
		t.Fatal(err)
	}
	if stateViewer.fit || stateViewer.zoom <= stateViewer.fitScale {
		t.Fatalf("zoom in did not exceed the fit scale: %.3f vs %.3f", stateViewer.zoom, stateViewer.fitScale)
	}
	zoomed := stateViewer.zoom
	if err := harness.driver.KeyDown(ui.KeyUnknown, "-"); err != nil {
		t.Fatal(err)
	}
	if stateViewer.zoom >= zoomed {
		t.Fatalf("zoom out did not reduce the scale: %.3f -> %.3f", zoomed, stateViewer.zoom)
	}

	harness.clickID(surface.ids.viewerFit)
	harness.settle()
	if !stateViewer.fit {
		t.Fatal("fit button did not restore fit")
	}
	harness.clickID(surface.ids.viewerActual)
	harness.settle()
	if stateViewer.fit || stateViewer.zoom != 1 {
		t.Fatal("1:1 button did not show actual size")
	}

	beforeWheel := stateViewer.zoom
	stage, ok := harness.findHit(surface.ids.viewerStage)
	if !ok {
		t.Fatal("viewer stage is missing")
	}
	if err := harness.driver.Scroll(ui.ScrollEvent{
		X:      stage.Bounds.X + stage.Bounds.Width/2,
		Y:      stage.Bounds.Y + stage.Bounds.Height/2,
		DeltaY: -120,
	}); err != nil {
		t.Fatal(err)
	}
	if stateViewer.zoom <= beforeWheel {
		t.Fatalf("wheel up did not zoom in: %.3f -> %.3f", beforeWheel, stateViewer.zoom)
	}

	if err := harness.driver.KeyDown(ui.KeyUnknown, "r"); err != nil {
		t.Fatal(err)
	}
	if stateViewer.rotation != 1 || stateViewer.cachedSourceID != harness.ui.fileBitmap(path).ID {
		t.Fatalf("rotate did not cache a clockwise bitmap: rotation %d source %d",
			stateViewer.rotation, stateViewer.cachedSourceID)
	}
	if err := harness.driver.KeyDown(ui.KeyShiftLeft, ""); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.KeyDown(ui.KeyUnknown, "r"); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.KeyUp(ui.KeyShiftLeft, ""); err != nil {
		t.Fatal(err)
	}
	if stateViewer.rotation != 0 {
		t.Fatalf("Shift+R did not rotate back: %d", stateViewer.rotation)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestImageViewerPanClampsAndDoubleClickTogglesFit(t *testing.T) {
	state := shellTestState(t)
	path := addViewerImage(t, &state, "file-image-pan", "大图.png", time.Now(), 512)
	harness := newChatSurfaceHarness(t, state, 1440, 960)
	harness.waitFor(func() bool { return bitmapReady(harness.ui.fileBitmap(path)) })
	harness.clickID(harness.ui.fileButtonIDs["file-image-pan"])
	harness.settle()
	surface := harness.ui.chatSurface()
	stateViewer := &surface.viewer
	for range 10 {
		if err := harness.driver.KeyDown(ui.KeyUnknown, "+"); err != nil {
			t.Fatal(err)
		}
	}
	if stateViewer.zoom < 4 {
		t.Fatalf("viewer did not zoom far enough for panning: %.2f", stateViewer.zoom)
	}
	stage, ok := harness.findHit(surface.ids.viewerStage)
	if !ok || stage.Bounds.Width <= 0 || stage.Bounds.Height <= 0 {
		t.Fatalf("viewer stage is missing: %+v", stage.Bounds)
	}
	centerX := stage.Bounds.X + stage.Bounds.Width/2
	centerY := stage.Bounds.Y + stage.Bounds.Height/2
	if err := harness.driver.Press(centerX, centerY, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Move(centerX+140, centerY+90); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Release(centerX+140, centerY+90, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if stateViewer.panX <= 0 || stateViewer.panY <= 0 {
		t.Fatalf("drag did not pan the zoomed image: %.1f, %.1f", stateViewer.panX, stateViewer.panY)
	}
	if stateViewer.drag {
		t.Fatal("pointer release did not end the pan")
	}

	if err := harness.driver.DoubleClick(centerX, centerY); err != nil {
		t.Fatal(err)
	}
	if !stateViewer.fit || stateViewer.panX != 0 || stateViewer.panY != 0 {
		t.Fatalf("double click did not return to fit: fit %v pan %.1f, %.1f",
			stateViewer.fit, stateViewer.panX, stateViewer.panY)
	}

	// A fitted image is smaller than the stage, so a click on the dark area
	// outside it closes the viewer, as WeChat does.
	if err := harness.driver.Click(stage.Bounds.X+4, stage.Bounds.Y+4); err != nil {
		t.Fatal(err)
	}
	if harness.ui.imageViewerOpen() {
		t.Fatal("clicking outside the image did not close the viewer")
	}
}

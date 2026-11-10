package desktop

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func viewerTitlebarDriver(tb testing.TB) (*UI, *app.Driver, *app.Invalidator) {
	tb.Helper()
	dir := tb.TempDir()
	path := filepath.Join(dir, "example.png")
	file := domain.File{ID: "photo", Name: "example.png", MIME: "image/png", ConversationID: "chat", SourcePath: path}
	u := New(newShellTestBackend(domain.State{
		Settings: domain.DefaultSettings(dir), Files: []domain.File{file},
	}))
	tb.Cleanup(u.Close)
	u.chatSurface().viewer = imageViewerState{open: true, fileID: file.ID, conversationID: file.ConversationID, fit: true, zoom: 1}
	u.imageCache[path] = ui.Bitmap{ID: 1, Version: 1, Width: 1, Height: 1, Stride: 4, Pix: []byte{120, 120, 120, 255}}
	config := u.imageViewerConfig()
	config.Width = config.MinWidth
	invalidator := new(app.Invalidator)
	config.Invalidator = invalidator
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(driver.Close)
	return u, driver, invalidator
}

func TestDetachedViewerTitlebarSeparatesWindowDragFromControls(t *testing.T) {
	u, d, _ := viewerTitlebarDriver(t)
	ids := u.chatSurface().ids
	var drag ui.Rect
	for _, hit := range d.Snapshot().Hits {
		if hit.MovesWindow {
			drag = hit.Bounds
			break
		}
	}
	if drag.Width <= 0 || d.PressAt(drag.X+drag.Width/2, drag.Y+drag.Height/2) != app.PressMove {
		t.Fatal("the empty part of the picture titlebar cannot drag the window")
	}
	snapshot := d.Snapshot()
	for _, id := range []ui.ID{ids.viewerZoomIn, ids.viewerRotate, ids.viewerSave, ids.viewerMenu.trigger, ids.viewerStage} {
		hit, ok := findSessionHit(snapshot.Hits, id)
		if !ok {
			t.Fatalf("missing viewer control %v at minimum width", id)
		}
		if kind := d.PressAt(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); kind == app.PressMove || kind == app.PressMoveOnDrag {
			t.Fatalf("%s initiates a window drag", hit.Label)
		}
	}
}

func BenchmarkDetachedViewerPaint(b *testing.B) {
	_, d, invalidator := viewerTitlebarDriver(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		invalidator.Invalidate()
		if err := d.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}

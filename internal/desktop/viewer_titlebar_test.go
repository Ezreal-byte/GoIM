package desktop

import (
	"bytes"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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
	u.surface.viewer = imageViewerState{open: true, fileID: file.ID, conversationID: file.ConversationID, fit: true, zoom: 1}
	u.imageCache[path] = gfx.Bitmap{ID: 1, Version: 1, Width: 1, Height: 1, Stride: 4, Pix: []byte{120, 120, 120, 255}}
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
	ids := u.surface.ids
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
	for _, id := range []gfx.ID{ids.viewerZoomIn, ids.viewerRotate, ids.viewerSave, ids.viewerMenu.trigger, ids.viewerStage} {
		hit, ok := findSessionHit(snapshot.Hits, id)
		if !ok {
			t.Fatalf("missing viewer control %v at minimum width", id)
		}
		if kind := d.PressAt(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); kind == app.PressMove || kind == app.PressMoveOnDrag {
			t.Fatalf("%s initiates a window drag", hit.Label)
		}
	}
}

func TestDetachedImageSecondaryClickOpensMenuAtPointer(t *testing.T) {
	u, d, invalidator := viewerTitlebarDriver(t)
	stage, ok := findSessionHit(d.Snapshot().Hits, u.surface.ids.viewerStage)
	if !ok {
		t.Fatal("missing image surface")
	}
	x, y := stage.Bounds.X+48, stage.Bounds.Y+50
	if err := d.Press(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if err := d.Release(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	menu := u.surface.viewer
	if !menu.menuOpen || !menu.menuAtPointer || menu.menuAt.X != x || menu.menuAt.Y != y {
		t.Fatal("secondary click failed to anchor the menu")
	}
	if _, ok := findSessionHit(d.Snapshot().Hits, u.surface.ids.viewerMenu.actual); !ok {
		t.Fatal("missing actual size action")
	}
	id := u.surface.ids.viewerMenu.copy
	if hit, ok := findSessionHit(d.Snapshot().Hits, id); !ok || !hit.Disabled {
		t.Fatal("copy must wait for the full image")
	}
	// The thumbnail is one gray pixel. The viewer's full bitmap instead has
	// two colored pixels and a padded stride.
	source := gfx.Bitmap{ID: 42, Version: 1, Width: 2, Height: 1, Stride: 12, Pix: []byte{0, 0, 128, 128, 255, 0, 0, 255, 0, 0, 0, 0}}
	u.surface.viewer.original = source
	u.surface.viewer.originalPath = u.fileLocalPath[u.surface.viewer.fileID]
	encoded := make(chan []byte, 1)
	u.writeClipboardPNG = func(_ string, data []byte) error {
		encoded <- data
		return nil
	}
	invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	hit, ok := findSessionHit(d.Snapshot().Hits, id)
	if !ok || hit.Disabled || hit.Label != "复制" {
		t.Fatalf("full image copy option: %+v", hit)
	}
	if err := d.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-encoded:
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 ||
			color.NRGBAModel.Convert(img.At(0, 0)) != (color.NRGBA{R: 255, A: 128}) ||
			color.NRGBAModel.Convert(img.At(1, 0)) != (color.NRGBA{B: 255, A: 255}) {
			t.Fatal("clipboard image lost resolution, color, or alpha")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("copy did not write an image")
	}
	if u.surface.viewer.menuOpen || source.Pix[0] != 0 {
		t.Fatal("copy left the menu open or changed the viewer pixels")
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

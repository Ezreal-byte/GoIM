package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"goim/internal/capture"
	"goim/internal/i18n"
)

func TestSelectionOverlayExportsOnlyMarkedRegion(t *testing.T) {
	i18n.SetLanguage(i18n.Chinese)
	e := newEditorForTest(t, 720, 440, "")
	overlay := newSelectionOverlay(e, sourceData{})
	var invalidator app.Invalidator
	session, err := headless.Open(overlay.config(&invalidator), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	d := session.Driver()
	drag := func(x, y, endX, endY float32) {
		t.Helper()
		if err := d.Press(x, y, ui.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
		if err := d.Move(endX, endY); err != nil {
			t.Fatal(err)
		}
		if err := d.Release(endX, endY, ui.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
	}

	drag(80, 60, 560, 340)
	wantCrop := image.Rect(40, 30, 280, 170)
	if got := e.document.Crop(); got != wantCrop {
		t.Fatalf("crop = %v, want %v", got, wantCrop)
	}
	if hit := findHit(t, d, rectID); hit.Label != "矩形" {
		t.Fatalf("rectangle label = %q", hit.Label)
	}
	clickControl(t, d, rectID)
	drag(140, 120, 400, 270)
	if len(e.document.Shapes()) != 1 {
		t.Fatal("annotation was not committed")
	}
	clickControl(t, d, undoID)
	if len(e.document.Shapes()) != 0 || e.document.Crop() != wantCrop {
		t.Fatal("undo changed more than the annotation")
	}
	clickControl(t, d, arrowID)
	drag(430, 260, 240, 110)
	if directory := os.Getenv("GOIM_SNIP_SHOT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, "screenshot-region.png")); err != nil {
			t.Fatal(err)
		}
	}
	clickControl(t, d, exportID)
	if e.outcome != outcomeConfirmed || e.err != nil {
		t.Fatalf("capture outcome %v, %v", e.outcome, e.err)
	}
	file, err := os.Open(e.output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := picture.Bounds().Size(), wantCrop.Size(); got != want {
		t.Fatalf("export size = %v, want %v", got, want)
	}
	r, g, b, _ := picture.At(167, 95).RGBA()
	if !(b > r && b > g) {
		t.Fatal("export does not contain the blue arrow")
	}
}

func TestSelectionOverlayClickSnapAndResizeAtScaledResolution(t *testing.T) {
	e := newEditorForTest(t, 720, 440, "")
	full, err := e.document.Render()
	if err != nil {
		t.Fatal(err)
	}
	source := sourceData{
		bounds:  image.Rect(-720, -50, 0, 390),
		windows: []image.Rectangle{image.Rect(-600, 50, -280, 290), image.Rect(-710, -40, -20, 380)},
	}
	o := newSelectionOverlay(e, source)
	var invalidator app.Invalidator
	session, err := headless.Open(o.config(&invalidator), nil, headless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	d := session.Driver()
	if err := d.Move(240, 210); err != nil {
		t.Fatal(err)
	}
	if got, want := o.hover, image.Rect(60, 50, 220, 170); got != want {
		t.Fatalf("hover picked %v, want frontmost %v", got, want)
	}
	if directory := os.Getenv("GOIM_SNIP_SHOT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, "screenshot-window-snap.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Click(240, 210); err != nil {
		t.Fatal(err)
	}
	if !o.selected || o.selection != o.hover {
		t.Fatalf("click did not select the hovered window: %v", o.selection)
	}
	// A fast follow-up press can have a double-click count; a drag still edits.
	drag := func(x, y, endX, endY float32, id ui.ID) {
		t.Helper()
		if target, ok := d.HitTest(x, y); !ok || target != id {
			t.Fatalf("grip (%v, %v) hits %v, want %v", x, y, target, id)
		}
		if err := d.PressCount(x, y, ui.PointerButtonPrimary, 2); err != nil {
			t.Fatal(err)
		}
		if err := d.Move(endX, endY); err != nil {
			t.Fatal(err)
		}
		if err := d.ReleaseCount(endX, endY, ui.PointerButtonPrimary, 2); err != nil {
			t.Fatal(err)
		}
		if e.done {
			t.Fatal("drag was treated as an export")
		}
	}
	drag(270, 100, 270, 84, selectionGrips[0].id)
	drag(440, 340, 468, 364, selectionGrips[6].id)
	drag(240, 210, 260, 224, moveSelectionID)
	want := image.Rect(70, 49, 244, 189)
	if o.selection != want {
		t.Fatalf("selection after edges and move = %v, want %v", o.selection, want)
	}
	clickControl(t, d, exportID)
	f, err := os.Open(e.output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoded, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Size() != want.Size() {
		t.Fatalf("saved %v, want original %v", decoded.Bounds().Size(), want.Size())
	}
	for y := 0; y < want.Dy(); y++ {
		for x := 0; x < want.Dx(); x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			sr, sg, sb, sa := full.At(want.Min.X+x, want.Min.Y+y).RGBA()
			if r != sr || g != sg || b != sb || a != sa {
				t.Fatalf("pixel %d,%d changed", x, y)
			}
		}
	}
}

func TestSelectionOverlayWholeScreenAndUndo(t *testing.T) {
	e := newEditorForTest(t, 720, 440, "")
	o := newSelectionOverlay(e, sourceData{})
	var invalidator app.Invalidator
	session, err := headless.Open(o.config(&invalidator), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	d := session.Driver()
	clickControl(t, d, screenSelectionID)
	if !o.selected || o.selection != e.document.SourceBounds() {
		t.Fatalf("expected a full-screen selection, got %v", o.selection)
	}
	for _, grip := range selectionGrips {
		findHit(t, d, grip.id)
	}
	clickControl(t, d, rectID)
	if err := d.Press(80, 120, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := d.Move(280, 290); err != nil {
		t.Fatal(err)
	}
	if err := d.Release(280, 290, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if len(o.shapes) != 1 {
		t.Fatal("annotation was not committed to full screen")
	}
	clickControl(t, d, undoID)
	if !o.selected || o.selection != e.document.SourceBounds() || len(o.shapes) != 0 {
		t.Fatal("undo removed the full-screen selection with the annotation")
	}
	clickControl(t, d, undoID)
	if o.selected {
		t.Fatal("undo did not clear the first full-screen selection")
	}
	if err := session.Keys("ctrl+a"); err != nil {
		t.Fatal(err)
	}
	if !o.selected || o.selection != e.document.SourceBounds() {
		t.Fatal("Ctrl+A did not select the entire screen")
	}
}

func BenchmarkSnipOverlaySelectionPaint(b *testing.B) {
	doc, err := capture.NewDocument(image.NewRGBA(image.Rect(0, 0, 720, 440)))
	if err != nil {
		b.Fatal(err)
	}
	e, err := newEditor(doc, "", sourceInfo{}, true, 720, 440)
	if err != nil {
		b.Fatal(err)
	}
	o := newSelectionOverlay(e, sourceData{})
	o.selectRegion(image.Rect(100, 80, 620, 340))
	var invalidator app.Invalidator
	d, err := app.NewDriver(o.config(&invalidator), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()
	if err := d.Advance(500 * time.Millisecond); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		invalidator.Invalidate()
		if err := d.Pump(); err != nil {
			b.Fatal(err)
		}
	}
}

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"goim/internal/capture"
)

type editorHarness struct {
	editor *editor
	driver *app.Driver
}

func newEditorForTest(t *testing.T, width, height int, output string) *editor {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 360, 220))
	for y := 0; y < picture.Bounds().Dy(); y++ {
		for x := 0; x < picture.Bounds().Dx(); x++ {
			value := uint8(40 + (x*3+y*5)%120)
			picture.SetRGBA(x, y, uiColor(value))
		}
	}
	document, err := capture.NewDocument(picture)
	if err != nil {
		t.Fatal(err)
	}
	if output == "" {
		output = filepath.Join(t.TempDir(), "snip.png")
	}
	editor, err := newEditor(document, output, sourceInfo{
		title: "Test display", detail: "synthetic screenshot", displays: 2, dpi: 96,
	}, false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	editor.followSystem = false
	return editor
}

func uiColor(value uint8) color.RGBA {
	return color.RGBA{R: value, G: value, B: value, A: 255}
}

func openEditorHarness(t *testing.T, editor *editor) *editorHarness {
	t.Helper()
	var invalidator app.Invalidator
	driver, err := app.NewDriver(editor.config(&invalidator), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return &editorHarness{editor: editor, driver: driver}
}

func findHit(t *testing.T, driver *app.Driver, id ui.ID) ui.Hit {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("control %v is not on screen", id)
	return ui.Hit{}
}

func clickControl(t *testing.T, driver *app.Driver, id ui.ID) {
	t.Helper()
	hit := findHit(t, driver, id)
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
}

func (harness *editorHarness) canvasPoint(t *testing.T, source capture.Point) ui.Point {
	t.Helper()
	snapshot := harness.driver.Snapshot()
	layout := computeLayout(ui.Size{Width: float32(snapshot.Width), Height: float32(snapshot.Height)})
	crop := harness.editor.document.Crop()
	imageBounds, _ := fitImageRect(layout.imageArea(), crop)
	if imageRectEmpty(imageBounds) {
		t.Fatalf("canvas has no image area: layout=%+v crop=%v", layout, crop)
	}
	point := sourceToView(source, crop, imageBounds)
	if !pointIn(point.X, point.Y, imageBounds) {
		t.Fatalf("source point %+v maps outside image area %+v", source, imageBounds)
	}
	if id, ok := harness.driver.HitTest(point.X, point.Y); !ok || id != canvasID {
		t.Fatalf("canvas point %+v hits %v, %v; want %v", point, id, ok, canvasID)
	}
	return point
}

func (harness *editorHarness) dragSource(t *testing.T, points ...capture.Point) {
	t.Helper()
	if len(points) < 2 {
		t.Fatal("drag needs at least two points")
	}
	start := harness.canvasPoint(t, points[0])
	if err := harness.driver.Press(start.X, start.Y, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	for _, source := range points[1:] {
		point := harness.canvasPoint(t, source)
		if err := harness.driver.Move(point.X, point.Y); err != nil {
			t.Fatal(err)
		}
	}
	end := harness.canvasPoint(t, points[len(points)-1])
	if err := harness.driver.Release(end.X, end.Y, ui.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
}

func TestToolButtonsSelectEachTool(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	for _, test := range []struct {
		id   ui.ID
		want tool
	}{
		{rectID, toolRectangle},
		{arrowID, toolArrow},
		{drawID, toolFreehand},
		{selectID, toolSelect},
	} {
		clickControl(t, harness.driver, test.id)
		if editor.tool != test.want {
			t.Fatalf("tool after clicking %v = %v, want %v", test.id, editor.tool, test.want)
		}
		if editor.toolLabel != test.want.String() {
			t.Fatalf("tool label after clicking %v = %q, want %q", test.id, editor.toolLabel, test.want.String())
		}
	}
}

func TestCropDragUsesSourceCoordinates(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	full := editor.document.Crop()

	harness.dragSource(t, capture.Point{X: 40, Y: 30}, capture.Point{X: 280, Y: 170})

	crop := editor.document.Crop()
	assertRectNear(t, crop, image.Rect(40, 30, 280, 170), 1)
	if crop == full {
		t.Fatal("crop drag did not change the document crop")
	}
	if editor.preview.Width != crop.Dx() || editor.preview.Height != crop.Dy() {
		t.Fatalf("preview size = %dx%d, want crop %dx%d",
			editor.preview.Width, editor.preview.Height, crop.Dx(), crop.Dy())
	}
	if want := fmt.Sprintf("%d × %d px", crop.Dx(), crop.Dy()); editor.cropLabel != want {
		t.Fatalf("crop label = %q, want %q", editor.cropLabel, want)
	}
	if !snapshotHasText(harness.driver.Snapshot(), editor.cropLabel) {
		t.Fatalf("frame does not show crop label %q", editor.cropLabel)
	}
}

func TestRectangleDragAddsAnAnnotation(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	clickControl(t, harness.driver, rectID)
	version := editor.version
	fullCrop := editor.document.Crop()

	harness.dragSource(t, capture.Point{X: 30, Y: 25}, capture.Point{X: 210, Y: 150})

	shapes := editor.document.Shapes()
	if len(shapes) != 1 {
		t.Fatalf("annotations = %d, want 1", len(shapes))
	}
	shape := shapes[0]
	if shape.Kind != capture.Rectangle {
		t.Fatalf("shape kind = %v, want rectangle", shape.Kind)
	}
	assertPointNear(t, shape.Start, capture.Point{X: 30, Y: 25}, 1)
	assertPointNear(t, shape.End, capture.Point{X: 210, Y: 150}, 1)
	if shape.Color != capture.DefaultShapeColor || shape.Width <= 0 {
		t.Fatalf("shape style = color %+v width %v", shape.Color, shape.Width)
	}
	if err := shape.Validate(); err != nil {
		t.Fatalf("editor committed an invalid rectangle: %v", err)
	}
	if editor.document.Crop() != fullCrop {
		t.Fatalf("annotation changed crop to %v", editor.document.Crop())
	}
	if editor.version != version+1 {
		t.Fatalf("preview version = %d, want %d", editor.version, version+1)
	}
}

func TestArrowDragAddsAnAnnotation(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	clickControl(t, harness.driver, arrowID)

	harness.dragSource(t, capture.Point{X: 40, Y: 170}, capture.Point{X: 300, Y: 45})

	shapes := editor.document.Shapes()
	if len(shapes) != 1 {
		t.Fatalf("annotations = %d, want 1", len(shapes))
	}
	shape := shapes[0]
	if shape.Kind != capture.Arrow {
		t.Fatalf("shape kind = %v, want arrow", shape.Kind)
	}
	assertPointNear(t, shape.Start, capture.Point{X: 40, Y: 170}, 1)
	assertPointNear(t, shape.End, capture.Point{X: 300, Y: 45}, 1)
	head := capture.ArrowHead(shape.Start, shape.End, shape.Width)
	if head[0] != shape.End {
		t.Fatalf("arrow tip = %+v, want end %+v", head[0], shape.End)
	}
	if err := shape.Validate(); err != nil {
		t.Fatalf("editor committed an invalid arrow: %v", err)
	}
}

func TestFreehandDragCollectsTheStroke(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	clickControl(t, harness.driver, drawID)
	points := []capture.Point{
		{X: 30, Y: 30},
		{X: 70, Y: 40},
		{X: 110, Y: 90},
		{X: 170, Y: 110},
		{X: 240, Y: 60},
	}

	harness.dragSource(t, points...)

	shapes := editor.document.Shapes()
	if len(shapes) != 1 {
		t.Fatalf("annotations = %d, want 1", len(shapes))
	}
	shape := shapes[0]
	if shape.Kind != capture.Freehand {
		t.Fatalf("shape kind = %v, want freehand", shape.Kind)
	}
	if len(shape.Points) < len(points) {
		t.Fatalf("freehand points = %d, want at least %d", len(shape.Points), len(points))
	}
	assertPointNear(t, shape.Points[0], points[0], 1)
	assertPointNear(t, shape.Points[len(shape.Points)-1], points[len(points)-1], 1)
	if err := shape.Validate(); err != nil {
		t.Fatalf("editor committed an invalid freehand: %v", err)
	}
	if editor.gesture != nil {
		t.Fatal("pointer release left a gesture active")
	}
}

func TestUndoRemovesAnnotationThenRestoresCrop(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	fullCrop := editor.document.Crop()
	harness.dragSource(t, capture.Point{X: 30, Y: 20}, capture.Point{X: 300, Y: 190})
	cropped := editor.document.Crop()
	clickControl(t, harness.driver, rectID)
	harness.dragSource(t, capture.Point{X: 60, Y: 50}, capture.Point{X: 220, Y: 150})
	if len(editor.document.Shapes()) != 1 {
		t.Fatal("annotation was not added")
	}

	clickControl(t, harness.driver, undoID)
	if len(editor.document.Shapes()) != 0 || editor.document.Crop() != cropped {
		t.Fatal("first undo did not remove only the annotation")
	}

	modifier := ui.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = ui.ModifierMeta
	}
	handled, err := harness.driver.ShortcutChord(modifier, 'z')
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("undo shortcut was not handled")
	}
	if editor.document.Crop() != fullCrop {
		t.Fatalf("second undo restored crop %v, want %v", editor.document.Crop(), fullCrop)
	}
	if editor.preview.Width != fullCrop.Dx() || editor.preview.Height != fullCrop.Dy() {
		t.Fatalf("preview after undo = %dx%d, want %dx%d",
			editor.preview.Width, editor.preview.Height, fullCrop.Dx(), fullCrop.Dy())
	}
}

func TestPreviewIsReusedAcrossRepaints(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	version := editor.version
	if len(editor.preview.Pix) == 0 {
		t.Fatal("editor has no preview pixels")
	}
	pixel := &editor.preview.Pix[0]

	hit := findHit(t, harness.driver, rectID)
	if err := harness.driver.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	clickControl(t, harness.driver, rectID)
	if editor.version != version {
		t.Fatalf("repaint rebuilt preview version %d -> %d", version, editor.version)
	}
	if &editor.preview.Pix[0] != pixel {
		t.Fatal("repaint replaced the preview pixel buffer")
	}

	harness.dragSource(t, capture.Point{X: 20, Y: 20}, capture.Point{X: 120, Y: 100})
	if editor.version != version+1 {
		t.Fatalf("annotation rebuild version = %d, want %d", editor.version, version+1)
	}
}

func TestEditorPaintHasNoSteadyStateAllocations(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	var canvas ui.Canvas
	size := ui.Size{Width: 1180, Height: 780}
	canvas.Reset()
	editor.paint(&canvas, size)
	allocations := testing.AllocsPerRun(50, func() {
		canvas.Reset()
		editor.paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("steady-state paint allocated %.1f objects per frame, want 0", allocations)
	}
}

func TestIdleEditorSchedulesNoFrames(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	var invalidator app.Invalidator
	config := editor.config(&invalidator)
	wakes := 0
	onWake := config.OnWake
	config.OnWake = func() {
		wakes++
		onWake()
	}
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Advance(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if wakes != 0 || driver.Transitioning() {
		t.Fatalf("idle editor ran %d wakes and scheduled frames=%v", wakes, driver.Transitioning())
	}
	if editor.version != 1 {
		t.Fatalf("idle editor rebuilt preview %d times", editor.version)
	}
}

func TestCancelPathsDoNotExport(t *testing.T) {
	tests := []struct {
		name   string
		cancel func(t *testing.T, harness *editorHarness)
	}{
		{
			name: "Cancel button",
			cancel: func(t *testing.T, harness *editorHarness) {
				clickControl(t, harness.driver, cancelID)
			},
		},
		{
			name: "Escape key",
			cancel: func(t *testing.T, harness *editorHarness) {
				if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "window close",
			cancel: func(t *testing.T, harness *editorHarness) {
				harness.driver.Close()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			editor := newEditorForTest(t, 1180, 780, "")
			harness := openEditorHarness(t, editor)
			test.cancel(t, harness)
			if editor.outcome != outcomeCanceled || !editor.done {
				t.Fatalf("cancel outcome = %v, done=%v; want canceled", editor.outcome, editor.done)
			}
			if _, err := os.Stat(editor.output); !os.IsNotExist(err) {
				t.Fatalf("canceled editor created %s: %v", editor.output, err)
			}
		})
	}
}

func TestExportWritesTheExactConfirmedPNG(t *testing.T) {
	editor := newEditorForTest(t, 1180, 780, "")
	harness := openEditorHarness(t, editor)
	harness.dragSource(t, capture.Point{X: 25, Y: 20}, capture.Point{X: 320, Y: 195})
	clickControl(t, harness.driver, rectID)
	harness.dragSource(t, capture.Point{X: 55, Y: 50}, capture.Point{X: 260, Y: 160})
	crop := editor.document.Crop()

	clickControl(t, harness.driver, exportID)

	if editor.outcome != outcomeConfirmed || !editor.done || editor.err != nil {
		t.Fatalf("export outcome = %v, done=%v, err=%v; want confirmed", editor.outcome, editor.done, editor.err)
	}
	info, err := os.Stat(editor.output)
	if err != nil {
		t.Fatalf("exported file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("exported PNG is empty")
	}
	file, err := os.Open(editor.output)
	if err != nil {
		t.Fatal(err)
	}
	config, decodeErr := png.DecodeConfig(file)
	file.Close()
	if decodeErr != nil {
		t.Fatalf("decode exported PNG: %v", decodeErr)
	}
	if config.Width != crop.Dx() || config.Height != crop.Dy() {
		t.Fatalf("exported size = %dx%d, want crop %dx%d", config.Width, config.Height, crop.Dx(), crop.Dy())
	}
	if !pngHasBlueInk(t, editor.output) {
		t.Fatal("exported PNG does not contain the blue annotation")
	}
	entries, err := os.ReadDir(filepath.Dir(editor.output))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(editor.output) {
		t.Fatalf("export directory contains temporary or extra files: %v", entries)
	}
}

func TestFailedExportReportsFailure(t *testing.T) {
	output := t.TempDir()
	editor := newEditorForTest(t, 1180, 780, output)
	harness := openEditorHarness(t, editor)
	clickControl(t, harness.driver, exportID)
	if editor.outcome != outcomeFailed || !editor.done || editor.err == nil {
		t.Fatalf("failed export outcome = %v, done=%v, err=%v", editor.outcome, editor.done, editor.err)
	}
	if !strings.Contains(editor.err.Error(), "export PNG") {
		t.Fatalf("export error = %v, want export context", editor.err)
	}
}

func TestEditorLayoutAtSupportedSizes(t *testing.T) {
	for _, size := range []ui.Size{
		{Width: 820, Height: 600},
		{Width: 1180, Height: 780},
		{Width: 1240, Height: 860},
	} {
		t.Run(fmt.Sprintf("%dx%d", int(size.Width), int(size.Height)), func(t *testing.T) {
			editor := newEditorForTest(t, int(size.Width), int(size.Height), "")
			var invalidator app.Invalidator
			session, err := headless.Open(editor.config(&invalidator), nil, headless.Options{
				Scale: 1, ReferenceText: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()

			for _, current := range []tool{toolSelect, toolRectangle, toolArrow, toolFreehand} {
				editor.selectTool(current)
				invalidator.Invalidate()
				if err := session.Driver().Pump(); err != nil {
					t.Fatal(err)
				}
				snapshot := session.Snapshot()
				for _, id := range []ui.ID{canvasID, selectID, rectID, arrowID, drawID, undoID, cancelID, exportID} {
					findHit(t, session.Driver(), id)
				}
				if problems := headless.CheckLayout(snapshot); len(problems) != 0 {
					for _, problem := range problems {
						t.Errorf("%s at %+v: %q %s", problem.Kind, problem.Bounds, problem.Text, problem.Detail)
					}
				}
			}
		})
	}
}

func TestEditorScreenshots(t *testing.T) {
	directory := os.Getenv("GOIM_SNIP_SHOT_DIR")
	if directory == "" {
		t.Skip("set GOIM_SNIP_SHOT_DIR to write headless screenshots")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		width  int
		height int
		dark   bool
	}{
		{name: "normal-light", width: 1180, height: 780},
		{name: "minimum-dark", width: 820, height: 600, dark: true},
		{name: "wide-light", width: 1240, height: 860},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			editor := newEditorForTest(t, test.width, test.height, "")
			editor.dark = test.dark
			if err := editor.document.SetCrop(image.Rect(12, 10, 348, 210)); err != nil {
				t.Fatal(err)
			}
			if err := editor.document.Add(capture.Shape{
				Kind:  capture.Rectangle,
				Start: capture.Point{X: 45, Y: 40},
				End:   capture.Point{X: 205, Y: 145},
				Color: capture.DefaultShapeColor,
				Width: capture.DefaultShapeWidth,
			}); err != nil {
				t.Fatal(err)
			}
			if err := editor.document.Add(capture.Shape{
				Kind:  capture.Arrow,
				Start: capture.Point{X: 245, Y: 180},
				End:   capture.Point{X: 315, Y: 65},
				Color: capture.DefaultShapeColor,
				Width: capture.DefaultShapeWidth,
			}); err != nil {
				t.Fatal(err)
			}
			if err := editor.refreshPreview(); err != nil {
				t.Fatal(err)
			}

			var invalidator app.Invalidator
			session, err := headless.Open(editor.config(&invalidator), nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if problems := headless.CheckLayout(session.Snapshot()); len(problems) != 0 {
				for _, problem := range problems {
					t.Errorf("%s at %+v: %q %s", problem.Kind, problem.Bounds, problem.Text, problem.Detail)
				}
			}
			path := filepath.Join(directory, "goim-snip-"+test.name+".png")
			if err := session.Screenshot(path); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(path); err != nil || info.Size() == 0 {
				t.Fatalf("screenshot %s: %v, %v", path, info, err)
			}
			t.Logf("wrote %s", path)
		})
	}
}

func BenchmarkEditorPaint(b *testing.B) {
	picture := image.NewRGBA(image.Rect(0, 0, 360, 220))
	document, err := capture.NewDocument(picture)
	if err != nil {
		b.Fatal(err)
	}
	editor, err := newEditor(document, filepath.Join(b.TempDir(), "snip.png"), sourceInfo{
		title: "Test display", detail: "synthetic screenshot", displays: 2, dpi: 96,
	}, false, 1180, 780)
	if err != nil {
		b.Fatal(err)
	}
	var canvas ui.Canvas
	size := ui.Size{Width: 1180, Height: 780}
	canvas.Reset()
	editor.paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		canvas.Reset()
		editor.paint(&canvas, size)
	}
}

func snapshotHasText(snapshot app.Snapshot, want string) bool {
	for _, text := range snapshot.Texts {
		if text.Value == want {
			return true
		}
	}
	return false
}

func assertPointNear(t *testing.T, got, want capture.Point, tolerance float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > tolerance || math.Abs(got.Y-want.Y) > tolerance {
		t.Fatalf("point = (%v,%v), want (%v,%v) within %v", got.X, got.Y, want.X, want.Y, tolerance)
	}
}

func assertRectNear(t *testing.T, got, want image.Rectangle, tolerance int) {
	t.Helper()
	if absInt(got.Min.X-want.Min.X) > tolerance ||
		absInt(got.Min.Y-want.Min.Y) > tolerance ||
		absInt(got.Max.X-want.Max.X) > tolerance ||
		absInt(got.Max.Y-want.Max.Y) > tolerance {
		t.Fatalf("rectangle = %v, want %v within %d px", got, want, tolerance)
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func pngHasBlueInk(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	picture, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	for y := picture.Bounds().Min.Y; y < picture.Bounds().Max.Y; y++ {
		for x := picture.Bounds().Min.X; x < picture.Bounds().Max.X; x++ {
			r, g, b, _ := picture.At(x, y).RGBA()
			if b > r+0x3000 && b > g+0x2000 {
				return true
			}
		}
	}
	return false
}

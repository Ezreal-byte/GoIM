package capture

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func testRaster(width, height int) *image.RGBA {
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetRGBA(x, y, color.RGBA{
				R: uint8(x*7 + 3),
				G: uint8(y*11 + 5),
				B: uint8(x*3 + y*5),
				A: 255,
			})
		}
	}
	return source
}

func TestDocumentOwnsSourceAndCropsPixels(t *testing.T) {
	source := testRaster(24, 16)
	original := source.RGBAAt(7, 5)
	document, err := NewDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	source.SetRGBA(7, 5, color.RGBA{R: 255, A: 255})
	if err := document.SetCrop(image.Rect(5, 3, 17, 12)); err != nil {
		t.Fatal(err)
	}
	rendered, err := document.Render()
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Bounds().Size() != image.Pt(12, 9) {
		t.Fatalf("rendered bounds = %v", rendered.Bounds())
	}
	if got := rendered.RGBAAt(2, 2); got != original {
		t.Fatalf("cropped source pixel = %+v, want %+v", got, original)
	}
}

func TestSetCropClipsAndRejectsEmptyResult(t *testing.T) {
	document, err := NewDocument(testRaster(20, 10))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetCrop(image.Rect(-4, -3, 8, 6)); err != nil {
		t.Fatal(err)
	}
	if got, want := document.Crop(), image.Rect(0, 0, 8, 6); got != want {
		t.Fatalf("clipped crop = %v, want %v", got, want)
	}
	if err := document.SetCrop(image.Rect(50, 50, 60, 60)); !errors.Is(err, ErrInvalidCrop) {
		t.Fatalf("outside crop error = %v, want ErrInvalidCrop", err)
	}
	if got := document.Crop(); got != image.Rect(0, 0, 8, 6) {
		t.Fatalf("rejected crop changed document to %v", got)
	}
}

func TestUndoRestoresCropAndShapesInOrder(t *testing.T) {
	document, err := NewDocument(testRaster(30, 20))
	if err != nil {
		t.Fatal(err)
	}
	if document.CanUndo() {
		t.Fatal("fresh document cannot undo")
	}
	if err := document.SetCrop(image.Rect(4, 4, 24, 18)); err != nil {
		t.Fatal(err)
	}
	rectangle := Shape{
		Kind: Rectangle, Start: Point{X: 5, Y: 5}, End: Point{X: 15, Y: 12},
		Color: DefaultShapeColor, Width: 2,
	}
	if err := document.Add(rectangle); err != nil {
		t.Fatal(err)
	}
	arrow := Shape{
		Kind: Arrow, Start: Point{X: 6, Y: 15}, End: Point{X: 20, Y: 8},
		Color: DefaultShapeColor, Width: 2,
	}
	if err := document.Add(arrow); err != nil {
		t.Fatal(err)
	}
	if len(document.Shapes()) != 2 || !document.CanUndo() {
		t.Fatal("document did not retain both annotations")
	}
	if !document.Undo() || len(document.Shapes()) != 1 || document.Crop() != image.Rect(4, 4, 24, 18) {
		t.Fatal("first undo did not remove only the arrow")
	}
	if !document.Undo() || len(document.Shapes()) != 0 {
		t.Fatal("second undo did not remove the rectangle")
	}
	if !document.Undo() || document.Crop() != image.Rect(0, 0, 30, 20) {
		t.Fatal("third undo did not restore the full crop")
	}
	if document.Undo() {
		t.Fatal("undo succeeded beyond the recorded history")
	}
}

func TestShapesAreDetachedAndInvalidAddsAreAtomic(t *testing.T) {
	document, err := NewDocument(testRaster(30, 20))
	if err != nil {
		t.Fatal(err)
	}
	freehand := Shape{
		Kind:   Freehand,
		Points: []Point{{X: 1, Y: 2}, {X: 5, Y: 6}},
		Color:  DefaultShapeColor, Width: 3,
	}
	if err := document.Add(freehand); err != nil {
		t.Fatal(err)
	}
	shapes := document.Shapes()
	shapes[0].Points[0] = Point{X: 999, Y: 999}
	shapes[0].Color = color.NRGBA{}
	if got := document.Shapes()[0]; got.Points[0] != (Point{X: 1, Y: 2}) || got.Color != DefaultShapeColor {
		t.Fatal("Shapes exposed mutable document storage")
	}
	before := len(document.Shapes())
	historyBefore := len(document.undo)
	invalid := Shape{Kind: Freehand, Points: []Point{{X: 1, Y: 2}}, Color: DefaultShapeColor, Width: 3}
	if err := document.Add(invalid); !errors.Is(err, ErrInvalidShape) {
		t.Fatalf("invalid annotation error = %v", err)
	}
	if len(document.Shapes()) != before || len(document.undo) != historyBefore {
		t.Fatal("rejected annotation changed the document")
	}
}

func TestSettingSameCropDoesNotCreateUndoStep(t *testing.T) {
	document, err := NewDocument(testRaster(10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetCrop(document.Crop()); err != nil {
		t.Fatal(err)
	}
	if document.CanUndo() {
		t.Fatal("setting the same crop recorded an undo step")
	}
}

func TestExportPNGAtomicallyReplacesAndHasCropDimensions(t *testing.T) {
	document, err := NewDocument(testRaster(40, 30))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetCrop(image.Rect(5, 4, 27, 23)); err != nil {
		t.Fatal(err)
	}
	if err := document.Add(Shape{
		Kind: Rectangle, Start: Point{X: 8, Y: 7}, End: Point{X: 22, Y: 18},
		Color: DefaultShapeColor, Width: 3,
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snip.png")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := document.ExportPNG(path); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 22 || config.Height != 19 {
		t.Fatalf("exported dimensions = %dx%d, want 22x19", config.Width, config.Height)
	}
	if info, err := os.Stat(path); err != nil || info.Size() < 8 {
		t.Fatalf("exported file = %v, %v", info, err)
	}
}

func TestFailedExportDoesNotLeaveTemporaryFile(t *testing.T) {
	document, err := NewDocument(testRaster(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := document.ExportPNG(directory); err == nil {
		t.Fatal("export to a directory unexpectedly succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed export left files: %v", entries)
	}
}

func TestExportRejectsEmptyPath(t *testing.T) {
	document, err := NewDocument(testRaster(2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.ExportPNG(""); err == nil {
		t.Fatal("empty output path unexpectedly succeeded")
	}
}

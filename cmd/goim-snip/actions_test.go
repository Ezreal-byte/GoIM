package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"

	"goim/internal/capture"
)

func TestAnnotationAndSaveAsShareTheSamePixels(t *testing.T) {
	e := newEditorForTest(t, 1100, 740, "")
	h := openEditorHarness(t, e)
	clickControl(t, h.driver, ellipseID)
	clickControl(t, h.driver, inkScope.At(1))
	clickControl(t, h.driver, widthScope.At(2))
	h.dragSource(t, capture.Point{X: 25, Y: 35}, capture.Point{X: 165, Y: 115})
	shapes := e.document.Shapes()
	if len(shapes) != 1 || shapes[0].Kind != capture.Ellipse || shapes[0].Color != inkPresets[1].color {
		t.Fatalf("ellipse did not retain the chosen style: %+v", shapes)
	}
	clickControl(t, h.driver, mosaicID)
	h.dragSource(t, capture.Point{X: 155, Y: 90}, capture.Point{X: 247, Y: 178})
	clickControl(t, h.driver, textID)
	p := h.canvasPoint(t, capture.Point{X: 40, Y: 135})
	if err := h.driver.Click(p.X, p.Y); err != nil {
		t.Fatal(err)
	}
	clickControl(t, h.driver, textFieldID)
	if err := h.driver.Type("GoIM 标注"); err != nil {
		t.Fatal(err)
	}
	clickControl(t, h.driver, textOKID)
	shapes = e.document.Shapes()
	if len(shapes) != 3 || shapes[1].Kind != capture.Mosaic || shapes[2].Kind != capture.Text ||
		shapes[2].Ink == nil || e.text.open {
		t.Fatalf("annotations were not committed: %v (%s)", len(shapes), e.notice)
	}
	clickControl(t, h.driver, undoID)
	if len(e.document.Shapes()) != 2 {
		t.Fatal("undo did not remove the label")
	}
	clickControl(t, h.driver, redoID)
	if len(e.document.Shapes()) != 3 {
		t.Fatal("redo did not restore the label")
	}
	if err := h.driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	clickControl(t, h.driver, saveID)
	if _, ok := h.driver.LastSaveFileRequest(); !ok {
		t.Fatal("Save As did not open the system picker")
	}
	if e.done || len(e.document.Shapes()) != 3 {
		t.Fatal("cancelling Save As lost the edited screenshot")
	}
	h.driver.SetClipboard("keep this")
	target := filepath.Join(t.TempDir(), "annotated.png")
	if err := h.driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	clickControl(t, h.driver, saveID)
	if e.outcome != outcomeSaved || h.driver.Clipboard() != "keep this" {
		t.Fatalf("Save As outcome = %v, notice = %q, clipboard = %q", e.outcome, e.notice, h.driver.Clipboard())
	}
	stream, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(stream)
	_ = stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(decoded.Bounds())
	draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	for i := 0; i < len(pixels.Pix); i += 4 {
		pixels.Pix[i], pixels.Pix[i+2] = pixels.Pix[i+2], pixels.Pix[i]
	}
	if !bytes.Equal(pixels.Pix, e.preview.Pix) {
		t.Fatal("saved PNG differs from the preview")
	}
}

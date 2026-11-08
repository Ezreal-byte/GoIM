package capture

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
)

// ErrNoSource reports that a document was created without a usable source.
var ErrNoSource = errors.New("capture: source image is nil or empty")

// ErrInvalidCrop reports a crop outside the source image or with no area.
var ErrInvalidCrop = errors.New("capture: crop is empty or outside the source image")

// ErrInvalidShape reports an annotation that cannot be rendered.
var ErrInvalidShape = errors.New("capture: invalid annotation")

// Document owns a screenshot, its current crop, annotations, and undo history.
// It is intended for use on the UI thread.
type Document struct {
	source *image.RGBA
	crop   image.Rectangle
	shapes []Shape
	undo   []undoEntry
}

type undoEntry struct {
	crop       image.Rectangle
	shapeCount int
}

// NewDocument copies source into an owned RGBA image and selects the whole
// image. Copying means later mutation of source cannot change what is exported.
func NewDocument(source image.Image) (*Document, error) {
	if source == nil {
		return nil, ErrNoSource
	}
	bounds := source.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil, ErrNoSource
	}
	owned := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(owned, owned.Bounds(), source, bounds.Min, draw.Src)
	return &Document{
		source: owned,
		crop:   owned.Bounds(),
	}, nil
}

// Crop returns the active source-image crop.
func (document *Document) Crop() image.Rectangle {
	if document == nil {
		return image.Rectangle{}
	}
	return document.crop
}

// SourceBounds returns the complete source image in document coordinates.
func (document *Document) SourceBounds() image.Rectangle {
	if document == nil || document.source == nil {
		return image.Rectangle{}
	}
	return document.source.Bounds()
}

// SetCrop selects a source-image rectangle and records an undo step. A crop
// partly outside the source is clipped; a crop with no remaining area is
// rejected. Setting the current crop is a no-op.
func (document *Document) SetCrop(crop image.Rectangle) error {
	if document == nil || document.source == nil {
		return ErrNoSource
	}
	crop = canonicalRect(crop).Intersect(document.source.Bounds())
	if crop.Empty() {
		return ErrInvalidCrop
	}
	if crop == document.crop {
		return nil
	}
	document.undo = append(document.undo, undoEntry{crop: document.crop, shapeCount: len(document.shapes)})
	document.crop = crop
	return nil
}

// Add records and appends one annotation.
func (document *Document) Add(shape Shape) error {
	if document == nil || document.source == nil {
		return ErrNoSource
	}
	if err := shape.Validate(); err != nil {
		return err
	}
	shape.Points = append([]Point(nil), shape.Points...)
	document.undo = append(document.undo, undoEntry{crop: document.crop, shapeCount: len(document.shapes)})
	document.shapes = append(document.shapes, shape)
	return nil
}

// Shapes returns a detached copy of the annotations in paint order.
func (document *Document) Shapes() []Shape {
	if document == nil {
		return nil
	}
	shapes := make([]Shape, len(document.shapes))
	for index, shape := range document.shapes {
		shapes[index] = shape
		shapes[index].Points = append([]Point(nil), shape.Points...)
	}
	return shapes
}

// CanUndo reports whether Undo would change the document.
func (document *Document) CanUndo() bool {
	return document != nil && len(document.undo) > 0
}

// Undo restores the state before the latest crop or annotation change.
func (document *Document) Undo() bool {
	if document == nil || len(document.undo) == 0 {
		return false
	}
	entry := document.undo[len(document.undo)-1]
	document.undo = document.undo[:len(document.undo)-1]
	if entry.shapeCount < len(document.shapes) {
		document.shapes = document.shapes[:entry.shapeCount]
	}
	document.crop = entry.crop
	return true
}

// Render returns the cropped screenshot with annotations composited in order.
func (document *Document) Render() (*image.RGBA, error) {
	if document == nil || document.source == nil {
		return nil, ErrNoSource
	}
	crop := document.crop.Intersect(document.source.Bounds())
	if crop.Empty() {
		return nil, ErrInvalidCrop
	}
	output := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(output, output.Bounds(), document.source, crop.Min, draw.Src)
	for _, shape := range document.shapes {
		rasterizeShape(output, shape, crop.Min)
	}
	return output, nil
}

// ExportPNG atomically renders the current crop and annotations to path.
func (document *Document) ExportPNG(path string) error {
	if path == "" {
		return fmt.Errorf("capture: output path is empty")
	}
	output, err := document.Render()
	if err != nil {
		return err
	}
	return writePNGAtomic(path, output)
}

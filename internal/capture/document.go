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
	source  *image.RGBA
	crop    image.Rectangle
	shapes  []Shape
	history []edit
	applied int
}

type edit struct {
	before, after image.Rectangle
	shape         Shape
}

// ExitSaved is the companion's result after saving to a user-chosen path.
// The parent leaves the clipboard untouched in this case.
const ExitSaved = 3

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
	return document.setCrop(crop, false)
}

// SelectCrop records an explicit initial selection, even for the full image.
func (document *Document) SelectCrop(crop image.Rectangle) error {
	return document.setCrop(crop, true)
}

func (document *Document) setCrop(crop image.Rectangle, explicit bool) error {
	if document == nil || document.source == nil {
		return ErrNoSource
	}
	crop = canonicalRect(crop).Intersect(document.source.Bounds())
	if crop.Empty() {
		return ErrInvalidCrop
	}
	if crop == document.crop && !explicit {
		return nil
	}
	document.record(edit{before: document.crop, after: crop})
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
	document.record(edit{shape: shape.clone()})
	return nil
}

// Shapes returns a detached copy of the annotations in paint order.
func (document *Document) Shapes() []Shape {
	if document == nil {
		return nil
	}
	shapes := make([]Shape, len(document.shapes))
	for index, shape := range document.shapes {
		shapes[index] = shape.clone()
	}
	return shapes
}

// CanUndo reports whether Undo would change the document.
func (document *Document) CanUndo() bool {
	return document != nil && document.applied > 0
}

// CanRedo reports whether an undone edit can be reapplied.
func (document *Document) CanRedo() bool {
	return document != nil && document.applied < len(document.history)
}

func (document *Document) record(change edit) {
	clear(document.history[document.applied:])
	document.history = append(document.history[:document.applied], change)
	document.Redo()
}

// Undo restores the state before the latest crop or annotation change.
func (document *Document) Undo() bool {
	if !document.CanUndo() {
		return false
	}
	document.applied--
	change := document.history[document.applied]
	if change.shape.Kind != 0 {
		i := len(document.shapes) - 1
		document.shapes[i] = Shape{}
		document.shapes = document.shapes[:i]
	} else {
		document.crop = change.before
	}
	return true
}

// Redo reapplies the next edit. A fresh edit discards the redo branch.
func (document *Document) Redo() bool {
	if !document.CanRedo() {
		return false
	}
	change := document.history[document.applied]
	document.applied++
	if change.shape.Kind != 0 {
		document.shapes = append(document.shapes, change.shape)
	} else {
		document.crop = change.after
	}
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

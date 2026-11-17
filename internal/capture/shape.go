package capture

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
)

// ShapeKind selects the geometry of an annotation.
type ShapeKind uint8

const (
	// Rectangle draws the outline between Start and End.
	Rectangle ShapeKind = iota + 1
	// Arrow draws a line from Start to End with a filled head at End.
	Arrow
	// Freehand draws a round stroke through Points.
	Freehand
	// Ellipse draws the outline inscribed in the box between Start and End.
	Ellipse
	// Mosaic covers its box with opaque square blocks. Width is the block edge.
	Mosaic
	// Text composites a rasterized label at Start.
	Text
)

const (
	// DefaultShapeWidth is the annotation width in source-image pixels.
	DefaultShapeWidth = 5.0
)

// DefaultShapeColor is GoIM's primary blue.
var DefaultShapeColor = color.NRGBA{R: 37, G: 99, B: 235, A: 255}

// Point is a pixel position in the source image. Fractions are retained for
// antialiased rendering.
type Point struct {
	X, Y float64
}

// Shape is one immutable-in-practice annotation. Rectangle uses Start and End,
// Arrow uses Start and End, and Freehand uses Points.
type Shape struct {
	Kind   ShapeKind
	Start  Point
	End    Point
	Points []Point
	Color  color.NRGBA
	Width  float64
	Ink    *image.RGBA
}

func (shape Shape) clone() Shape {
	shape.Points = append([]Point(nil), shape.Points...)
	if shape.Ink != nil {
		ink := image.NewRGBA(image.Rect(0, 0, shape.Ink.Bounds().Dx(), shape.Ink.Bounds().Dy()))
		draw.Draw(ink, ink.Bounds(), shape.Ink, shape.Ink.Bounds().Min, draw.Src)
		shape.Ink = ink
	}
	return shape
}

// Validate reports whether a shape can be rendered.
func (shape Shape) Validate() error {
	if shape.Kind != Text && (shape.Width <= 0 || shape.Width > 512 || !finite(shape.Width)) {
		return fmt.Errorf("%w: width must be between 0 and 512", ErrInvalidShape)
	}
	switch shape.Kind {
	case Rectangle, Ellipse, Mosaic:
		if !finitePoint(shape.Start) || !finitePoint(shape.End) ||
			shape.Start.X == shape.End.X || shape.Start.Y == shape.End.Y {
			return fmt.Errorf("%w: annotation corners do not form an area", ErrInvalidShape)
		}
	case Arrow:
		if !finitePoint(shape.Start) || !finitePoint(shape.End) ||
			math.Hypot(shape.End.X-shape.Start.X, shape.End.Y-shape.Start.Y) < 1 {
			return fmt.Errorf("%w: arrow is too short", ErrInvalidShape)
		}
	case Freehand:
		if len(shape.Points) < 2 || len(shape.Points) > 1_000_000 {
			return fmt.Errorf("%w: freehand needs between 2 and 1000000 points", ErrInvalidShape)
		}
		for _, point := range shape.Points {
			if !finitePoint(point) {
				return fmt.Errorf("%w: freehand contains a non-finite point", ErrInvalidShape)
			}
		}
	case Text:
		if !finitePoint(shape.Start) || shape.Ink == nil || shape.Ink.Bounds().Empty() {
			return fmt.Errorf("%w: text needs a rasterized label and a position", ErrInvalidShape)
		}
	default:
		return fmt.Errorf("%w: unknown shape kind %d", ErrInvalidShape, shape.Kind)
	}
	return nil
}

// Bounds returns the shape's inclusive source-pixel extents, including stroke
// width.
func (shape Shape) Bounds() image.Rectangle {
	if shape.Kind == Text && shape.Ink != nil {
		at := image.Pt(int(math.Floor(shape.Start.X)), int(math.Floor(shape.Start.Y)))
		return image.Rectangle{Min: at, Max: at.Add(shape.Ink.Bounds().Size())}
	}
	minX, minY, maxX, maxY := shape.extents()
	if shape.Kind == Mosaic {
		return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
	}
	half := shape.Width / 2
	return image.Rect(
		int(math.Floor(minX-half)),
		int(math.Floor(minY-half)),
		int(math.Ceil(maxX+half))+1,
		int(math.Ceil(maxY+half))+1,
	)
}

func (shape Shape) extents() (minX, minY, maxX, maxY float64) {
	switch shape.Kind {
	case Freehand:
		if len(shape.Points) == 0 {
			return 0, 0, 0, 0
		}
		minX, minY = shape.Points[0].X, shape.Points[0].Y
		maxX, maxY = minX, minY
		for _, point := range shape.Points[1:] {
			minX, minY = math.Min(minX, point.X), math.Min(minY, point.Y)
			maxX, maxY = math.Max(maxX, point.X), math.Max(maxY, point.Y)
		}
		return minX, minY, maxX, maxY
	default:
		minX, maxX = math.Min(shape.Start.X, shape.End.X), math.Max(shape.Start.X, shape.End.X)
		minY, maxY = math.Min(shape.Start.Y, shape.End.Y), math.Max(shape.Start.Y, shape.End.Y)
		if shape.Kind == Arrow {
			head := arrowHead(shape.Start, shape.End, shape.Width)
			for _, point := range head {
				minX, maxX = math.Min(minX, point.X), math.Max(maxX, point.X)
				minY, maxY = math.Min(minY, point.Y), math.Max(maxY, point.Y)
			}
		}
		return minX, minY, maxX, maxY
	}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePoint(point Point) bool {
	return finite(point.X) && finite(point.Y)
}

// ArrowHead returns the filled triangular head for an arrow. The tip is End;
// the base points lie back along the shaft. UI previews and exported pixels use
// the same geometry.
func ArrowHead(start, end Point, width float64) [3]Point {
	return arrowHead(start, end, width)
}

func arrowHead(start, end Point, width float64) [3]Point {
	dx, dy := end.X-start.X, end.Y-start.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return [3]Point{end, end, end}
	}
	ux, uy := dx/length, dy/length
	headLength := math.Min(math.Max(width*4.5, 12), length*0.45)
	headHalf := headLength * 0.55
	baseX, baseY := end.X-ux*headLength, end.Y-uy*headLength
	return [3]Point{
		end,
		{X: baseX - uy*headHalf, Y: baseY + ux*headHalf},
		{X: baseX + uy*headHalf, Y: baseY - ux*headHalf},
	}
}

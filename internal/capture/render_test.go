package capture

import (
	"image"
	"image/color"
	"testing"
)

func whiteDocument(t *testing.T, width, height int) *Document {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for index := 0; index < len(source.Pix); index += 4 {
		source.Pix[index+0] = 255
		source.Pix[index+1] = 255
		source.Pix[index+2] = 255
		source.Pix[index+3] = 255
	}
	document, err := NewDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func nearColor(got color.RGBA, want color.RGBA, tolerance int) bool {
	difference := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return difference(got.R, want.R) <= tolerance &&
		difference(got.G, want.G) <= tolerance &&
		difference(got.B, want.B) <= tolerance &&
		got.A == want.A
}

func TestRectangleRendersOutlineAndLeavesInterior(t *testing.T) {
	document := whiteDocument(t, 44, 34)
	red := color.NRGBA{R: 230, G: 32, B: 48, A: 255}
	if err := document.Add(Shape{
		Kind: Rectangle, Start: Point{X: 6, Y: 6}, End: Point{X: 38, Y: 28},
		Color: red, Width: 4,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, err := document.Render()
	if err != nil {
		t.Fatal(err)
	}
	wantRed := color.RGBA{R: red.R, G: red.G, B: red.B, A: 255}
	for _, point := range []image.Point{{22, 6}, {22, 28}, {6, 17}, {38, 17}} {
		if got := rendered.RGBAAt(point.X, point.Y); !nearColor(got, wantRed, 3) {
			t.Errorf("stroke at %v = %+v, want %+v", point, got, wantRed)
		}
	}
	if got := rendered.RGBAAt(22, 17); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("rectangle interior = %+v, want white", got)
	}
	if got := rendered.RGBAAt(22, 2); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("pixel outside rectangle = %+v, want white", got)
	}
}

func TestRectangleWithReversedCornersStillRenders(t *testing.T) {
	document := whiteDocument(t, 24, 24)
	if err := document.Add(Shape{
		Kind: Rectangle, Start: Point{X: 18, Y: 18}, End: Point{X: 4, Y: 4},
		Color: DefaultShapeColor, Width: 3,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, _ := document.Render()
	if got := rendered.RGBAAt(11, 4); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("reversed rectangle did not paint its top edge: %+v", got)
	}
}

func TestArrowRendersShaftAndFilledHead(t *testing.T) {
	document := whiteDocument(t, 100, 50)
	red := color.NRGBA{R: 220, G: 30, B: 40, A: 255}
	if err := document.Add(Shape{
		Kind: Arrow, Start: Point{X: 10, Y: 25}, End: Point{X: 80, Y: 25},
		Color: red, Width: 4,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, _ := document.Render()
	wantRed := color.RGBA{R: red.R, G: red.G, B: red.B, A: 255}
	for _, point := range []image.Point{{12, 25}, {50, 25}, {78, 25}, {70, 29}, {70, 21}} {
		if got := rendered.RGBAAt(point.X, point.Y); !nearColor(got, wantRed, 5) {
			t.Errorf("arrow pixel at %v = %+v, want approximately %+v", point, got, wantRed)
		}
	}
	if got := rendered.RGBAAt(50, 18); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("pixel above arrow = %+v, want white", got)
	}
	if got := rendered.RGBAAt(70, 36); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("pixel outside arrowhead = %+v, want white", got)
	}
}

func TestFreehandUsesOneCompositeMask(t *testing.T) {
	document := whiteDocument(t, 60, 40)
	blue := color.NRGBA{R: 20, G: 80, B: 220, A: 255}
	if err := document.Add(Shape{
		Kind:   Freehand,
		Points: []Point{{X: 5, Y: 10}, {X: 25, Y: 10}, {X: 25, Y: 30}, {X: 50, Y: 30}},
		Color:  blue, Width: 4,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, _ := document.Render()
	want := color.RGBA{R: blue.R, G: blue.G, B: blue.B, A: 255}
	for _, point := range []image.Point{{15, 10}, {25, 20}, {38, 30}} {
		if got := rendered.RGBAAt(point.X, point.Y); !nearColor(got, want, 3) {
			t.Errorf("freehand pixel at %v = %+v, want %+v", point, got, want)
		}
	}
	if got := rendered.RGBAAt(5, 5); got != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Errorf("freehand pixel outside path = %+v, want white", got)
	}
}

func TestAnnotationCoordinatesFollowCrop(t *testing.T) {
	document := whiteDocument(t, 50, 50)
	if err := document.SetCrop(image.Rect(10, 10, 40, 40)); err != nil {
		t.Fatal(err)
	}
	if err := document.Add(Shape{
		Kind: Rectangle, Start: Point{X: 15, Y: 15}, End: Point{X: 35, Y: 35},
		Color: DefaultShapeColor, Width: 2,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, _ := document.Render()
	if rendered.Bounds().Size() != image.Pt(30, 30) {
		t.Fatalf("render size = %v", rendered.Bounds().Size())
	}
	if got := rendered.RGBAAt(5, 15); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("annotation was not translated into crop coordinates")
	}
	if got := rendered.RGBAAt(25, 35); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("right edge of translated rectangle was not painted")
	}
}

func TestAnnotationsOutsideCropAreClippedSafely(t *testing.T) {
	document := whiteDocument(t, 20, 20)
	if err := document.SetCrop(image.Rect(5, 5, 15, 15)); err != nil {
		t.Fatal(err)
	}
	if err := document.Add(Shape{
		Kind:   Freehand,
		Points: []Point{{X: -100, Y: -100}, {X: 50, Y: 50}, {X: 100, Y: 100}},
		Color:  DefaultShapeColor, Width: 5,
	}); err != nil {
		t.Fatal(err)
	}
	rendered, err := document.Render()
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Bounds().Size() != image.Pt(10, 10) {
		t.Fatalf("clipped render size = %v", rendered.Bounds().Size())
	}
	if got := rendered.RGBAAt(0, 0); got == (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatal("diagonal freehand should cross the top-left of the crop")
	}
}

func TestShapeValidation(t *testing.T) {
	valid := []Shape{
		{Kind: Rectangle, Start: Point{X: 0, Y: 0}, End: Point{X: 2, Y: 2}, Color: DefaultShapeColor, Width: 1},
		{Kind: Arrow, Start: Point{X: 0, Y: 0}, End: Point{X: 2, Y: 0}, Color: DefaultShapeColor, Width: 1},
		{Kind: Freehand, Points: []Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, Color: DefaultShapeColor, Width: 1},
	}
	for _, shape := range valid {
		if err := shape.Validate(); err != nil {
			t.Errorf("valid shape %+v rejected: %v", shape, err)
		}
	}
	invalid := []Shape{
		{},
		{Kind: Rectangle, Start: Point{X: 1, Y: 1}, End: Point{X: 1, Y: 3}, Width: 1},
		{Kind: Arrow, Start: Point{X: 1, Y: 1}, End: Point{X: 1, Y: 1}, Width: 1},
		{Kind: Freehand, Points: []Point{{X: 1, Y: 1}}, Width: 1},
		{Kind: Freehand, Points: []Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, Width: 0},
	}
	for _, shape := range invalid {
		if err := shape.Validate(); err == nil {
			t.Errorf("invalid shape %+v was accepted", shape)
		}
	}
}

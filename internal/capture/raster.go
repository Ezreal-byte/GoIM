package capture

import (
	"image"
	"image/color"
	"math"
)

func rasterizeShape(dst *image.RGBA, shape Shape, origin image.Point) {
	if dst == nil || shape.Validate() != nil {
		return
	}
	bounds := shape.Bounds()
	bounds.Min = bounds.Min.Sub(origin)
	bounds.Max = bounds.Max.Sub(origin)
	bounds = bounds.Intersect(dst.Bounds())
	if bounds.Empty() {
		return
	}
	mask := make([]byte, bounds.Dx()*bounds.Dy())
	translate := func(point Point) Point {
		return Point{X: point.X - float64(origin.X), Y: point.Y - float64(origin.Y)}
	}
	radius := shape.Width / 2
	switch shape.Kind {
	case Rectangle:
		start, end := translate(shape.Start), translate(shape.End)
		minX, maxX := math.Min(start.X, end.X), math.Max(start.X, end.X)
		minY, maxY := math.Min(start.Y, end.Y), math.Max(start.Y, end.Y)
		addStroke(mask, bounds, []Point{
			{X: minX, Y: minY},
			{X: maxX, Y: minY},
			{X: maxX, Y: maxY},
			{X: minX, Y: maxY},
			{X: minX, Y: minY},
		}, radius)
	case Arrow:
		start, end := translate(shape.Start), translate(shape.End)
		addStroke(mask, bounds, []Point{start, end}, radius)
		head := arrowHead(start, end, shape.Width)
		addTriangle(mask, bounds, head)
	case Freehand:
		points := make([]Point, len(shape.Points))
		for index, point := range shape.Points {
			points[index] = translate(point)
		}
		addStroke(mask, bounds, points, radius)
	}
	blendMask(dst, bounds, mask, shape.Color)
}

func addStroke(mask []byte, maskBounds image.Rectangle, points []Point, radius float64) {
	if len(points) == 0 || radius <= 0 {
		return
	}
	if len(points) == 1 {
		addStrokeSegment(mask, maskBounds, points[0], points[0], radius)
		return
	}
	for index := 1; index < len(points); index++ {
		addStrokeSegment(mask, maskBounds, points[index-1], points[index], radius)
	}
}

func addStrokeSegment(mask []byte, maskBounds image.Rectangle, start, end Point, radius float64) {
	const reach = 1.0
	minX := int(math.Floor(math.Min(start.X, end.X) - radius - reach))
	maxX := int(math.Ceil(math.Max(start.X, end.X) + radius + reach))
	minY := int(math.Floor(math.Min(start.Y, end.Y) - radius - reach))
	maxY := int(math.Ceil(math.Max(start.Y, end.Y) + radius + reach))
	minX, minY = max(minX, maskBounds.Min.X), max(minY, maskBounds.Min.Y)
	maxX, maxY = min(maxX, maskBounds.Max.X), min(maxY, maskBounds.Max.Y)
	if minX >= maxX || minY >= maxY {
		return
	}
	dx, dy := end.X-start.X, end.Y-start.Y
	lengthSquared := dx*dx + dy*dy
	edge := radius + 0.5
	edgeSquared := edge * edge
	width := maskBounds.Dx()
	for y := minY; y < maxY; y++ {
		py := float64(y) + 0.5
		row := (y - maskBounds.Min.Y) * width
		for x := minX; x < maxX; x++ {
			px := float64(x) + 0.5
			qx, qy := start.X, start.Y
			if lengthSquared > 0 {
				t := ((px-start.X)*dx + (py-start.Y)*dy) / lengthSquared
				t = min(max(t, 0), 1)
				qx, qy = start.X+t*dx, start.Y+t*dy
			}
			ex, ey := px-qx, py-qy
			distanceSquared := ex*ex + ey*ey
			if distanceSquared >= edgeSquared {
				continue
			}
			coverage := edge - math.Sqrt(distanceSquared)
			coverage = min(max(coverage, 0), 1)
			value := byte(coverage*255 + 0.5)
			index := row + x - maskBounds.Min.X
			if value > mask[index] {
				mask[index] = value
			}
		}
	}
}

func addTriangle(mask []byte, maskBounds image.Rectangle, vertices [3]Point) {
	minX := int(math.Floor(min(vertices[0].X, min(vertices[1].X, vertices[2].X))))
	maxX := int(math.Ceil(max(vertices[0].X, max(vertices[1].X, vertices[2].X))))
	minY := int(math.Floor(min(vertices[0].Y, min(vertices[1].Y, vertices[2].Y))))
	maxY := int(math.Ceil(max(vertices[0].Y, max(vertices[1].Y, vertices[2].Y))))
	minX, minY = max(minX, maskBounds.Min.X), max(minY, maskBounds.Min.Y)
	maxX, maxY = min(maxX, maskBounds.Max.X), min(maxY, maskBounds.Max.Y)
	if minX >= maxX || minY >= maxY {
		return
	}
	const samples = 4
	const sampleCount = samples * samples
	width := maskBounds.Dx()
	for y := minY; y < maxY; y++ {
		row := (y - maskBounds.Min.Y) * width
		for x := minX; x < maxX; x++ {
			covered := 0
			for sampleY := 0; sampleY < samples; sampleY++ {
				py := float64(y) + (float64(sampleY)+0.5)/samples
				for sampleX := 0; sampleX < samples; sampleX++ {
					px := float64(x) + (float64(sampleX)+0.5)/samples
					if pointInTriangle(Point{X: px, Y: py}, vertices) {
						covered++
					}
				}
			}
			if covered == 0 {
				continue
			}
			value := byte((covered*255 + sampleCount/2) / sampleCount)
			index := row + x - maskBounds.Min.X
			if value > mask[index] {
				mask[index] = value
			}
		}
	}
}

func pointInTriangle(point Point, vertices [3]Point) bool {
	a, b, c := vertices[0], vertices[1], vertices[2]
	d1 := edgeSign(point, a, b)
	d2 := edgeSign(point, b, c)
	d3 := edgeSign(point, c, a)
	hasNegative := d1 < 0 || d2 < 0 || d3 < 0
	hasPositive := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNegative && hasPositive)
}

func edgeSign(point, start, end Point) float64 {
	return (point.X-end.X)*(start.Y-end.Y) - (start.X-end.X)*(point.Y-end.Y)
}

func blendMask(dst *image.RGBA, bounds image.Rectangle, mask []byte, color color.NRGBA) {
	width := bounds.Dx()
	alpha := uint32(color.A)
	if alpha == 0 {
		return
	}
	red, green, blue := uint32(color.R), uint32(color.G), uint32(color.B)
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < width; x++ {
			coverage := uint32(mask[y*width+x])
			if coverage == 0 {
				continue
			}
			source := coverage * alpha / 255
			inverse := 255 - source
			offset := dst.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
			dst.Pix[offset+0] = byte((red*source + uint32(dst.Pix[offset+0])*inverse + 127) / 255)
			dst.Pix[offset+1] = byte((green*source + uint32(dst.Pix[offset+1])*inverse + 127) / 255)
			dst.Pix[offset+2] = byte((blue*source + uint32(dst.Pix[offset+2])*inverse + 127) / 255)
			dst.Pix[offset+3] = byte((255*source + uint32(dst.Pix[offset+3])*inverse + 127) / 255)
		}
	}
}

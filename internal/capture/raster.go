package capture

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

func rasterizeShape(dst *image.RGBA, shape Shape, origin image.Point) {
	if dst == nil || shape.Validate() != nil {
		return
	}
	bounds := shape.Bounds()
	bounds.Min = bounds.Min.Sub(origin)
	bounds.Max = bounds.Max.Sub(origin)
	unclipped := bounds
	bounds = bounds.Intersect(dst.Bounds())
	if bounds.Empty() {
		return
	}
	if shape.Kind == Text {
		draw.Draw(dst, bounds, shape.Ink, bounds.Min.Sub(unclipped.Min).Add(shape.Ink.Bounds().Min), draw.Over)
		return
	}
	if shape.Kind == Mosaic {
		pixelate(dst, bounds, unclipped.Min, max(2, int(shape.Width)))
		return
	}
	mask := make([]byte, bounds.Dx()*bounds.Dy())
	translate := func(point Point) Point {
		return Point{X: point.X - float64(origin.X), Y: point.Y - float64(origin.Y)}
	}
	radius := shape.Width / 2
	switch shape.Kind {
	case Ellipse:
		start, end := translate(shape.Start), translate(shape.End)
		cx, cy := (start.X+end.X)/2, (start.Y+end.Y)/2
		rx, ry := math.Abs(end.X-start.X)/2, math.Abs(end.Y-start.Y)/2
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				px, py := (float64(x)+.5-cx)/rx, (float64(y)+.5-cy)/ry
				gradient := math.Hypot(px/rx, py/ry)
				distance := min(rx, ry)
				if gradient > 0 {
					distance = math.Abs(px*px+py*py-1) / (2 * gradient)
				}
				a := min(max(radius+.5-distance, 0), 1)
				mask[(y-bounds.Min.Y)*bounds.Dx()+x-bounds.Min.X] = byte(a*255 + .5)
			}
		}
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

func pixelate(dst *image.RGBA, bounds image.Rectangle, origin image.Point, size int) {
	x0 := origin.X + ((bounds.Min.X-origin.X)/size)*size
	y0 := origin.Y + ((bounds.Min.Y-origin.Y)/size)*size
	for y := y0; y < bounds.Max.Y; y += size {
		for x := x0; x < bounds.Max.X; x += size {
			block := bounds.Intersect(image.Rect(x, y, x+size, y+size))
			var red, green, blue uint64
			for py := block.Min.Y; py < block.Max.Y; py++ {
				for px := block.Min.X; px < block.Max.X; px++ {
					i := dst.PixOffset(px, py)
					red += uint64(dst.Pix[i])
					green += uint64(dst.Pix[i+1])
					blue += uint64(dst.Pix[i+2])
				}
			}
			count := uint64(block.Dx()) * uint64(block.Dy())
			if count != 0 {
				r, g, b := uint8(red/count), uint8(green/count), uint8(blue/count)
				for py := block.Min.Y; py < block.Max.Y; py++ {
					i := dst.PixOffset(block.Min.X, py)
					for range block.Dx() {
						dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = r, g, b, 255
						i += 4
					}
				}
			}
		}
	}
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

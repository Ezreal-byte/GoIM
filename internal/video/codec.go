package video

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
)

const (
	DefaultMaxWidth  = 640
	DefaultMaxHeight = 360
	DefaultQuality   = 65
	MaxFrameBytes    = 256 << 10
	MaxDecodeWidth   = 1920
	MaxDecodeHeight  = 1080
)

// EncodeJPEG downscales frame to fit inside maxWidth x maxHeight (preserving
// aspect ratio) and encodes it as a compact baseline JPEG.
func EncodeJPEG(frame Frame, maxWidth, maxHeight, quality int) ([]byte, error) {
	if frame.Width <= 0 || frame.Height <= 0 || len(frame.BGRA) < frame.Width*frame.Height*4 {
		return nil, errors.New("video: invalid BGRA frame")
	}
	if maxWidth <= 0 {
		maxWidth = DefaultMaxWidth
	}
	if maxHeight <= 0 {
		maxHeight = DefaultMaxHeight
	}
	if quality <= 0 || quality > 100 {
		quality = DefaultQuality
	}
	dstW, dstH := fitSize(frame.Width, frame.Height, maxWidth, maxHeight)
	img := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if dstW == frame.Width && dstH == frame.Height {
		bgraToRGBA(img.Pix, frame.BGRA[:dstW*dstH*4])
	} else {
		scaleBGRAToRGBA(img.Pix, dstW, dstH, frame.BGRA, frame.Width, frame.Height)
	}
	var buf bytes.Buffer
	buf.Grow(24 << 10)
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeJPEG validates and decodes a JPEG frame into tightly packed top-down BGRA.
func DecodeJPEG(data []byte) (Frame, error) {
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return Frame{}, errors.New("video: invalid JPEG payload size")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Frame{}, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDecodeWidth || cfg.Height > MaxDecodeHeight {
		return Frame{}, errors.New("video: decoded frame dimensions out of range")
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return Frame{}, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || w > MaxDecodeWidth || h > MaxDecodeHeight {
		return Frame{}, errors.New("video: decoded bounds out of range")
	}
	bgra := make([]byte, w*h*4)
	switch src := img.(type) {
	case *image.YCbCr:
		ycbcrToBGRA(bgra, src)
	case *image.RGBA:
		rgbaToBGRARect(bgra, src)
	case *image.NRGBA:
		nrgbaToBGRARect(bgra, src)
	default:
		off := 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := src.At(x, y).RGBA()
				bgra[off] = uint8(b >> 8)
				bgra[off+1] = uint8(g >> 8)
				bgra[off+2] = uint8(r >> 8)
				bgra[off+3] = 255
				off += 4
			}
		}
	}
	return Frame{Width: w, Height: h, BGRA: bgra}, nil
}

func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	w, h := srcW, srcH
	if w*maxH >= h*maxW {
		h = max(1, h*maxW/w)
		w = maxW
	} else {
		w = max(1, w*maxH/h)
		h = maxH
	}
	return w, h
}

func bgraToRGBA(dst, src []byte) {
	for i := 0; i+3 < len(src); i += 4 {
		dst[i] = src[i+2]
		dst[i+1] = src[i+1]
		dst[i+2] = src[i]
		dst[i+3] = 255
	}
}

func scaleBGRAToRGBA(dst []byte, dstW, dstH int, src []byte, srcW, srcH int) {
	for y := 0; y < dstH; y++ {
		sy := y * srcH / dstH
		srcRow := sy * srcW * 4
		dstRow := y * dstW * 4
		for x := 0; x < dstW; x++ {
			sx := x * srcW / dstW
			si := srcRow + sx*4
			di := dstRow + x*4
			dst[di] = src[si+2]
			dst[di+1] = src[si+1]
			dst[di+2] = src[si]
			dst[di+3] = 255
		}
	}
}

func rgbaToBGRARect(dst []byte, src *image.RGBA) {
	b := src.Bounds()
	w := b.Dx()
	for y := 0; y < b.Dy(); y++ {
		sOff := src.PixOffset(b.Min.X, b.Min.Y+y)
		dOff := y * w * 4
		for x := 0; x < w; x++ {
			si := sOff + x*4
			di := dOff + x*4
			dst[di] = src.Pix[si+2]
			dst[di+1] = src.Pix[si+1]
			dst[di+2] = src.Pix[si]
			dst[di+3] = 255
		}
	}
}

func nrgbaToBGRARect(dst []byte, src *image.NRGBA) {
	b := src.Bounds()
	w := b.Dx()
	for y := 0; y < b.Dy(); y++ {
		sOff := src.PixOffset(b.Min.X, b.Min.Y+y)
		dOff := y * w * 4
		for x := 0; x < w; x++ {
			si := sOff + x*4
			di := dOff + x*4
			dst[di] = src.Pix[si+2]
			dst[di+1] = src.Pix[si+1]
			dst[di+2] = src.Pix[si]
			dst[di+3] = 255
		}
	}
}

func ycbcrToBGRA(dst []byte, src *image.YCbCr) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	off := 0
	for y := 0; y < h; y++ {
		yy := b.Min.Y + y
		yRow := src.YOffset(b.Min.X, yy)
		for x := 0; x < w; x++ {
			xx := b.Min.X + x
			yi := yRow + x
			ci := src.COffset(xx, yy)
			r, g, bl := ycbcrPixel(src.Y[yi], src.Cb[ci], src.Cr[ci])
			dst[off] = bl
			dst[off+1] = g
			dst[off+2] = r
			dst[off+3] = 255
			off += 4
		}
	}
}

// YUY2ToBGRA converts packed YUY2 (Y0 U Y1 V) into tightly packed BGRA.
func YUY2ToBGRA(dst, src []byte, width, height int) {
	if width <= 0 || height <= 0 || len(src) < width*height*2 || len(dst) < width*height*4 {
		return
	}
	pairs := (width * height) / 2
	for i := 0; i < pairs; i++ {
		si := i * 4
		di := i * 8
		y0, u, y1, v := src[si], src[si+1], src[si+2], src[si+3]
		r0, g0, b0 := ycbcrPixel(y0, u, v)
		r1, g1, b1 := ycbcrPixel(y1, u, v)
		dst[di], dst[di+1], dst[di+2], dst[di+3] = b0, g0, r0, 255
		dst[di+4], dst[di+5], dst[di+6], dst[di+7] = b1, g1, r1, 255
	}
}

// NV12ToBGRA converts semi-planar NV12 (Y plane followed by interleaved UV) to BGRA.
func NV12ToBGRA(dst, src []byte, width, height int) {
	ySize := width * height
	if width <= 0 || height <= 0 || len(src) < ySize+ySize/2 || len(dst) < ySize*4 {
		return
	}
	uvPlane := src[ySize:]
	for y := 0; y < height; y++ {
		yRow := y * width
		uvRow := (y / 2) * width
		dRow := y * width * 4
		for x := 0; x < width; x++ {
			yv := src[yRow+x]
			uvOff := uvRow + (x &^ 1)
			u, v := uvPlane[uvOff], uvPlane[uvOff+1]
			r, g, b := ycbcrPixel(yv, u, v)
			di := dRow + x*4
			dst[di], dst[di+1], dst[di+2], dst[di+3] = b, g, r, 255
		}
	}
}

func ycbcrPixel(y, cb, cr uint8) (uint8, uint8, uint8) {
	yy := int32(y) * 0x10101
	cb1 := int32(cb) - 128
	cr1 := int32(cr) - 128
	r := yy + 91881*cr1
	g := yy - 22554*cb1 - 46802*cr1
	b := yy + 116130*cb1
	return clampByte(r >> 16), clampByte(g >> 16), clampByte(b >> 16)
}

func clampByte(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

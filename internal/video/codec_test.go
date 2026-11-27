package video

import "testing"

func TestEncodeDecodeJPEGRoundTripAndScale(t *testing.T) {
	w, h := 1280, 720
	bgra := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			off := (y*w + x) * 4
			bgra[off] = uint8(x * 255 / w)
			bgra[off+1] = uint8(y * 255 / h)
			bgra[off+2] = 180
			bgra[off+3] = 255
		}
	}
	encoded, err := EncodeJPEG(Frame{Width: w, Height: h, BGRA: bgra}, 640, 360, 70)
	if err != nil {
		t.Fatalf("EncodeJPEG: %v", err)
	}
	if len(encoded) == 0 || len(encoded) > MaxFrameBytes {
		t.Fatalf("unexpected encoded size %d", len(encoded))
	}
	decoded, err := DecodeJPEG(encoded)
	if err != nil {
		t.Fatalf("DecodeJPEG: %v", err)
	}
	if decoded.Width != 640 || decoded.Height != 360 {
		t.Fatalf("decoded size = %dx%d, want 640x360", decoded.Width, decoded.Height)
	}
	if len(decoded.BGRA) != 640*360*4 {
		t.Fatalf("decoded len = %d", len(decoded.BGRA))
	}
}

func TestDecodeJPEGRejectsOversizedOrInvalid(t *testing.T) {
	if _, err := DecodeJPEG(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
	if _, err := DecodeJPEG(make([]byte, MaxFrameBytes+1)); err == nil {
		t.Fatal("expected error for oversized payload")
	}
	if _, err := DecodeJPEG([]byte("not a jpeg")); err == nil {
		t.Fatal("expected error for invalid jpeg")
	}
}

func TestYUY2AndNV12Converters(t *testing.T) {
	// Pure white-ish Y=235, U=128, V=128
	yuy2 := []byte{235, 128, 235, 128, 16, 128, 16, 128}
	dst := make([]byte, 4*4)
	YUY2ToBGRA(dst, yuy2, 2, 2)
	if dst[0] < 220 || dst[1] < 220 || dst[2] < 220 || dst[3] != 255 {
		t.Fatalf("unexpected bright pixel from YUY2: %v", dst[:4])
	}
	if dst[8] > 30 || dst[9] > 30 || dst[10] > 30 {
		t.Fatalf("unexpected dark pixel from YUY2: %v", dst[8:12])
	}

	nv12 := []byte{235, 235, 16, 16, 128, 128}
	dstNV := make([]byte, 4*4)
	NV12ToBGRA(dstNV, nv12, 2, 2)
	if dstNV[0] < 220 || dstNV[8] > 30 {
		t.Fatalf("unexpected NV12 conversion: bright=%v dark=%v", dstNV[:4], dstNV[8:12])
	}
}

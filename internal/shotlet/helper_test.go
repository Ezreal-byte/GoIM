package shotlet

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"
)

func TestReadShotletFramePreservesRGBA(t *testing.T) {
	pixels := []byte{210, 100, 54, 128, 20, 90, 185, 255}
	frame, err := readFrame(bytes.NewReader(shotletFrame(2, 1, pixels)))
	if err != nil {
		t.Fatal(err)
	}
	if got := frame.Bounds(); got != image.Rect(0, 0, 2, 1) {
		t.Fatalf("decoded bounds = %v", got)
	}
	if !bytes.Equal(frame.Pix, pixels) {
		t.Fatalf("decoded pixels = %v, want %v", frame.Pix, pixels)
	}
}

func TestReadShotletFrameRejectsInvalidOutput(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":         nil,
		"bad magic":     append([]byte("OTHER1\n\n"), shotletFrame(1, 1, make([]byte, 4))[len(resultMagic):]...),
		"empty image":   shotletFrame(0, 1, nil),
		"oversized":     shotletFrame(1<<20, 1<<20, nil),
		"short payload": shotletFrame(1, 1, make([]byte, 3)),
		"extra payload": shotletFrame(1, 1, make([]byte, 5)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readFrame(bytes.NewReader(data)); err == nil {
				t.Fatal("invalid helper output was accepted")
			}
		})
	}
}

func shotletFrame(width, height uint32, pixels []byte) []byte {
	frame := binary.LittleEndian.AppendUint32([]byte(resultMagic), width)
	frame = binary.LittleEndian.AppendUint32(frame, height)
	return append(frame, pixels...)
}

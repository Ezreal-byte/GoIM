package shotlet

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxHelperPathMatchesTarAndDeb(t *testing.T) {
	root := t.TempDir()
	for _, executable := range []string{filepath.Join(root, "GoIM"), filepath.Join(root, "bin", "GoIM")} {
		if got, want := helperPath(executable, "linux"), filepath.Join(root, "lib", "goim", "shotlet", "shotlet-capture"); got != want {
			t.Fatalf("helper for %s = %s, want %s", executable, got, want)
		}
	}
	portable := filepath.Join(root, "bin", "lib", "goim", "shotlet", "shotlet-capture")
	if err := os.MkdirAll(filepath.Dir(portable), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portable, []byte("helper"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := helperPath(filepath.Join(root, "bin", "GoIM"), "linux"); got != portable {
		t.Fatalf("portable extraction named bin resolved %s", got)
	}
}

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

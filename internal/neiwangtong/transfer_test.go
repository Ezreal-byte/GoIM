package neiwangtong

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func createTestPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for x := 0; x < 32; x++ {
		for y := 0; y < 32; y++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestTransferServerAvatarAndFile(t *testing.T) {
	srv, err := StartTransferServer(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 0, nil)
	if err != nil {
		t.Fatalf("StartTransferServer failed: %v", err)
	}
	defer srv.Close()

	// 1. Test Avatar serving
	testPNG := createTestPNG()
	srv.SetAvatar(testPNG)

	target := netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(srv.Port()))
	fetched, err := FetchOfferedAvatar(context.Background(), netip.AddrFrom4([4]byte{127, 0, 0, 1}), target)
	if err != nil {
		t.Fatalf("FetchOfferedAvatar failed: %v", err)
	}
	if len(fetched) != len(testPNG) {
		t.Fatalf("Fetched avatar length %d, expected %d", len(fetched), len(testPNG))
	}

	// 2. Test File serving
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.txt")
	testData := []byte("hello native neiwangtong file transfer!")
	if err := os.WriteFile(srcFile, testData, 0644); err != nil {
		t.Fatal(err)
	}

	taskID := uint64(98765)
	srv.RegisterFile(taskID, srcFile, int64(len(testData)))

	destFile := filepath.Join(tmpDir, "dest.txt")
	var lastDone, lastTotal int64
	err = DownloadFile(context.Background(), target, taskID, int64(len(testData)), destFile, func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}
	if lastDone != int64(len(testData)) || lastTotal != int64(len(testData)) {
		t.Errorf("Progress mismatch: done=%d, total=%d", lastDone, lastTotal)
	}
	readBack, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBack, testData) {
		t.Fatalf("File data mismatch: got %s, want %s", string(readBack), string(testData))
	}
}

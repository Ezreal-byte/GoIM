package neiwangtong

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	err = downloadFile(context.Background(), loopback, target, taskID, int64(len(testData)), destFile, func(done, total int64) {
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

func TestTransferServerMultiplexesAvatarAndFileOnOneConnection(t *testing.T) {
	srv, err := StartTransferServer(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	pngBytes := createTestPNG()
	srv.SetAvatar(pngBytes)
	dir := t.TempDir()
	source := filepath.Join(dir, "multiplex.txt")
	payload := []byte("avatar followed by a file")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	const fileID = 918273
	srv.RegisterFile(fileID, source, int64(len(payload)))

	conn, err := net.DialTimeout("tcp4", netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(srv.Port())).String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(avatarRequest()); err != nil {
		t.Fatal(err)
	}
	var confirmation [108]byte
	if _, err := io.ReadFull(conn, confirmation[:]); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(confirmation[8:12]) != 2 {
		t.Fatal("avatar confirmation was not received")
	}
	var imageHead [100]byte
	if _, err := io.ReadFull(conn, imageHead[:]); err != nil {
		t.Fatal(err)
	}
	imageFrameLength := binary.BigEndian.Uint32(imageHead[:4])
	if imageFrameLength != uint32(100+len(pngBytes)) || binary.BigEndian.Uint32(imageHead[8:12]) != 4 {
		t.Fatal("invalid avatar data frame")
	}
	imageBody := make([]byte, imageFrameLength-100)
	if _, err := io.ReadFull(conn, imageBody); err != nil || !bytes.Equal(imageBody, pngBytes) {
		t.Fatalf("avatar body mismatch: %v", err)
	}
	if _, err := conn.Write(nativeFileRequest(fileID)); err != nil {
		t.Fatal(err)
	}
	var fileHead [100]byte
	if _, err := io.ReadFull(conn, fileHead[:]); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(fileHead[:4]) != uint32(100+len(payload)) ||
		binary.BigEndian.Uint32(fileHead[8:12]) != 4 ||
		binary.BigEndian.Uint32(fileHead[56:60]) != fileID {
		t.Fatal("invalid file data frame")
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, received); err != nil || !bytes.Equal(received, payload) {
		t.Fatalf("file body mismatch: %v", err)
	}
}

func TestReverseFileChannel(t *testing.T) {
	ready := make(chan struct {
		fileID uint64
		remote netip.Addr
	}, 1)
	srv, err := startTransferServer(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 0, nil, nil, func(fileID uint64, remote netip.Addr) {
		ready <- struct {
			fileID uint64
			remote netip.Addr
		}{fileID, remote}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	target := netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(srv.ReversePort()))
	conn, err := OpenReverseFileChannel(context.Background(), target, 424242)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case event := <-ready:
		if event.fileID != 424242 || event.remote != netip.AddrFrom4([4]byte{127, 0, 0, 1}) {
			t.Fatalf("unexpected reverse event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("reverse file callback was not received")
	}
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var probe [1]byte
	if _, err := conn.Read(probe[:]); err == nil {
		t.Fatal("reverse bootstrap channel unexpectedly received data")
	} else if !errors.Is(err, os.ErrDeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("reverse bootstrap channel closed before transfer completion: %v", err)
	}
	srv.finishReverse(424242)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(probe[:]); !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("reverse bootstrap channel was not closed after completion: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for len(srv.slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(srv.slots) != 0 {
		t.Fatal("reverse file completion leaked a transfer slot")
	}
}

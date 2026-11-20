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
	"sync/atomic"
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

func TestRegisteredGroupFileStopsServingAfterMembershipIsRevoked(t *testing.T) {
	srv, err := StartTransferServer(loopback, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	dir := t.TempDir()
	data := []byte("group member's selected file")
	source := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	var member atomic.Bool
	member.Store(true)
	const selector = 3689
	if err := srv.registerFileWithAccess(selector, 782, source, int64(len(data)), loopback, true, FileAccess{Allowed: member.Load}); err != nil {
		t.Fatal(err)
	}
	target := netip.AddrPortFrom(loopback, uint16(srv.Port()))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dest := filepath.Join(dir, "authorized.txt")
	if err := downloadFile(ctx, loopback, target, selector, int64(len(data)), dest, true, nil); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.ReadFile(dest); err != nil || !bytes.Equal(actual, data) {
		t.Fatalf("authorized transfer failed: %v", err)
	}
	member.Store(false)
	dest = filepath.Join(dir, "removed.txt")
	if err := downloadFile(ctx, loopback, target, selector, int64(len(data)), dest, true, nil); err == nil {
		t.Fatal("a removed group member could still retrieve the registered file")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the denied request left a download on disk: %v", err)
	}
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
	err = downloadFile(context.Background(), loopback, target, taskID, int64(len(testData)), destFile, true, func(done, total int64) {
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
	if binary.BigEndian.Uint32(confirmation[8:12]) != 2 || binary.BigEndian.Uint64(confirmation[60:68]) != uint64(len(pngBytes)) {
		t.Fatal("avatar confirmation did not contain the PNG length")
	}
	var imageHead [100]byte
	if _, err := io.ReadFull(conn, imageHead[:]); err != nil {
		t.Fatal(err)
	}
	imageFrameLength := binary.BigEndian.Uint32(imageHead[:4])
	if imageFrameLength != uint32(100+len(pngBytes)) || binary.BigEndian.Uint32(imageHead[8:12]) != 4 ||
		binary.BigEndian.Uint64(imageHead[60:68]) != uint64(len(pngBytes)) || binary.BigEndian.Uint32(imageHead[76:80]) != uint32(len(pngBytes)) {
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

func TestNativeDownloadRemovesInterruptedFileAndCanRetry(t *testing.T) {
	t.Run("discard ordinary partial", func(t *testing.T) { testInterruptedFile(t, false) })
	t.Run("resume retained prefix", func(t *testing.T) { testInterruptedFile(t, true) })
}

func TestNativeUploadReportsDroppedSocketAndKeepsOffer(t *testing.T) {
	const task, selector = 341, 792
	paused, resume := make(chan struct{}), make(chan struct{})
	failed := make(chan int64, 1)
	srv, err := startTransferServer(loopback, 0, func(id uint64, done, _ int64, failure error) {
		if id != task {
			return
		}
		if failure != nil {
			failed <- done
		} else if done == nativeFileBlock {
			close(paused)
			<-resume
		}
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	data := bytes.Repeat([]byte("retained prefix and remaining bytes"), 10240)
	root := t.TempDir()
	source, dest := filepath.Join(root, "source"), filepath.Join(root, "partial")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	if err := srv.registerFile(selector, task, source, size, loopback, true); err != nil {
		t.Fatal(err)
	}
	target := netip.AddrPortFrom(loopback, uint16(srv.Port()))
	conn, err := net.DialTCP("tcp4", nil, net.TCPAddrFromAddrPort(target))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(nativeFileRequest(selector)); err != nil {
		t.Fatal(err)
	}
	frame, err := readNativeTCPFrame(conn, nativeFileHeader+nativeFileBlock)
	if err != nil || len(frame) != nativeFileHeader+nativeFileBlock {
		t.Fatalf("first native frame: %d bytes, %v", len(frame), err)
	}
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("the sender did not stop after the first block")
	}
	if err := conn.SetLinger(0); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	close(resume)
	select {
	case done := <-failed:
		// The kernel may accept another block after the peer resets the
		// connection. Progress is committed only after a successful write, so
		// the exact amount at which the following write fails varies by OS.
		if done < nativeFileBlock || done >= size {
			t.Fatalf("failure reported at %d bytes, expected 1..%d bytes", done, size-1)
		}
	case <-ctx.Done():
		t.Fatal("the dropped socket did not report a failed upload")
	}
	if err := os.WriteFile(dest, frame[nativeFileHeader:], 0600); err != nil {
		t.Fatal(err)
	}
	if err := downloadFileTo(ctx, loopback, target, selector, size, dest, true, true, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("the original offer did not resume: %v", err)
	}
}

func testInterruptedFile(t *testing.T, resumable bool) {
	resume := make(chan struct{})
	defer func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	}()
	srv, err := StartTransferServer(loopback, 0, func(_ uint64, done, _ int64) {
		if done == nativeFileBlock {
			<-resume
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	dir := t.TempDir()
	source, dest := filepath.Join(dir, "source.bin"), filepath.Join(dir, "received.bin")
	data := bytes.Repeat([]byte("retry after disconnect\x00"), 50000)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	const task, selector = 319, 791
	if err := srv.registerFile(selector, task, source, int64(len(data)), loopback, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := netip.AddrPortFrom(loopback, uint16(srv.Port()))
	// Revoke the sender's file after the first block has reached disk.
	err = downloadFileTo(ctx, loopback, target, selector, int64(len(data)), dest, true, resumable, func(done, _ int64) {
		if done == nativeFileBlock {
			srv.UnregisterFile(selector)
			close(resume)
		}
	})
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected the revoked send to interrupt the stream, got %v", err)
	}
	if resumable {
		st, err := os.Stat(dest)
		if err != nil || st.Size() != nativeFileBlock {
			t.Fatalf("interrupted transfer did not retain exactly one block: %v", err)
		}
	} else if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("interrupted transfer left a destination: %v", err)
	}
	if err := srv.registerFile(selector, task, source, int64(len(data)), loopback, true); err != nil {
		t.Fatal(err)
	}
	var first int64
	if err := downloadFileTo(ctx, loopback, target, selector, int64(len(data)), dest, true, resumable, func(done, _ int64) {
		if first == 0 {
			first = done
		}
	}); err != nil {
		t.Fatal(err)
	}
	if resumable && first != 2*nativeFileBlock {
		t.Fatalf("resume did not skip the first block; next progress=%d", first)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("retry did not restore all bytes: %v", err)
	}
}

func TestReceiveStockFileFrames(t *testing.T) {
	const selector = 27926
	payload := bytes.Repeat([]byte("stock file block stream"), 4000)
	for _, badOffset := range []bool{false, true} {
		name := "complete"
		if badOffset {
			name = "invalid offset"
		}
		t.Run(name, func(t *testing.T) {
			local, remote := net.Pipe()
			defer local.Close()
			go func() {
				defer remote.Close()
				for offset := 0; offset < len(payload); {
					chunk := payload[offset:min(len(payload), offset+32768)]
					var h [100]byte
					binary.BigEndian.PutUint32(h[:4], uint32(100+len(chunk)))
					binary.BigEndian.PutUint32(h[4:8], 1)
					binary.BigEndian.PutUint32(h[8:12], 4)
					binary.BigEndian.PutUint32(h[56:60], selector)
					binary.BigEndian.PutUint64(h[60:68], uint64(len(payload)))
					wireOffset := offset
					if badOffset && offset > 0 {
						wireOffset--
					}
					binary.BigEndian.PutUint64(h[68:76], uint64(wireOffset))
					binary.BigEndian.PutUint32(h[76:80], uint32(len(chunk)))
					if _, err := remote.Write(h[:]); err != nil {
						return
					}
					if _, err := remote.Write(chunk); err != nil {
						return
					}
					offset += len(chunk)
				}
			}()
			var got bytes.Buffer
			err := receiveFileFrames(local, &got, selector, int64(len(payload)), nil)
			if badOffset {
				if err == nil || got.Len() != 32768 {
					t.Fatalf("expected rejection before writing the second block: %v, got %d bytes", err, got.Len())
				}
			} else if err != nil || !bytes.Equal(got.Bytes(), payload) {
				t.Fatalf("complete download mismatch: %v, got %d bytes", err, got.Len())
			}
		})
	}
}

func TestReverseFileChannel(t *testing.T) {
	srv, err := StartTransferServer(loopback, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	key := reverseKey{remote: loopback, selector: 424242}
	ready, err := srv.expectReverse(key)
	if err != nil {
		t.Fatal(err)
	}
	target := netip.AddrPortFrom(loopback, uint16(srv.ReversePort()))
	conn, err := OpenReverseFileChannel(context.Background(), target, 424242)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case <-ready:
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
	srv.finishReverse(key)
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

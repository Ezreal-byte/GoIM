package remotedesktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"goim/internal/domain"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--goim-remote" {
		// Fixture for inherited pipes, independent of the Rust media engine.
		var header [5]byte
		if _, err := io.ReadFull(os.Stdin, header[:]); err != nil || header[0] != controlPacket {
			os.Exit(2)
		}
		length := binary.BigEndian.Uint32(header[1:])
		if length == 0 || length > maxControl {
			os.Exit(2)
		}
		body := make([]byte, length)
		var start command
		if _, err := io.ReadFull(os.Stdin, body); err != nil ||
			json.Unmarshal(body, &start) != nil || start.Type != "start" {
			os.Exit(2)
		}
		if start.Role == "host" {
			fmt.Fprintln(os.Stderr, "goim-rd: libc.so.6: version GLIBC_2.34 not found")
			os.Exit(127)
		}
		if start.Role == "viewer" && start.Control && !start.Audio {
			_ = writePacket(os.Stdout, controlPacket, []byte(`{"type":"ready"}`))
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
		if start.Role != "viewer" || !start.Audio || start.Control {
			os.Exit(2)
		}
		if writePacket(os.Stdout, controlPacket, []byte(`{"type":"ready"}`)) != nil ||
			writePacket(os.Stdout, controlPacket, []byte(`{"type":"audio_devices","devices":["Test Speaker"]}`)) != nil {
			os.Exit(2)
		}
		pixels := []byte{0, 0, 0, 1, 0, 0, 0, 1, 0, 40, 80, 255}
		if writePacket(os.Stdout, pixelPacket, pixels) != nil {
			os.Exit(2)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRemoteDesktopReportsMissingHelperRuntime(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tunnel, peer := net.Pipe()
	defer tunnel.Close()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = localEngine(exe)(ctx, tunnel, Start{Role: "host"}, nil, Events{})
	if Reason(err) != "unsupported" {
		t.Fatalf("expected a missing runtime reason, got %v", err)
	}
}

func TestRemoteDesktopCancellationUnblocksChildPipe(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tunnel, peer := net.Pipe()
	defer tunnel.Close()
	defer peer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	stopped := make(chan error, 1)
	go func() {
		stopped <- localEngine(exe)(ctx, tunnel, Start{Role: "viewer", Control: true}, nil,
			Events{Ready: func() { close(ready) }})
	}()
	select {
	case <-ready:
	case err := <-stopped:
		t.Fatalf("fixture exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not become ready")
	}
	blocked := make(chan struct{})
	go func() {
		_, _ = peer.Write(make([]byte, 4*maxTunnel))
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("fixture drained the data that should block its input pipe")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("a full child input pipe prevented cancellation")
	}
}

func TestRemoteDesktopPrivatePipesAndCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tunnel, peer := net.Pipe()
	defer tunnel.Close()
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	frames := make(chan domain.RemoteDesktopFrame, 1)
	devices := make(chan []string, 1)
	stopped := make(chan error, 1)
	go func() {
		stopped <- localEngine(exe)(ctx, tunnel, Start{Role: "viewer", Audio: true}, nil, Events{
			Ready:        func() { close(ready) },
			Frame:        func(frame domain.RemoteDesktopFrame) { frames <- frame },
			AudioDevices: func(names []string) { devices <- names },
		})
	}()
	select {
	case <-ready:
	case err := <-stopped:
		t.Fatalf("subprocess exited before hello: %v", err)
	case <-ctx.Done():
		t.Fatal("subprocess did not become ready")
	}
	select {
	case names := <-devices:
		if len(names) != 1 || names[0] != "Test Speaker" {
			t.Fatal("bad audio device payload", names)
		}
	case <-ctx.Done():
		t.Fatal("no output devices")
	}
	select {
	case frame := <-frames:
		if frame.Width != 1 || frame.Height != 1 || !bytes.Equal(frame.BGRA, []byte{0, 40, 80, 255}) {
			t.Fatal("bad frame payload", frame)
		}
	case <-ctx.Done():
		t.Fatal("no pixel data")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("subprocess did not exit after closing its input")
	}
}

func TestRemoteDesktopRejectsMalformedOrPrematureFrames(t *testing.T) {
	for _, payload := range [][]byte{
		{0, 0, 0, 1, 0, 0, 0, 1},              // missing pixels
		{0, 0, 0, 1, 0, 0, 0, 0, 1, 2, 3, 4},  // zero height
		{0, 0, 15, 1, 0, 0, 0, 1, 1, 2, 3, 4}, // width over cap
	} {
		var input bytes.Buffer
		_ = writePacket(&input, controlPacket, []byte(`{"type":"ready"}`))
		_ = writePacket(&input, pixelPacket, payload)
		err := readChild(&input, nil, Start{Role: "viewer"}, Events{
			Ready: func() {}, Frame: func(domain.RemoteDesktopFrame) { t.Error("accepted invalid frame") },
		})
		if err == nil {
			t.Fatal("invalid frame went undetected")
		}
	}
	var input bytes.Buffer
	_ = writePacket(&input, pixelPacket, []byte{0, 0, 0, 1, 0, 0, 0, 1, 1, 2, 3, 4})
	if err := readChild(&input, nil, Start{Role: "viewer"}, Events{}); err == nil {
		t.Fatal("helper could present a frame before ready")
	}
}

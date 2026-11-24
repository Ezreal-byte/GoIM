// Package remotedesktop brokers an authorized GoIM session to a local engine.
package remotedesktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"goim/internal/domain"
)

// ValidationBuild is enabled by the opt-in validation packager.
var ValidationBuild string

const (
	controlPacket = 1
	tunnelPacket  = 2
	pixelPacket   = 3

	maxControl = 16 << 10
	maxTunnel  = 64 << 10
	maxWidth   = 3840
	maxHeight  = 2160
)

type Start struct {
	Role    string
	Control bool
	Audio   bool
}

type Events struct {
	Ready        func()
	Frame        func(domain.RemoteDesktopFrame)
	AudioDevices func([]string)
}

type Engine func(ctx context.Context, tunnel net.Conn, grant Start, input <-chan domain.RemoteDesktopInput, events Events) error

type engineError string

func (e engineError) Error() string { return string(e) }

func Reason(err error) string {
	var code engineError
	if errors.As(err, &code) {
		return string(code)
	}
	if err == nil {
		return "ended"
	}
	return "failed"
}

// Discover checks the opt-in install slot without starting a process.
func Discover() Engine {
	if override := os.Getenv("GOIM_REMOTE_DESKTOP_HELPER"); override != "" {
		if filepath.IsAbs(override) && helperFile(override) {
			return localEngine(override)
		}
		return nil
	}
	if ValidationBuild != "1" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	var paths []string
	switch runtime.GOOS {
	case "windows":
		paths = []string{filepath.Join(dir, "remote", "goim-rd.exe")}
	case "darwin":
		paths = []string{filepath.Join(dir, "..", "Helpers", "GoIMRemote.app", "Contents", "MacOS", "goim-rd")}
	case "linux":
		paths = []string{
			filepath.Join(dir, "lib", "goim", "remote", "goim-rd"),
			filepath.Join(dir, "..", "lib", "goim", "remote", "goim-rd"),
		}
	}
	for _, name := range paths {
		if helperFile(name) {
			return localEngine(name)
		}
	}
	return nil
}

func helperFile(name string) bool {
	info, err := os.Stat(name)
	return err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0111 != 0)
}

type command struct {
	Type    string `json:"type"`
	Role    string `json:"role,omitempty"`
	Control bool   `json:"control"`
	Audio   bool   `json:"audio"`
	domain.RemoteDesktopInput
}

type event struct {
	Type    string   `json:"type"`
	Code    string   `json:"code"`
	Devices []string `json:"devices"`
}

func localEngine(path string) Engine {
	return func(ctx context.Context, tunnel net.Conn, grant Start, input <-chan domain.RemoteDesktopInput, events Events) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "--goim-remote")
		cmd.Dir = filepath.Dir(path)
		configureProcess(cmd)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		defer stdin.Close()
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		defer stdout.Close()
		// Closing the private input pipe lets the helper release remote input
		// before a wedged process is killed. The network must not hold it open.
		cmd.Cancel = func() error {
			_ = tunnel.SetDeadline(time.Now())
			return stdin.Close()
		}
		cmd.WaitDelay = 2 * time.Second
		if err = cmd.Start(); err != nil {
			return err
		}

		writeDone := make(chan struct{})
		go func() {
			defer close(writeDone)
			defer stdin.Close()
			if err := writeChild(ctx, stdin, tunnel, grant, input); err != nil {
				cancel()
			}
		}()
		result := readChild(stdout, tunnel, grant, events)
		cancel()
		_ = tunnel.SetDeadline(time.Now())
		<-writeDone
		_ = cmd.Wait()
		return result
	}
}

func writeChild(ctx context.Context, w io.Writer, tunnel net.Conn, grant Start, input <-chan domain.RemoteDesktopInput) error {
	if err := writeControl(w, command{Type: "start", Role: grant.Role, Control: grant.Control, Audio: grant.Audio}); err != nil {
		return err
	}
	// A single writer owns the pipe. Limit networking to one pending chunk
	// so input and media cannot accumulate in unbounded queues.
	chunks := make(chan []byte)
	finished := make(chan struct{})
	stopRead := make(chan struct{})
	defer func() {
		close(stopRead)
		_ = tunnel.SetReadDeadline(time.Now())
		<-finished
	}()
	// The reader has one reserved buffer. Hand it back before reuse.
	reuse := make(chan []byte, 1)
	reuse <- make([]byte, maxTunnel)
	go func() {
		defer close(finished)
		defer close(chunks)
		for {
			var buffer []byte
			select {
			case buffer = <-reuse:
			case <-ctx.Done():
				return
			case <-stopRead:
				return
			}
			n, err := tunnel.Read(buffer[:cap(buffer)])
			if n > 0 {
				select {
				case chunks <- buffer[:n]:
				case <-ctx.Done():
					return
				case <-stopRead:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case control, ok := <-input:
			if !ok {
				input = nil
				continue
			}
			if err := writeControl(w, command{Type: "input", RemoteDesktopInput: control}); err != nil {
				return err
			}
		case chunk, ok := <-chunks:
			if !ok {
				return io.EOF
			}
			if err := writePacket(w, tunnelPacket, chunk); err != nil {
				return err
			}
			reuse <- chunk
		}
	}
}

func writeControl(w io.Writer, value command) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writePacket(w, controlPacket, payload)
}

func writePacket(w io.Writer, kind byte, payload []byte) error {
	var header [5]byte
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeFull(w, header[:]); err != nil {
		return err
	}
	return writeFull(w, payload)
}

func writeFull(w io.Writer, p []byte) error {
	n, err := w.Write(p)
	if err == nil && n != len(p) {
		return io.ErrShortWrite
	}
	return err
}

func readChild(r io.Reader, tunnel net.Conn, grant Start, events Events) error {
	var header [5]byte
	buffer := make([]byte, maxTunnel)
	active := false
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return err
		}
		size := binary.BigEndian.Uint32(header[1:])
		switch header[0] {
		case controlPacket:
			if size == 0 || size > maxControl {
				return errors.New("invalid engine event length")
			}
			if _, err := io.ReadFull(r, buffer[:size]); err != nil {
				return err
			}
			var e event
			if err := json.Unmarshal(buffer[:size], &e); err != nil {
				return err
			}
			switch e.Type {
			case "ready":
				if !active {
					active = true
					events.Ready()
				}
			case "audio_devices":
				if !active || grant.Role != "viewer" || !grant.Audio || len(e.Devices) > 64 {
					return errors.New("unexpected audio device list")
				}
				for _, name := range e.Devices {
					if name == "" || len(name) > 256 {
						return errors.New("invalid audio device name")
					}
				}
				events.AudioDevices(e.Devices)
			case "closed":
				return nil
			case "error":
				switch e.Code {
				case "permission", "unsupported", "failed":
					return engineError(e.Code)
				default:
					return engineError("failed")
				}
			default:
				return errors.New("unexpected engine event")
			}
		case tunnelPacket:
			if size == 0 || size > maxTunnel {
				return errors.New("invalid tunnel packet length")
			}
			if _, err := io.ReadFull(r, buffer[:size]); err != nil {
				return err
			}
			_ = tunnel.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := writeFull(tunnel, buffer[:size]); err != nil {
				return err
			}
		case pixelPacket:
			if grant.Role != "viewer" || !active {
				return errors.New("unexpected viewer frame")
			}
			if err := readFrame(r, size, events.Frame); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected engine packet type %d", header[0])
		}
	}
}

func readFrame(r io.Reader, size uint32, frame func(domain.RemoteDesktopFrame)) error {
	if size < 8 || size > 8+4*maxWidth*maxHeight {
		return errors.New("invalid viewer frame length")
	}
	var dims [8]byte
	if _, err := io.ReadFull(r, dims[:]); err != nil {
		return err
	}
	w, h := binary.BigEndian.Uint32(dims[:4]), binary.BigEndian.Uint32(dims[4:])
	if w == 0 || h == 0 || w > maxWidth || h > maxHeight || size-8 != 4*w*h {
		return errors.New("invalid viewer frame dimensions")
	}
	pixels := make([]byte, size-8)
	if _, err := io.ReadFull(r, pixels); err != nil {
		return err
	}
	frame(domain.RemoteDesktopFrame{Width: int(w), Height: int(h), BGRA: pixels})
	return nil
}

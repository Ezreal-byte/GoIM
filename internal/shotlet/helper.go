// Package shotlet runs the pinned Shotlet capture helper outside GoIM's UI process.
package shotlet

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	resultMagic = "ZMSHOT1\n"
	maxPixels   = 67_108_864 // SHOTLET_CAPTURE_MAX_PIXELS in shotlet/capture.h
)

var ErrScreenRecordingPermission = errors.New("Shotlet needs macOS screen recording permission")

// HelperPath is relative to the installed GoIM executable, including inside its app bundle.
func HelperPath(goExecutable string) string {
	dir := filepath.Dir(goExecutable)
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(dir, "shotlet", "shotlet-capture.exe")
	case "darwin":
		return filepath.Join(dir, "..", "Helpers", "ShotletCapture.app", "Contents", "MacOS", "ShotletCapture")
	default:
		return ""
	}
}

// Capture returns nil without an error when the user cancels or completes an
// action that Shotlet handles itself, such as saving or pinning.
func Capture(ctx context.Context, path, language, theme string) (*image.NRGBA, error) {
	args := []string{"--language", language, "--theme", theme}
	if runtime.GOOS == "darwin" {
		if err := authorizeScreenRecording(ctx, path); err != nil {
			return nil, err
		}
		args = append(args, "--screen-recording", "1")
	}
	result, err := captureFrame(ctx, path, args...)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 2, 4: // SHOTLET_CAPTURE_CANCELLED, SHOTLET_CAPTURE_ACTION_DONE
			return nil, nil
		case 3: // SHOTLET_CAPTURE_PERMISSION_REQUIRED
			return nil, ErrScreenRecordingPermission
		}
	}
	return result, err
}

// SelfTest verifies the actual packaged executable and wire format with a
// synthetic pixel. It never starts a desktop capture or requests permission.
func SelfTest(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := captureFrame(ctx, path, "--self-test")
	if err != nil {
		return err
	}
	if result.Bounds() != image.Rect(0, 0, 1, 1) || result.NRGBAAt(0, 0) != (color.NRGBA{R: 255, A: 255}) {
		return errors.New("unexpected Shotlet self-test image")
	}
	return nil
}

func authorizeScreenRecording(ctx context.Context, path string) error {
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	granted, err := screenRecordingPermission(check, path, "--permissions")
	cancel()
	if err != nil || granted {
		return err
	}
	// Capture is initiated by the user. Run the request using the same signed
	// helper as the permission check and the real capture.
	granted, err = screenRecordingPermission(ctx, path, "--request-permission", "screen")
	if err == nil && !granted {
		return ErrScreenRecordingPermission
	}
	return err
}

func screenRecordingPermission(ctx context.Context, path string, args ...string) (bool, error) {
	output, err := helperCommand(ctx, path, args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, fmt.Errorf("Shotlet permission check: %w", err)
	}
	var status struct {
		Version         int   `json:"version"`
		ScreenRecording *bool `json:"screen_recording"`
	}
	if json.Unmarshal(output, &status) != nil || status.Version != 1 || status.ScreenRecording == nil {
		return false, errors.New("invalid Shotlet permission response")
	}
	return *status.ScreenRecording, nil
}

func helperCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, path, args...)
	hideConsole(command)
	return command
}

func captureFrame(ctx context.Context, path string, args ...string) (*image.NRGBA, error) {
	command := helperCommand(ctx, path, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	var stderr diagnostic
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	result, readErr := readFrame(stdout)
	// Closing also unblocks the producer if a malformed frame was rejected.
	_ = stdout.Close()
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if waitErr != nil {
		if detail := strings.TrimSpace(string(stderr.data[:stderr.used])); detail != "" {
			return nil, fmt.Errorf("Shotlet: %s (%w)", detail, waitErr)
		}
		return nil, fmt.Errorf("Shotlet: %w", waitErr)
	}
	return result, readErr
}

type diagnostic struct {
	data [4096]byte
	used int
}

func (d *diagnostic) Write(p []byte) (int, error) {
	d.used += copy(d.data[d.used:], p)
	return len(p), nil
}

func readFrame(reader io.Reader) (*image.NRGBA, error) {
	var header [len(resultMagic) + 8]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, fmt.Errorf("read Shotlet header: %w", err)
	}
	if string(header[:len(resultMagic)]) != resultMagic {
		return nil, errors.New("invalid Shotlet result header")
	}
	width := binary.LittleEndian.Uint32(header[len(resultMagic):])
	height := binary.LittleEndian.Uint32(header[len(resultMagic)+4:])
	if width == 0 || height == 0 || uint64(width)*uint64(height) > maxPixels {
		return nil, fmt.Errorf("invalid Shotlet image dimensions %dx%d", width, height)
	}
	result := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	if _, err := io.ReadFull(reader, result.Pix); err != nil {
		return nil, fmt.Errorf("read Shotlet pixels: %w", err)
	}
	var extra [1]byte
	if _, err := io.ReadFull(reader, extra[:]); err != io.EOF {
		return nil, errors.New("unexpected data after Shotlet image")
	}
	return result, nil
}

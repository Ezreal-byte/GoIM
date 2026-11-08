package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goim/internal/capture"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		want     options
		wantErr  string
		wantHelp bool
	}{
		{
			name: "whole desktop by default",
			args: []string{"-output", "snip.png"},
			want: options{output: "snip.png", display: -1},
		},
		{
			name: "explicit whole desktop",
			args: []string{"-output", "snip.png", "-all"},
			want: options{output: "snip.png", display: -1, all: true},
		},
		{
			name: "one display",
			args: []string{"-output", "snip.png", "-display", "2"},
			want: options{output: "snip.png", display: 2},
		},
		{
			name: "input PNG",
			args: []string{"-output", "snip.png", "-input", "source.png"},
			want: options{output: "snip.png", input: "source.png", display: -1},
		},
		{
			name:     "help",
			args:     []string{"-h"},
			wantHelp: true,
		},
		{
			name:    "output required",
			args:    []string{},
			wantErr: "-output is required",
		},
		{
			name:    "display must be an index",
			args:    []string{"-output", "snip.png", "-display", "-2"},
			wantErr: "-display must be zero or greater",
		},
		{
			name:    "display and all conflict",
			args:    []string{"-output", "snip.png", "-display", "0", "-all"},
			wantErr: "-all cannot be combined with -display",
		},
		{
			name:    "input and all conflict",
			args:    []string{"-output", "snip.png", "-input", "source.png", "-all"},
			wantErr: "-input cannot be combined with -display or -all",
		},
		{
			name:    "input and display conflict",
			args:    []string{"-output", "snip.png", "-input", "source.png", "-display", "1"},
			wantErr: "-input cannot be combined with -display or -all",
		},
		{
			name:    "unexpected positional argument",
			args:    []string{"-output", "snip.png", "extra"},
			wantErr: `unexpected argument "extra"`,
		},
		{
			name:    "unknown flag",
			args:    []string{"-output", "snip.png", "-unknown"},
			wantErr: "flag provided but not defined",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := parseOptions(test.args, &output)
			if test.wantHelp {
				if !errors.Is(err, flag.ErrHelp) {
					t.Fatalf("parseOptions error = %v, want flag.ErrHelp", err)
				}
				if !strings.Contains(output.String(), "usage:") {
					t.Fatalf("help output = %q, want usage", output.String())
				}
				return
			}
			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("parseOptions unexpectedly succeeded with %+v", got)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("parseOptions error = %q, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOptions: %v", err)
			}
			if got != test.want {
				t.Fatalf("parseOptions = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseOptionsPrintsUsageWhenOutputIsMissing(t *testing.T) {
	var output bytes.Buffer
	if _, err := parseOptions(nil, &output); err == nil {
		t.Fatal("missing output unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "usage: goim-snip") {
		t.Fatalf("output = %q, want command usage", output.String())
	}
}

func TestDecodeInputPreservesPixelsAndUsesLocalBounds(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 4, 3))
	colors := []color.RGBA{
		{R: 255, G: 10, B: 20, A: 255},
		{R: 12, G: 240, B: 30, A: 255},
		{R: 40, G: 50, B: 230, A: 255},
		{R: 2, G: 4, B: 6, A: 255},
	}
	for index, value := range colors {
		source.SetRGBA(index, index%3, value)
	}
	path := filepath.Join(t.TempDir(), "input.png")
	writePNG(t, path, source)

	decoded, bounds, err := decodeInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if bounds != image.Rect(0, 0, 4, 3) || decoded.Bounds() != bounds {
		t.Fatalf("bounds = %v, decoded bounds = %v, want local 4x3", bounds, decoded.Bounds())
	}
	for index, want := range colors {
		if got := decoded.RGBAAt(index, index%3); got != want {
			t.Errorf("pixel %d = %+v, want %+v", index, got, want)
		}
	}
}

func TestDecodeInputRejectsInvalidAndOversizedPNGs(t *testing.T) {
	directory := t.TempDir()
	invalid := filepath.Join(directory, "invalid.png")
	if err := os.WriteFile(invalid, []byte("not a PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeInput(invalid); err == nil || !strings.Contains(err.Error(), "read input PNG") {
		t.Fatalf("invalid input error = %v, want PNG read failure", err)
	}

	oversized := filepath.Join(directory, "oversized.png")
	if err := os.WriteFile(oversized, pngHeader(10_001, 10_000), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeInput(oversized); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized input error = %v, want pixel-limit failure", err)
	}
}

func TestLoadImportedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	writePNG(t, path, image.NewRGBA(image.Rect(0, 0, 7, 5)))
	source, err := loadSource(options{input: path})
	if err != nil {
		t.Fatal(err)
	}
	if source.image.Bounds().Size() != image.Pt(7, 5) || source.bounds.Size() != image.Pt(7, 5) {
		t.Fatalf("source bounds = %v, %v", source.image.Bounds(), source.bounds)
	}
	if !source.info.imported || source.info.title != "Imported PNG" || source.info.detail != filepath.Base(path) {
		t.Fatalf("source info = %+v", source.info)
	}
}

func TestInitialWindowSize(t *testing.T) {
	tests := []struct {
		name     string
		displays []capture.Display
		width    int
		height   int
	}{
		{name: "no display metadata", width: 1180, height: 780},
		{
			name:     "small display clamps to the minimum",
			displays: []capture.Display{{Work: image.Rect(0, 0, 700, 500), Primary: true}},
			width:    820,
			height:   600,
		},
		{
			name:     "large display clamps to the maximum",
			displays: []capture.Display{{Work: image.Rect(0, 0, 1920, 1040), Primary: true}},
			width:    1240,
			height:   860,
		},
		{
			name: "high DPI uses logical work area",
			displays: []capture.Display{{
				Work: image.Rect(0, 0, 2560, 1400), Primary: true, DPI: 192,
			}},
			width:  1216,
			height: 636,
		},
		{
			name: "primary display wins over enumeration order",
			displays: []capture.Display{
				{Work: image.Rect(0, 0, 700, 500)},
				{Work: image.Rect(0, 0, 1366, 768), Primary: true},
			},
			width:  1240,
			height: 704,
		},
		{
			name:     "unknown work area uses defaults",
			displays: []capture.Display{{Bounds: image.Rect(0, 0, 1280, 720), Primary: true}},
			width:    1180,
			height:   780,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := initialWindowSize(test.displays)
			if width != test.width || height != test.height {
				t.Fatalf("initialWindowSize = %dx%d, want %dx%d", width, height, test.width, test.height)
			}
		})
	}
}

func TestDisplayCount(t *testing.T) {
	for count, want := range map[int]string{
		0: "0 displays",
		1: "1 display",
		2: "2 displays",
	} {
		if got := displayCount(count); got != want {
			t.Errorf("displayCount(%d) = %q, want %q", count, got, want)
		}
	}
}

func writePNG(t *testing.T, path string, picture image.Image) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, picture); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func pngHeader(width, height uint32) []byte {
	var output bytes.Buffer
	output.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8] = 8  // eight bits per channel
	data[9] = 6  // truecolor with alpha
	data[10] = 0 // deflate
	data[11] = 0 // adaptive filtering
	data[12] = 0 // no interlace
	chunk := []byte("IHDR")
	_ = binary.Write(&output, binary.BigEndian, uint32(len(data)))
	output.Write(chunk)
	output.Write(data)
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write(chunk)
	_, _ = checksum.Write(data)
	_ = binary.Write(&output, binary.BigEndian, checksum.Sum32())
	return output.Bytes()
}

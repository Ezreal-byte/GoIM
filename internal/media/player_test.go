package media

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBundledDecoder(t *testing.T) {
	dir := os.Getenv("GOIM_MEDIA_BUNDLE")
	if dir == "" {
		t.Skip("packaged decoder is exercised in release CI")
	}
	name := "goim-media"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	helper := filepath.Join(dir, name)
	file, err := filepath.Abs("testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	video := command(ctx, helper, file, 0, "-map", "0:v:0", "-an", "-vf", "fps=24,scale=w='min(960,iw)':h='min(600,ih)':force_original_aspect_ratio=decrease", "-c:v", "ppm", "-f", "image2pipe", "pipe:1")
	output, err := video.Output()
	if err != nil {
		t.Fatal(err)
	}
	var rgb []byte
	reader := bufio.NewReader(bytes.NewReader(output))
	var first []byte
	different := false
	count := 0
	for {
		if _, err := reader.Peek(1); err != nil {
			break
		}
		frame, err := readFrame(reader, &rgb)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Width != 96 || frame.Height != 64 {
			t.Fatal(frame.Width, frame.Height)
		}
		if count == 0 {
			first = frame.Pixels
		} else if !bytes.Equal(first, frame.Pixels) {
			different = true
		}
		count++
	}
	if !different || count < 20 {
		t.Fatalf("decoded %d frames, movement %v", count, different)
	}
	pcm, err := command(ctx, helper, file, 300*time.Millisecond, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1").Output()
	if err != nil || len(pcm) < 10000 || bytes.Equal(pcm, make([]byte, len(pcm))) {
		t.Fatalf("seek and decode audio: %d bytes, %v", len(pcm), err)
	}
}

// Package media decodes local chat video in a cancellable helper process.
// Only frames for the active viewer are retained. No decoder runs at startup.
package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"goim/internal/voice"
)

const fps = 24

type Frame struct {
	Pixels        []byte
	Width, Height int
	Number        uint32
}
type Snapshot struct {
	Frame    Frame
	Position time.Duration
	Ended    bool
	Err      error
}
type Player struct {
	mu     sync.Mutex
	view   Snapshot
	cancel context.CancelFunc
	wake   func()
}

func HelperPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "goim-media"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}
func Start(helper, path, speaker string, volume int, offset time.Duration, wake func()) *Player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Player{cancel: cancel, wake: wake, view: Snapshot{Position: offset}}
	go p.run(ctx, helper, path, speaker, volume, offset)
	return p
}
func (p *Player) Stop() {
	if p != nil {
		p.cancel()
	}
}
func (p *Player) Snapshot() Snapshot { p.mu.Lock(); defer p.mu.Unlock(); return p.view }
func (p *Player) notify() {
	if p.wake != nil {
		p.wake()
	}
}

func command(ctx context.Context, helper, path string, offset time.Duration, output ...string) *exec.Cmd {
	// Disable network and external playlist formats even in a development build
	// using a full FFmpeg executable. Only the selected local container is input.
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-max_alloc", "268435456", "-threads", "2", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpeg,mpegts,flv,asf,ogg"}
	if offset > 0 {
		args = append(args, "-ss", strconv.FormatFloat(offset.Seconds(), 'f', 3, 64))
	}
	args = append(args, "-i", path, "-filter_threads", "1", "-threads", "1")
	cmd := exec.CommandContext(ctx, helper, append(args, output...)...)
	hideWindow(cmd)
	return cmd
}
func (p *Player) run(ctx context.Context, helper, path, speaker string, volume int, offset time.Duration) {
	local, cancel := context.WithCancel(ctx)
	defer cancel()
	result := p.decode(local, helper, path, speaker, volume, offset)
	if ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	p.view.Ended = true
	p.view.Err = result
	p.mu.Unlock()
	p.notify()
}
func (p *Player) decode(ctx context.Context, helper, path, speaker string, volume int, offset time.Duration) error {
	if !filepath.IsAbs(path) {
		return errors.New("视频尚未保存到本机")
	}
	if _, err := os.Stat(helper); err != nil {
		return errors.New("找不到视频解码器，请重新安装完整软件包")
	}
	cmd := command(ctx, helper, path, offset, "-map", "0:v:0", "-an", "-vf", "fps=24,scale=w='min(960,iw)':h='min(600,ih)':force_original_aspect_ratio=decrease", "-c:v", "ppm", "-f", "image2pipe", "pipe:1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = pipe.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReaderSize(pipe, 64<<10)
	rgb := make([]byte, 0)
	var start, lastPaint time.Time
	for i := uint32(0); ; i++ {
		frame, err := readFrame(reader, &rgb)
		if err == io.EOF && i > 0 {
			if cmd.Wait() != nil {
				return errors.New("无法解码视频，文件可能不完整或编码不受支持")
			}
			return nil
		}
		if err != nil {
			return errors.New("无法解码视频，文件可能不完整或编码不受支持")
		}
		if i == 0 {
			start = time.Now()
			go playAudio(ctx, helper, path, speaker, volume, offset)
		}
		elapsed := time.Duration(i) * time.Second / fps
		wait := time.Until(start.Add(elapsed))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		} else if wait < -time.Second/fps && time.Since(lastPaint) < 200*time.Millisecond {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastPaint = time.Now()
		frame.Number = i + 1
		p.mu.Lock()
		p.view.Frame = frame
		p.view.Position = offset + elapsed
		p.mu.Unlock()
		p.notify()
	}
}
func readFrame(r *bufio.Reader, rgb *[]byte) (Frame, error) {
	var frame Frame
	line, err := r.ReadSlice('\n')
	if err != nil {
		return frame, err
	}
	if string(line) != "P6\n" {
		return frame, errors.New("invalid frame header")
	}
	line, err = r.ReadSlice('\n')
	if err != nil {
		return frame, err
	}
	parts := strings.Fields(string(line))
	if len(parts) != 2 {
		return frame, errors.New("invalid frame dimensions")
	}
	width, we := strconv.Atoi(parts[0])
	height, he := strconv.Atoi(parts[1])
	if we != nil || he != nil || width < 1 || width > 960 || height < 1 || height > 600 {
		return frame, errors.New("invalid frame dimensions")
	}
	line, err = r.ReadSlice('\n')
	if err != nil || string(line) != "255\n" {
		return frame, errors.New("invalid frame depth")
	}
	n := width * height
	if cap(*rgb) < n*3 {
		*rgb = make([]byte, n*3)
	} else {
		*rgb = (*rgb)[:n*3]
	}
	if _, err = io.ReadFull(r, *rgb); err != nil {
		return frame, err
	}
	pixels := make([]byte, n*4)
	for i := range n {
		p, q := i*4, i*3
		pixels[p], pixels[p+1], pixels[p+2], pixels[p+3] = (*rgb)[q+2], (*rgb)[q+1], (*rgb)[q], 255
	}
	return Frame{Width: width, Height: height, Pixels: pixels}, nil
}
func playAudio(ctx context.Context, helper, path, speaker string, volume int, offset time.Duration) {
	cmd := command(ctx, helper, path, offset, "-map", "0:a:0", "-vn", "-ac", "1", "-ar", fmt.Sprint(voice.SampleRate), "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if cmd.Start() != nil {
		return
	}
	defer func() { pipe.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var pcm [voice.PCMFrameBytes]byte
	n, err := io.ReadFull(pipe, pcm[:])
	if n == 0 || ctx.Err() != nil {
		return
	}
	playback, openErr := voice.OpenPlayback(speaker)
	if openErr != nil {
		return
	}
	var once sync.Once
	closeDevice := func() { once.Do(func() { _ = playback.Close() }) }
	stop := context.AfterFunc(ctx, closeDevice)
	defer func() { stop(); closeDevice() }()
	for n > 0 && ctx.Err() == nil {
		data := pcm[:n]
		voice.ScalePCM16LE(data, volume)
		for len(data) > 0 {
			written, e := playback.Write(data)
			if e != nil || written < 1 || written > len(data) {
				return
			}
			data = data[written:]
		}
		if err != nil {
			return
		}
		n, err = io.ReadFull(pipe, pcm[:])
	}
}

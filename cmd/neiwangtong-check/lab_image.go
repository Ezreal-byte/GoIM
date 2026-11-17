package main

import (
	"bytes"
	"context"
	"crypto/md5" // native chat image wire digest
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkImages(ctx context.Context, r *report, service *neiwangtong.Service, peer string) bool {
	passed := true
	for i, format := range []string{"png", "jpeg", "gif"} {
		in := r.run("inline_"+format+"_from_reference", func() (string, error) {
			return l.receiveImage(ctx, service, peer, format)
		})
		out := r.run("inline_"+format+"_to_reference", func() (string, error) {
			return l.sendImage(ctx, service, peer, format, uint64(i))
		})
		passed = passed && in && out
	}
	return passed
}

func (l *labChecks) receiveImage(ctx context.Context, service *neiwangtong.Service, peer, format string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := "聊天图片-" + l.runID + "." + imageFixtureExtension(format)
	expected, err := writeFixtureImage(filepath.Join(dir, name), format, 27, 17, color.RGBA{R: 133, A: 255})
	if err != nil {
		return "", err
	}
	if err := l.drive(ctx, "send-image", "C:\\goim-qa\\"+name); err != nil {
		return "", err
	}
	got, err := wait(ctx, l.opt.timeout, l.imageRefs, func(got labImage) bool { return got.peer == peer })
	if err != nil {
		return "", err
	}
	data, err := service.FetchChatImage(ctx, peer, got.ref)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(data, expected) {
		cfg, gotFormat, err := image.DecodeConfig(bytes.NewReader(data))
		return "", fmt.Errorf("original changed the %s fixture to %s %dx%d (%d bytes): %v", format, gotFormat, cfg.Width, cfg.Height, len(data), err)
	}
	return fmt.Sprintf("%s image, %d bytes, sha256=%x", format, len(data), sha256.Sum256(data)), nil
}

func (l *labChecks) sendImage(ctx context.Context, service *neiwangtong.Service, peer, format string, index uint64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
	defer cancel()
	path := filepath.Join(l.opt.labDir, "发送图片-"+l.runID+"."+imageFixtureExtension(format))
	data, err := writeFixtureImage(path, format, 68, 34, color.RGBA{G: 203, A: 255})
	if err != nil {
		return "", err
	}
	task, selector := 1236+index, 601234+index
	id := fmt.Sprintf("%s%016x", l.runID, index)
	if err := service.SendChatImage(ctx, peer, 7791+index, id, task, selector,
		int64(len(data)), md5.Sum(data), path); err != nil {
		return "", err
	}
	_, err = wait(ctx, l.opt.timeout, l.progress, func(p labProgress) bool {
		return p.peer == peer && p.task == task && p.complete && p.done == int64(len(data))
	})
	if err != nil {
		return "", err
	}
	cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache")
	if err := waitForCachedImage(ctx, cache, data); err != nil {
		return "", fmt.Errorf("reference requested all %d %s bytes but did not save a matching image: %w", len(data), format, err)
	}
	return fmt.Sprintf("reference cached %s, %d bytes, sha256=%x", format, len(data), sha256.Sum256(data)), nil
}

func imageFixtureExtension(format string) string {
	if format == "jpeg" {
		return "jpg"
	}
	return format
}

func writeFixtureImage(path, format string, width, height int, pixel color.RGBA) ([]byte, error) {
	rect := image.Rect(0, 0, width, height)
	img := image.NewRGBA(rect)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 || y >= height/2 {
				img.SetRGBA(x, y, pixel)
			} else {
				img.SetRGBA(x, y, color.RGBA{B: 180, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		palette := color.Palette{pixel, color.RGBA{B: 173, A: 255}}
		first, second := image.NewPaletted(rect, palette), image.NewPaletted(rect, palette)
		for i := range second.Pix {
			second.Pix[i] = 1
		}
		err = gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 16}})
	default:
		return nil, errors.New("unknown image fixture format")
	}
	if err != nil {
		return nil, err
	}
	data := buf.Bytes()
	return data, os.WriteFile(path, data, 0600)
}

func waitForCachedImage(ctx context.Context, root string, expected []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		found := false
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || !entry.Type().IsRegular() {
				return nil
			}
			if stat, err := entry.Info(); err != nil || stat.Size() != int64(len(expected)) {
				return nil
			}
			if data, err := os.ReadFile(path); err == nil && bytes.Equal(data, expected) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

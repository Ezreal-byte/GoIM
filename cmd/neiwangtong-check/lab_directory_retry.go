package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/neiwangtong"
)

func (l *labChecks) checkOriginalDirectoryRetry(ctx context.Context, service *neiwangtong.Service, peer string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	source := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "nwtlab", "Desktop", "GoIMQA目录")
	files, err := makeDirectorySample(source)
	if err != nil {
		return "", err
	}
	if err := l.drive(ctx, "send-folder", `C:\users\nwtlab\Desktop\GoIMQA目录`); err != nil {
		return "", err
	}
	event, err := wait(ctx, 8*time.Second, l.folders, func(event labDirectory) bool {
		return event.peer == peer && event.offer.Name == "GoIMQA目录" && event.offer.Size == 2*sampleSize
	})
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(l.opt.labDir, "folder-receive-")
	if err != nil {
		return "", err
	}
	first, disconnect := context.WithTimeout(ctx, 8*time.Second)
	defer disconnect()
	var firstBytes int64
	err = service.ReceiveDirectory(first, peer, event.offer, stage, func(done int64) {
		if firstBytes == 0 {
			firstBytes = done
			disconnect()
		}
	})
	if firstBytes == 0 || err == nil {
		return "", fmt.Errorf("could not interrupt the original directory at a known offset: %v", err)
	}
	if l.opt.labFolderResend {
		originalTask := event.offer.TaskID
		if err := service.StopDirectoryReceive(peer, originalTask); err != nil {
			return "", fmt.Errorf("release the interrupted offer in the original sender: %w", err)
		}
		if err := l.drive(ctx, "send-folder", `C:\users\nwtlab\Desktop\GoIMQA目录`); err != nil {
			return "", err
		}
		event, err = wait(ctx, 8*time.Second, l.folders, func(event labDirectory) bool {
			return event.peer == peer && event.offer.Name == "GoIMQA目录" && event.offer.Size == 2*sampleSize &&
				event.offer.TaskID != originalTask
		})
		if err != nil {
			return "", fmt.Errorf("the original did not issue a fresh folder offer: %w", err)
		}
	}
	retry, err := os.MkdirTemp(l.opt.labDir, "folder-retry-")
	if err != nil {
		return "", err
	}
	if err := service.ReceiveDirectory(ctx, peer, event.offer, retry, nil); err != nil {
		return "", fmt.Errorf("original did not finish the folder after losing the data connection at %d bytes: %w", firstBytes, err)
	}
	if l.opt.labFolderResend {
		return fmt.Sprintf("the original reoffered the folder after a drop at %d bytes; every item matched SHA-256", firstBytes), verifyDirectorySample(retry, files)
	}
	return fmt.Sprintf("original reopened the same offer after interruption at %d bytes", firstBytes), verifyDirectorySample(retry, files)
}

func (l *labChecks) checkStockDirectoryResume(ctx context.Context, service *neiwangtong.Service, peer string, cfg neiwangtong.Config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	const fileName = "资料.bin"
	const size = 512 << 10
	source := filepath.Join(l.opt.labDir, "GoIMQA续传目录-"+l.runID)
	if err := os.Mkdir(source, 0700); err != nil {
		return "", err
	}
	digest, err := makeSample(filepath.Join(source, fileName), size)
	if err != nil {
		return "", err
	}
	sourceBytes, err := os.ReadFile(filepath.Join(source, fileName))
	if err != nil {
		return "", err
	}
	dir, err := neiwangtong.InspectDirectory(source)
	if err != nil {
		return "", err
	}
	task := l.runID + l.runID[:14] + "02"
	paused := &labDirectoryGate{task: task, started: make(chan int64, 1), resume: make(chan struct{})}
	l.folderGate.Store(paused)
	release := func() {
		if l.folderGate.CompareAndSwap(paused, nil) {
			close(paused.resume)
		}
	}
	defer release()
	if err := service.SendDirectory(peer, task, dir); err != nil {
		return "", err
	}
	if err := l.drive(ctx, "accept-file"); err != nil {
		return "", err
	}
	var offset int64
	select {
	case offset = <-paused.started:
	case <-ctx.Done():
		return "", errors.New("the original never started receiving the directory data")
	}
	var target string
	if err := waitSandboxFile(ctx, 5*time.Second, func() (bool, error) {
		if located := l.findStockDirectory(dir.Name); located != "" {
			target = filepath.Join(located, fileName)
			data, err := os.ReadFile(target)
			if err != nil {
				return false, ignoreAbsent(err)
			}
			return int64(len(data)) == offset && bytes.Equal(data, sourceBytes[:offset]), nil
		}
		return false, nil
	}); err != nil {
		return "", fmt.Errorf("original did not retain a %d-byte directory item: %w", offset, err)
	}
	if err := service.Close(); err != nil {
		return "", err
	}
	release()
	service, err = neiwangtong.Start(cfg)
	if err != nil {
		return "", err
	}
	defer service.Close()
	got, err := service.ProbePeer(ctx, l.opt.target, int(neiwangtong.ControlPort), int(neiwangtong.AvatarPort))
	if err != nil || got.ID != peer {
		return "", fmt.Errorf("could not reconnect to the same reference client: %v", err)
	}
	if l.opt.labFolderResend {
		task = l.runID + l.runID[:14] + "03"
	}
	firstOnRetry := &labDirectoryGate{task: task, started: make(chan int64, 1)}
	l.folderGate.Store(firstOnRetry)
	defer l.folderGate.CompareAndSwap(firstOnRetry, nil)
	if err := service.SendDirectory(peer, task, dir); err != nil {
		return "", err
	}
	match := func(result labDirectoryResult) bool { return result.peer == peer && result.task == task }
	result, err := wait(ctx, 3*time.Second, l.folderDone, match)
	if err != nil && ctx.Err() == nil {
		if err := l.drive(ctx, "accept-file"); err != nil {
			return "", err
		}
		result, err = wait(ctx, 10*time.Second, l.folderDone, match)
	}
	if err := errors.Join(err, result.err); err != nil {
		return "", fmt.Errorf("directory retry after %d retained bytes: %w", offset, err)
	}
	var savedPath string
	name := filepath.Base(filepath.Dir(target))
	if err := waitSandboxFile(ctx, 4*time.Second, func() (bool, error) {
		savedPath = l.findStockDirectoryMatching(func(path string, entry fs.DirEntry) bool {
			if !strings.Contains(entry.Name(), name) {
				return false
			}
			_, err := verifySample(filepath.Join(path, fileName), size, digest)
			return err == nil
		})
		return savedPath != "", nil
	}); err != nil {
		return "", fmt.Errorf("original acknowledged the retry without a matching complete directory: %w", err)
	}
	select {
	case sent := <-firstOnRetry.started:
		if l.opt.labFolderResend {
			if sent != offset {
				return "", fmt.Errorf("expected the fresh offer to start with a %d-byte block, got %d bytes", offset, sent)
			}
			return fmt.Sprintf("the original took a new invitation after a drop at %d bytes; it received from zero and the final SHA-256 matches", offset), nil
		}
		if sent == offset {
			return "", fmt.Errorf("original kept %d bytes but requested a full retry; the resulting complete file has matching SHA-256", offset)
		}
	default:
		return "", errors.New("no measured start offset for the completed retry")
	}
	if filepath.Join(savedPath, fileName) != target {
		return "", errors.New("original materialized the retry under a new directory")
	}
	return fmt.Sprintf("directory completed after a drop at %d bytes; final SHA-256 matches", offset), nil
}

func (l *labChecks) findStockDirectory(name string) string {
	return l.findStockDirectoryMatching(func(_ string, entry fs.DirEntry) bool { return entry.Name() == name })
}

func (l *labChecks) findStockDirectoryMatching(match func(string, fs.DirEntry) bool) string {
	cache := filepath.Join(l.opt.labDir, "wine", "drive_c")
	var located string
	_ = filepath.WalkDir(cache, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && match(path, entry) {
			located = path
			return fs.SkipAll
		}
		return nil
	})
	return located
}

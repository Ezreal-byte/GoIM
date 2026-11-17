package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"goim/internal/neiwangtong"
)

const emptyDirectoryFixture = "空目录/仍空"

type directoryFixtureFile struct {
	size   int64
	digest [32]byte
}

func makeDirectorySample(root string) (map[string]directoryFixtureFile, error) {
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(emptyDirectoryFixture)), 0700); err != nil {
		return nil, err
	}
	files := map[string]directoryFixtureFile{}
	for rel, size := range map[string]int64{"顶层.bin": sampleSize, "资料/子目录/中文文件.bin": sampleSize, "资料/空文件.txt": 0} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		digest, err := makeSample(path, size)
		if err != nil {
			return nil, err
		}
		files[rel] = directoryFixtureFile{size, digest}
	}
	return files, nil
}

func verifyDirectorySample(root string, files map[string]directoryFixtureFile) error {
	seen := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, ok := files[filepath.ToSlash(rel)]
		if !ok || !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected directory item %s", rel)
		}
		seen++
		_, err = verifySample(path, file.size, file.digest)
		return err
	})
	if err == nil && seen != len(files) {
		err = errors.New("directory sample is missing files")
	}
	if err == nil {
		entries, readErr := os.ReadDir(filepath.Join(root, filepath.FromSlash(emptyDirectoryFixture)))
		if readErr != nil || len(entries) != 0 {
			err = fmt.Errorf("the empty nested directory was not preserved: %v", readErr)
		}
	}
	return err
}

func (l *labChecks) checkDirectories(ctx context.Context, r *report, service *neiwangtong.Service, peer string) bool {
	in := r.run("directory_from_reference", func() (string, error) {
		ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
		defer cancel()
		source := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "nwtlab", "Desktop", "GoIMQA目录")
		files, err := makeDirectorySample(source)
		if err != nil {
			return "", err
		}
		if err := l.drive(ctx, "send-folder", "C:\\users\\nwtlab\\Desktop\\GoIMQA目录"); err != nil {
			return "", err
		}
		event, err := wait(ctx, l.opt.timeout, l.folders, func(got labDirectory) bool {
			return got.peer == peer && got.offer.Name == "GoIMQA目录" && got.offer.Size == 2*sampleSize
		})
		if err != nil {
			return "", err
		}
		destination := filepath.Join(l.opt.labDir, "goim-directory")
		if err := os.Mkdir(destination, 0700); err != nil {
			return "", err
		}
		if err := service.ReceiveDirectory(ctx, peer, event.offer, destination, nil); err != nil {
			return "", err
		}
		return "preserved an empty nested directory and empty file; all SHA-256 values match", verifyDirectorySample(destination, files)
	})
	out := r.run("directory_to_reference", func() (string, error) {
		ctx, cancel := context.WithTimeout(ctx, l.opt.timeout)
		defer cancel()
		source := filepath.Join(l.opt.labDir, "GoIMQA输出-"+l.runID)
		files, err := makeDirectorySample(source)
		if err != nil {
			return "", err
		}
		dir, err := neiwangtong.InspectDirectory(source)
		if err != nil {
			return "", err
		}
		id := l.runID + l.runID
		if err := service.SendDirectory(peer, id, dir); err != nil {
			return "", err
		}
		if err := l.drive(ctx, "accept-file"); err != nil {
			return "", err
		}
		event, err := wait(ctx, l.opt.timeout, l.folderDone, func(got labDirectoryResult) bool {
			return got.peer == peer && got.task == id
		})
		if err != nil {
			return "", err
		}
		if event.err != nil {
			return "", event.err
		}
		if event.done != dir.Size {
			return "", errors.New("reference acknowledged an incomplete directory")
		}
		cache := filepath.Join(l.opt.labDir, "wine", "drive_c")
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			var located string
			_ = filepath.WalkDir(cache, func(path string, entry fs.DirEntry, err error) error {
				if err == nil && entry.IsDir() && entry.Name() == dir.Name {
					located = path
					return fs.SkipAll
				}
				return nil
			})
			if located != "" {
				return "reference preserved an empty nested directory and empty file; all SHA-256 values match", verifyDirectorySample(located, files)
			}
			select {
			case <-ctx.Done():
				return "", errors.New("reference directory is missing from its receive directory")
			case <-ticker.C:
			}
		}
	})
	return in && out
}

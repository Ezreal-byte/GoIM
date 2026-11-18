package neiwangtong

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

type directoryTarget struct {
	path     string
	manifest string
}

func (d directoryTarget) prepare(manifest []byte, items []directoryItem) error {
	if d.manifest == "" {
		return nil
	}
	versioned := true
	for _, item := range items {
		if item.size > 0 && item.modified.IsZero() {
			versioned = false
			break
		}
	}
	revision := sha256.Sum256(manifest[folderManifestHead:])
	saved, err := os.ReadFile(d.manifest)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if versioned && bytes.Equal(revision[:], saved) {
		return os.MkdirAll(d.path, 0700)
	}
	// Invalidate the saved version before replacing any part of the tree.
	if err := os.Remove(d.manifest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(d.path); err != nil {
		return err
	}
	if err := os.MkdirAll(d.path, 0700); err != nil {
		return err
	}
	if versioned {
		return os.WriteFile(d.manifest, revision[:], 0600)
	}
	return nil
}

func (d directoryTarget) offsets(root *os.Root, items []directoryItem) ([]int64, error) {
	offsets := make([]int64, len(items))
	if d.manifest == "" {
		return offsets, nil
	}
	for i, item := range items {
		if item.size == 0 {
			continue
		}
		info, err := root.Lstat(filepath.FromSlash(item.relative))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("内网通文件夹暂存文件无效")
		}
		if info.Size() <= item.size {
			// The original closes the stream when asked for an offset at EOF.
			// Fetch the final byte again for an already staged file.
			offsets[i] = min(info.Size(), item.size-1)
		}
	}
	return offsets, nil
}

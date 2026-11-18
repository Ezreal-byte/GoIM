package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"goim/internal/core"
	"goim/internal/domain"
)

func verifyFiles(ctx context.Context, node *core.Node, peer, chat, dir, token string,
	command func(context.Context, ...string) ([]byte, error), run func(string, func() (string, error)) bool) bool {
	const size = 98321
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return false
	}
	digest := sha256.Sum256(data)
	sent := run("file_to_original", func() (string, error) {
		name := "GoIM中文-" + token + ".bin"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			return "", err
		}
		node.Submit(domain.Command{Kind: domain.SendFiles, ConversationID: chat, Paths: []string{path}})
		cache := filepath.Join(dir, "wine", "drive_c", "goim-qa", "received")
		err := wait(ctx, node, func(s domain.State) bool {
			completed := false
			for _, task := range s.Transfers {
				if task.Direction == "send" && task.Name == name && task.Status == "completed" {
					completed = true
				}
			}
			if !completed {
				return false
			}
			matched := false
			_ = filepath.WalkDir(cache, func(path string, item fs.DirEntry, err error) error {
				if err == nil && item.Type().IsRegular() && item.Name() == name {
					if b, e := os.ReadFile(path); e == nil && sha256.Sum256(b) == digest {
						matched = true
						return fs.SkipAll
					}
				}
				return nil
			})
			return matched
		})
		if err != nil {
			for _, task := range node.Snapshot().Transfers {
				if task.Name == name && task.Direction == "send" {
					return "", fmt.Errorf("%w; transfer=%s, bytes=%d, detail=%s", err, task.Status, task.Done, task.Error)
				}
			}
		}
		return fmt.Sprintf("original saved %d bytes with a Chinese filename; sha256=%x", size, digest), err
	})
	received := run("file_from_original", func() (string, error) {
		return verifyFileFromOriginal(ctx, node, peer, chat, dir, "stock-file-原版-"+token+".bin", size, 0, command)
	})
	retried := run("file_retry_with_saved_prefix", func() (string, error) {
		return verifyFileFromOriginal(ctx, node, peer, chat, dir, "stock-file-重试-"+token+".bin", 8<<20, 32<<10, command)
	})
	return sent && received && retried
}

func verifyFileFromOriginal(ctx context.Context, node *core.Node, peer, chat, dir, name string, size, prefix int,
	command func(context.Context, ...string) ([]byte, error)) (string, error) {
	// Independent contents catch a stale or incorrect file selector.
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	if _, err := command(ctx, "send-file", "IPMSG QA file "+name, path); err != nil {
		return "", err
	}
	var id string
	if err := wait(ctx, node, func(s domain.State) bool {
		for _, task := range s.Transfers {
			if task.Name == name && task.Direction == "receive" && task.PeerID == peer && task.ConversationID == chat &&
				task.Status == "offered" && task.Size == int64(size) {
				id = task.ID
				return true
			}
		}
		return false
	}); err != nil {
		return "", err
	}
	if prefix > 0 {
		partials := filepath.Join(dir, "goim", "partials")
		if err := os.MkdirAll(partials, 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(partials, id+".part"), data[:prefix], 0600); err != nil {
			return "", err
		}
	}
	dest := filepath.Join(dir, "downloads", name)
	node.Submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: id, Paths: []string{dest}})
	if err := wait(ctx, node, func(s domain.State) bool {
		for _, task := range s.Transfers {
			if task.ID == id && task.Status == "completed" && task.Done == int64(size) {
				return true
			}
		}
		return false
	}); err != nil {
		for _, task := range node.Snapshot().Transfers {
			if task.ID == id {
				return "", fmt.Errorf("%w; transfer=%s, bytes=%d, detail=%s", err, task.Status, task.Done, task.Error)
			}
		}
		return "", err
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		return "", err
	}
	if sha256.Sum256(b) != digest {
		return "", errors.New("original file content did not match the fixture")
	}
	if prefix > 0 {
		return fmt.Sprintf("GoIM recovered the original's %d-byte file with a saved %d-byte prefix; sha256=%x", size, prefix, digest), nil
	}
	return fmt.Sprintf("GoIM saved the original's %d-byte file with a Chinese filename; sha256=%x", size, digest), nil
}

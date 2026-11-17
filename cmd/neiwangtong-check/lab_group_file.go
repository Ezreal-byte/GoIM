package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"goim/internal/core"
	"goim/internal/domain"
)

func (l *labChecks) checkGroupFiles(ctx context.Context, r *report, node *core.Node, peer, chat, room string) bool {
	sent := r.run("group_file_to_original", func() (string, error) {
		name := "GoIM-group-" + l.runID + ".bin"
		source := filepath.Join(l.opt.labDir, name)
		digest, err := makeSample(source, sampleSize)
		if err != nil {
			return "", err
		}
		node.Submit(domain.Command{Kind: domain.SendFiles, ConversationID: chat, Paths: []string{source}})
		if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
			return slices.ContainsFunc(s.Transfers, func(t domain.Transfer) bool {
				return t.Name == name && t.ConversationID == chat && t.PeerID == peer && t.Status == "offered"
			})
		}); err != nil {
			return "", err
		}
		if err := l.driveFor(ctx, "group-file-receive", room); err != nil {
			return "", err
		}
		cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv")
		if err := checkGroupFileCache(ctx, cache, name, digest); err != nil {
			return "", err
		}
		return fmt.Sprintf("original group client saved %d bytes; sha256=%x", sampleSize, digest), nil
	})
	received := r.run("group_file_from_original", func() (string, error) {
		dir := filepath.Join(l.opt.labDir, "wine", "drive_c", "goim-qa")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		name := "原版群文件-" + l.runID + ".bin"
		digest, err := makeSample(filepath.Join(dir, name), sampleSize)
		if err != nil {
			return "", err
		}
		if err := l.driveFor(ctx, "group-file-send", room, `C:\goim-qa\`+name); err != nil {
			return "", err
		}
		var transfer domain.Transfer
		if _, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
			for _, t := range s.Transfers {
				if t.Name == name && t.ConversationID == chat && t.PeerID == peer && t.Direction == "receive" &&
					t.Status == "offered" && t.RemoteShareID != "" && t.Size == sampleSize {
					transfer = t
					return true
				}
			}
			return false
		}); err != nil {
			return "", err
		}
		dest := filepath.Join(l.opt.labDir, "received-"+name)
		node.Submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID, Paths: []string{dest}})
		if _, err := waitCoreState(ctx, node, 8*time.Second, func(s domain.State) bool {
			return slices.ContainsFunc(s.Transfers, func(t domain.Transfer) bool {
				return t.ID == transfer.ID && t.Status == "completed" && t.Path == dest && t.Done == sampleSize
			})
		}); err != nil {
			return "", err
		}
		data, err := os.ReadFile(dest)
		if err != nil || sha256.Sum256(data) != digest {
			return "", errors.Join(errors.New("the received group file did not match the fixture"), err)
		}
		return fmt.Sprintf("GoIM saved %d bytes in the original group; sha256=%x", len(data), digest), nil
	})
	return sent && received
}

func checkGroupFileCache(ctx context.Context, dir, name string, digest [32]byte) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		matched := false
		_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && e.Type().IsRegular() && e.Name() == name {
				if data, err := os.ReadFile(path); err == nil && len(data) == sampleSize {
					if sha256.Sum256(data) == digest {
						matched = true
						return fs.SkipAll
					}
				}
			}
			return nil
		})
		if matched {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return errors.New("the original did not write a matching group file")
		}
	}
}

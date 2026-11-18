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

func (l *labChecks) checkGroupFiles(ctx context.Context, r *report, current **core.Node, peer, chat, room string) bool {
	node := *current
	sent := r.run("group_file_to_original_after_goim_restart", func() (string, error) {
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
		if err := node.Close(); err != nil {
			return "", err
		}
		data := filepath.Join(l.opt.labDir, "goim-recall")
		next, err := core.New(core.Options{DataDir: data, Name: l.qaName,
			DownloadDir: filepath.Join(data, "downloads"), Listen: ":0", DisableIPMSG: true})
		if err != nil {
			return "", err
		}
		node, *current = next, next
		if _, err := waitCoreState(ctx, node, 12*time.Second, func(s domain.State) bool {
			return s.Peer(peer).Online && s.Conversation(chat).GroupID != ""
		}); err != nil {
			return "", err
		}
		if err := l.driveFor(ctx, "group-file-receive", room); err != nil {
			return "", err
		}
		cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv")
		if err := checkGroupFileCache(ctx, cache, name, sampleSize, digest); err != nil {
			return "", err
		}
		return fmt.Sprintf("original accepted the existing invitation after GoIM restarted; %d bytes, sha256=%x", sampleSize, digest), nil
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

func checkGroupFileCache(ctx context.Context, dir, name string, size int64, digest [32]byte) error {
	return waitGroupCacheFile(ctx, dir, func(path, filename string) bool {
		if filename != name {
			return false
		}
		_, err := verifySample(path, size, digest)
		return err == nil
	})
}

func waitGroupCacheFile(ctx context.Context, dir string, match func(path, filename string) bool) error {
	return waitSandboxFile(ctx, 8*time.Second, func() (bool, error) {
		matched := false
		_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err == nil && e.Type().IsRegular() && match(path, e.Name()) {
				matched = true
				return fs.SkipAll
			}
			return nil
		})
		return matched, nil
	})
}

func (l *labChecks) checkGroupFolder(ctx context.Context, node *core.Node, peer, chat, room string) (string, error) {
	name := "GoIMQA目录"
	root := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "nwtlab", "Desktop", name)
	for _, child := range []string{"多层/子目录", "空目录"} {
		if err := os.MkdirAll(filepath.Join(root, child), 0700); err != nil {
			return "", err
		}
	}
	digest, err := makeSample(filepath.Join(root, "多层", "子目录", "sample.bin"), sampleSize)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0600); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "send-folder", room, `C:\users\nwtlab\Desktop\GoIMQA目录`); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-reply", room, ""); err != nil {
		return "", err
	}
	var transfer domain.Transfer
	if _, err := waitCoreState(ctx, node, 6*time.Second, func(s domain.State) bool {
		for _, t := range s.Transfers {
			if t.Name == name && t.ConversationID == chat && t.PeerID == peer && t.Direction == "receive" &&
				t.Status == "offered" && t.RemoteShareID != "" && s.File(t.FileID).Folder {
				transfer = t
				return true
			}
		}
		return false
	}); err != nil {
		return "", err
	}
	dest := filepath.Join(l.opt.labDir, "received-group-folder")
	node.Submit(domain.Command{Kind: domain.AcceptTransfer, TransferID: transfer.ID, Paths: []string{dest}})
	if _, err := waitCoreState(ctx, node, 10*time.Second, func(s domain.State) bool {
		return slices.ContainsFunc(s.Transfers, func(t domain.Transfer) bool {
			return t.ID == transfer.ID && t.Status == "completed" && t.Path == dest && t.Done == sampleSize && t.Size == sampleSize
		})
	}); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dest, "多层", "子目录", "sample.bin"))
	if err != nil || sha256.Sum256(data) != digest {
		return "", errors.Join(errors.New("the received group folder file did not match"), err)
	}
	info, err := os.Stat(filepath.Join(dest, "空目录"))
	if err != nil || !info.IsDir() {
		return "", errors.Join(errors.New("the received group folder lost an empty child"), err)
	}
	data, err = os.ReadFile(filepath.Join(dest, "empty.txt"))
	if err != nil || len(data) != 0 {
		return "", errors.Join(errors.New("the received group folder lost a zero-byte file"), err)
	}
	return fmt.Sprintf("received the original group folder, nested Chinese paths and empty entries; sha256=%x", digest), nil
}

func (l *labChecks) checkGroupFolderToOriginal(ctx context.Context, node *core.Node, peer, chat, room string) (string, error) {
	name := "GoIM群目录-" + l.runID
	root := filepath.Join(l.opt.labDir, name)
	if err := os.MkdirAll(filepath.Join(root, "多层", "子目录"), 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "空目录"), 0700); err != nil {
		return "", err
	}
	file := "group-folder-" + l.runID + ".bin"
	digest, err := makeSample(filepath.Join(root, "多层", "子目录", file), sampleSize)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0600); err != nil {
		return "", err
	}
	node.Submit(domain.Command{Kind: domain.SendFiles, ConversationID: chat, Paths: []string{root}})
	if _, err := waitCoreState(ctx, node, 5*time.Second, func(s domain.State) bool {
		return slices.ContainsFunc(s.Transfers, func(t domain.Transfer) bool {
			return t.Name == name && t.ConversationID == chat && t.PeerID == peer && t.Direction == "send" &&
				t.Status == "offered" && t.RemoteShareID != "" && s.File(t.FileID).Folder
		})
	}); err != nil {
		return "", err
	}
	if err := l.driveFor(ctx, "group-file-receive", room); err != nil {
		return "", err
	}
	cache := filepath.Join(l.opt.labDir, "wine", "drive_c", "users", "Public", "Nwt", "cache", "recv")
	if err := checkGroupFileCache(ctx, cache, file, sampleSize, digest); err != nil {
		return "", err
	}
	var output string
	_ = filepath.WalkDir(cache, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == file {
			output = filepath.Dir(filepath.Dir(filepath.Dir(path)))
			return fs.SkipAll
		}
		return nil
	})
	for _, child := range []string{"empty.txt", "空目录"} {
		info, err := os.Stat(filepath.Join(output, child))
		if err != nil || child == "空目录" && !info.IsDir() || child == "empty.txt" && info.Size() != 0 {
			return "", errors.Join(errors.New("the original group folder lost an empty entry"), err)
		}
	}
	return fmt.Sprintf("the original group saved Chinese paths, empty directory and zero-byte file; sha256=%x", digest), nil
}

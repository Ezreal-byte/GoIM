package core

import (
	"errors"
	"os"
	"path/filepath"

	"goim/internal/domain"
)

// Cancelled offers and invalid prefixes cannot be resumed. Ordinary I/O errors,
// offline peers and application shutdown keep their prefix for the next attempt.
func discardReceivePartial(t domain.Transfer) bool {
	if t.Direction != "receive" || t.WebSender != "" || t.ShareProject != "" || t.ShareRequest {
		return false
	}
	if t.Status == "completed" || t.Revoked || t.Status == "rejected" || t.Status == "cancelled" {
		return true
	}
	if t.Status != "failed" && t.Status != "waiting" {
		return false
	}
	switch t.Error {
	case errInvalidPrefix.Error(), errSourceChanged.Error(), errInvalidInlineImage.Error(), errInvalidLegacyImage.Error(), errStagedIntegrity.Error(), errNativeDirectoryResend.Error():
		return true
	}
	return t.PrintJobID != "" && t.Status == "failed"
}

var (
	errStagedIntegrity    = errors.New("接收文件在发布前未通过完整性校验")
	errInvalidPrefix      = errors.New("完整性校验失败，请重试")
	errSourceChanged      = errors.New("源文件已变化或传输权限失效")
	errInvalidInlineImage = errors.New("发送内容不是受支持的聊天图片")
	errInvalidLegacyImage = errors.New("对方发送的内容不是受支持的聊天图片")
)

func (p *persisted) queuePartialCleanup(t domain.Transfer) {
	if !discardReceivePartial(t) || !validFileName(t.ID) || !filepath.IsLocal(t.ID) {
		return
	}
	if p.PartialCleanup == nil {
		p.PartialCleanup = make(map[string]bool)
	}
	p.PartialCleanup[t.ID] = true
}

// Caller holds n.mu. A committed cancellation may race the worker closing its
// file, especially on Windows. The journal survives exit until cleanup succeeds.
func (n *Node) discardIdleReceivePartialsLocked() bool {
	changed := false
	for id, scratch := range n.data.ReceiveScratch {
		if _, publishing := n.data.ReceivePublications[id]; publishing || n.transfers[id] != nil {
			continue
		}
		if err := scratch.remove(id); err == nil {
			delete(n.data.ReceiveScratch, id)
			changed = true
		}
	}
	if len(n.data.PartialCleanup) == 0 {
		return changed
	}
	for i := range n.data.State.Transfers {
		t := &n.data.State.Transfers[i]
		if !n.data.PartialCleanup[t.ID] || n.transfers[t.ID] != nil {
			continue
		}
		if _, publishing := n.data.ReceivePublications[t.ID]; publishing {
			continue
		}
		if err := n.removeReceivePartial(*t); err != nil {
			continue
		}
		if t.Status != "completed" {
			t.Done, t.Speed = 0, 0
		}
		t.FileStage, t.FolderStage = "", ""
		delete(n.data.PartialCleanup, t.ID)
		changed = true
	}
	return changed
}

func (n *Node) removeReceivePartial(t domain.Transfer) error {
	if !validFileName(t.ID) || !filepath.IsLocal(t.ID) {
		return errors.New("无效的传输任务")
	}
	dir := "partials"
	if t.LegacyTransport == transportNWT {
		dir = "nwt-partials"
	}
	part := filepath.Join(dir, t.ID+".part")
	stage := nativeGroupFolderStage(t)
	fileStage := nativeFileStage(t)
	for _, path := range []string{filepath.Join(n.dir, part), fileStage, stage} {
		if err := checkReceiveDestination(path, t.Path, stage != "" && path == stage); err != nil {
			return err
		}
	}
	root, err := os.OpenRoot(n.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if stage != "" {
		if err := os.RemoveAll(stage); err != nil {
			return err
		}
	}
	if fileStage != "" {
		if err := os.Remove(fileStage); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func checkReceiveDestination(stage, destination string, directory bool) error {
	if stage == "" || destination == "" {
		return nil
	}
	info, err := os.Stat(stage)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	final, err := os.Stat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(info, final) {
		return errors.New("临时文件与接收目标相同，无法清理")
	}
	if directory {
		// A custom final path may be a symlink into the staging directory.
		finalPath, err := filepath.EvalSymlinks(destination)
		if err != nil {
			return err
		}
		stagePath, err := filepath.EvalSymlinks(stage)
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(stagePath, finalPath); err == nil && filepath.IsLocal(rel) {
			return errors.New("接收目标位于暂存目录内，无法清理")
		}
	}
	return nil
}

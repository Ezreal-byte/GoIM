package core

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"goim/internal/domain"
)

func (n *Node) clearTransferPartial(id string) error {
	if !validFileName(id) || !filepath.IsLocal(id) {
		return errors.New("无效的传输任务")
	}
	var problem error
	err := n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, id)
		if i < 0 || !p.State.Transfers[i].CanClearPartial() {
			problem = errors.New("仅可清理已中断接收的临时文件")
			return nil
		}
		t := &p.State.Transfers[i]
		if _, publishing := p.ReceivePublications[id]; publishing || n.transfers[id] != nil {
			problem = errors.New("传输仍在处理中，请稍后重试")
			return nil
		}
		if problem = n.removeReceivePartial(*t); problem != nil {
			return nil
		}
		t.Done, t.Speed, t.FolderStage, t.UpdatedAt = 0, 0, "", time.Now()
		p.State.Notice, p.State.Error = "已清理传输临时文件", ""
		return nil
	})
	if err != nil {
		return err
	}
	return problem
}

func (n *Node) removeReceivePartial(t domain.Transfer) error {
	dir := "partials"
	if t.LegacyTransport == transportNWT {
		dir = "nwt-partials"
	}
	part := filepath.Join(dir, t.ID+".part")
	stage := nativeGroupFolderStage(t)
	// A custom download destination can alias a private path through a link.
	destination, err := os.Stat(t.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if destination != nil {
		for _, path := range []string{filepath.Join(n.dir, part), stage} {
			if path == "" {
				continue
			}
			info, err := os.Stat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if info != nil && os.SameFile(info, destination) {
				return errors.New("临时文件与接收目标相同，无法清理")
			}
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
		return os.RemoveAll(stage)
	}
	return nil
}

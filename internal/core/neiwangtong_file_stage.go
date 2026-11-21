package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
)

func nativeReceiveWriteError(err error) error {
	if receiveDiskFull(err) {
		return fmt.Errorf("接收目录空间不足，请另存到其他磁盘或释放空间后重试：%w", err)
	}
	return err
}

func nativeFileStage(t domain.Transfer) string {
	if t.Direction != "receive" || t.LegacyTransport != transportNWT || t.FileStage == "" ||
		t.Path == "" || t.FileStage == t.Path || filepath.Dir(t.FileStage) != filepath.Dir(t.Path) ||
		!strings.HasPrefix(filepath.Base(t.FileStage), ".goim-receive-"+t.ID+"-") {
		return ""
	}
	return t.FileStage
}

// Relocate a saved prefix only after its replacement is durable. A hard link
// avoids a second allocation on the same volume; changing volumes copies only
// the bytes already received, without touching the original on failure.
func stageNativePrefix(ctx context.Context, source, stage string, info os.FileInfo) error {
	if info != nil && os.Link(source, stage) == nil {
		return nil
	}
	out, err := os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if info != nil {
		var in *os.File
		in, err = os.Open(source)
		if err == nil {
			var written int64
			written, err = io.Copy(out, receiveReader{ctx, in})
			_ = in.Close()
			if err == nil && written != info.Size() {
				err = errors.New("续传文件大小已变化")
			}
		}
	}
	if err == nil {
		err = out.Sync()
	}
	if err = errors.Join(err, out.Close()); err != nil {
		_ = os.Remove(stage)
	}
	return err
}

func (n *Node) prepareNativeFileStage(ctx context.Context, t domain.Transfer, destination string) (string, int64, error) {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", 0, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return "", 0, errors.New("同名文件已存在，请选择另存为")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	}
	source := n.nativePartialPath(t.ID) // pre-upgrade transfers used the app data volume
	if t.FileStage != "" {
		source = nativeFileStage(t)
		if source == "" {
			return "", 0, errors.New("续传文件位置无效")
		}
	}
	info, err := os.Lstat(source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	}
	var offset int64
	if info != nil {
		if !info.Mode().IsRegular() {
			return "", 0, errors.New("续传位置不是普通文件")
		}
		offset = info.Size()
	}
	stage := source
	if info == nil || t.FileStage == "" || filepath.Dir(source) != parent {
		stage = filepath.Join(parent, ".goim-receive-"+t.ID+"-"+newID())
		if err := stageNativePrefix(ctx, source, stage, info); err != nil {
			return "", 0, err
		}
	}
	accepted := false
	err = n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, t.ID)
		if i >= 0 && ctx.Err() == nil && canPublishReceive(p, p.State.Transfers[i]) {
			task := &p.State.Transfers[i]
			task.Path, task.FileStage, task.Status, task.Error = destination, stage, "transferring", ""
			task.Done, task.Speed, task.UpdatedAt = offset, 0, time.Now()
			accepted = true
		}
		return nil
	})
	if err != nil || !accepted {
		if stage != source {
			_ = os.Remove(stage)
		}
		if err == nil {
			err = context.Canceled
		}
		return "", 0, err
	}
	if info != nil && source != stage {
		_ = os.Remove(source)
	}
	return stage, offset, nil
}

func (n *Node) publishNativeFileStage(ctx context.Context, t domain.Transfer, stage, destination string) error {
	info, err := os.Lstat(stage)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != t.Size {
		return errors.New("接收文件大小与发送方声明不符")
	}
	digest, err := fileHashContext(ctx, stage)
	if err != nil {
		return err
	}
	return n.publishReceive(ctx, t.ID, receivePublication{
		Stage: stage, Destination: destination, Digest: digest, Size: t.Size, ResumableFile: true,
	})
}

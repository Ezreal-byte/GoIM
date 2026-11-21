package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"goim/internal/filemove"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
)

// receivePublication records an immutable, same-volume stage. An atomic move
// consumes the stage, so recovery can distinguish publication from a conflict.
type receivePublication struct {
	Stage, Destination, Digest string
	Size                       int64
	Directory                  bool
	ResumableFile              bool   `json:",omitempty"`
	Name, MIME                 string `json:",omitempty"`
}

type receiveReader struct {
	ctx context.Context
	in  io.Reader
}

func (r receiveReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.in.Read(buffer)
}

// receiveScratch records work that must start from zero after an interruption.
type receiveScratch struct {
	Stage, Destination string
	Directory          bool
}

func (p *persisted) rememberReceiveScratch(id string, scratch receiveScratch) {
	if p.ReceiveScratch == nil {
		p.ReceiveScratch = make(map[string]receiveScratch)
	}
	p.ReceiveScratch[id] = scratch
}

func (s receiveScratch) remove(id string) error {
	prefix := ".goim-receive-"
	if s.Directory {
		prefix = directoryStagePrefix
	}
	suffix, ok := strings.CutPrefix(filepath.Base(s.Stage), prefix+id+"-")
	if !validFileName(id) || !filepath.IsLocal(id) || !filepath.IsAbs(s.Stage) || !ok || suffix == "" || filepath.Dir(s.Stage) != filepath.Dir(s.Destination) {
		return errors.New("接收暂存位置无效")
	}
	if err := checkReceiveDestination(s.Stage, s.Destination, s.Directory); err != nil {
		return err
	}
	if s.Directory {
		return os.RemoveAll(s.Stage)
	}
	if err := os.Remove(s.Stage); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (n *Node) stageReceiveFile(ctx context.Context, id, destination string, in io.Reader, size int64, expected string) (receivePublication, error) {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return receivePublication{}, err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return receivePublication{}, err
	}
	file, err := os.CreateTemp(parent, ".goim-receive-"+id+"-*")
	if err != nil {
		return receivePublication{}, err
	}
	stage := file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(stage)
		}
	}()
	registered := false
	if err := n.durable(func(p *persisted) []pending {
		if i := transferIndex(&p.State, id); i >= 0 && ctx.Err() == nil && canPublishReceive(p, p.State.Transfers[i]) {
			p.rememberReceiveScratch(id, receiveScratch{Stage: stage, Destination: destination})
			registered = true
		}
		return nil
	}); err != nil {
		return receivePublication{}, err
	}
	if !registered {
		return receivePublication{}, context.Canceled
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), receiveReader{ctx, in})
	if err != nil {
		return receivePublication{}, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if written != size || expected != "" && digest != expected {
		return receivePublication{}, errStagedIntegrity
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return receivePublication{}, err
	}
	keep = true
	return receivePublication{Stage: stage, Destination: destination, Digest: digest, Size: size}, nil
}

func receiveDirectoryDigest(ctx context.Context, directory string) (string, int64, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return "", 0, err
	}
	if !info.IsDir() {
		return "", 0, errors.New("文件夹暂存位置无效")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", 0, err
	}
	defer root.Close()
	hash := sha256.New()
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == "." {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			fmt.Fprintf(hash, "directory %q\n", path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("接收目录含有链接或特殊文件")
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		content := sha256.New()
		size, err := io.Copy(content, receiveReader{ctx, file})
		if err := errors.Join(err, file.Close()); err != nil {
			return err
		}
		fmt.Fprintf(hash, "file %q %d %x\n", path, size, content.Sum(nil))
		total += size
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), total, err
}

func (n *Node) receivePublication(id string) (receivePublication, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	publication, ok := n.data.ReceivePublications[id]
	return publication, ok
}

// Called under the state lock before accepting a cancellation. A successful
// move wins even if there was an error recording completion in the database.
func receiveAlreadyPublished(p *persisted, id string) bool {
	publication, ok := p.ReceivePublications[id]
	if !ok {
		return false
	}
	if _, err := os.Lstat(publication.Stage); !os.IsNotExist(err) {
		return false
	}
	_, err := os.Lstat(publication.Destination)
	return err == nil
}

func canPublishReceive(p *persisted, task domain.Transfer) bool {
	return task.Direction == "receive" && !terminalTransfer(task.Status) && !task.Revoked &&
		!p.fileMessageRecalled(task) && conversationAllows(&p.State, task.ConversationID, task.PeerID)
}

func (n *Node) beginReceive(ctx context.Context, id, dest string, offset int64) error {
	accepted := false
	err := n.durable(func(p *persisted) []pending {
		if i := transferIndex(&p.State, id); i >= 0 && ctx.Err() == nil && canPublishReceive(p, p.State.Transfers[i]) {
			task := &p.State.Transfers[i]
			task.Path, task.Status, task.Error = dest, "transferring", ""
			task.Done, task.Speed, task.UpdatedAt = offset, 0, time.Now()
			accepted = true
		}
		return nil
	})
	if err == nil && !accepted {
		return context.Canceled
	}
	return err
}

func (n *Node) publishReceivedFile(ctx context.Context, id, partial, destination string, size int64, expected string) error {
	file, err := os.Open(partial)
	if err != nil {
		return err
	}
	publication, err := n.stageReceiveFile(ctx, id, destination, file, size, expected)
	_ = file.Close()
	if err != nil {
		return err
	}
	return n.publishReceive(ctx, id, publication)
}

func (n *Node) publishReceive(ctx context.Context, id string, publication receivePublication) error {
	if !publication.Directory && !publication.ResumableFile {
		defer func() {
			if current, ok := n.receivePublication(id); !ok || current != publication {
				_ = os.Remove(publication.Stage)
			}
		}()
	}
	accepted := false
	err := n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, id)
		if i < 0 || ctx.Err() != nil || !canPublishReceive(p, p.State.Transfers[i]) {
			return nil
		}
		if _, exists := p.ReceivePublications[id]; exists {
			return nil
		}
		if p.ReceivePublications == nil {
			p.ReceivePublications = make(map[string]receivePublication)
		}
		p.ReceivePublications[id] = publication
		task := &p.State.Transfers[i]
		task.Path, task.Status = publication.Destination, "verifying"
		accepted = true
		return nil
	})
	if err != nil {
		return err
	}
	if !accepted {
		return context.Canceled
	}
	return n.finishReceivePublication(ctx, id, publication, false, nil)
}

func (n *Node) recoverReceivePublication(ctx context.Context, id string, publication receivePublication) error {
	path := publication.Stage
	_, err := os.Lstat(path)
	installed := os.IsNotExist(err)
	if installed {
		path = publication.Destination
	}
	var digest string
	var size int64
	if publication.Directory {
		digest, size, err = receiveDirectoryDigest(ctx, path)
	} else if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() {
		size = info.Size()
		digest, err = fileHashContext(ctx, path)
	} else {
		err = errors.New("接收暂存文件不可用")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil && (digest != publication.Digest || size != publication.Size) {
		err = errors.New("接收文件与持久化发布记录不匹配")
	}
	return n.finishReceivePublication(ctx, id, publication, installed && err == nil, err)
}

func (n *Node) finishReceivePublication(ctx context.Context, id string, publication receivePublication, installed bool, problem error) error {
	discard := false
	err := n.durable(func(p *persisted) []pending {
		if current, ok := p.ReceivePublications[id]; !ok || current != publication {
			return nil
		}
		i := transferIndex(&p.State, id)
		keepPrefix := problem == nil && publication.ResumableFile && i >= 0 && canPublishReceive(p, p.State.Transfers[i])
		if i < 0 || !installed && !canPublishReceive(p, p.State.Transfers[i]) {
			problem = context.Canceled
		} else if ctx.Err() != nil {
			problem = ctx.Err()
			return nil // preserve the intent across application shutdown
		} else if problem == nil && !installed {
			problem = filemove.Exclusive(publication.Stage, publication.Destination)
		}
		delete(p.ReceivePublications, id)
		if problem != nil {
			discard = !keepPrefix && (!publication.Directory || i < 0 || p.State.Transfers[i].FolderStage == "")
			if discard && i >= 0 && p.State.Transfers[i].FileStage == publication.Stage {
				p.State.Transfers[i].FileStage = ""
			}
			return nil
		}
		return recordPublishedReceive(p, i, publication)
	})
	if err == nil && discard {
		removeReceiveStage(publication, id)
	}
	if err == nil && problem == nil {
		n.startPrintIfReady(id)
	}
	return errors.Join(err, problem)
}

func recordPublishedReceive(p *persisted, index int, publication receivePublication) []pending {
	task := &p.State.Transfers[index]
	if j := fileIndex(&p.State, task.FileID); j >= 0 {
		file := &p.State.Files[j]
		file.Hash, file.Size = publication.Digest, publication.Size
		if publication.MIME != "" {
			file.Name, file.MIME, task.Name = publication.Name, publication.MIME, publication.Name
		}
	}
	task.Size = publication.Size
	task.ShareStage = ""
	task.FileStage = ""
	return p.completeReceiveState(task, publication.Destination)
}

func removeReceiveStage(publication receivePublication, id string) {
	parent := filepath.Dir(publication.Destination)
	if publication.Directory {
		if filepath.Dir(publication.Stage) == parent && strings.HasPrefix(filepath.Base(publication.Stage), directoryStagePrefix+id+"-") {
			_ = os.RemoveAll(publication.Stage)
		}
	} else if filepath.Dir(publication.Stage) == parent && strings.HasPrefix(filepath.Base(publication.Stage), ".goim-receive-") {
		_ = os.Remove(publication.Stage)
	}
}

func (p *persisted) completeReceiveState(t *domain.Transfer, path string) []pending {
	t.Status, t.Path, t.Error = "completed", path, ""
	t.Done, t.Speed, t.UpdatedAt = t.Size, 0, time.Now()
	p.queuePartialCleanup(*t)
	if t.ShareProject != "" || t.RemoteImageMD5 != "" {
		return nil
	}
	return []pending{{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: "completed"}}}
}

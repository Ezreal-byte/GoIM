package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
	"goim/internal/neiwangtong"
)

var errNativeDirectoryResend = errors.New("文件夹传输已中断，请让对方重新发送")

const directoryStagePrefix = ".goim-directory-"

func nativeGroupFolderStage(task domain.Transfer) string {
	stage := task.FolderStage
	if task.LegacyTransport != transportNWT || task.Direction != "receive" || task.RemoteShareID == "" ||
		task.Path == "" || stage == "" || filepath.Dir(stage) != filepath.Dir(task.Path) {
		return ""
	}
	suffix, ok := strings.CutPrefix(filepath.Base(stage), directoryStagePrefix+task.ID+"-")
	if !ok || suffix == "" {
		return ""
	}
	return stage
}

func (n *Node) removeFinishedGroupFolderStage(id string) {
	var stage string
	n.mu.Lock()
	if i := transferIndex(&n.data.State, id); i >= 0 && n.transfers[id] == nil {
		task := n.data.State.Transfers[i]
		if terminalTransfer(task.Status) {
			stage = nativeGroupFolderStage(task)
		}
	}
	n.mu.Unlock()
	if stage == "" || os.RemoveAll(stage) != nil {
		return
	}
	_ = n.change(func(s *domain.State) {
		if i := transferIndex(s, id); i >= 0 && s.Transfers[i].FolderStage == stage && terminalTransfer(s.Transfers[i].Status) {
			s.Transfers[i].FolderStage = ""
		}
	})
}

func (n *Node) sendNeiwangtongDirectory(conversationID, path, target string) error {
	s := n.snapshot(false)
	c := s.Conversation(conversationID)
	nativeGroup := c.Kind == "group" && n.isNativeGroup(c.GroupID)
	if !nativeGroup && (c.Kind != "direct" || s.Peer(c.PeerID).Transport != transportNWT || target != "" && target != c.PeerID) {
		return errors.New("文件夹直传目前仅支持内网通会话")
	}
	targets, err := recipients(s, c)
	if err != nil {
		return err
	}
	if target != "" {
		if !contains(targets, target) {
			return errors.New("该联系人不在此内网通群中")
		}
		targets = []string{target}
	}
	for _, id := range targets {
		if s.Peer(id).Transport != transportNWT {
			return errors.New("一个群只能选择同一种客户端的联系人")
		}
	}
	dir, err := neiwangtong.InspectDirectory(path)
	if err != nil {
		return err
	}
	now := time.Now()
	f := domain.File{ID: newID(), Name: dir.Name, Size: dir.Size, Hash: dir.Stamp, Folder: true,
		SourcePath: dir.Path, SenderID: s.Self.ID, ConversationID: conversationID, CreatedAt: now}
	return n.durable(func(p *persisted) []pending {
		s := &p.State
		s.Files = append(s.Files, f)
		addMessageState(s, domain.Message{ID: f.ID, FileID: f.ID, Kind: "file", Text: attachmentPreview(f),
			ConversationID: conversationID, SenderID: f.SenderID, CreatedAt: now, Status: "sent"})
		p.MessageTargets[f.ID] = targets
		var offers []pending
		for _, peer := range targets {
			offers = append(offers, appendFileOffer(s, peer, conversationID, f, ""))
		}
		return offers
	})
}

func (n *Node) receiveNeiwangtongDirectory(peerID string, offer neiwangtong.FolderOffer) {
	if !validFileName(offer.Name) || offer.Size < 0 {
		return
	}
	taskID := strings.ToLower(offer.TaskID)
	_ = n.change(func(s *domain.State) {
		peer := n.compatibilitySenderLocked(transportNWT, peerID)
		if peer.ID == "" {
			return
		}
		id := legacyStableID("nwt-directory", peerID, taskID)
		if fileIndex(s, id) >= 0 {
			return
		}
		conversationID := ensureDirect(s, peer)
		now := time.Now()
		f := domain.File{ID: id, Name: offer.Name, Size: offer.Size, Folder: true, Hash: strings.Repeat("0", 64),
			SenderID: peer.ID, ConversationID: conversationID, CreatedAt: now}
		s.Files = append(s.Files, f)
		s.Transfers = append(s.Transfers, domain.Transfer{ID: id, FileID: id, ConversationID: conversationID,
			PeerID: peer.ID, Direction: "receive", Status: "offered", Name: offer.Name, Size: offer.Size,
			UpdatedAt: now, LegacyTransport: transportNWT, RemotePeerID: peerID, RemotePacket: taskID,
			FolderMaxPath: offer.MaxItemPath, FolderMaxItem: offer.MaxItemSize})
		addMessageState(s, domain.Message{ID: id, FileID: id, Kind: "file", Text: attachmentPreview(f),
			ConversationID: conversationID, SenderID: peer.ID, CreatedAt: now, Status: "delivered"})
	})
}

func (n *Node) neiwangtongDirectoryPending(s *neiwangtong.Service, peerID string, value pending, t domain.Transfer, f domain.File) error {
	dir, err := neiwangtong.InspectDirectory(f.SourcePath)
	if err != nil {
		return err
	}
	if dir.Stamp != f.Hash || dir.Size != f.Size || dir.Name != f.Name {
		return errors.New("源文件夹已变化，请重新发送")
	}
	task := t.RemotePacket
	if task == "" {
		task = t.ID
	}
	ready := false
	share := ""
	room := ""
	if group, ok := strings.CutPrefix(t.ConversationID, "group:"); ok {
		var err error
		room, err = n.activeNativeGroup(group, t.PeerID)
		if err != nil {
			return err
		}
		share = legacyStableID("nwt-group-folder", f.ID)
	}
	if err := n.change(func(st *domain.State) {
		if i := transferIndex(st, t.ID); i >= 0 {
			taskState := &st.Transfers[i]
			if taskState.Revoked || terminalTransfer(taskState.Status) || taskState.RemotePacket != t.RemotePacket {
				return
			}
			taskState.RemotePacket, taskState.RemotePeerID = task, peerID
			taskState.RemoteShareID = share
			taskState.Status = "offered"
			taskState.LegacyTransport = transportNWT
			ready = true
		}
	}); err != nil {
		return err
	}
	if !ready {
		return nil
	}
	if room != "" {
		return s.SendGroupFolder(peerID, room, nativeMessagePacket(value), f.ID, share, task, dir)
	}
	return s.SendDirectory(peerID, task, dir)
}

// Stock 3.4 does not resume this transfer at a nonzero offset. Keep the
// user's transfer and give the recipient a fresh invitation to start over.
func (n *Node) retryNeiwangtongDirectory(t domain.Transfer) error {
	return n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, t.ID)
		if i < 0 {
			return nil
		}
		current := &p.State.Transfers[i]
		if current.RemotePacket != t.RemotePacket || current.Revoked ||
			(current.Status != "failed" && current.Status != "waiting") || p.fileMessageRecalled(*current) {
			return nil
		}
		for id, value := range p.Outbox {
			if value.Envelope.TransferID == current.ID && value.Envelope.Type == "file_offer" {
				delete(p.Outbox, id)
			}
		}
		current.Done, current.Speed = 0, 0
		if current.RemoteShareID == "" {
			current.RemotePacket = newID()
		}
		return []pending{appendFileOffer(&p.State, current.PeerID, current.ConversationID, p.State.File(current.FileID), current.ID)}
	})
}

func (n *Node) downloadNeiwangtongDirectory(ctx context.Context, service *neiwangtong.Service, peerID string,
	t domain.Transfer, f domain.File, destination string) error {
	if t.Revoked {
		return errNativeDirectoryResend
	}
	groupFolder := t.RemoteShareID != ""
	if groupFolder {
		groupID, ok := strings.CutPrefix(t.ConversationID, "group:")
		if !ok {
			return errors.New("内网通群文件夹引用无效")
		}
		if _, err := n.activeNativeGroup(groupID, t.PeerID); err != nil {
			return err
		}
	}
	if destination == "" {
		destination = t.Path
	}
	if destination == "" {
		settings := n.snapshot(false).Settings
		destination = filepath.Join(settings.DownloadDir, f.Name)
		for index := 1; ; index++ {
			_, err := os.Lstat(destination)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if settings.DuplicatePolicy == "ask" {
				return errors.New("同名文件夹已存在，请选择另存为")
			}
			destination = filepath.Join(settings.DownloadDir, fmt.Sprintf("%s (%d)", f.Name, index))
		}
	}
	parent := filepath.Dir(destination)
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("同名文件夹已存在，请选择另存为")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	oldStage := nativeGroupFolderStage(t)
	stage := oldStage
	keepStage := false
	info, statErr := os.Lstat(stage)
	if stage == "" || filepath.Dir(stage) != parent || statErr != nil || !info.IsDir() {
		var err error
		stage, err = os.MkdirTemp(parent, directoryStagePrefix+t.ID+"-")
		if err != nil {
			return err
		}
		defer func() {
			if !keepStage {
				_ = os.RemoveAll(stage)
			}
		}()
	}
	ready := false
	if err := n.change(func(st *domain.State) {
		if i := transferIndex(st, t.ID); i >= 0 {
			transfer := &st.Transfers[i]
			if !terminalTransfer(transfer.Status) && ctx.Err() == nil {
				transfer.Path, transfer.Status, transfer.Error = destination, "transferring", ""
				transfer.Done, transfer.Speed, transfer.UpdatedAt = 0, 0, time.Now()
				if groupFolder {
					transfer.FolderStage = stage
				}
				ready = true
			}
		}
	}); err != nil {
		return err
	}
	if !ready {
		return nil
	}
	keepStage = groupFolder
	if oldStage != "" && oldStage != stage {
		_ = os.RemoveAll(oldStage)
	}
	offer := neiwangtong.FolderOffer{TaskID: t.RemotePacket, Name: f.Name, Size: f.Size,
		MaxItemPath: t.FolderMaxPath, MaxItemSize: t.FolderMaxItem}
	var last time.Time
	progress := func(done int64) {
		if done == t.Size || time.Since(last) >= 150*time.Millisecond {
			n.setTransfer(t.ID, "transferring", done, 0, "")
			last = time.Now()
		}
	}
	content := stage
	var err error
	if groupFolder {
		content = filepath.Join(stage, "files")
		_, err = service.ResumeGroupDirectory(ctx, peerID, t.RemoteShareID, f.Name, content, filepath.Join(stage, "manifest"), func(done, total int64) {
			if done == 0 {
				_ = n.change(func(st *domain.State) {
					if i, j := transferIndex(st, t.ID), fileIndex(st, f.ID); i >= 0 && j >= 0 && !terminalTransfer(st.Transfers[i].Status) {
						st.Transfers[i].Size, st.Files[j].Size = total, total
					}
				})
			}
			progress(done)
		})
	} else {
		err = service.ReceiveDirectory(ctx, peerID, offer, stage, progress)
	}
	if err != nil {
		if groupFolder {
			return err
		}
		// Release the old invitation when the data stream fails. Stock can
		// send the folder again after this, but did not reopen the old offer.
		if ctx.Err() == nil && !errors.Is(err, neiwangtong.ErrNotConnected) && service.StopDirectoryReceive(peerID, offer.TaskID) == nil {
			if saveErr := n.change(func(st *domain.State) {
				if i := transferIndex(st, t.ID); i >= 0 && st.Transfers[i].RemotePacket == offer.TaskID {
					st.Transfers[i].Revoked = true
				}
			}); saveErr != nil {
				return saveErr
			}
			return errNativeDirectoryResend
		}
		if errors.Is(err, neiwangtong.ErrFolderConnectTimeout) {
			return errNativeDirectoryResend
		}
		return err
	}
	if err := installNewDirectory(ctx, content, destination); err != nil {
		return err
	}
	return n.completeReceive(t, destination)
}

// Publish under an exclusively created destination; an existing directory is
// never merged with or replaced. Both trees live on the same volume.
func installNewDirectory(ctx context.Context, stage, destination string) error {
	parent := filepath.Dir(destination)
	staged, err := filepath.Rel(parent, stage)
	if err != nil || !filepath.IsLocal(staged) || staged == "." {
		return errors.New("文件夹暂存位置无效")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	dest := filepath.Base(destination)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Mkdir(dest, 0700); err != nil {
		return fmt.Errorf("无法创建文件夹: %w", err)
	}
	created := []string{dest}
	err = fs.WalkDir(os.DirFS(stage), ".", func(relative string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil || relative == "." {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.Join(dest, filepath.FromSlash(relative))
		var err error
		if item.IsDir() {
			err = root.Mkdir(name, 0700)
		} else {
			source := filepath.Join(staged, filepath.FromSlash(relative))
			err = root.Link(source, name)
			if err != nil && !errors.Is(err, fs.ErrExist) {
				err = copyDirectoryFile(root, source, name)
			}
		}
		if err == nil {
			created = append(created, name)
		}
		return err
	})
	if err != nil {
		for i := len(created) - 1; i >= 0; i-- {
			_ = root.Remove(created[i])
		}
	}
	return err
}

// Some download volumes, including exFAT, cannot create hard links.
func copyDirectoryFile(root *os.Root, source, dest string) error {
	in, err := root.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if err = errors.Join(err, out.Close()); err != nil {
		_ = root.Remove(dest)
	}
	return err
}

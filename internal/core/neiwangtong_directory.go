package core

import (
	"context"
	"errors"
	"fmt"
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
	dir, err := neiwangtong.InspectDirectory(path)
	if err != nil {
		return err
	}
	now := time.Now()
	f := domain.File{ID: newID(), Name: dir.Name, Size: dir.Size, Hash: dir.Stamp, Folder: true,
		SourcePath: dir.Path, ConversationID: conversationID, CreatedAt: now}
	var denied error
	err = n.durable(func(p *persisted) []pending {
		s := &p.State
		c := s.Conversation(conversationID)
		nativeGroup := c.Kind == "group" && p.NativeGroups[c.GroupID].WireID != ""
		if !nativeGroup && (c.Kind != "direct" || s.Peer(c.PeerID).Transport != transportNWT) {
			denied = errors.New("文件夹直传目前仅支持内网通会话")
			return nil
		}
		targets, err := attachmentRecipients(*s, c, target)
		if err != nil {
			denied = err
			return nil
		}
		for _, id := range targets {
			if s.Peer(id).Transport != transportNWT {
				denied = errors.New("一个群只能选择同一种客户端的联系人")
				return nil
			}
		}
		f.SenderID = s.Self.ID
		s.Files = append(s.Files, f)
		addMessageState(s, domain.Message{ID: f.ID, FileID: f.ID, Kind: "file", Text: attachmentPreview(f),
			ConversationID: conversationID, SenderID: f.SenderID, CreatedAt: now, Status: "sent"})
		p.MessageTargets[f.ID] = targets
		var offers []pending
		for _, peer := range targets {
			offers = append(offers, appendFileOffer(s, peer, conversationID, f, "", false))
		}
		return offers
	})
	return errors.Join(err, denied)
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
			(current.Status != "failed" && current.Status != "waiting") || p.fileMessageRecalled(*current) ||
			!conversationAllows(&p.State, current.ConversationID, current.PeerID) {
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
		return []pending{appendFileOffer(&p.State, current.PeerID, current.ConversationID, p.State.File(current.FileID), current.ID, current.FromShare)}
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
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	destination = absolute
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
	if groupFolder {
		content = filepath.Join(stage, "files")
		first := true
		_, err = service.ResumeGroupDirectory(ctx, peerID, t.RemoteShareID, f.Name, content, filepath.Join(stage, "manifest"), func(done, total int64) {
			if first {
				first = false
				t.Size = total
				last = time.Now()
				_ = n.change(func(st *domain.State) {
					if i, j := transferIndex(st, t.ID), fileIndex(st, f.ID); i >= 0 && j >= 0 && !terminalTransfer(st.Transfers[i].Status) {
						st.Transfers[i].Size, st.Files[j].Size = total, total
						st.Transfers[i].Done, st.Transfers[i].Speed, st.Transfers[i].UpdatedAt = done, 0, last
					}
				})
				return
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
	digest, size, err := receiveDirectoryDigest(ctx, content)
	if err != nil {
		return err
	}
	err = n.publishReceive(ctx, t.ID, receivePublication{Stage: content, Destination: destination, Digest: digest, Size: size, Directory: true})
	if pending, ok := n.receivePublication(t.ID); ok && pending.Stage == content {
		keepStage = true
	}
	return err
}

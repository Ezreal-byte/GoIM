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

func (n *Node) sendNeiwangtongDirectory(conversationID, path, target string) error {
	s := n.snapshot(false)
	c := s.Conversation(conversationID)
	if c.Kind != "direct" || s.Peer(c.PeerID).Transport != transportNWT || target != "" && target != c.PeerID {
		return errors.New("文件夹直传目前仅支持内网通单聊")
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
		p.MessageTargets[f.ID] = []string{c.PeerID}
		return []pending{appendFileOffer(s, c.PeerID, conversationID, f, "")}
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

func (n *Node) neiwangtongDirectoryPending(s *neiwangtong.Service, peerID string, t domain.Transfer, f domain.File) error {
	dir, err := neiwangtong.InspectDirectory(f.SourcePath)
	if err != nil {
		return err
	}
	if dir.Stamp != f.Hash || dir.Size != f.Size || dir.Name != f.Name {
		return errors.New("源文件夹已变化，请重新发送")
	}
	task := t.ID
	if err := n.change(func(st *domain.State) {
		if i := transferIndex(st, t.ID); i >= 0 {
			taskState := &st.Transfers[i]
			taskState.RemotePacket, taskState.RemotePeerID = task, peerID
			taskState.LegacyTransport = transportNWT
		}
	}); err != nil {
		return err
	}
	return s.SendDirectory(peerID, task, dir)
}

func (n *Node) downloadNeiwangtongDirectory(ctx context.Context, service *neiwangtong.Service, peerID string,
	t domain.Transfer, f domain.File, destination string) error {
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
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".goim-directory-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	n.setTransfer(t.ID, "transferring", 0, 0, "")
	offer := neiwangtong.FolderOffer{TaskID: t.RemotePacket, Name: f.Name, Size: f.Size,
		MaxItemPath: t.FolderMaxPath, MaxItemSize: t.FolderMaxItem}
	last := time.Now()
	if err := service.ReceiveDirectory(ctx, peerID, offer, stage, func(done int64) {
		if done == t.Size || time.Since(last) >= 150*time.Millisecond {
			n.setTransfer(t.ID, "transferring", done, 0, "")
			last = time.Now()
		}
	}); err != nil {
		return err
	}
	if err := installNewDirectory(ctx, stage, destination); err != nil {
		return err
	}
	return n.completeReceive(t, destination)
}

// Publish under an exclusively created destination; an existing directory is
// never merged with or replaced. Both trees live on the same volume.
func installNewDirectory(ctx context.Context, stage, destination string) error {
	root, err := os.OpenRoot(filepath.Dir(destination))
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
			source := filepath.Join(filepath.Base(stage), filepath.FromSlash(relative))
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

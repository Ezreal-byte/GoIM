package core

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goim/internal/domain"
	"goim/internal/shareclient"
)

func (n *Node) queueShareDownloads(items []domain.ShareDownload) error {
	// Member URLs are derived from the authenticated connection. A command
	// supplies only catalog identities and an optional scoped password session.
	s := n.Snapshot()
	tasks := make([]domain.Transfer, 0, len(items))
	for _, item := range items {
		f := s.File(item.ProjectID)
		if f.ID == "" || !f.Shared || f.SenderID == s.Self.ID {
			return errors.New("分享已失效或对方不在线")
		}
		t := domain.Transfer{ID: newID(), FileID: f.ID, PeerID: f.SenderID, Direction: "receive", FromShare: true, Name: f.Name, Size: f.Size, UpdatedAt: time.Now()}
		if f.ShareFolder || f.ShareProtected {
			t.FileID = t.ID // independent of the shared project's own metadata
			t.ShareProject, t.ShareIndex, t.ShareFolder = f.ID, item.Index, item.Folder
			if item.Index > 0 {
				t.Size = 0
			}
			t.Name = item.Name
			if t.Name == "" {
				t.Name = f.Name
			}
			if !validFileName(t.Name) || item.Index < 0 {
				return errors.New("无效的共享项目")
			}
			if f.ShareProtected && item.Session.Token == "" {
				return errors.New("请重新打开共享并输入密码")
			}
			t.Status = "queued"
		} else {
			if !validFile(f) {
				return errors.New("分享已失效")
			}
			t.Status, t.ShareRequest = "requesting", true
		}
		tasks = append(tasks, t)
	}
	err := n.durable(func(p *persisted) []pending {
		var deliveries []pending
		for i, t := range tasks {
			if t.ShareRequest {
				// Old peers allocate their own transfer ID. Keep just one outstanding
				// request per catalog file until that peer returns its offer.
				if j := pendingShareRequest(&p.State, t.PeerID, t.FileID, ""); j >= 0 && p.State.Transfers[j].Status == "requesting" {
					continue
				}
				deliveries = append(deliveries, pending{PeerID: t.PeerID, Envelope: envelope{Type: "get_shared", File: domain.File{ID: t.FileID}, TransferID: t.ID}})
			} else if session := items[i].Session; session.Token != "" {
				if n.shareSessions == nil {
					n.shareSessions = make(map[string]domain.ShareSession)
				}
				n.shareSessions[t.ID] = session
			}
			p.State.Transfers = append(p.State.Transfers, t)
		}
		return deliveries
	})
	if err == nil {
		n.startShareWorker()
	}
	return err
}

// Versions through 0.1.18 reply with a fresh offer ID only. Prefer the
// active request in that case, while suppressing replies after cancellation.
func pendingShareRequest(s *domain.State, peer, file, request string) int {
	previous := -1
	retrying := false
	for i, t := range s.Transfers {
		if request == "" && t.FromShare && t.Direction == "receive" && t.PeerID == peer && t.FileID == file && t.Status == "retry_requested" {
			retrying = true
		}
		if !t.ShareRequest || t.PeerID != peer || t.FileID != file {
			continue
		}
		if request != "" {
			if t.ID == request {
				return i
			}
		} else if t.Status == "requesting" {
			return i
		} else {
			previous = i
		}
	}
	if retrying {
		return -1
	}
	return previous
}

// Reserve a distinct destination before starting a batch item. Native peer
// transfers and member HTTP downloads can finish in either order.
func (n *Node) shareDestination(ctx context.Context, task domain.Transfer, dir string) (string, error) {
	var dest string
	var issue error
	err := n.change(func(s *domain.State) {
		i := transferIndex(s, task.ID)
		if i < 0 || ctx.Err() != nil || terminalTransfer(s.Transfers[i].Status) {
			issue = context.Canceled
			return
		}
		taken := make(map[string]bool)
		for _, other := range s.Transfers {
			if other.ID != task.ID && other.Direction == "receive" && other.Path != "" && !terminalTransfer(other.Status) && other.Status != "failed" {
				taken[strings.ToLower(other.Path)] = true
			}
		}
		dest, issue = availableReceiveDestination(dir, task.Name, "rename", taken)
		if issue == nil {
			s.Transfers[i].Path = dest
		}
	})
	return dest, errors.Join(issue, err)
}

func (n *Node) startShareWorker() {
	n.mu.Lock()
	if n.shareWorker || n.closing {
		n.mu.Unlock()
		return
	}
	n.shareWorker = true
	n.mu.Unlock()
	n.async(func() {
		for {
			n.mu.Lock()
			var task domain.Transfer
			for _, t := range n.data.State.Transfers {
				if t.ShareProject != "" && t.Status == "queued" {
					task = t
					break
				}
			}
			if n.closing || task.ID == "" {
				n.shareWorker = false
				n.mu.Unlock()
				return
			}
			session := n.shareSessions[task.ID]
			n.mu.Unlock()
			ctx, finish, ok := n.transferContext(task.ID)
			if !ok {
				n.setTransfer(task.ID, "failed", 0, 0, "请重新下载共享项目")
				continue
			}
			err := n.downloadShare(ctx, task, session)
			if err != nil && ctx.Err() == nil {
				n.change(func(s *domain.State) {
					i := transferIndex(s, task.ID)
					if _, journaled := n.data.ReceivePublications[task.ID]; i >= 0 && !journaled && !terminalTransfer(s.Transfers[i].Status) {
						t := &s.Transfers[i]
						t.ShareStage = ""
						updateTransferProgress(t, "failed", t.Done, 0, err.Error())
					}
				})
			} else if err == nil {
				n.mu.Lock()
				delete(n.shareSessions, task.ID)
				n.mu.Unlock()
			}
			finish()
		}
	})
}

type shareProgress struct {
	n    *Node
	ctx  context.Context
	id   string
	done int64
	last time.Time
}

func (p *shareProgress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if time.Since(p.last) >= 250*time.Millisecond {
		err := p.n.change(func(s *domain.State) {
			if i := transferIndex(s, p.id); i >= 0 && p.ctx.Err() == nil && s.Transfers[i].Status == "transferring" {
				updateTransferProgress(&s.Transfers[i], "transferring", p.done, 0, "")
			}
		})
		if err != nil {
			return 0, err
		}
		p.last = time.Now()
	}
	return len(b), nil
}
func (n *Node) downloadShare(ctx context.Context, t domain.Transfer, session domain.ShareSession) error {
	s := n.Snapshot()
	f := s.File(t.ShareProject)
	if f.MemberURL == "" || f.SenderID != t.PeerID {
		return errors.New("分享已失效或对方不在线")
	}
	client, err := shareclient.New(f.MemberURL)
	if err != nil {
		return err
	}
	defer client.Close()
	client.Restore(session)
	page, err := client.List(ctx, "", "")
	if err != nil {
		return err
	}
	if page.Locked {
		return errors.New("请重新打开共享并输入密码")
	}
	plan, err := client.Plan(ctx, page, t.Name, t.ShareIndex, t.ShareFolder)
	if err != nil {
		return err
	}
	dest, err := n.shareDestination(ctx, t, s.Settings.DownloadDir)
	if err != nil {
		return err
	}
	pub := receivePublication{Destination: dest, Directory: t.ShareFolder, Size: plan.Bytes}
	parent := filepath.Dir(dest)
	var single *os.File
	if t.ShareFolder {
		pub.Stage, err = os.MkdirTemp(parent, directoryStagePrefix+t.ID+"-")
	} else {
		single, err = os.CreateTemp(parent, ".goim-receive-"+t.ID+"-")
		if err == nil {
			pub.Stage = single.Name()
		}
	}
	if err != nil {
		return err
	}
	defer func() {
		if single != nil {
			_ = single.Close()
		}
		if _, journaled := n.receivePublication(t.ID); !journaled {
			removeReceiveStage(pub, t.ID)
		}
	}()
	accepted := false
	err = n.change(func(state *domain.State) {
		i := transferIndex(state, t.ID)
		if i < 0 || state.Transfers[i].Status != "queued" || ctx.Err() != nil {
			return
		}
		row := &state.Transfers[i]
		row.ShareStage, row.Path, row.Size = pub.Stage, dest, plan.Bytes
		updateTransferProgress(row, "transferring", 0, 0, "")
		accepted = true
	})
	if err != nil {
		return err
	}
	if !accepted {
		return context.Canceled
	}
	progress := &shareProgress{n: n, ctx: ctx, id: t.ID}
	for _, part := range plan.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(pub.Stage, part.Path)
		if part.Entry.Folder {
			if part.Path != "" {
				if err := os.Mkdir(path, 0700); err != nil {
					return err
				}
			}
			continue
		}
		out := single
		if out == nil {
			out, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
		}
		_, copyErr := client.Copy(ctx, part.Entry, io.MultiWriter(out, progress))
		closeErr := out.Close()
		if single == out {
			single = nil
		}
		if err = errors.Join(copyErr, closeErr); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n.change(func(s *domain.State) {
		if i := transferIndex(s, t.ID); i >= 0 && ctx.Err() == nil && s.Transfers[i].Status == "transferring" {
			updateTransferProgress(&s.Transfers[i], "verifying", progress.done, 0, "")
		}
	})
	if t.ShareFolder {
		pub.Digest, _, err = receiveDirectoryDigest(ctx, pub.Stage)
	} else {
		pub.Digest, err = fileHashContext(ctx, pub.Stage)
	}
	if err != nil {
		return err
	}
	return n.publishReceive(ctx, t.ID, pub)
}

// After a restart the session cookie is gone. Incomplete private stages can
// be discarded. An outstanding publication intent is recovered independently.
func resetShareDownload(t *domain.Transfer) {
	if t.ShareStage != "" && filepath.Dir(t.ShareStage) == filepath.Dir(t.Path) {
		name := filepath.Base(t.ShareStage)
		if t.ShareFolder && strings.HasPrefix(name, directoryStagePrefix+t.ID+"-") {
			_ = os.RemoveAll(t.ShareStage)
		} else if !t.ShareFolder && strings.HasPrefix(name, ".goim-receive-"+t.ID+"-") {
			_ = os.Remove(t.ShareStage)
		}
	}
	t.ShareStage = ""
	if !terminalTransfer(t.Status) && t.Status != "failed" {
		updateTransferProgress(t, "failed", 0, 0, "上次下载已中断，请重试")
	}
}

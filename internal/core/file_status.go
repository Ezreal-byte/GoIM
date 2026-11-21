package core

import (
	"errors"
	"goim/internal/domain"
	"strings"
	"time"
)

func (n *Node) completeReceive(t domain.Transfer, path string) error {
	return n.durable(func(p *persisted) []pending {
		if i := transferIndex(&p.State, t.ID); i >= 0 && canPublishReceive(p, p.State.Transfers[i]) {
			return completeReceiveState(&p.State.Transfers[i], path)
		}
		return nil
	})
}
func (n *Node) receiveFileStatus(peer string, e envelope) error {
	return n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, e.TransferID)
		if i < 0 {
			if e.Status == "cancelled" || e.Status == "rejected" {
				p.TransferTombstones[peer+":"+e.TransferID] = e.Status
			}
			return nil
		}
		t := &p.State.Transfers[i]
		if t.ShareProject != "" || t.PeerID != peer || terminalTransfer(t.Status) {
			return nil
		}
		switch e.Status {
		case "completed":
			if t.Direction != "send" {
				return nil
			}
			t.Done = t.Size
		case "cancelled", "rejected":
			if receiveAlreadyPublished(p, t.ID) {
				return nil
			}
			p.TransferTombstones[peer+":"+e.TransferID] = e.Status
			if cancel := n.transfers[t.ID]; cancel != nil {
				cancel()
			}
		case "failed", "waiting":
			if t.Direction != "send" {
				return nil
			}
		default:
			return nil
		}
		t.Status = e.Status
		if len(e.Error) <= 2048 {
			t.Error = e.Error
		}
		if e.Status == "failed" || e.Status == "cancelled" || e.Status == "rejected" {
			if j := printJobForTransferIndex(&p.State, t.ID); j >= 0 && !p.State.PrintJobs[j].Terminal() {
				problem := e.Error
				if strings.TrimSpace(problem) == "" {
					problem = "文件未能送达打印机主人"
				}
				p.State.PrintJobs[j].Status = domain.PrintStatusFailed
				p.State.PrintJobs[j].Error = problem
				p.State.PrintJobs[j].UpdatedAt = time.Now()
			}
		}
		t.Speed = 0
		t.UpdatedAt = time.Now()
		return nil
	})
}
func (n *Node) retryTransfer(t domain.Transfer) error {
	if t.ShareRequest {
		if t.Status != "cancelled" && t.Status != "rejected" && t.Status != "failed" {
			return nil
		}
		return n.queueShareDownloads([]domain.ShareDownload{{ProjectID: t.FileID}})
	}
	if t.ShareProject != "" {
		err := n.change(func(s *domain.State) {
			if i := transferIndex(s, t.ID); i >= 0 && (s.Transfers[i].Status == "failed" || s.Transfers[i].Status == "cancelled" || s.Transfers[i].Status == "rejected") {
				updateTransferProgress(&s.Transfers[i], "queued", 0, 0, "")
			}
		})
		if err == nil {
			n.startShareWorker()
		}
		return err
	}
	if unboundIPMSGTransfer(t) {
		return errUnboundIPMSGPeer
	}
	n.mu.Lock()
	recalled := n.data.fileMessageRecalled(t)
	n.mu.Unlock()
	if recalled {
		return errors.New("文件消息已撤回")
	}
	if t.Status == "completed" {
		return nil
	}
	s := n.snapshot(false)
	if t.Direction == "receive" {
		if t.LegacyTransport == transportNWT && t.Revoked && s.File(t.FileID).Folder {
			return errNativeDirectoryResend
		}
		if t.Status == "cancelled" || t.Status == "rejected" {
			if t.LegacyTransport == legacyIPMSG {
				return n.retryLegacyReceive(t)
			}
			if t.LegacyTransport == transportNWT {
				return errors.New("此内网通任务已结束，请让对方重新发送")
			}
			return n.durable(func(p *persisted) []pending {
				if p.fileMessageRecalled(t) || !conversationAllows(&p.State, t.ConversationID, t.PeerID) {
					return nil
				}
				if i := transferIndex(&p.State, t.ID); i >= 0 {
					p.State.Transfers[i].Status = "retry_requested"
				}
				return []pending{{PeerID: t.PeerID, Envelope: envelope{Type: "file_retry", TransferID: t.ID, File: domain.File{ID: t.FileID}}}}
			})
		}
		n.startReceive(t.ID, t.Path)
		return nil
	}
	f := s.File(t.FileID)
	if f.SourcePath == "" {
		return errors.New("源文件不可用")
	}
	if t.LegacyTransport == transportNWT && f.Folder && !t.Revoked && t.RemotePacket != "" &&
		(t.Status == "failed" || t.Status == "waiting") {
		return n.retryNeiwangtongDirectory(t)
	}
	transferID := ""
	if t.LegacyTransport == transportNWT && !t.Revoked && !f.Folder && !f.InlineImage &&
		(t.Status == "failed" || t.Status == "waiting") && t.RemotePacket != "" && t.RemoteFile != "" {
		transferID = t.ID
	}
	return n.reofferFile(t, transferID)
}
func (n *Node) receiveFileRetry(peer string, e envelope) error {
	s := n.snapshot(false)
	i := transferIndex(&s, e.TransferID)
	if i < 0 {
		return errors.New("找不到原文件任务")
	}
	t := s.Transfers[i]
	if t.Direction != "send" || t.PeerID != peer || t.FileID != e.File.ID {
		return errors.New("无效的重新接收请求")
	}
	if t.Revoked {
		return n.denyFileRetry(peer, e.TransferID, "发送方已取消，请等待重新发送")
	}
	if t.Status == "transferring" {
		return nil
	}
	f := s.File(t.FileID)
	if t.FromShare && (!f.Shared || f.ShareProtected) {
		return n.denyFileRetry(peer, e.TransferID, "此分享已不可用")
	}
	if f.SourcePath == "" {
		return n.denyFileRetry(peer, e.TransferID, "源文件不可用")
	}
	hash, err := fileHashContext(n.ctx, f.SourcePath)
	if err != nil || hash != f.Hash {
		return n.denyFileRetry(peer, e.TransferID, "源文件已变化，请重新发送")
	}
	if c := s.Conversation(t.ConversationID); c.Kind == "group" {
		g := s.Group(c.GroupID)
		if g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return n.denyFileRetry(peer, e.TransferID, "群文件权限已失效")
		}
	}
	return n.reofferFile(t, "")
}
func (n *Node) denyFileRetry(peer, id, reason string) error {
	return n.durable(func(*persisted) []pending {
		return []pending{{PeerID: peer, Envelope: envelope{Type: "file_status", TransferID: id, Status: "cancelled", Error: reason}}}
	})
}

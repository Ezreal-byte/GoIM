package core

import (
	"errors"
	"goim/internal/domain"
	"time"
)

func (n *Node) completeReceive(t domain.Transfer, path string) error {
	return n.durable(func(p *persisted) []pending {
		if i := transferIndex(&p.State, t.ID); i >= 0 {
			v := &p.State.Transfers[i]
			if v.Status == "cancelled" || v.Status == "rejected" {
				return nil
			}
			v.Status = "completed"
			v.Done = v.Size
			v.Speed = 0
			v.Path = path
			v.Error = ""
			v.UpdatedAt = time.Now()
		}
		return []pending{{PeerID: t.PeerID, Envelope: envelope{Type: "file_status", TransferID: t.ID, Status: "completed"}}}
	})
}
func (n *Node) receiveFileStatus(peer string, e envelope) error {
	return n.durable(func(p *persisted) []pending {
		i := transferIndex(&p.State, e.TransferID)
		if i < 0 {
			return nil
		}
		t := &p.State.Transfers[i]
		if t.PeerID != peer || terminalTransfer(t.Status) {
			return nil
		}
		switch e.Status {
		case "completed":
			if t.Direction != "send" {
				return nil
			}
			t.Done = t.Size
		case "cancelled", "rejected":
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
		t.Speed = 0
		t.UpdatedAt = time.Now()
		return nil
	})
}
func (n *Node) retryTransfer(t domain.Transfer) error {
	if t.Status == "completed" {
		return nil
	}
	s := n.Snapshot()
	if t.Direction == "receive" {
		if t.Status == "cancelled" || t.Status == "rejected" {
			return n.durable(func(p *persisted) []pending {
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
	n.offerFile(t.PeerID, t.ConversationID, f, "")
	return nil
}
func (n *Node) receiveFileRetry(peer string, e envelope) error {
	s := n.Snapshot()
	i := transferIndex(&s, e.TransferID)
	if i < 0 {
		return errors.New("找不到原文件任务")
	}
	t := s.Transfers[i]
	if t.Direction != "send" || t.PeerID != peer || t.FileID != e.File.ID {
		return errors.New("无效的重新接收请求")
	}
	if t.Status == "transferring" {
		return nil
	}
	f := s.File(t.FileID)
	if c := s.Conversation(t.ConversationID); c.Kind == "group" {
		g := s.Group(c.GroupID)
		if g.Dissolved || !contains(g.Members, peer) || !contains(g.Members, s.Self.ID) {
			return errors.New("群文件权限已失效")
		}
	}
	n.offerFile(peer, t.ConversationID, f, "")
	return nil
}

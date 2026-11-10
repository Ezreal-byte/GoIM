package core

import (
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"goim/internal/domain"
)

const recallPreview = "消息已撤回"

func messageRecallKey(sender, conversation, id string) string {
	return sender + ":" + conversation + ":" + id
}

func messageIndex(s *domain.State, conversation, id string) int {
	for i, m := range s.Messages {
		if m.ID == id && m.ConversationID == conversation {
			return i
		}
	}
	return -1
}

func refreshMessagePreview(s *domain.State, conversation string) {
	i := convIndex(s, conversation)
	if i < 0 {
		return
	}
	c := &s.Conversations[i]
	c.Preview = ""
	c.UpdatedAt = time.Time{}
	incoming := 0
	for _, m := range s.Messages {
		if m.ConversationID != conversation || m.Deleted {
			continue
		}
		if m.SenderID != s.Self.ID && m.Kind != "system" {
			incoming++
		}
		if m.CreatedAt.Before(c.UpdatedAt) {
			continue
		}
		c.UpdatedAt = m.CreatedAt
		switch {
		case m.Recalled:
			c.Preview = recallPreview
		case m.Kind == "image" || m.Kind == "file":
			c.Preview = attachmentPreview(s.File(m.FileID))
		default:
			c.Preview = m.Text
		}
	}
	c.Unread = min(c.Unread, incoming)
}

func (n *Node) deleteMessage(conversation, id string) error {
	return n.change(func(s *domain.State) {
		if i := messageIndex(s, conversation, id); i >= 0 {
			m := &s.Messages[i]
			if m.Deleted {
				return
			}
			c := convIndex(s, conversation)
			if c >= 0 && m.SenderID != s.Self.ID && m.Kind != "system" {
				newer := 0
				for j := i + 1; j < len(s.Messages); j++ {
					if other := s.Messages[j]; other.ConversationID == conversation && !other.Deleted && other.Kind != "system" && other.SenderID != s.Self.ID {
						newer++
					}
				}
				if newer < s.Conversations[c].Unread {
					s.Conversations[c].Unread--
				}
			}
			m.Deleted, m.Text = true, ""
			refreshMessagePreview(s, conversation)
		}
	})
}

func (n *Node) rememberEmoji(value string) error {
	if value == "" || len(value) > 40 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return nil
	}
	return n.change(func(s *domain.State) {
		recent := []string{value}
		for _, old := range s.Settings.RecentEmoji {
			if len(recent) >= 10 {
				break
			}
			if old != value {
				recent = append(recent, old)
			}
		}
		s.Settings.RecentEmoji = recent
	})
}

func (n *Node) recallMessage(conversation, id string) error {
	var rejected error
	var cancelIDs []string
	err := n.durable(func(p *persisted) []pending {
		s := &p.State
		i := messageIndex(s, conversation, id)
		if i < 0 || s.Messages[i].Deleted || s.Messages[i].SenderID != s.Self.ID || s.Messages[i].Kind == "system" {
			rejected = errors.New("只能撤回自己发送的消息")
			return nil
		}
		m := s.Messages[i]
		if m.Recalled {
			return nil
		}
		targets := p.MessageTargets[id]
		if len(targets) == 0 {
			for _, t := range s.Transfers {
				if m.FileID != "" && t.FileID == m.FileID && t.Direction == "send" && !slices.Contains(targets, t.PeerID) {
					targets = append(targets, t.PeerID)
				}
			}
		}
		if len(targets) == 0 {
			targets, rejected = recipients(*s, s.Conversation(conversation))
			if rejected != nil {
				return nil
			}
		}
		for _, peer := range targets {
			if peer == "" || !s.Peer(peer).RecallSupported || s.Peer(peer).Transport == legacyIPMSG {
				rejected = errors.New("对方客户端不支持撤回，请更新 GoIM 后重试")
				return nil
			}
		}
		cancelIDs = applyMessageRecall(p, m)
		var deliveries []pending
		for _, peer := range targets {
			deliveries = append(deliveries, pending{PeerID: peer, Envelope: envelope{Type: "message_recall", Message: domain.Message{
				ID: id, SenderID: m.SenderID, ConversationID: conversation,
			}}})
		}
		return deliveries
	})
	if err != nil {
		return err
	}
	n.cancelRecalledTransfers(cancelIDs)
	return rejected
}

func (n *Node) receiveMessageRecall(peer string, e envelope) error {
	m := e.Message
	id, err := hex.DecodeString(m.ID)
	if e.ID == "" || err != nil || len(id) != 16 || m.ID != strings.ToLower(m.ID) || m.SenderID != peer {
		return errors.New("无效消息撤回")
	}
	var cancelIDs []string
	err = n.durable(func(p *persisted) []pending {
		s := &p.State
		if i := messageIndex(s, m.ConversationID, m.ID); i >= 0 {
			stored := s.Messages[i]
			if stored.SenderID == peer && stored.Kind != "system" && !stored.Recalled {
				cancelIDs = applyMessageRecall(p, stored)
			}
			return nil
		}
		// The recall may overtake a pending message on reconnect. Authenticate
		// the conversation before retaining the marker that suppresses a replay.
		if m.ConversationID != directID(s.Self.ID, peer) {
			group := s.Group(strings.TrimPrefix(m.ConversationID, "group:"))
			if !strings.HasPrefix(m.ConversationID, "group:") || group.ID == "" || !contains(group.Members, peer) || !contains(group.Members, s.Self.ID) {
				return nil
			}
		}
		p.MessageRecalls[messageRecallKey(peer, m.ConversationID, m.ID)] = true
		return nil
	})
	if err == nil {
		n.cancelRecalledTransfers(cancelIDs)
	}
	return err
}

func applyMessageRecall(p *persisted, m domain.Message) []string {
	s := &p.State
	p.MessageRecalls[messageRecallKey(m.SenderID, m.ConversationID, m.ID)] = true
	if i := messageIndex(s, m.ConversationID, m.ID); i >= 0 {
		s.Messages[i].Recalled, s.Messages[i].Text = true, ""
	}
	for key, work := range p.Outbox {
		e := work.Envelope
		if e.Type == "message" && e.Message.ID == m.ID && e.Message.SenderID == m.SenderID && e.Message.ConversationID == m.ConversationID ||
			m.FileID != "" && (e.Type == "file_offer" || e.Type == "file_retry") && e.File.ID == m.FileID {
			delete(p.Outbox, key)
		}
	}
	var cancelled []string
	if m.FileID != "" {
		if i := fileIndex(s, m.FileID); i >= 0 && s.Files[i].ConversationID == m.ConversationID && !s.Files[i].Shared {
			s.Files = slices.Delete(s.Files, i, i+1)
		}
		for i := range s.Transfers {
			t := &s.Transfers[i]
			if t.FileID != m.FileID || t.ConversationID != m.ConversationID {
				continue
			}
			t.Revoked = true
			if t.Status == "completed" {
				continue
			}
			t.Status, t.Error, t.Speed = "cancelled", "文件消息已撤回", 0
			t.UpdatedAt = time.Now()
			p.TransferTombstones[t.PeerID+":"+t.ID] = "cancelled"
			cancelled = append(cancelled, t.ID)
		}
	}
	refreshMessagePreview(s, m.ConversationID)
	return cancelled
}

func (p *persisted) fileMessageRecalled(t domain.Transfer) bool {
	sender := t.PeerID
	if t.Direction == "send" {
		sender = p.State.Self.ID
	}
	return p.MessageRecalls[messageRecallKey(sender, t.ConversationID, t.FileID)]
}

func (n *Node) cancelRecalledTransfers(ids []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, id := range ids {
		if cancel := n.transfers[id]; cancel != nil {
			cancel()
		}
	}
}

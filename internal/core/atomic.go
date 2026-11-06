package core

import (
	"context"
	"encoding/json"

	"goim/internal/domain"
)

// durable commits a visible state change and its outbound work together. Network
// writes happen only after the transaction succeeds, so recovery always has an
// outbox for every operation already shown to the user.
func (n *Node) durable(mutator func(*persisted) []pending) error {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return context.Canceled
	}
	backup, _ := json.Marshal(n.data)
	deliveries := mutator(&n.data)
	if next := n.data.State.Settings.PendingNetwork; next != "" {
		active := false
		for _, t := range n.data.State.Transfers {
			if t.Status == "transferring" || t.Status == "verifying" {
				active = true
				break
			}
		}
		if !active {
			n.data.State.Settings.Network = next
			n.data.State.Settings.PendingNetwork = ""
			n.data.State.Notice = "已切换局域网接口"
		}
	}
	for i := range deliveries {
		n.data.NextSequence++
		deliveries[i].Sequence = n.data.NextSequence
		e := &deliveries[i].Envelope
		if e.ID == "" {
			e.ID = newID()
		}
		e.Version = protocolVersion
		n.data.Outbox[e.ID] = deliveries[i]
	}
	n.data.State.Revision++
	err := saveStore(n.db, &n.data)
	if err != nil {
		var restored persisted
		_ = json.Unmarshal(backup, &restored)
		n.data = restored
		n.data.State.Error = "保存失败: " + err.Error()
	}
	n.mu.Unlock()
	n.signal()
	if err != nil {
		return err
	}
	var flushed = make(map[string]bool)
	for _, d := range deliveries {
		if !flushed[d.PeerID] {
			flushed[d.PeerID] = true
			n.flushPeer(d.PeerID)
		}
	}
	return nil
}

func addMessageState(s *domain.State, m domain.Message) {
	s.Messages = append(s.Messages, m)
	if i := convIndex(s, m.ConversationID); i >= 0 {
		s.Conversations[i].Preview = m.Text
		s.Conversations[i].UpdatedAt = m.CreatedAt
		if m.SenderID == s.Self.ID {
			s.Conversations[i].Draft = ""
		} else {
			s.Conversations[i].Unread++
		}
	}
}

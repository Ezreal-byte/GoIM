package core

import (
	"time"

	"goim/internal/domain"
)

func (n *Node) recordRemoteDesktopHistoryLocked(session *remoteSession) {
	state := &n.data.State
	id := "remote-desktop:" + session.view.ID
	for _, message := range state.Messages {
		if message.ID == id {
			return
		}
	}

	now := time.Now()
	record := domain.RemoteDesktopRecord{
		Incoming: session.view.Incoming, Reason: session.view.Reason,
		Connected: !session.connectedAt.IsZero(),
	}
	if record.Connected {
		record.DurationSeconds = max(int(now.Sub(session.connectedAt).Seconds()), 0)
	}
	text := "远程桌面 · " + record.Outcome()
	if record.Connected {
		text += " · " + record.Duration()
	}
	conversationID := ensureDirect(state, state.Peer(session.view.PeerID))
	index := convIndex(state, conversationID)
	draft := state.Conversations[index].Draft
	addMessageState(state, domain.Message{
		ID: id, ConversationID: conversationID, SenderID: state.Self.ID,
		Kind: "system", Text: text, CreatedAt: now, Status: "delivered", RemoteDesktop: &record,
	})
	state.Conversations[index].Draft = draft
	state.Revision++
	if err := saveStore(n.db, &n.data); err != nil {
		state.Error = "保存远程桌面记录失败：" + err.Error()
	}
}

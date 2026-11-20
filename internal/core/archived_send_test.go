package core

import (
	"goim/internal/domain"
	"testing"
)

func TestArchivedAliasRejectsSendWithoutDiscardingSavedDraft(t *testing.T) {
	n := testNode(t, "archive")
	const text = "保留草稿"
	var conversation string
	if err := n.change(func(s *domain.State) {
		current := domain.Peer{ID: "current", Name: "current"}
		old := domain.Peer{ID: "previous", Name: "old", Transport: transportNWT, CompatibilityAliasOf: current.ID}
		s.Peers = append(s.Peers, current, old)
		conversation = ensureDirect(s, old)
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.execute(domain.Command{Kind: domain.SaveDraft, ConversationID: conversation, Text: text}); err != nil {
		t.Fatal(err)
	}
	if err := n.execute(domain.Command{Kind: domain.SendText, ConversationID: conversation, Text: text}); err == nil {
		t.Fatal("archived route accepted send")
	}
	state := n.Snapshot()
	if state.Conversation(conversation).Draft != text || len(state.ChatMessages(conversation)) != 0 {
		t.Fatal("rejected text changed history or draft")
	}
}

package core

import (
	"errors"
	"slices"

	"goim/internal/domain"
)

// chatDraftStep is the unsent suffix before this message is committed.
type chatDraftStep []domain.ChatDraftPart

func (step chatDraftStep) check(s *domain.State, conversation domain.Conversation) error {
	if step == nil {
		return nil
	}
	if s.Locked {
		return errors.New("应用已锁定，请解锁后发送消息。")
	}
	if conversation.ID == "" || conversation.Hidden {
		return errors.New("会话已关闭，无法继续发送草稿。")
	}
	return nil
}

// Commit the draft progress with the message and outbox. A different draft
// saved from another window while a file was being read is left alone.
func (step chatDraftStep) advance(s *domain.State, id string) {
	if step == nil {
		return
	}
	if at := convIndex(s, id); at >= 0 {
		draft := &s.Conversations[at]
		if slices.Equal(draft.DraftParts, step) {
			draft.DraftParts = draft.DraftParts[1:]
			draft.Draft = domain.ChatDraftText(draft.DraftParts)
		}
	}
}

func (n *Node) sendChatDraft(id string, parts []domain.ChatDraftPart) error {
	if len(parts) == 0 {
		return nil
	}
	n.mu.Lock()
	switch {
	case n.chatDraftsSending[id]:
		n.mu.Unlock()
		return errors.New("此会话正在发送草稿。")
	case !slices.Equal(n.data.State.Conversation(id).DraftParts, parts):
		n.mu.Unlock()
		return errors.New("草稿未保存或已在其他窗口修改，请编辑草稿后重试。")
	}
	if n.chatDraftsSending == nil {
		n.chatDraftsSending = make(map[string]bool)
	}
	n.chatDraftsSending[id] = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.chatDraftsSending, id)
		n.mu.Unlock()
	}()

	for index, part := range parts {
		step := chatDraftStep(parts[index:])
		var err error
		if part.Path != "" {
			err = n.sendFileWithLink(id, part.Path, false, "", browserShare{}, step)
		} else {
			err = n.sendTextWithDraft(id, part.Text, step)
		}
		if err != nil {
			return &domain.ChatDraftSendError{Accepted: index, Cause: err}
		}
	}
	return nil
}

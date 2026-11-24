package core

import (
	"errors"
	"slices"

	"goim/internal/domain"
)

func (n *Node) sendChatDraft(id string, parts []domain.ChatDraftPart) error {
	for index, part := range parts {
		n.mu.Lock()
		locked := n.data.State.Locked
		n.mu.Unlock()
		if locked {
			return &domain.ChatDraftSendError{Accepted: index, Cause: errors.New("应用已锁定，请解锁后发送消息。")}
		}

		var err error
		if part.Path != "" {
			err = n.sendFile(id, part.Path, false, "")
		} else {
			err = n.sendText(id, part.Text)
		}
		if err != nil {
			return &domain.ChatDraftSendError{Accepted: index, Cause: err}
		}

		// Advance the persisted draft even if the composer closes during a send.
		// Leave a draft edited in another window alone.
		if err := n.change(func(state *domain.State) {
			at := convIndex(state, id)
			if at < 0 {
				return
			}
			draft := &state.Conversations[at]
			if slices.Equal(draft.DraftParts, parts[index:]) {
				draft.DraftParts = draft.DraftParts[1:]
				draft.Draft = domain.ChatDraftText(draft.DraftParts)
			}
		}); err != nil {
			return &domain.ChatDraftSendError{Accepted: index + 1, Cause: err}
		}
	}
	return nil
}

package domain

import "strings"

// ChatDraftPart is one text segment or one local attachment in composer order.
// Local attachment paths are stored in the local draft and never sent to peers.
type ChatDraftPart struct {
	Text string
	Path string
}

func ChatDraftText(parts []ChatDraftPart) string {
	var text strings.Builder
	for _, part := range parts {
		text.WriteString(part.Text)
	}
	return text.String()
}

// ChatDraftSendError identifies the prefix already accepted for delivery.
type ChatDraftSendError struct {
	Accepted int
	Cause    error
}

func (e *ChatDraftSendError) Error() string { return e.Cause.Error() }
func (e *ChatDraftSendError) Unwrap() error { return e.Cause }

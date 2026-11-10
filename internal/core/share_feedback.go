package core

import "goim/internal/domain"

func (n *Node) emitShareFeedback(title, detail string, failed bool) {
	n.mu.Lock()
	n.shareFeedback = domain.ShareFeedback{Sequence: n.shareFeedback.Sequence + 1, Title: title, Detail: detail, Failed: failed}
	n.mu.Unlock()
	n.signal()
}

func (n *Node) finishShareCommand(title string, err error) {
	if err != nil {
		n.emitShareFeedback("分享操作失败", err.Error(), true)
	} else if title != "" {
		n.emitShareFeedback(title, "", false)
	}
}

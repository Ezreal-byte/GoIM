package desktop

import "testing"

func TestPendingGroupDoesNotPretendToSendWithoutRecipients(t *testing.T) {
	state := shellTestState(t)
	for i := range state.Groups {
		if state.Groups[i].ID == state.Conversation("conv-group").GroupID {
			state.Groups[i].Members = []string{state.Self.ID}
		}
	}
	h := newShellTestHarness(t, state)
	if h.hasHit(h.ui.ids.chatEditor) || !h.hasText("正在等待成员加入") {
		t.Fatal("unconfirmed group exposed an unusable sender")
	}
}

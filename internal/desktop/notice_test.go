package desktop

import "testing"

func TestBackendNoticeAppearsOnlyInPrimaryShell(t *testing.T) {
	state := shellTestState(t)
	backend := newShellTestBackend(state)
	main := New(backend)
	detached := newDetachedConversationView(backend, "conv-alice")
	t.Cleanup(main.Close)
	t.Cleanup(detached.Close)

	state.Notice = "个人资料已保存"
	state.Revision++
	backend.setState(state)
	main.refresh()
	detached.refresh()

	if main.toasts[0].message.Title != "个人资料已保存" {
		t.Fatalf("primary toast title = %q", main.toasts[0].message.Title)
	}
	for index, toast := range detached.toasts {
		if !toast.id.IsZero() {
			t.Fatalf("detached shell displayed backend notice in slot %d: %+v", index, toast.message)
		}
	}
}

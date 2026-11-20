package desktop

import (
	"errors"
	"testing"

	"goim/internal/domain"
)

func TestArchivedCompatibilityComposerRoutesToCurrentContact(t *testing.T) {
	state := shellTestState(t)
	state.Peers[0].Transport = domain.TransportNeiwangtong
	state.Peers[0].CompatibilityAliasOf = state.Peers[1].ID
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()
	if h.hasHit(h.ui.ids.chatSend) || h.hasHit(h.ui.ids.chatFolder) || !h.hasHit(h.ui.ids.chatCurrentContact) {
		t.Fatal("archived conversation exposes a send action or has no current contact action")
	}
	h.clickID(h.ui.ids.chatCurrentContact)
	cmd, ok := findShellCommand(h.backend.commandLog(), domain.StartChat)
	if !ok || cmd.PeerID != state.Peers[1].ID {
		t.Fatalf("wrong navigation: %+v", cmd)
	}
}

func TestRejectedChatSubmissionKeepsDraftAndLaterEdit(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.backend.deferCommandResult = true
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.typeIntoLabel("消息输入框", "原文")
	h.clickID(h.ui.ids.chatSend)
	cmd := h.lastCommand()
	if cmd.Kind != domain.SendText || cmd.OnResult == nil {
		t.Fatalf("no pending text command: %+v", cmd)
	}
	cmd.OnResult(errors.New("unavailable"))
	h.ui.onWake()
	if got := h.ui.chatEditor.String(); got != "原文" {
		t.Fatalf("lost denied text %q", got)
	}
	h.ui.sendChatText()
	cmd = h.lastCommand()
	h.ui.setChatEditorText("新草稿")
	cmd.OnResult(nil)
	h.ui.onWake()
	if got := h.ui.chatEditor.String(); got != "新草稿" {
		t.Fatalf("completion cleared a later edit %q", got)
	}
}

func TestNeiwangtongFolderPickerAndGroupAvatarCapabilities(t *testing.T) {
	state := shellTestState(t)
	state.Groups[0].Transport = domain.TransportNeiwangtong
	state.Peers[0].Transport = domain.TransportIPMSG
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-group")
	h.settle()
	path := t.TempDir()
	if err := h.driver.QueueOpenDirectory(path, nil); err != nil {
		t.Fatal(err)
	}
	h.clickID(h.ui.ids.chatFolder)
	cmd := h.lastCommand()
	if cmd.Kind != domain.SendFiles || cmd.ConversationID != "conv-group" || len(cmd.Paths) != 1 || cmd.Paths[0] != path {
		t.Fatalf("bad folder selection: %+v", cmd)
	}
	h.clickID(h.ui.ids.chatHeaderMembers)
	h.settle()
	if !h.hasHit(h.ui.ids.memberAvatar) {
		t.Fatal("native group owner cannot edit the group avatar")
	}
	h.ui.closeDrawer()
	h.ui.selectConversation("conv-alice")
	h.settle()
	if h.hasHit(h.ui.ids.chatFolder) {
		t.Fatal("IPMSG offers unsupported folder send")
	}
}

package desktop

import (
	"testing"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
)

func TestChatNavigationFocusesComposer(t *testing.T) {
	for _, entry := range []string{"conversation", "current conversation", "contact", "tray", "return to messages"} {
		t.Run(entry, func(t *testing.T) {
			state := shellTestState(t)
			state.Conversations[1].Unread = 1
			h := newShellTestHarness(t, state)
			h.settle()
			switch entry {
			case "conversation":
				h.clickLabel("Alice")
			case "current conversation":
				h.clickLabel("Alice")
				h.clickID(h.ui.ids.sidebarSearch)
				h.clickLabel("Alice")
			case "contact":
				h.clickLabel("联系人")
				h.clickLabel("Alice")
				h.clickID(h.ui.ids.contactTalk)
			case "tray":
				h.ui.setSection(sectionSettings)
				h.wake()
				r := desktopRuntime{ui: h.ui}
				r.openUnreadChat("conv-alice")
				h.wake()
			case "return to messages":
				h.clickLabel("Alice")
				h.clickLabel("联系人")
				h.clickID(h.ui.ids.navMessages)
			}
			h.settle()
			if h.ui.selected != "conv-alice" || h.snapshot().Focused != h.ui.ids.chatEditorField {
				t.Fatalf("chat %q opened without composer focus: %v", h.ui.selected, h.snapshot().Focused)
			}
			if err := h.driver.Type("直接输入"); err != nil {
				t.Fatal(err)
			}
			if got := h.ui.chatEditor.String(); got != "直接输入" {
				t.Fatalf("typing after opening chat produced %q", got)
			}
		})
	}
}

func TestNewChatFocusesAfterConversationArrives(t *testing.T) {
	state := shellTestState(t)
	h := newShellTestHarness(t, state)
	h.clickLabel("联系人")
	h.clickLabel("Bob")
	h.clickID(h.ui.ids.contactTalk)
	if h.ui.pendingPeer != "peer-b" {
		t.Fatal("new chat was not requested")
	}
	state.Conversations = append(state.Conversations, domain.Conversation{
		ID: "conv-bob", Kind: "direct", PeerID: "peer-b", Title: "Bob",
	})
	state.Revision++
	h.publish(state)
	h.settle()
	if h.ui.selected != "conv-bob" || h.snapshot().Focused != h.ui.ids.chatEditorField {
		t.Fatal("asynchronous new chat did not focus its composer")
	}
}

func TestChatUpdatesDoNotStealSearchFocus(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.clickLabel("Alice")
	if _, err := h.driver.ShortcutChord(gfx.ModifierControl, 'k'); err != nil {
		t.Fatal(err)
	}
	state := h.backend.Snapshot()
	state.Revision++
	state.Conversations[1].Unread++
	h.publish(state)
	h.settle()
	if h.snapshot().Focused != h.ui.ids.sidebarSearch {
		t.Fatal("background chat update stole sidebar search focus")
	}
	if _, err := h.driver.ShortcutChord(gfx.ModifierControl, 'f'); err != nil {
		t.Fatal(err)
	}
	h.ui.selectConversation(h.ui.selected)
	h.wake()
	if h.snapshot().Focused != h.ui.ids.chatSearchField {
		t.Fatal("autofocus stole an open chat search field")
	}
}

func TestReadOnlyChatDoesNotFocusAbsentComposer(t *testing.T) {
	state := shellTestState(t)
	state.Groups[0].Left = true
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-group")
	h.wake()
	if h.hasHit(h.ui.ids.chatEditorField) || h.snapshot().Focused == h.ui.ids.chatEditorField {
		t.Fatal("read-only conversation focused an absent composer")
	}
}

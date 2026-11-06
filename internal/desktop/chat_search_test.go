package desktop

import (
	"fmt"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

func waitForShellWake(t *testing.T, harness *shellTestHarness) {
	t.Helper()
	select {
	case <-harness.driver.Wakes():
	case <-time.After(2 * time.Second):
		t.Fatal("shell did not receive the invalidation")
	}
	if err := harness.driver.Pump(); err != nil {
		t.Fatal(err)
	}
}

func markReadCommands(harness *shellTestHarness) []domain.Command {
	harness.t.Helper()
	var commands []domain.Command
	for _, command := range harness.backend.commandLog() {
		if command.Kind == domain.MarkRead {
			commands = append(commands, command)
		}
	}
	return commands
}

func shellStateWithReadConversation(t *testing.T) domain.State {
	t.Helper()
	state := shellTestState(t)
	for index := range state.Conversations {
		state.Conversations[index].Unread = 0
	}
	return state
}

func TestChatSearchUsesAFieldSeparateFromSidebarQuickFind(t *testing.T) {
	harness := newShellTestHarness(t, shellTestState(t))
	if harness.hasHit(harness.ui.ids.chatSearchField) {
		t.Fatal("the in-chat search field was visible before it was opened")
	}

	harness.clickID(harness.ui.ids.chatHeaderSearch)
	if !harness.ui.chatFind.open || !harness.hasHit(harness.ui.ids.chatSearchField) {
		t.Fatal("the header search control did not open the in-chat find bar")
	}
	requireShellLayout(t, harness.snapshot())
	harness.clickID(harness.ui.ids.chatSearchClose)
	if harness.ui.chatFind.open || harness.hasHit(harness.ui.ids.chatSearchField) {
		t.Fatal("the close control did not dismiss the in-chat find bar")
	}

	handled, err := harness.driver.ShortcutChord(ui.ModifierControl, 'f')
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !harness.ui.chatFind.open {
		t.Fatal("configured Ctrl+F did not open the in-chat find bar")
	}
	if harness.snapshot().Focused != harness.ui.ids.chatSearchField {
		t.Fatal("Ctrl+F did not focus the in-chat search field")
	}
	if harness.ui.focusSearch {
		t.Fatal("Ctrl+F still requested the sidebar search field")
	}
	if err := harness.driver.Type("核对"); err != nil {
		t.Fatal(err)
	}
	if harness.ui.chatFind.query != "核对" || harness.ui.sidebarSearch.Text() != "" {
		t.Fatalf("search state leaked between fields: chat=%q sidebar=%q",
			harness.ui.chatFind.query, harness.ui.sidebarSearch.Text())
	}
	if !harness.hasText("1 条匹配") {
		t.Fatal("the in-chat find bar did not show the real match count")
	}
	if harness.hasText("看一下最新预览。") || !harness.hasText("收到，我来核对交互细节。") {
		t.Fatal("the in-chat query did not filter the current message timeline")
	}

	handled, err = harness.driver.ShortcutChord(ui.ModifierControl, 'k')
	if err != nil {
		t.Fatal(err)
	}
	if !handled || harness.snapshot().Focused != harness.ui.ids.sidebarSearch {
		t.Fatal("configured Ctrl+K did not focus the sidebar quick-find")
	}
	harness.typeInto(harness.ui.ids.sidebarSearch, "Alice")
	if harness.ui.sidebarSearch.Text() != "Alice" || harness.ui.chatFind.query != "核对" {
		t.Fatal("typing in sidebar quick-find changed the in-chat query")
	}
	if !harness.ui.chatFind.open {
		t.Fatal("sidebar quick-find unexpectedly closed the in-chat find bar")
	}
}

func TestChatSearchFiltersMessageTextAndFileNamesAndRestoresScroll(t *testing.T) {
	state := shellTestState(t)
	now := time.Now()
	for index := 0; index < 24; index++ {
		state.Messages = append(state.Messages, domain.Message{
			ID: fmt.Sprintf("filler-%02d", index), ConversationID: "conv-group",
			SenderID: "peer-a", Kind: "text", Text: fmt.Sprintf("较早的普通消息 %02d", index),
			CreatedAt: now.Add(-time.Duration(40-index) * time.Minute), Status: "delivered",
		})
	}
	harness := newShellTestHarness(t, state)

	chat := harness.ui.lastChat
	if err := harness.driver.Scroll(ui.ScrollEvent{
		X: chat.X + chat.Width/2, Y: chat.Y + 300, DeltaY: -500, Precise: true,
	}); err != nil {
		t.Fatal(err)
	}
	savedScroll := harness.ui.chatView.ScrollState(harness.ui.ids.chatMessages).Offset
	if savedScroll <= 0 {
		t.Fatal("the chat did not scroll away from its end before searching")
	}

	handled, err := harness.driver.ShortcutChord(ui.ModifierControl, 'f')
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !harness.ui.chatFind.open {
		t.Fatal("configured Ctrl+F did not open the in-chat find bar")
	}
	if harness.ui.chatFind.savedOffset != savedScroll {
		t.Fatalf("saved chat scroll = %v, want %v", harness.ui.chatFind.savedOffset, savedScroll)
	}
	if err := harness.driver.Type("zip"); err != nil {
		t.Fatal(err)
	}
	if harness.ui.chatFind.query != "zip" || !harness.hasText("1 条匹配") {
		t.Fatalf("file-name query results were not real: query=%q count=%d",
			harness.ui.chatFind.query, harness.ui.chatFind.matchCount)
	}
	if !harness.hasText("设计素材.zip") || harness.hasText("较早的普通消息 01") {
		t.Fatal("a file-name query did not filter the current chat timeline")
	}

	if err := harness.driver.SelectText(0, utf8.RuneCountInString(harness.ui.chatFind.editor.String())); err != nil {
		t.Fatal(err)
	}
	if err := harness.driver.Type("核对"); err != nil {
		t.Fatal(err)
	}
	if harness.ui.chatFind.query != "核对" || harness.ui.chatFind.matchCount != 1 {
		t.Fatalf("message-text query = %q with %d matches", harness.ui.chatFind.query, harness.ui.chatFind.matchCount)
	}
	if harness.hasText("设计素材.zip") || !harness.hasText("收到，我来核对交互细节。") {
		t.Fatal("a message-text query did not filter the current chat timeline")
	}

	if err := harness.driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if harness.ui.chatFind.open {
		t.Fatal("Escape did not close the in-chat find bar")
	}
	if !harness.hasText("较早的普通消息 01") || !harness.hasText("设计素材.zip") {
		t.Fatal("closing search did not restore all conversation messages")
	}
	restoredScroll := harness.ui.chatView.ScrollState(harness.ui.ids.chatMessages).Offset
	if difference := restoredScroll - savedScroll; difference < -1 || difference > 1 {
		t.Fatalf("restored chat scroll = %v, want %v", restoredScroll, savedScroll)
	}
	waitForShellWake(t, harness)
	restoredScroll = harness.ui.chatView.ScrollState(harness.ui.ids.chatMessages).Offset
	if difference := restoredScroll - savedScroll; difference < -1 || difference > 1 {
		t.Fatalf("chat scroll after restoring follow mode = %v, want %v", restoredScroll, savedScroll)
	}
	requireShellLayout(t, harness.snapshot())
}

func TestSetWindowVisibleInvalidatesOnlyOnChange(t *testing.T) {
	harness := newShellTestHarness(t, shellStateWithReadConversation(t))
	if !harness.ui.windowVisible.Load() {
		t.Fatal("a new shell is not visible by default")
	}

	harness.ui.SetWindowVisible(false)
	waitForShellWake(t, harness)
	harness.ui.SetWindowVisible(false)
	select {
	case <-harness.driver.Wakes():
		t.Fatal("setting the same visibility requested another frame")
	default:
	}
	harness.ui.SetWindowVisible(true)
	waitForShellWake(t, harness)
}

func TestMarkReadRequiresVisibleSelectedChatOutsidePanels(t *testing.T) {
	t.Run("visible selected chat", func(t *testing.T) {
		state := shellStateWithReadConversation(t)
		harness := newShellTestHarness(t, state)
		state.Conversations[0].Unread = 3
		state.Revision++
		harness.publish(state)

		commands := markReadCommands(harness)
		if len(commands) != 1 || commands[0].ConversationID != "conv-group" {
			t.Fatalf("MarkRead commands = %+v", commands)
		}
		harness.publish(state)
		if commands = markReadCommands(harness); len(commands) != 1 {
			t.Fatalf("an unchanged unread snapshot caused another MarkRead: %+v", commands)
		}
	})

	t.Run("hidden window", func(t *testing.T) {
		state := shellStateWithReadConversation(t)
		harness := newShellTestHarness(t, state)
		harness.ui.SetWindowVisible(false)
		waitForShellWake(t, harness)

		state.Conversations[0].Unread = 4
		state.Revision++
		harness.publish(state)
		if commands := markReadCommands(harness); len(commands) != 0 {
			t.Fatalf("a hidden window marked its selected chat read: %+v", commands)
		}

		harness.ui.SetWindowVisible(true)
		waitForShellWake(t, harness)
		commands := markReadCommands(harness)
		if len(commands) != 1 || commands[0].ConversationID != "conv-group" {
			t.Fatalf("showing the window did not clear the selected unread chat: %+v", commands)
		}
	})

	t.Run("settings panel", func(t *testing.T) {
		state := shellStateWithReadConversation(t)
		harness := newShellTestHarness(t, state)
		harness.clickID(harness.ui.ids.navSettings)

		state.Conversations[0].Unread = 2
		state.Revision++
		harness.publish(state)
		if commands := markReadCommands(harness); len(commands) != 0 {
			t.Fatalf("settings marked an unseen selected chat read: %+v", commands)
		}

		harness.clickID(harness.ui.ids.navMessages)
		commands := markReadCommands(harness)
		if len(commands) != 1 || commands[0].ConversationID != "conv-group" {
			t.Fatalf("returning to the visible chat did not clear unread: %+v", commands)
		}
	})
}

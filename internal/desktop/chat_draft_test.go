package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"

	"goim/internal/domain"
)

func TestMixedChatDraftPreviewBackspaceAndSendOrder(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.settle()
	cache := t.TempDir()
	h.ui.clipboardCacheDir = func() (string, error) { return cache, nil }
	image, err := os.ReadFile(writeExtraTestPNG(t, "paste-source.png"))
	if err != nil {
		t.Fatal(err)
	}
	key := func(key gfx.Key) {
		t.Helper()
		if err := h.driver.Key(key, 0); err != nil {
			t.Fatal(err)
		}
	}
	paste := func() {
		t.Helper()
		if err := h.driver.SetClipboardImage(image); err != nil {
			t.Fatal(err)
		}
		if _, err := h.driver.ShortcutChord(gfx.ModifierControl, 'v'); err != nil {
			t.Fatal(err)
		}
		h.settle()
	}

	h.typeInto(h.ui.ids.chatEditorField, "前后")
	key(gfx.KeyLeft)
	paste()
	draft := h.ui.mixedDraft("conv-alice")
	if draft == nil || len(draft.files) != 1 {
		t.Fatal("image paste never reached the composer")
	}
	path := draft.files[0].file.SourcePath
	want := []domain.ChatDraftPart{{Text: "前"}, {Path: path}, {Text: "后"}}
	if !slices.Equal(draft.parts(), want) || h.snapshot().Focused != draft.texts[1].id {
		t.Fatalf("draft did not insert at caret or move focus after the image: %+v", draft.parts())
	}
	h.waitFor(func() bool { return bitmapReady(h.ui.fileBitmap(path)) })
	h.clickID(draft.files[0].id)
	if !h.ui.imageViewerOpen() || h.ui.viewerPath() != path {
		t.Fatal("draft image did not open in the viewer")
	}
	key(gfx.KeyEscape)
	h.settle()
	if h.ui.imageViewerOpen() || h.snapshot().Focused != draft.texts[1].id {
		t.Fatal("closing the image viewer did not restore the composer")
	}

	paste()
	draft = h.ui.mixedDraft("conv-alice")
	if len(draft.files) != 2 {
		t.Fatal("second image did not remain in the draft")
	}
	key(gfx.KeyBackspace)
	h.settle()
	if !slices.Equal(draft.parts(), want) {
		t.Fatalf("Backspace removed text or the wrong image: %+v", draft.parts())
	}

	if hasCommandKind(h.backend.commandLog(), domain.SendFiles) || hasCommandKind(h.backend.commandLog(), domain.SendChatDraft) {
		t.Fatal("local preview, paste, or deletion sent a message")
	}
	h.backend.deferCommandResult = true
	h.clickID(h.ui.ids.chatSend)
	cmd, _ := findShellCommand(h.backend.commandLog(), domain.SendChatDraft)
	if !slices.Equal(cmd.DraftParts, want) || cmd.OnResult == nil || !h.ui.chatDraftPending() {
		t.Fatalf("wrong send sequence: %+v", cmd)
	}
	cmd.OnResult(&domain.ChatDraftSendError{Accepted: 1, Cause: errors.New("transfer unavailable")})
	h.ui.onWake()
	if remaining := h.ui.mixedDraft("conv-alice"); remaining == nil || !slices.Equal(remaining.parts(), want[1:]) {
		t.Fatalf("failed batch did not retain just the unsent suffix: %+v", remaining)
	}
}

func TestMixedChatDraftPersistsPerConversationAndRemovesFile(t *testing.T) {
	state := shellTestState(t)
	path := filepath.Join(t.TempDir(), "report.pdf")
	for index := range state.Conversations {
		if state.Conversations[index].ID == "conv-alice" {
			state.Conversations[index].Draft = "后文"
			state.Conversations[index].DraftParts = []domain.ChatDraftPart{{Path: path}, {Text: "后文"}}
		}
	}
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()
	draft := h.ui.mixedDraft("conv-alice")
	if draft == nil || draft.files[0].file.SourcePath != path {
		t.Fatal("persisted file was not restored")
	}
	h.ui.selectConversation("conv-bob")
	h.settle()
	if h.ui.mixedDraft("conv-bob") != nil {
		t.Fatal("draft leaked to the other conversation")
	}
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.clickID(draft.files[0].id)
	h.settle()
	if err := h.driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if h.ui.mixedDraft("conv-alice") != nil || h.ui.chatEditor.String() != "后文" {
		t.Fatal("Backspace did not return the conversation to its remaining text")
	}
}

func TestMixedChatDraftPastesClipboardFilesInsteadOfFilename(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.settle()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "一.pdf"), filepath.Join(dir, "二.txt")}
	h.driver.SetClipboardFiles(paths)
	h.clickID(h.ui.ids.chatEditorField)
	if _, err := h.driver.Shortcut('v'); err != nil {
		t.Fatal(err)
	}
	h.settle()
	draft := h.ui.mixedDraft("conv-alice")
	want := []domain.ChatDraftPart{{Path: paths[0]}, {Path: paths[1]}}
	if draft == nil || !slices.Equal(draft.parts(), want) {
		t.Fatalf("clipboard filenames replaced the actual attachments: %+v", draft)
	}
	if err := h.driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	h.settle()
	cmd, _ := findShellCommand(h.backend.commandLog(), domain.SendChatDraft)
	if !slices.Equal(cmd.DraftParts, want) {
		t.Fatalf("Enter did not submit the pasted files in order: %+v", cmd)
	}
}

func TestMixedChatDraftScrollsToCaretAfterManyAttachments(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	path := writeExtraTestPNG(t, "attachments.png")
	h.ui.stageChatFiles(slices.Repeat([]string{path}, 30))
	h.settle()

	id := h.ui.activeChatFieldID()
	hit, ok := h.findHit(id)
	if !ok || h.snapshot().Focused != id || hit.Bounds.Bottom() > h.ui.draftScroll.Rect().Bottom()+1 {
		t.Fatalf("caret is hidden after paste: editor=%+v focus=%+v bounds=%+v", id, h.snapshot().Focused, hit.Bounds)
	}
	if err := h.driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if got := len(h.ui.mixedDraft("conv-alice").files); got != 29 {
		t.Fatalf("Backspace left %d attachments, want 29", got)
	}
}

func TestMixedChatDraftDoesNotSaveUnchangedDraftFromAnotherWindow(t *testing.T) {
	state := shellTestState(t)
	path := filepath.Join(t.TempDir(), "report.pdf")
	for index := range state.Conversations {
		if state.Conversations[index].ID == "conv-alice" {
			state.Conversations[index].DraftParts = []domain.ChatDraftPart{{Path: path}}
		}
	}
	h := newShellTestHarness(t, state)
	h.ui.selectConversation("conv-alice")
	h.settle()

	before := len(h.backend.commandLog())
	h.ui.saveCurrentDraft()
	if hasCommandKind(h.backend.commandLog()[before:], domain.SaveDraft) {
		t.Fatal("an untouched draft was written back from a second window")
	}
	h.backend.deferCommandResult = true
	h.clickID(h.ui.ids.chatSend)
	command, _ := findShellCommand(h.backend.commandLog(), domain.SendChatDraft)
	command.OnResult(nil)
	h.ui.onWake()
	if h.ui.mixedDraft("conv-alice") != nil {
		t.Fatal("sent draft is still visible")
	}
	if hasCommandKind(h.backend.commandLog()[before:], domain.SaveDraft) {
		t.Fatal("send completion overwrote the persisted draft")
	}
}

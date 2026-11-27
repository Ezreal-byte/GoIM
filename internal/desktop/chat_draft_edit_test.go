package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"goim/internal/domain"
)

func TestMixedDraftKeyboardSelectionReplacementAndUndo(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.typeInto(h.ui.ids.chatEditorField, "前")
	paths := []string{filepath.Join(t.TempDir(), "附件.pdf"), writeExtraTestPNG(t, "图片.png")}
	h.ui.stageChatFiles(paths)
	h.ui.insertChatText("后")
	h.settle()
	want := h.ui.mixedDraft(h.ui.selected).parts()
	key := func(k gfx.Key, modifiers gfx.Modifiers) {
		t.Helper()
		if err := h.driver.Key(k, modifiers); err != nil {
			t.Fatal(err)
		}
		h.settle()
	}
	shortcut := func(k rune) {
		t.Helper()
		if _, err := h.driver.ShortcutChord(gfx.ModifierControl, k); err != nil {
			t.Fatal(err)
		}
		h.settle()
	}
	key(gfx.KeyLeft, 0)
	key(gfx.KeyLeft, 0)
	d := h.ui.mixedDraft(h.ui.selected)
	if d.selectedFile() != 1 || h.snapshot().Focused != d.files[1].id {
		t.Fatal("Left must select the image at the text boundary")
	}
	key(gfx.KeyLeft, 0)
	if d.selected != nil || d.active != 1 {
		t.Fatal("Left must enter the empty field between attachments")
	}
	key(gfx.KeyLeft, 0)
	if d.selectedFile() != 0 {
		t.Fatal("Left skipped the file")
	}
	key(gfx.KeyRight, 0)
	key(gfx.KeyRight, 0)
	if d.selectedFile() != 1 {
		t.Fatal("Right skipped the image")
	}
	key(gfx.KeyDelete, 0)
	shortcut('z')
	d = h.ui.mixedDraft(h.ui.selected)
	if d == nil || !slices.Equal(d.parts(), want) {
		t.Fatal("Undo did not restore the deleted image and its order")
	}
	shortcut('a')
	if err := h.driver.Type("替换"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if h.ui.mixedDraft(h.ui.selected) != nil || h.ui.chatEditor.String() != "替换" {
		t.Fatal("typing must replace the complete mixed selection")
	}
	shortcut('z')
	d = h.ui.mixedDraft(h.ui.selected)
	if d == nil || !slices.Equal(d.parts(), want) {
		t.Fatal("Undo after the last attachment was removed lost the draft")
	}
	shortcut('y')
	if h.ui.chatEditor.String() != "替换" || h.ui.mixedDraft(h.ui.selected) != nil {
		t.Fatal("Redo did not repeat replacement")
	}
	shortcut('z')
	// Cut uses the application selection, even if the clipboard still holds files.
	h.driver.SetClipboardFiles(paths)
	shortcut('a')
	shortcut('x')
	if h.ui.chatEditor.String() != "" || h.ui.mixedDraft(h.ui.selected) != nil {
		t.Fatal("Cut pasted stale clipboard files instead of deleting the selection")
	}
	shortcut('z')
	if !slices.Equal(h.ui.mixedDraft(h.ui.selected).parts(), want) {
		t.Fatal("Cut was not one undo unit")
	}
	if hasCommandKind(h.backend.commandLog(), domain.SendChatDraft) {
		t.Fatal("editing sent the draft")
	}
}

func TestMixedDraftGapAndIMEKeepAttachmentUntilCommit(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	path := writeExtraTestPNG(t, "插图.png")
	h.ui.stageChatFiles([]string{path, filepath.Join(t.TempDir(), "报告.pdf")})
	h.waitFor(func() bool { return bitmapReady(h.ui.fileBitmap(path)) })
	h.settle()
	d := h.ui.mixedDraft(h.ui.selected)
	image, _ := h.findHit(d.files[0].id)
	gap, ok := h.findHit(d.texts[1].id)
	if !ok || gap.Bounds.Height < image.Bounds.Height || gap.Bounds.Width < 12 {
		t.Fatal("gap does not cover the height of adjacent attachments")
	}
	h.clickID(d.files[0].id)
	h.clickID(d.texts[1].id)
	if h.snapshot().Focused != d.texts[1].id || d.selected != nil {
		t.Fatal("clicking next to a selected image did not restore the caret")
	}
	if err := h.driver.Type("中间"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	h.clickID(d.files[0].id)
	want := d.parts()
	if err := h.driver.Compose("zhong"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.parts(), want) {
		t.Fatal("IME preedit removed an attachment")
	}
	h.ui.sendChatText()
	if hasCommandKind(h.backend.commandLog(), domain.SendChatDraft) {
		t.Fatal("send committed an unfinished composition")
	}
	h.ui.onChatTextEvent(app.TextEvent{Kind: app.TextCancelComposition})
	h.settle()
	if !slices.Equal(d.parts(), want) || d.selectedFile() != 0 {
		t.Fatal("IME cancellation changed the draft selection")
	}
	if err := h.driver.Compose("zhong"); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.Type("中文"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	d = h.ui.mixedDraft(h.ui.selected)
	if len(d.files) != 1 || d.texts[0].editor.String() != "中文中间" {
		t.Fatalf("IME commit did not replace exactly the selected image: %+v", d.parts())
	}
	if _, err := h.driver.ShortcutChord(gfx.ModifierControl, 'z'); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if !slices.Equal(h.ui.mixedDraft(h.ui.selected).parts(), want) {
		t.Fatal("IME replacement was not one undo unit")
	}
}

func TestMixedDraftScreenshotQA(t *testing.T) {
	directory := os.Getenv("GOIM_CHAT_QA_SCREENSHOTS")
	if directory == "" {
		t.Skip("set GOIM_CHAT_QA_SCREENSHOTS for composer screenshots")
	}
	for _, theme := range []string{"light", "dark"} {
		state := shellTestState(t)
		state.Settings.Theme = theme
		u := New(newShellTestBackend(state))
		defer u.Close()
		u.selectConversation("conv-alice")
		u.insertChatText("请查看这份文件和截图：")
		path := addViewerImage(t, &state, "draft-preview", "截图.png", time.Now(), 512)
		if bitmap, ok := decodeBitmap(path, 512); ok {
			bitmap.ID, bitmap.Version = 1, 1
			u.imageCache[path] = bitmap
		}
		u.stageChatFiles([]string{filepath.Join(t.TempDir(), "会议记录.pdf"), path})
		u.insertChatText("这里继续输入说明。")
		config := chatSurfaceConfig(u)
		config.Width, config.Height = 1100, 760
		session, err := headless.Open(config, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		u.selectDraftFile(u.mixedDraft(u.selected), 1)
		if err := session.Wait(50 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, "mixed-composer-"+theme+".png")); err != nil {
			t.Fatal(err)
		}
		u.screenshotMenuOpen = true
		u.invalidator.Invalidate()
		if err := session.Wait(50 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := session.Screenshot(filepath.Join(directory, "screenshot-options-"+theme+".png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMixedDraftShiftSelectionAcrossParts(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.ui.stageChatFiles([]string{filepath.Join(t.TempDir(), "附件.pdf")})
	h.ui.insertChatText("尾")
	h.settle()
	for range 2 {
		if err := h.driver.Key(gfx.KeyLeft, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
		h.settle()
	}
	d := h.ui.mixedDraft(h.ui.selected)
	if !d.fileSelected(0) {
		t.Fatal("Shift+Left stopped at the attachment boundary")
	}
	if err := h.driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if h.ui.mixedDraft(h.ui.selected) != nil || h.ui.chatEditor.String() != "" {
		t.Fatal("Backspace did not remove the complete cross-part selection")
	}
}

func TestDraftUndoDoesNotLeakAcrossConversations(t *testing.T) {
	h := newShellTestHarness(t, shellTestState(t))
	h.ui.selectConversation("conv-alice")
	h.settle()
	h.typeInto(h.ui.ids.chatEditorField, "Alice 的草稿")
	h.ui.selectConversation("conv-bob")
	h.settle()
	want := h.ui.chatEditor.String()
	if _, err := h.driver.ShortcutChord(gfx.ModifierControl, 'z'); err != nil {
		t.Fatal(err)
	}
	if h.ui.chatEditor.String() != want {
		t.Fatal("Undo exposed another conversation's draft")
	}
}

package desktop

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

// Each attachment sits between two native editing fields. Empty fields keep
// both sides of adjacent attachments reachable with the keyboard.
type mixedChatDraft struct {
	texts  []*draftText
	files  []draftFile
	active int
	saved  []domain.ChatDraftPart
}

type draftText struct {
	id     gfx.ID
	editor *text.Editor
	field  ui.Field

	measuredText string
	measuredSize float32
	textWidth    float32
	lineCount    int
}

type draftFile struct {
	id   gfx.ID
	file domain.File
}

type pendingMixedChatDraft struct {
	parts []domain.ChatDraftPart
	done  <-chan error
}

func (u *UI) newDraftText(value string) *draftText {
	u.chatDraftSequence++
	segment := &draftText{
		id:     u.tree.ID(u.ids.chat, "draft-text-"+strconv.FormatUint(u.chatDraftSequence, 10)),
		editor: text.NewEditor(value),
	}
	segment.field.Bind(segment.editor)
	return segment
}

func (u *UI) newDraftFile(path string) draftFile {
	u.chatDraftSequence++
	key := "draft-file-" + strconv.FormatUint(u.chatDraftSequence, 10)
	return draftFile{
		id:   u.tree.ID(u.ids.chat, key),
		file: domain.File{ID: key, Name: filepath.Base(path), SourcePath: path},
	}
}

func (u *UI) mixedDraft(id string) *mixedChatDraft { return u.chatDrafts[id] }

func (u *UI) chatDraftPending() bool {
	_, waiting := u.pendingChatDrafts[u.selected]
	return waiting
}

func (u *UI) canStageChatFiles() bool {
	return u.selected != "" && !u.chatDraftPending() &&
		u.chatReadOnlyReason(u.state.Conversation(u.selected)) == ""
}

func (u *UI) stageChatFiles(paths []string) {
	if len(paths) == 0 || !u.canStageChatFiles() || u.chatEditor == nil {
		return
	}
	if _, composing := u.chatEditor.Composition(); composing {
		return
	}
	draft := u.mixedDraft(u.selected)
	selection := u.chatEditor.Selection()
	if draft == nil {
		draft = &mixedChatDraft{texts: []*draftText{u.newDraftText(u.chatEditor.String())}}
		u.chatDrafts[u.selected] = draft
	}
	current := draft.texts[draft.active]
	runes := []rune(current.editor.String())
	left, right := string(runes[:selection.Start]), string(runes[selection.End:])
	current.editor.SetSelection(text.Range{Start: 0, End: current.editor.RuneCount()})
	current.editor.Insert(left)
	current.editor.MoveTo(current.editor.RuneCount(), false)
	current.editor.ClearHistory()

	files := make([]draftFile, 0, len(paths))
	next := make([]*draftText, 0, len(paths))
	for _, path := range paths {
		files = append(files, u.newDraftFile(path))
		next = append(next, u.newDraftText(""))
	}
	last := next[len(next)-1].editor
	last.Insert(right)
	last.MoveTo(0, false)
	last.ClearHistory()
	index := draft.active
	draft.files = slices.Insert(draft.files, index, files...)
	draft.texts = slices.Insert(draft.texts, index+1, next...)
	u.selectDraftText(draft, index+len(files))
	u.persistDraft(u.selected, "")
}

func (u *UI) selectDraftText(draft *mixedChatDraft, index int) {
	draft.active = index
	u.chatEditor = draft.texts[index].editor
	u.closeMention()
	u.emojiOpen = false
	u.focusChat = true
	u.invalidator.Invalidate()
}

func (u *UI) activeChatField() *ui.Field {
	if draft := u.mixedDraft(u.selected); draft != nil {
		return &draft.texts[draft.active].field
	}
	return &u.chatEditorField
}

func (u *UI) activeChatFieldID() gfx.ID {
	if draft := u.mixedDraft(u.selected); draft != nil {
		return draft.texts[draft.active].id
	}
	return u.ids.chatEditorField
}

func (u *UI) matchDraftEditor(editor *text.Editor) bool {
	draft := u.mixedDraft(u.selected)
	if draft == nil {
		return editor == u.chatEditor
	}
	for index, segment := range draft.texts {
		if segment.editor == editor {
			if u.chatEditor != editor {
				u.closeMention()
			}
			draft.active, u.chatEditor = index, editor
			return true
		}
	}
	return false
}

func (u *UI) draftBoundaryCommand(command app.TextCommand) bool {
	draft := u.mixedDraft(u.selected)
	if draft == nil || u.chatDraftPending() {
		return false
	}
	selection := u.chatEditor.Selection()
	if selection.Start != selection.End {
		return false
	}
	if _, composing := u.chatEditor.Composition(); composing {
		return false
	}
	index, caret := draft.active, selection.Start
	switch {
	case caret == 0 && index > 0 && command == app.TextDeleteBackward:
		u.removeDraftFile(draft, index-1)
	case caret == u.chatEditor.RuneCount() && index < len(draft.files) && command == app.TextDeleteForward:
		u.removeDraftFile(draft, index)
	case caret == 0 && index > 0 && command == app.TextMoveLeft:
		previous := draft.texts[index-1].editor
		previous.MoveTo(previous.RuneCount(), false)
		u.selectDraftText(draft, index-1)
	case caret == u.chatEditor.RuneCount() && index < len(draft.files) && command == app.TextMoveRight:
		draft.texts[index+1].editor.MoveTo(0, false)
		u.selectDraftText(draft, index+1)
	default:
		return false
	}
	return true
}

func (u *UI) removeDraftFile(draft *mixedChatDraft, index int) {
	left, right := draft.texts[index].editor, draft.texts[index+1].editor
	caret := left.RuneCount()
	left.MoveTo(caret, false)
	left.Insert(right.String())
	left.MoveTo(caret, false)
	left.ClearHistory()
	draft.files = slices.Delete(draft.files, index, index+1)
	draft.texts = slices.Delete(draft.texts, index+1, index+2)
	if len(draft.files) == 0 {
		delete(u.chatDrafts, u.selected)
		delete(u.draftSaved, u.selected)
		u.chatEditor = u.chatPlainEditor
		u.setChatEditorText(left.String())
		u.chatEditor.MoveTo(caret, false)
		u.focusChat = true
		u.syncChatInput(true)
		u.persistDraft(u.selected, left.String())
		return
	}
	u.selectDraftText(draft, index)
	u.persistDraft(u.selected, "")
}

func (draft *mixedChatDraft) parts() []domain.ChatDraftPart {
	parts := make([]domain.ChatDraftPart, 0, 2*len(draft.files)+1)
	for index, segment := range draft.texts {
		if value := segment.editor.CommittedString(); value != "" {
			parts = append(parts, domain.ChatDraftPart{Text: value})
		}
		if index < len(draft.files) {
			parts = append(parts, domain.ChatDraftPart{Path: draft.files[index].file.SourcePath})
		}
	}
	return parts
}

func (u *UI) restoreMixedDraft(id string, parts []domain.ChatDraftPart) *mixedChatDraft {
	draft := &mixedChatDraft{saved: parts}
	var value strings.Builder
	for _, part := range parts {
		if part.Path == "" {
			value.WriteString(part.Text)
			continue
		}
		draft.texts = append(draft.texts, u.newDraftText(value.String()))
		value.Reset()
		draft.files = append(draft.files, u.newDraftFile(part.Path))
	}
	if len(draft.files) == 0 {
		return nil
	}
	draft.texts = append(draft.texts, u.newDraftText(value.String()))
	draft.active = len(draft.texts) - 1
	u.chatDrafts[id] = draft
	return draft
}

func (u *UI) sendMixedChatDraft(draft *mixedChatDraft) {
	if u.backend == nil || u.chatDraftPending() {
		return
	}
	if _, waiting := u.pendingTexts[u.selected]; waiting {
		return
	}
	id := u.selected
	parts := draft.parts()
	u.persistDraft(id, "")
	done := make(chan error, 1)
	u.pendingChatDrafts[id] = pendingMixedChatDraft{parts: parts, done: done}
	u.closeMention()
	u.emojiOpen = false
	u.submit(domain.Command{
		Kind: domain.SendChatDraft, ConversationID: id, DraftParts: parts,
		OnResult: func(err error) {
			done <- err
			u.invalidator.Invalidate()
		},
	})
	u.finishMixedChatSubmissions()
}

func (u *UI) finishMixedChatSubmissions() {
	for id, pending := range u.pendingChatDrafts {
		select {
		case err := <-pending.done:
			delete(u.pendingChatDrafts, id)
			accepted := len(pending.parts)
			if err != nil {
				var failure *domain.ChatDraftSendError
				accepted = 0
				if errors.As(err, &failure) {
					accepted = min(max(failure.Accepted, 0), len(pending.parts))
				}
				u.showToast(i18n.T("发送失败"), err.Error(), components.ToastError)
			}
			if accepted == 0 {
				if id == u.selected {
					u.focusChat = true
				}
				continue
			}
			remaining := pending.parts[accepted:]
			delete(u.chatDrafts, id)
			u.drafts[id] = domain.ChatDraftText(remaining)
			u.draftSaved[id] = u.drafts[id]
			u.restoreMixedDraft(id, remaining)
			if id == u.selected {
				u.loadDraft(id)
				u.focusChat = true
				u.chatScrollToEnd = true
			}
		default:
		}
	}
}

func (u *UI) drawMixedChatComposer(f ui.C, draft *mixedChatDraft) {
	u.mentionEditor = ui.Box{}
	u.draftScroll = ui.Scroll(f.Part("draft-scroll")).FillWidth().Grow(1).Basis(0).MinHeight(chatEditorMinHeight).Scrollbars().Children(func() {
		ui.Flow(f.Part("draft-flow")).FillWidth().Items(ui.End).
			PadSides(6, chatContentInset, 6, chatContentInset).Gap(2).LineGap(5).Children(func() {
			width := max(float32(40), u.lastChat.Width-2*chatContentInset-16)
			size := textSize(f, 14)
			for index, segment := range draft.texts {
				fieldWidth, fieldHeight := segment.dimensions(f.Canvas(), size, width)
				editor := ui.TextArea(f, &segment.field, i18n.T("消息输入框"), "", ui.WithID(segment.id)).
					Width(fieldWidth).Height(fieldHeight).MinHeight(fieldHeight).PadSides(2, 1, 2, 1).
					Background(ui.Color{}).Border(0, ui.Color{}).Shadow(gfx.ShadowNone).Disabled(u.chatDraftPending())
				if index == draft.active {
					u.mentionEditor = editor
				}
				if index < len(draft.files) {
					u.drawDraftFile(f, draft.files[index], index+1)
				}
			}
		})
	})
}

func (u *UI) revealDraftEditorAfterThumbnailChange(path string) {
	draft := u.mixedDraft(u.selected)
	if draft == nil || u.focusedID != draft.texts[draft.active].id {
		return
	}
	for _, attachment := range draft.files {
		if attachment.file.SourcePath == path {
			u.focusChat = true
			return
		}
	}
}

// The active field must enter the scroll viewport before Viva can focus it.
func (u *UI) revealDraftEditor(canvas *gfx.Canvas) bool {
	if u.mixedDraft(u.selected) == nil || !u.draftScroll.Valid() {
		return false
	}
	viewport, editor := u.draftScroll.Rect(), u.mentionEditor.Rect()
	scroll := u.draftScroll.ScrollState()
	offset := scroll.Offset
	switch {
	case editor.Y < viewport.Y:
		offset += editor.Y - viewport.Y
	case editor.Bottom() > viewport.Bottom():
		offset += min(editor.Bottom()-viewport.Bottom(), editor.Y-viewport.Y)
	}
	offset = min(max(offset, 0), scroll.Max())
	if offset >= scroll.Offset-0.5 && offset <= scroll.Offset+0.5 {
		return false
	}
	scroll.ScrollTo(offset)
	canvas.RequestFrame()
	return true
}

func (segment *draftText) dimensions(canvas *gfx.Canvas, size, limit float32) (width, height float32) {
	value := segment.editor.String()
	if segment.measuredText != value || segment.measuredSize != size {
		segment.measuredText, segment.measuredSize = value, size
		segment.lineCount, segment.textWidth = 0, 0
		for line := range strings.SplitSeq(value, "\n") {
			segment.lineCount++
			segment.textWidth = max(segment.textWidth, canvas.TextWidth(line, size))
		}
	}
	width = min(limit, max(6, segment.textWidth+5))
	if segment.lineCount > 1 {
		width = limit
	}
	lines := min(4, max(segment.lineCount, int(segment.textWidth/(width-2))+1))
	return width, 4 + (size+6)*float32(lines)
}

func (u *UI) drawDraftFile(f ui.C, attachment draftFile, right int) {
	file := attachment.file
	image := fileIsImage(file)
	bitmap := u.fileBitmap(file.SourcePath)
	label := i18n.T("草稿附件") + " · " + file.Name
	box := ui.Row(f.Key(file.ID)).Height(u.textExtent(28, 14)).MaxWidth(158).Radius(6).Clip().Items(ui.Center).
		Background(u.palette.fileCard).Border(1, u.palette.fileBorder).
		Hover(ui.Look{}.Background(u.palette.selected)).
		Focus(ui.Look{}.Ring(1.5, u.theme.Ring)).
		HitWith(attachment.id, label, gfx.HitOptions{Role: gfx.RoleButton}).Tooltip(file.Name)
	if image && bitmapReady(bitmap) {
		fitted := fitDraftBitmap(bitmap, gfx.Rect{Width: 138, Height: 82})
		box.Size(max(30, fitted.Width), max(24, fitted.Height)).Draw(func(d *ui.DrawContext) {
			d.Canvas.DrawBitmap(bitmap, fitDraftBitmap(bitmap, d.Rect))
		})
	} else {
		box.PadX(5).Gap(4).Children(func() {
			drawFileIcon(f, u.theme, fileTypeIcon(file), 18, 13)
			ui.Text(f, file.Name).MinWidth(0).FontSize(textSize(f, 12)).Ellipsis()
		})
	}
	if box.Clicked() {
		draft := u.mixedDraft(u.selected)
		draft.active = right
		draft.texts[right].editor.MoveTo(0, false)
		u.chatEditor = draft.texts[right].editor
		if image {
			u.openMediaViewer(imageViewerState{fileID: file.ID, conversationID: u.selected, fromDraft: true, images: []domain.File{file}})
		} else {
			u.selectDraftText(draft, right)
		}
	}
}

func fitDraftBitmap(bitmap gfx.Bitmap, rect gfx.Rect) gfx.Rect {
	scale := min(rect.Width/float32(bitmap.Width), rect.Height/float32(bitmap.Height))
	width, height := float32(bitmap.Width)*scale, float32(bitmap.Height)*scale
	return gfx.Rect{X: rect.X + (rect.Width-width)/2, Y: rect.Y + (rect.Height-height)/2, Width: width, Height: height}
}

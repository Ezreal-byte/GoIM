package desktop

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"goim/internal/domain"
)

// Positions count text runes and one atomic position per attachment. The
// persisted draft still contains only ordered text and file paths.
type draftSelection struct{ anchor, caret int }

func (s draftSelection) bounds() (int, int) { return min(s.anchor, s.caret), max(s.anchor, s.caret) }

func (d *mixedChatDraft) textOffset(index int) int {
	position := index
	for _, segment := range d.texts[:index] {
		position += segment.editor.RuneCount()
	}
	return position
}

func (d *mixedChatDraft) selection() draftSelection {
	if d.selected != nil {
		return *d.selected
	}
	e := d.texts[d.active].editor
	s := e.Selection()
	return draftSelection{d.textOffset(d.active) + s.Start + s.End - e.Caret(), d.textOffset(d.active) + e.Caret()}
}

func (d *mixedChatDraft) selectedFile() int {
	if d.selected == nil {
		return -1
	}
	start, end := d.selected.bounds()
	position := 0
	for index := range d.files {
		position += d.texts[index].editor.RuneCount()
		if start == position && end == position+1 {
			return index
		}
		position++
	}
	return -1
}

func (d *mixedChatDraft) fileSelected(index int) bool {
	if d.selected == nil {
		return false
	}
	start, end := d.selected.bounds()
	position := d.textOffset(index) + d.texts[index].editor.RuneCount()
	return start <= position && end > position
}

func (u *UI) selectDraftRange(d *mixedChatDraft, selection draftSelection) {
	length := d.textOffset(len(d.files)) + d.texts[len(d.files)].editor.RuneCount()
	selection.anchor = max(0, min(length, selection.anchor))
	selection.caret = max(0, min(length, selection.caret))
	start, end := selection.bounds()
	d.selected = &selection
	for index, segment := range d.texts {
		offset, count := d.textOffset(index), segment.editor.RuneCount()
		segment.editor.MoveTo(max(0, min(count, start-offset)), false)
		segment.editor.MoveTo(max(0, min(count, end-offset)), true)
		if selection.caret >= offset && selection.caret <= offset+count {
			d.active = index
		}
	}
	u.chatEditor = d.texts[d.active].editor
	if start == end {
		d.selected = nil
	}
	u.closeMention()
	u.emojiOpen = false
	u.focusChat = true
	u.invalidator.Invalidate()
}

func (u *UI) selectDraftFile(d *mixedChatDraft, index int) {
	position := d.textOffset(index) + d.texts[index].editor.RuneCount()
	u.selectDraftRange(d, draftSelection{position, position + 1})
}

func (u *UI) clearDraftSelection(d *mixedChatDraft) {
	if d.selected == nil {
		return
	}
	d.selected = nil
	for index, segment := range d.texts {
		if index != d.active {
			segment.editor.MoveTo(segment.editor.Caret(), false)
		}
	}
}

// replaceDraftSelection is used for an application selection that crosses an
// attachment. Ordinary editing and composition remain native text operations.
func (u *UI) replaceDraftSelection(value string) bool {
	d := u.mixedDraft(u.selected)
	if d == nil || d.selected == nil {
		return false
	}
	start, end := d.selected.bounds()
	var parts []domain.ChatDraftPart
	position := 0
	inserted := false
	appendText := func(value string) {
		if value == "" {
			return
		}
		if len(parts) > 0 && parts[len(parts)-1].Path == "" {
			parts[len(parts)-1].Text += value
		} else {
			parts = append(parts, domain.ChatDraftPart{Text: value})
		}
	}
	for index, segment := range d.texts {
		runes := []rune(segment.editor.CommittedString())
		appendText(string(runes[:max(0, min(len(runes), start-position))]))
		if start >= position && start <= position+len(runes) && !inserted {
			appendText(value)
			inserted = true
		}
		appendText(string(runes[max(0, min(len(runes), end-position)):]))
		position += len(runes)
		if index < len(d.files) {
			if position < start || position >= end {
				parts = append(parts, domain.ChatDraftPart{Path: d.files[index].file.SourcePath})
			}
			position++
		}
	}
	u.restoreDraftEdit(draftEditSnapshot{parts: parts, selection: draftSelection{start + utf8.RuneCountInString(value), start + utf8.RuneCountInString(value)}})
	return true
}

func (u *UI) draftSelectionCommand(command app.TextCommand) bool {
	d := u.mixedDraft(u.selected)
	if d == nil {
		return false
	}
	s := d.selection()
	start, end := s.bounds()
	switch command {
	case app.TextSelectAll:
		u.selectDraftRange(d, draftSelection{0, d.textOffset(len(d.files)) + d.texts[len(d.files)].editor.RuneCount()})
		return true
	case app.TextDeleteBackward, app.TextDeleteForward, app.TextDeleteWordBackward, app.TextDeleteWordForward:
		return u.replaceDraftSelection("")
	case app.TextMoveLeft, app.TextMoveRight:
		if d.selected != nil {
			position := start
			if command == app.TextMoveRight {
				position = end
			}
			u.selectDraftRange(d, draftSelection{position, position})
			return true
		}
	case app.TextMoveLeftAndModifySelection, app.TextMoveRightAndModifySelection,
		app.TextMoveLineStart, app.TextMoveLineEnd, app.TextMoveLineStartAndModifySelection, app.TextMoveLineEndAndModifySelection,
		app.TextMoveWordLeft, app.TextMoveWordRight, app.TextMoveWordLeftAndModifySelection, app.TextMoveWordRightAndModifySelection,
		app.TextMoveDocumentStart, app.TextMoveDocumentEnd, app.TextMoveDocumentStartAndModifySelection, app.TextMoveDocumentEndAndModifySelection:
		// Let Viva's editor handle grapheme and word boundaries in the logical
		// document. This runs only for an explicit navigation event, never paint.
		var value strings.Builder
		for index, segment := range d.texts {
			value.WriteString(segment.editor.String())
			if index < len(d.files) {
				value.WriteRune('\uFFFC')
			}
		}
		editor := text.NewEditor(value.String())
		editor.MoveTo(s.anchor, false)
		editor.MoveTo(s.caret, true)
		app.TextEvent{Kind: app.TextCommandEvent, Command: command}.Apply(editor)
		r := editor.Selection()
		u.selectDraftRange(d, draftSelection{r.Start + r.End - editor.Caret(), editor.Caret()})
		return true
	}
	return false
}

func (u *UI) selectedDraftCopyText() string {
	d := u.mixedDraft(u.selected)
	if d == nil || d.selected == nil {
		return ""
	}
	start, end := d.selected.bounds()
	var value strings.Builder
	for index, segment := range d.texts {
		offset := d.textOffset(index)
		runes := []rune(segment.editor.String())
		value.WriteString(string(runes[max(0, min(len(runes), start-offset)):max(0, min(len(runes), end-offset))]))
		if index < len(d.files) && d.fileSelected(index) {
			value.WriteString(d.files[index].file.Name)
		}
	}
	return value.String()
}

type draftEditSnapshot struct {
	parts     []domain.ChatDraftPart
	selection draftSelection
}

type draftEditHistory struct {
	undo, redo []draftEditSnapshot
}

func (u *UI) draftEditSnapshot() draftEditSnapshot {
	if d := u.mixedDraft(u.selected); d != nil {
		return draftEditSnapshot{parts: d.parts(), selection: d.selection()}
	}
	s := u.chatEditor.Selection()
	value := u.chatEditor.CommittedString()
	var parts []domain.ChatDraftPart
	if value != "" {
		parts = []domain.ChatDraftPart{{Text: value}}
	}
	return draftEditSnapshot{parts, draftSelection{s.Start + s.End - u.chatEditor.Caret(), u.chatEditor.Caret()}}
}

func (u *UI) beginDraftEdit() func() {
	if u.draftEditDepth > 0 || u.chatEditor == nil {
		return func() {}
	}
	u.draftEditDepth++
	id, before := u.selected, u.draftEditSnapshot()
	return func() {
		u.draftEditDepth--
		if u.selected != id || u.chatDraftPending() || slices.Equal(before.parts, u.draftEditSnapshot().parts) {
			return
		}
		if u.draftHistory == nil {
			u.draftHistory = make(map[string]*draftEditHistory)
		}
		history := u.draftHistory[id]
		if history == nil {
			history = &draftEditHistory{}
			u.draftHistory[id] = history
		}
		if len(history.undo) == 64 {
			history.undo = slices.Delete(history.undo, 0, 1)
		}
		history.undo = append(history.undo, before)
		history.redo = nil
	}
}

func (u *UI) draftUndo(command app.TextCommand) bool {
	history := u.draftHistory[u.selected]
	if history == nil || (command != app.TextUndo && command != app.TextRedo) {
		return false
	}
	source, destination := &history.undo, &history.redo
	if command == app.TextRedo {
		source, destination = destination, source
	}
	if len(*source) > 0 {
		previous := (*source)[len(*source)-1]
		*source = (*source)[:len(*source)-1]
		*destination = append(*destination, u.draftEditSnapshot())
		u.restoreDraftEdit(previous)
	}
	return true
}

func (u *UI) restoreDraftEdit(snapshot draftEditSnapshot) {
	delete(u.chatDrafts, u.selected)
	delete(u.draftSaved, u.selected)
	if d := u.restoreMixedDraft(u.selected, snapshot.parts); d != nil {
		d.saved = nil
		u.selectDraftRange(d, snapshot.selection)
		u.persistDraft(u.selected, "")
	} else {
		u.chatEditor = u.chatPlainEditor
		u.setChatEditorText(domain.ChatDraftText(snapshot.parts))
		u.chatEditor.MoveTo(snapshot.selection.anchor, false)
		u.chatEditor.MoveTo(snapshot.selection.caret, true)
		u.persistDraft(u.selected, u.chatEditor.String())
	}
	u.focusChat = true
	u.syncChatInput(true)
	u.invalidator.Invalidate()
}

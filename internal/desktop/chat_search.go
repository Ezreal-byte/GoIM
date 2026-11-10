package desktop

import (
	"goim/internal/i18n"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"

	"goim/internal/domain"
)

type chatFindState struct {
	open           bool
	conversationID string
	editor         *text.Editor
	input          app.TextInput
	query          string
	matches        []domain.Message
	matchCount     int
	savedOffset    float32
	restorePending bool
	restoreApply   bool
	focus          bool
}

func (u *UI) ensureChatSearchEditor() *text.Editor {
	if u.chatFind.editor == nil {
		u.chatFind.editor = text.NewEditor("")
	}
	return u.chatFind.editor
}

func replaceChatSearchText(editor *text.Editor, value string) {
	editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
	editor.Insert(value)
}

func (u *UI) openChatSearch() {
	if u.selected == "" || u.isPanelSection() {
		return
	}
	u.closeChatPickers()
	u.moreOpen = false
	editor := u.ensureChatSearchEditor()
	if u.chatFind.open {
		if u.focusedID == u.ids.chatSearchField {
			editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
		}
		u.chatFind.focus = true
		u.focusSearch = false
		return
	}
	offset := float32(0)
	if state := u.chatView.ScrollState(u.ids.chatMessages); state != nil {
		offset = state.Offset
	}
	u.chatFind.open = true
	u.chatFind.conversationID = u.selected
	u.chatFind.query = ""
	u.chatFind.matches = u.chatFind.matches[:0]
	u.chatFind.matchCount = len(u.messages)
	u.chatFind.savedOffset = offset
	u.chatFind.restorePending = false
	u.chatFind.restoreApply = false
	u.chatFind.focus = true
	replaceChatSearchText(editor, "")
	u.focusSearch = false
}

func (u *UI) closeChatSearch() {
	if !u.chatFind.open {
		return
	}
	u.chatFind.restorePending = u.chatFind.conversationID == u.selected
	u.chatFind.restoreApply = false
	u.chatFind.open = false
	u.chatFind.focus = false
	u.chatFind.query = ""
	u.chatFind.matches = u.chatFind.matches[:0]
	u.chatFind.matchCount = 0
	if u.chatFind.editor != nil {
		replaceChatSearchText(u.chatFind.editor, "")
	}
}

func (u *UI) resetChatSearch() {
	u.chatFind.open = false
	u.chatFind.conversationID = ""
	u.chatFind.query = ""
	u.chatFind.matches = u.chatFind.matches[:0]
	u.chatFind.matchCount = 0
	u.chatFind.savedOffset = 0
	u.chatFind.restorePending = false
	u.chatFind.restoreApply = false
	u.chatFind.focus = false
	if u.chatFind.editor != nil {
		replaceChatSearchText(u.chatFind.editor, "")
	}
}

func (u *UI) rebuildChatSearchMatches() {
	if u.chatFind.editor == nil || !u.chatFind.open {
		u.chatFind.query = ""
		u.chatFind.matches = u.chatFind.matches[:0]
		u.chatFind.matchCount = 0
		return
	}
	query := strings.TrimSpace(u.chatFind.editor.String())
	u.chatFind.query = query
	u.chatFind.matches = u.chatFind.matches[:0]
	if query == "" {
		u.chatFind.matchCount = len(u.messages)
		return
	}
	for _, message := range u.messages {
		if u.messageMatchesChatSearch(message, query) {
			u.chatFind.matches = append(u.chatFind.matches, message)
		}
	}
	u.chatFind.matchCount = len(u.chatFind.matches)
}

func (u *UI) messageMatchesChatSearch(message domain.Message, query string) bool {
	if containsFold(message.Text, query) {
		return true
	}
	if message.FileID == "" {
		return false
	}
	file := u.state.File(message.FileID)
	return file.ID != "" && containsFold(file.Name, query)
}

func (u *UI) visibleChatMessages() []domain.Message {
	if u.chatFind.open && u.chatFind.query != "" {
		return u.chatFind.matches
	}
	return u.messages
}

func (u *UI) chatSearchTextInput(context app.TextInputContext) *app.TextInput {
	if !u.chatFind.open || context.ID != u.ids.chatSearchField {
		return nil
	}
	if u.chatFind.input.OnEvent == nil {
		u.chatFind.input.OnEvent = u.onChatSearchTextEvent
	}
	u.chatFind.input.State = app.TextStateOf(u.ensureChatSearchEditor())
	return &u.chatFind.input
}

func (u *UI) onChatSearchTextEvent(event app.TextEvent) (app.TextState, error) {
	editor := u.ensureChatSearchEditor()
	state := app.TextStateOf(editor)
	if event.Kind == app.TextPasteNoText || event.Kind == app.TextCommandEvent &&
		(event.Command == app.TextInsertNewline || event.Command == app.TextInsertLineBreak) {
		return state, nil
	}
	event.Apply(editor)
	u.rebuildChatSearchMatches()
	state = app.TextStateOf(editor)
	state.Redraw = true
	u.chatFind.input.State = state
	return state, nil
}

func (u *UI) drawChatSearchBar(f *flex.Frame) {
	if !u.chatFind.open {
		return
	}
	editor := u.ensureChatSearchEditor()
	value := editor.String()
	selection := editor.Selection()
	composition, _ := editor.Composition()
	focused := f.Canvas().Focused() == u.ids.chatSearchField
	f.Row().FillWidth().Height(52).PadSides(12, 36, 10, 36).
		Background(u.palette.header).Items(flex.Center).Gap(10).Children(func() {
		f.Icon(u.iconSearch).Size(16, 16).Color(u.palette.secondaryText)
		f.Input(components.InputSnapshot{
			ID: u.ids.chatSearchField, Label: i18n.T("搜索当前会话"), Value: value,
			Placeholder: i18n.T("搜索消息文字或文件名"), Hint: i18n.T("筛选当前会话的消息"),
			Focused: focused, PlatformEditing: focused && value != "",
			ShowCaret: focused && value == "", Caret: editor.Caret(),
			SelectionStart: selection.Start, SelectionEnd: selection.End,
			CompositionStart: composition.Start, CompositionEnd: composition.End,
		}).Height(34).Grow(1)
		f.Text(intString(u.chatFind.matchCount) + i18n.T(" 条匹配")).FontSize(11).Muted()
		close := u.iconButton(f, u.ids.chatSearchClose, u.iconX, i18n.T("关闭会话内搜索"), 28)
		if close.Clicked() {
			u.closeChatSearch()
		}
	})
}

package desktop

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type chatFindState struct {
	open           bool
	conversationID string
	editor         *text.Editor
	field          ui.Field
	input          app.TextInput
	query          string
	matches        []domain.Message
	matchCount     int
	savedOffset    float32
	savedAtEnd     bool
	restorePending bool
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
	if u.presentHistory != nil {
		u.presentHistory(u.selected)
		return
	}
	editor := u.ensureChatSearchEditor()
	if u.chatFind.open {
		if u.focusedID == u.ids.chatSearchField {
			editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
		}
		u.chatFind.focus = true
		u.focusSearch = false
		return
	}
	u.chatFind.open = true
	u.chatFind.conversationID = u.selected
	u.chatFind.query = ""
	u.chatFind.matches = u.chatFind.matches[:0]
	u.chatFind.matchCount = len(u.messages)
	u.chatFind.savedOffset = u.timeline.offset
	u.chatFind.savedAtEnd = u.timeline.conversation != u.selected || u.timeline.atEnd
	u.chatFind.restorePending = false
	u.chatFind.focus = true
	replaceChatSearchText(editor, "")
	u.focusSearch = false
}

func (u *UI) closeChatSearch() {
	if !u.chatFind.open {
		return
	}
	u.chatFind.restorePending = u.chatFind.conversationID == u.selected
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

func (u *UI) drawChatSearchBar(f ui.C) {
	if !u.chatFind.open {
		return
	}
	ui.Row(f).FillWidth().Height(chatSearchExtent).PadX(chatContentInset).
		Items(ui.Center).Gap(10).Children(func() {
		ui.Icon(f, u.iconSearch).Size(16, 16).Color(u.palette.secondaryText)
		ui.TextField(f, &u.chatFind.field, i18n.T("搜索当前会话"), i18n.T("搜索消息文字或文件名"), ui.WithID(u.ids.chatSearchField)).
			Height(34).Grow(1)
		ui.Text(f, intString(u.chatFind.matchCount)+i18n.T(" 条匹配")).FontSize(textSize(f, 11)).Muted()
		close := u.iconButton(f, u.ids.chatSearchClose, u.iconX, i18n.T("关闭会话内搜索"), 28)
		if close.Clicked() {
			u.closeChatSearch()
		}
	})
}

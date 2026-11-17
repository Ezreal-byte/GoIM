package desktop

import (
	"math"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	conversationDragThreshold = float32(10)
	detachedWindowWidth       = 920
	detachedWindowHeight      = 760
	detachedWindowMinWidth    = 640
	detachedWindowMinHeight   = 520
)

// newDetachedConversationView creates the independent conversation window
// presented after a row is dragged out of the main GoIM window. It shares the
// backend contract while retaining its own editor, focus and scroll state.
func newDetachedConversationView(backend domain.Backend, conversationID string) *UI {
	u := newUIShell(backend, false)
	u.detachedWindow = true
	u.saveCurrentDraft()
	u.selected = conversationID
	u.loadDraft(conversationID)
	u.rebuildSelection()
	u.chatScrollToEnd = true
	return u
}

func (u *UI) DetachedConfig() app.Config {
	title := "GoIM"
	if conversation := u.state.Conversation(u.selected); conversation.ID != "" {
		if name := strings.TrimSpace(u.conversationTitle(conversation)); name != "" {
			title = name + " · GoIM"
		}
	}
	clear := colorToApp(u.palette.sidebar)
	return app.Config{
		Title:          title,
		Width:          detachedWindowWidth,
		Height:         detachedWindowHeight,
		MinWidth:       detachedWindowMinWidth,
		MinHeight:      detachedWindowMinHeight,
		WindowMode:     app.WindowTitled,
		ClearColor:     clear,
		Tree:           &u.tree,
		Paint:          u.paintDetached,
		TextInput:      u.textInput,
		Invalidator:    &u.invalidator,
		OnWake:         u.onWake,
		OnScroll:       u.onScroll,
		OnPointer:      u.onPointer,
		OnKey:          u.onKey,
		OnShortcut:     u.onShortcut,
		OnFileDrop:     u.onFileDrop,
		OnMaximize:     u.setWindowMaximized,
		OnFullscreen:   u.setWindowFullscreen,
		OnClosed:       u.Close,
		WindowControls: u.windowControls,
	}
}

func (u *UI) paintDetached(canvas *gfx.Canvas, size ui.Size) {
	u.theme = themeForName(u.state.Settings.Theme)
	u.palette = paletteFor(u.theme)
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.palette.chat)
	u.focusedID = canvas.Focused()
	u.emojiButton, u.mentionEditor, u.composerBox = ui.Box{}, ui.Box{}, ui.Box{}
	u.lastBody, u.lastChat = bounds, bounds

	u.drawChatSurface(canvas, bounds, u.state.Conversation(u.selected))
	u.hover.Paint(canvas, u.theme, bounds, components.TooltipOptions{Layer: 55}, 0)
	u.drawToasts(canvas, bounds)
	u.drawOverlays(canvas, bounds)
	u.drawChatImageViewer(canvas)
	u.markSelectedConversationRead()
}

func (u *UI) dragConversationPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if u.detachedWindow || u.lockVisible() || u.modalOpen() || u.isPanelSection() ||
		u.section != sectionMessages {
		return gfx.EventResult{}, false
	}
	drag := &u.conversationDrag
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary || event.Target.IsZero() {
			return gfx.EventResult{}, false
		}
		if id := u.conversationIDForHit(event.Target); id != "" {
			*drag = conversationDragState{id: id, startX: event.ScreenX, startY: event.ScreenY}
		}
		return gfx.EventResult{}, false
	case gfx.PointerMove:
		if drag.id == "" || !event.Captured {
			return gfx.EventResult{}, false
		}
		if !drag.dragging {
			dx, dy := event.ScreenX-drag.startX, event.ScreenY-drag.startY
			drag.dragging = math.Hypot(float64(dx), float64(dy)) >= float64(conversationDragThreshold)
		}
		if drag.dragging {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	case gfx.PointerUp:
		if drag.id == "" {
			return gfx.EventResult{}, false
		}
		id, dragging := drag.id, drag.dragging
		*drag = conversationDragState{}
		if !dragging {
			return gfx.EventResult{}, false
		}
		if u.pointerOutsideWindow(event) {
			u.requestDetachedConversation(id)
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerExit:
		// A captured drag keeps delivering moves after the pointer leaves. An
		// uncaptured exit means no drag started, so the next press begins clean.
		if !event.Captured {
			*drag = conversationDragState{}
		}
	}
	return gfx.EventResult{}, false
}

func (u *UI) conversationIDForHit(id gfx.ID) string {
	for conversationID, rowID := range u.convRowIDs {
		if rowID == id {
			return conversationID
		}
	}
	return ""
}

func (u *UI) pointerOutsideWindow(event gfx.PointerEvent) bool {
	if u.nativeWindow != nil {
		if frame, ok := u.nativeWindow.Frame(); ok && frame.Width > 0 && frame.Height > 0 {
			return event.ScreenX < frame.X || event.ScreenY < frame.Y ||
				event.ScreenX >= frame.X+frame.Width || event.ScreenY >= frame.Y+frame.Height
		}
	}
	return event.X < 0 || event.Y < 0 || event.X >= event.Viewport.Width || event.Y >= event.Viewport.Height
}

func (u *UI) requestDetachedConversation(conversationID string) bool {
	conversation := u.state.Conversation(conversationID)
	if conversation.ID == "" || conversation.Hidden || u.detached[conversationID] {
		return false
	}
	u.saveCurrentDraft()
	u.detached[conversationID] = true
	if u.presentDetached == nil || !u.presentDetached(conversationID) {
		delete(u.detached, conversationID)
		u.showToast(i18n.T("无法打开独立会话窗口"), i18n.T("请稍后重试。"), components.ToastWarning)
		u.invalidator.Invalidate()
		return false
	}
	if u.selected == conversationID {
		u.selected = ""
		for _, candidate := range u.conversations {
			if candidate.ID != conversationID && !u.detached[candidate.ID] {
				u.selected = candidate.ID
				break
			}
		}
		u.loadDraft(u.selected)
		u.rebuildSelection()
	}
	u.invalidator.Invalidate()
	return true
}

func (u *UI) restoreDetachedConversation(conversationID string) {
	if !u.detached[conversationID] {
		return
	}
	delete(u.detached, conversationID)
	u.invalidator.Invalidate()
}

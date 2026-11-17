package desktop

import (
	"errors"
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
	detachedTitlebarHeight    = 40
	conversationPreviewWidth  = 248
	conversationPreviewHeight = 54
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
		WindowMode:     app.WindowCustomTitlebar,
		TitlebarHeight: detachedTitlebarHeight,
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
	titlebar := ui.Rect{Width: bounds.Width, Height: min(detachedTitlebarHeight, bounds.Height)}
	body := ui.Rect{X: bounds.X, Y: titlebar.Bottom(), Width: bounds.Width, Height: max(bounds.Height-titlebar.Height, 0)}
	u.lastBody, u.lastChat = body, body

	u.drawDetachedTitlebar(canvas, titlebar)
	u.drawChatSurface(canvas, body, u.state.Conversation(u.selected))
	u.hover.Paint(canvas, u.theme, body, components.TooltipOptions{Layer: 55}, 0)
	u.drawToasts(canvas, body)
	u.drawOverlays(canvas, body)
	u.drawChatImageViewer(canvas)
	u.markSelectedConversationRead()
}

func (u *UI) drawDetachedTitlebar(canvas *gfx.Canvas, bounds ui.Rect) {
	if bounds.Height <= 0 {
		return
	}
	canvas.Fill(bounds, u.palette.rail)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Bottom() - 1, Width: bounds.Width, Height: 1}, u.palette.border)
	leading, trailing := app.TitlebarInsets()
	title := u.conversationTitle(u.state.Conversation(u.selected))
	if strings.TrimSpace(title) == "" {
		title = "GoIM"
	}
	u.titlePage.Draw(canvas, bounds, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().
			PadSides(leading+8, trailing+12, 4, 4).Items(ui.Center).Gap(8).Children(func() {
			pin := ui.Row(f).Size(30, 30).Radius(7).Items(ui.Center).Justify(ui.Center).
				Background(func() ui.Color {
					if u.detachedPinned {
						return u.palette.selected
					}
					return ui.Color{}
				}()).
				Hover(ui.Look{}.Background(u.palette.hover)).
				HitWith(u.ids.detachedPin, i18n.T("置顶会话窗口"), gfx.HitOptions{
					Role: gfx.RoleButton, Selected: u.detachedPinned,
				}).Children(func() {
				color := u.palette.icon
				if u.detachedPinned {
					color = u.palette.selectedText
				}
				ui.Icon(f, u.iconPin).Size(16, 16).Color(color)
			})
			if f.Canvas().FocusVisible() == u.ids.detachedPin {
				pin.Stroke(1.5, u.theme.Ring)
			}
			if pin.Clicked() {
				u.toggleDetachedPinned()
			}
			ui.Text(f, title).FontSize(14).Weight(gfx.WeightSemibold).Grow(1).Ellipsis()
		})
	})
}

func (u *UI) toggleDetachedPinned() {
	pinner, ok := u.nativeWindow.(interface{ SetAlwaysOnTop(bool) error })
	if !ok {
		u.showToast(i18n.T("无法更改会话窗口置顶"), i18n.T("当前系统不支持窗口置顶。"), components.ToastWarning)
		return
	}
	next := !u.detachedPinned
	if err := pinner.SetAlwaysOnTop(next); err != nil {
		u.showToast(i18n.T("无法更改会话窗口置顶"), err.Error(), components.ToastWarning)
		return
	}
	u.detachedPinned = next
	u.invalidator.Invalidate()
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
			title := u.conversationTitle(u.state.Conversation(id))
			*drag = conversationDragState{
				id: id, title: title, startX: event.ScreenX, startY: event.ScreenY,
				currentX: event.X, currentY: event.Y,
			}
		}
		return gfx.EventResult{}, false
	case gfx.PointerMove:
		if drag.id == "" || !event.Captured {
			return gfx.EventResult{}, false
		}
		if !drag.dragging {
			dx, dy := event.ScreenX-drag.startX, event.ScreenY-drag.startY
			drag.dragging = math.Hypot(float64(dx), float64(dy)) >= float64(conversationDragThreshold)
			if drag.dragging {
				u.openConversationDragPreview(drag, event)
			}
		}
		if drag.dragging {
			drag.currentX, drag.currentY = event.X, event.Y
			if drag.preview != nil {
				drag.preview.MoveTo(u.conversationDragPreviewOrigin(event))
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	case gfx.PointerUp:
		if drag.id == "" {
			return gfx.EventResult{}, false
		}
		id, dragging := drag.id, drag.dragging
		preview := drag.preview
		*drag = conversationDragState{}
		if preview != nil {
			preview.Close()
		}
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
			preview := drag.preview
			*drag = conversationDragState{}
			if preview != nil {
				preview.Close()
			}
		}
	}
	return gfx.EventResult{}, false
}

func (u *UI) openConversationDragPreview(drag *conversationDragState, event gfx.PointerEvent) {
	if drag == nil || drag.preview != nil || drag.previewOpenFail || u.openDragPreview == nil {
		return
	}
	title := compactConversationPreviewTitle(drag.title)
	palette := u.palette
	preview, err := u.openDragPreview(app.DragPreviewConfig{
		Width: conversationPreviewWidth, Height: conversationPreviewHeight,
		Origin: u.conversationDragPreviewOrigin(event),
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			card := ui.Rect{X: 4, Y: 4, Width: max(size.Width-8, 0), Height: max(size.Height-10, 0)}
			shadow := card
			shadow.Y += 3
			canvas.Rounded(shadow, 11, gfx.RGBA(0, 0, 0, 0.14))
			canvas.Rounded(card, 11, palette.sidebar)
			canvas.RoundedOutline(card, 11, 1, palette.border)
			mark := ui.Rect{X: card.X + 10, Y: card.Y + 9, Width: 30, Height: 30}
			paintGoIMMark(canvas, mark, titleMark)
			canvas.Label(title, ui.Rect{
				X: mark.Right() + 10, Y: card.Y + 9, Width: max(card.Width-60, 0), Height: 21,
			}, gfx.TextStyle{Size: 14, Weight: gfx.WeightSemibold, Color: palette.icon})
			canvas.Label(i18n.T("拖到窗口外打开独立会话"), ui.Rect{
				X: mark.Right() + 10, Y: card.Y + 30, Width: max(card.Width-60, 0), Height: 16,
			}, gfx.TextStyle{Size: 10.5, Color: palette.secondaryText})
		},
	})
	if err != nil {
		if !errors.Is(err, app.ErrNoDragPreview) {
			drag.previewOpenFail = true
		}
		return
	}
	drag.preview = preview
}

func (u *UI) conversationDragPreviewOrigin(event gfx.PointerEvent) gfx.Point {
	return gfx.Point{X: event.ScreenX - 30, Y: event.ScreenY - conversationPreviewHeight/2}
}

func compactConversationPreviewTitle(title string) string {
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) > 22 {
		return string(runes[:21]) + "…"
	}
	if title == "" {
		return "GoIM"
	}
	return title
}

func (u *UI) drawConversationDragGhost(canvas *gfx.Canvas, bounds ui.Rect) {
	drag := u.conversationDrag
	if !drag.dragging || drag.preview != nil || drag.id == "" {
		return
	}
	card := ui.Rect{
		X:     min(max(drag.currentX-30, bounds.X+4), bounds.Right()-conversationPreviewWidth-4),
		Y:     min(max(drag.currentY-conversationPreviewHeight/2, bounds.Y+4), bounds.Bottom()-conversationPreviewHeight-4),
		Width: conversationPreviewWidth, Height: conversationPreviewHeight,
	}
	canvas.Rounded(card, 11, u.palette.sidebar)
	canvas.RoundedOutline(card, 11, 1, u.palette.border)
	canvas.Label(compactConversationPreviewTitle(drag.title), ui.Rect{
		X: card.X + 50, Y: card.Y + 9, Width: card.Width - 60, Height: 21,
	}, gfx.TextStyle{Size: 14, Weight: gfx.WeightSemibold, Color: u.palette.icon})
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
	u.selected = ""
	u.detachedBlank = true
	u.loadDraft("")
	u.rebuildSelection()
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

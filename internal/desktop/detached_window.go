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
	detachedTitlebarHeight    = 40
	conversationPreviewWidth  = 248
	conversationPreviewHeight = 64
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

func (u *UI) detachedTitle() string {
	if !u.lockVisible() {
		if name := strings.TrimSpace(u.conversationTitle(u.state.Conversation(u.selected))); name != "" {
			return name + " · GoIM"
		}
	}
	return "GoIM"
}

func (u *UI) DetachedConfig() app.Config {
	clear := colorToApp(u.palette.sidebar)
	return app.Config{
		Title:          u.detachedTitle(),
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
		OnActivate:     u.windowActivated,
		OnDeactivate:   u.windowDeactivated,
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
	u.theme = themeForSettings(u.state.Settings)
	u.palette = paletteFor(u.theme, u.state.Settings.ColorScheme)
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.palette.chat)
	u.focusedID = canvas.Focused()
	u.emojiButton, u.mentionEditor, u.composerBox = ui.Box{}, ui.Box{}, ui.Box{}
	if u.lockVisible() {
		u.textSelection.clear()
		return
	}
	titlebar := ui.Rect{Width: bounds.Width, Height: min(detachedTitlebarHeight, bounds.Height)}
	body := ui.Rect{X: bounds.X, Y: titlebar.Bottom(), Width: bounds.Width, Height: max(bounds.Height-titlebar.Height, 0)}
	u.lastBody, u.lastChat = body, body

	u.drawDetachedTitlebar(canvas, titlebar)
	u.drawChat(canvas, body)
	u.hover.Paint(canvas, u.theme, body, components.TooltipOptions{Layer: 55}, 0)
	u.drawToasts(canvas, body)
	u.drawOverlays(canvas, body)
	u.drawChatImageViewer(canvas)
	u.markSelectedConversationRead()
}

func (u *UI) concealConversation(concealed bool) {
	u.detachedConcealed = concealed
	if concealed {
		u.closeImageViewer()
		u.closeChatPickers()
		u.closeShellMenu()
		u.closeDrawer()
		u.moreOpen = false
		u.confirm = confirmation{}
	}
	u.invalidator.Invalidate()
}

func (u *UI) drawDetachedTitlebar(canvas *gfx.Canvas, bounds ui.Rect) {
	if bounds.Height <= 0 {
		return
	}
	canvas.Fill(bounds, u.palette.rail)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Bottom() - 1, Width: bounds.Width, Height: 1}, u.palette.border)
	leading, trailing := canvas.TitlebarInsets()
	title := u.conversationTitle(u.state.Conversation(u.selected))
	if strings.TrimSpace(title) == "" {
		title = "GoIM"
	}
	u.titlePage.Draw(canvas, bounds, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().
			PadSides(4, trailing+12, 4, leading+8).Items(ui.Center).Gap(8).Children(func() {
			pin := ui.Row(f).Size(30, 30).Radius(radiusControl).Items(ui.Center).Justify(ui.Center).
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
			ui.Text(f, title).FontSize(textSize(f, 14)).Weight(gfx.WeightSemibold).Grow(1).Ellipsis()
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
			conversation := u.state.Conversation(id)
			*drag = conversationDragState{
				id: id, card: u.conversationCard(conversation), selected: id == u.selected,
				startX: event.ScreenX, startY: event.ScreenY,
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
	card, selected := drag.card, drag.selected
	preview, err := u.openDragPreview(app.DragPreviewConfig{
		Width: conversationPreviewWidth, Height: conversationPreviewHeight,
		Origin: u.conversationDragPreviewOrigin(event),
		Paint: func(canvas *gfx.Canvas, size gfx.Size) {
			bounds := ui.Rect{X: 4, Y: 3, Width: max(size.Width-8, 0), Height: max(size.Height-9, 0)}
			u.paintConversationDragCard(canvas, bounds, card, selected)
		},
	})
	if err != nil {
		// Probe once per gesture. An unavailable Linux compositor must not
		// reconnect to X11 on every pointer movement.
		drag.previewOpenFail = true
		return
	}
	drag.preview = preview
}

func (u *UI) conversationDragPreviewOrigin(event gfx.PointerEvent) gfx.Point {
	return gfx.Point{X: event.ScreenX - 30, Y: event.ScreenY - conversationPreviewHeight/2}
}

func (u *UI) drawConversationDragGhost(canvas *gfx.Canvas, bounds ui.Rect) {
	drag := u.conversationDrag
	if !drag.dragging || drag.preview != nil || drag.id == "" {
		return
	}
	outer := ui.Rect{
		X:     min(max(drag.currentX-30, bounds.X+4), bounds.Right()-conversationPreviewWidth-4),
		Y:     min(max(drag.currentY-conversationPreviewHeight/2, bounds.Y+4), bounds.Bottom()-conversationPreviewHeight-4),
		Width: conversationPreviewWidth, Height: conversationPreviewHeight,
	}
	card := ui.Rect{X: outer.X + 4, Y: outer.Y + 3, Width: outer.Width - 8, Height: outer.Height - 9}
	u.paintConversationDragCard(canvas, card, drag.card, drag.selected)
}

func (u *UI) paintConversationDragCard(canvas *gfx.Canvas, card ui.Rect, data conversationCardData, selected bool) {
	if card.Width <= 0 || card.Height <= 0 || u.previewPage == nil {
		return
	}
	shadow := card
	shadow.Y += 3
	canvas.Rounded(shadow, radiusCard, gfx.RGBA(0, 0, 0, 0.14))
	fill := u.palette.sidebar
	if selected {
		fill = u.palette.selected
	}
	canvas.Rounded(card, radiusCard, fill)
	canvas.RoundedOutline(card, radiusCard, 1, u.palette.border)
	u.previewPage.Draw(canvas, card, func(f ui.C) {
		ui.Row(f).Fill().PadSides(8, 7, 8, 7).Items(ui.Center).Gap(9).Children(func() {
			u.conversationCardBody(f, data)
		})
	})
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

func (u *UI) showDetachedConversation(conversationID string) {
	if !u.detached[conversationID] {
		u.requestDetachedConversation(conversationID)
		return
	}
	u.saveCurrentDraft()
	u.selected = ""
	u.detachedBlank = true
	u.loadDraft("")
	u.rebuildSelection()
	if u.presentDetached != nil {
		u.presentDetached(conversationID)
	}
	u.invalidator.Invalidate()
}

func (u *UI) restoreDetachedConversation(conversationID string) {
	if !u.detached[conversationID] {
		return
	}
	delete(u.detached, conversationID)
	u.invalidator.Invalidate()
}

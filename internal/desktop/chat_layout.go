package desktop

import (
	"github.com/vivaui/viva/gfx"
	"goim/internal/i18n"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

const (
	chatSidebarMinWidth   = float32(240)
	chatSidebarMaxWidth   = float32(420)
	chatWorkspaceMinWidth = float32(620)
	chatHistoryMinHeight  = float32(200)
	chatComposerMinHeight = float32(132)
	chatReadOnlyMinHeight = float32(104)
	chatSplitterExtent    = float32(8)
	chatFilesMinWidth     = float32(260)
	chatFilesMaxWidth     = float32(400)
	chatHeaderExtent      = float32(64)
	chatDividerExtent     = float32(1)
	chatSearchExtent      = float32(53)
	chatComposerDefault   = float32(148)
)

type chatSurfaceIDs struct {
	viewerScope                             gfx.ID
	viewerStage                             gfx.ID
	viewerClose                             gfx.ID
	viewerPrevious                          gfx.ID
	viewerNext                              gfx.ID
	viewerFit                               gfx.ID
	viewerActual                            gfx.ID
	viewerZoomOut                           gfx.ID
	viewerZoomIn                            gfx.ID
	viewerRotate                            gfx.ID
	viewerPin, viewerFullscreen, viewerSave gfx.ID
	viewerMenu                              viewerMenuIDs
}

type chatSurface struct {
	ids       chatSurfaceIDs
	workspace *ui.Page
	sidebar   ui.SplitterState
	history   ui.SplitterState

	historyReady   bool
	searchOpen     bool
	historyBefore  float32
	historyClamp   float32
	historyRestore float32

	sidebarWidth float32
	filesPane    ui.Rect
	maximized    bool
	fullscreen   bool
	viewer       imageViewerState

	filesWindowRestoreHeight int
	filesWindowRestoreWidth  int
	filesWindowRestoreValid  bool
	filesWindowExpanded      bool
}

type windowGeometry interface {
	Frame() (ui.Rect, bool)
	SetSize(width, height int) error
}

func (u *UI) newChatSurface(workspace *ui.Page) *chatSurface {
	return &chatSurface{
		workspace: workspace,
		sidebar:   ui.SplitterState{Pos: sidebarWidth},
		ids: chatSurfaceIDs{
			viewerScope:      u.tree.ID(u.ids.dialog, "image-viewer-scope"),
			viewerStage:      u.tree.ID(u.ids.dialog, "image-viewer-stage"),
			viewerClose:      u.tree.ID(u.ids.dialog, "image-viewer-close"),
			viewerPrevious:   u.tree.ID(u.ids.dialog, "image-viewer-previous"),
			viewerNext:       u.tree.ID(u.ids.dialog, "image-viewer-next"),
			viewerFit:        u.tree.ID(u.ids.dialog, "image-viewer-fit"),
			viewerActual:     u.tree.ID(u.ids.dialog, "image-viewer-actual"),
			viewerZoomOut:    u.tree.ID(u.ids.dialog, "image-viewer-zoom-out"),
			viewerZoomIn:     u.tree.ID(u.ids.dialog, "image-viewer-zoom-in"),
			viewerRotate:     u.tree.ID(u.ids.dialog, "image-viewer-rotate"),
			viewerPin:        u.tree.ID(u.ids.dialog, "image-viewer-pin"),
			viewerFullscreen: u.tree.ID(u.ids.dialog, "image-viewer-fullscreen"),
			viewerSave:       u.tree.ID(u.ids.dialog, "image-viewer-save"),
			viewerMenu:       newViewerMenuIDs(&u.tree, u.ids.dialog),
		},
	}
}

// drawMainBody owns the rail and the resizable workspace between the rail,
// contact list, chat, and panel sections. The native shell calls it once per
// paint after the title bar and lock screen.
func (u *UI) drawMainBody(canvas *gfx.Canvas, body ui.Rect) {
	rail := ui.Rect{
		X: body.X, Y: body.Y,
		Width: min(railWidth, body.Width), Height: body.Height,
	}
	u.drawRail(canvas, rail)
	if u.isPanelSection() {
		if u.presentViewer == nil && u.surface.viewer.open {
			u.closeImageViewer()
		}
		main := ui.Rect{
			X: railWidth, Y: body.Y,
			Width: max(body.Width-railWidth, 0), Height: body.Height,
		}
		u.panels.Draw(canvas, u.theme, main, u.section, u.state, u.submit)
		return
	}
	u.drawChatWorkspace(canvas, body)
}

func (u *UI) drawChatWorkspace(canvas *gfx.Canvas, body ui.Rect) {
	surface := u.surface
	surface.clampSidebar()
	workspace := ui.Rect{
		X: min(railWidth, body.Width), Y: body.Y,
		Width: max(body.Width-railWidth, 0), Height: body.Height,
	}
	var sidebar, chat ui.Box
	surface.workspace.Draw(canvas, workspace, func(f ui.C) {
		ui.Row(f).Fill().Children(func() {
			sidebar = ui.Row(f).Width(surface.sidebar.Pos)
			ui.Splitter(f, &surface.sidebar).
				Mins(chatSidebarMinWidth, chatWorkspaceMinWidth).
				Label(i18n.T("调整联系人和聊天区域宽度"))
			chat = ui.Row(f).Grow(1)
		})
	})

	sidebarBounds := sidebar.Rect()
	chatBounds := chat.Rect()
	surface.sidebarWidth = sidebarBounds.Width
	u.drawSidebar(canvas, sidebarBounds)
	if sidebarBounds.Width > 1 {
		// The splitter is the single divider between the two panes.
		canvas.Fill(ui.Rect{
			X: sidebarBounds.X + sidebarBounds.Width - 1, Y: sidebarBounds.Y,
			Width: 1, Height: sidebarBounds.Height,
		}, u.palette.sidebar)
	}
	if u.section == sectionContacts {
		u.drawContactDetails(canvas, chatBounds)
	} else {
		u.drawChat(canvas, chatBounds)
	}
	u.lastChat = chatBounds
}

func (s *chatSurface) clampSidebar() {
	if s.sidebar.Pos <= 0 {
		s.sidebar.Pos = sidebarWidth
	}
	s.sidebar.Pos = min(max(s.sidebar.Pos, chatSidebarMinWidth), chatSidebarMaxWidth)
}

func (u *UI) chatSidebarBoundary() float32 {
	width := u.surface.sidebarWidth
	if width <= 0 {
		width = sidebarWidth
	}
	return railWidth + width
}

func (u *UI) prepareChatHistory(chatHeight float32, conversation domain.Conversation, searchDeclared bool) {
	surface := u.surface
	if surface.historyReady {
		u.clampChatHistory(chatHeight, conversation, searchDeclared)
		return
	}
	chrome := chatHeaderExtent + chatDividerExtent
	composer := chatComposerDefault
	if u.chatReadOnlyReason(conversation) != "" {
		composer = chatReadOnlyMinHeight
	}
	height := chatHeight - chrome - chatSplitterExtent - composer
	surface.history.Pos = max(chatHistoryMinHeight, height)
	surface.historyReady = true
	u.clampChatHistory(chatHeight, conversation, searchDeclared)
}

// clampChatHistory keeps the history pane out of the composer when the
// conversation search bar appears. The size the user dragged to comes back
// when search closes, unless they moved the splitter while it was open.
func (u *UI) clampChatHistory(chatHeight float32, conversation domain.Conversation, searchDeclared bool) {
	surface := u.surface
	open := u.chatFind.open
	if open != surface.searchOpen {
		if open {
			surface.historyBefore = surface.history.Pos
			surface.historyRestore = 0
		} else if surface.historyClamp > 0 && surface.history.Pos == surface.historyClamp {
			surface.historyRestore = surface.historyBefore
		}
		surface.searchOpen = open
		surface.historyClamp = 0
	}
	if !open && !searchDeclared && surface.historyRestore > 0 {
		surface.history.Pos = max(surface.historyRestore, chatHistoryMinHeight)
		surface.historyRestore = 0
	}
	chrome := chatHeaderExtent + chatDividerExtent
	if open || searchDeclared {
		chrome += chatSearchExtent
	}
	composer := chatComposerMinHeight
	if u.chatReadOnlyReason(conversation) != "" {
		composer = chatReadOnlyMinHeight
	}
	maxHistory := chatHeight - chrome - chatSplitterExtent - composer
	if maxHistory > 0 && surface.history.Pos > maxHistory {
		surface.history.Pos = max(maxHistory, chatHistoryMinHeight)
		surface.historyClamp = surface.history.Pos
	}
}

func (u *UI) chatComposerMinimum(conversation domain.Conversation) float32 {
	if u.chatReadOnlyReason(conversation) != "" {
		return chatReadOnlyMinHeight
	}
	return chatComposerMinHeight
}

// chatOverlayKey runs before the shell's normal keyboard routing. It keeps the
// image viewer modal, closes the docked files pane, and lets a focused splitter
// resize either side with the keyboard.
func (u *UI) chatOverlayKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if u.lockVisible() {
		return gfx.EventResult{}, false
	}
	if u.imageViewerOpen() {
		return u.imageViewerKey(event), true
	}
	if u.modalOpen() {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape && u.drawer == "files" {
		if u.activeFileID != "" && u.fileReturn == "list" {
			u.activeFileID = ""
			u.fileReturn = ""
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		u.closeDrawer()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	surface := u.surface
	if result := surface.sidebar.Key(event); result.Handled {
		return result, true
	}
	if result := surface.history.Key(event); result.Handled {
		return result, true
	}
	return gfx.EventResult{}, false
}

func (u *UI) chatOverlayShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if u.lockVisible() {
		return gfx.EventResult{}, false
	}
	if u.imageViewerOpen() {
		return u.imageViewerKey(event), true
	}
	return gfx.EventResult{}, false
}

func (u *UI) chatOverlayWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if u.lockVisible() {
		return gfx.EventResult{}, false
	}
	if u.imageViewerOpen() {
		return u.imageViewerWheel(event), true
	}
	if u.modalOpen() || u.isPanelSection() {
		return gfx.EventResult{}, false
	}
	if u.drawer == "files" {
		surface := u.surface
		if pointInside(event.X, event.Y, surface.filesPane) {
			return u.drawerPage.Wheel(event), true
		}
		if event.X < u.chatSidebarBoundary() {
			return u.sidebarPage.Wheel(event), true
		}
		return u.chatPage.Wheel(event), true
	}
	return gfx.EventResult{}, false
}

// setWindowMaximized records the state of the shell's frame. A normal window is
// expanded by the files-pane width; maximized windows keep their frame.
func (u *UI) setWindowMaximized(maximized bool) {
	surface := u.surface
	surface.maximized = maximized
	if !maximized {
		u.syncFilesWindow()
	} else {
		surface.filesWindowExpanded = false
	}
}

func (u *UI) setWindowFullscreen(fullscreen bool) {
	surface := u.surface
	surface.fullscreen = fullscreen
	if fullscreen {
		surface.filesWindowExpanded = false
	} else {
		u.syncFilesWindow()
	}
}

func (u *UI) attachNativeWindow(window windowGeometry) {
	u.nativeWindow = window
}

// syncFilesWindow expands a normal window by the width of the docked files
// pane, then restores the exact content size when the pane closes. Maximized and
// fullscreen windows keep their frame and let the pane compress the chat side.
func (u *UI) syncFilesWindow() {
	surface := u.surface
	open := u.drawer == "files" && u.state.Conversation(u.selected).ID != ""
	if !open {
		if surface.filesWindowRestoreValid && !surface.maximized && !surface.fullscreen && u.nativeWindow != nil {
			_ = u.nativeWindow.SetSize(surface.filesWindowRestoreWidth, surface.filesWindowRestoreHeight)
		}
		surface.filesWindowRestoreValid = false
		surface.filesWindowExpanded = false
		return
	}
	if surface.maximized || surface.fullscreen || surface.filesWindowExpanded || surface.filesWindowRestoreValid || u.nativeWindow == nil {
		return
	}
	frame, ok := u.nativeWindow.Frame()
	if !ok || frame.Width < 1 || frame.Height < 1 {
		return
	}
	width, height := int(frame.Width), int(frame.Height)
	extra := int(chatFilesPaneWidth(frame.Width))
	if extra < 1 {
		return
	}
	surface.filesWindowRestoreWidth, surface.filesWindowRestoreHeight = width, height
	surface.filesWindowRestoreValid = true
	if err := u.nativeWindow.SetSize(width+extra, height); err != nil {
		surface.filesWindowRestoreValid = false
		return
	}
	surface.filesWindowExpanded = true
}

// chatFilesPaneWidth gives the docked file pane a readable share of the chat
// column, bounded so both sides remain usable at the minimum window size.
func chatFilesPaneWidth(chatWidth float32) float32 {
	if chatWidth <= 0 {
		return 0
	}
	width := min(max(chatWidth*0.38, chatFilesMinWidth), chatFilesMaxWidth)
	return min(width, chatWidth)
}

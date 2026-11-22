package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

const (
	chatSidebarMinWidth      = float32(240)
	chatSidebarMaxWidth      = float32(420)
	chatWorkspaceMinWidth    = float32(620)
	chatHistoryMinHeight     = float32(200)
	chatComposerToolHeight   = float32(40)
	chatComposerFooterHeight = float32(42)
	chatEditorMinHeight      = float32(64)
	chatComposerMinHeight    = chatComposerToolHeight + chatEditorMinHeight + chatComposerFooterHeight
	chatComposerDefaultMax   = float32(260)
	chatReadOnlyMinHeight    = float32(104)
	chatSplitterExtent       = float32(8)
	chatFilesMinWidth        = float32(260)
	chatFilesMaxWidth        = float32(420)
	chatHeaderExtent         = float32(60)
	chatSearchExtent         = float32(52)
	chatContentInset         = float32(16)
)

type chatSurfaceIDs struct {
	viewerScope                             gfx.ID
	viewerStage                             gfx.ID
	videoPlay, videoBack, videoForward      gfx.ID
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

	historySpan       float32
	historyPlaced     float32
	composerPreferred float32

	sidebarWidth float32
	filesPane    ui.Rect
	maximized    bool
	fullscreen   bool
	viewer       imageViewerState
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
			viewerScope: u.tree.ID(u.ids.dialog, "image-viewer-scope"),
			videoPlay:   u.tree.ID(u.ids.dialog, "video-play"), videoBack: u.tree.ID(u.ids.dialog, "video-back"), videoForward: u.tree.ID(u.ids.dialog, "video-forward"),
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
		Width: min(u.railExtent(), body.Width), Height: body.Height,
	}
	u.drawRail(canvas, rail)
	if u.isPanelSection() {
		main := ui.Rect{
			X: u.railExtent(), Y: body.Y,
			Width: max(body.Width-u.railExtent(), 0), Height: body.Height,
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
		X: min(u.railExtent(), body.Width), Y: body.Y,
		Width: max(body.Width-u.railExtent(), 0), Height: body.Height,
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
	return u.railExtent() + width
}

func (s *chatSurface) layoutHistory(chatHeight, composerMinimum float32, searchVisible bool) {
	if s.historySpan > 0 && s.history.Pos != s.historyPlaced {
		s.composerPreferred = s.historySpan - s.history.Pos
	}
	span := chatHeight - chatHeaderExtent - chatSplitterExtent
	if searchVisible {
		span -= chatSearchExtent
	}
	preferred := s.composerPreferred
	if preferred <= 0 {
		preferred = composerMinimum
		if composerMinimum == chatComposerMinHeight {
			preferred = min(chatHeight*0.29, chatComposerDefaultMax)
		}
	}
	s.historySpan = span
	s.historyPlaced = max(chatHistoryMinHeight, span-max(composerMinimum, preferred))
	s.history.Pos = s.historyPlaced
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
	if u.drawer != "" {
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

func (u *UI) setWindowMaximized(maximized bool) {
	u.surface.maximized = maximized
}

func (u *UI) setWindowFullscreen(fullscreen bool) {
	u.surface.fullscreen = fullscreen
}

func (u *UI) attachNativeWindow(window windowGeometry) {
	u.nativeWindow = window
	if topmost, ok := window.(interface{ AlwaysOnTop() bool }); ok {
		u.mainPinned = topmost.AlwaysOnTop()
	}
}

package desktop

import (
	"github.com/vivaui/viva/gfx"
	"goim/internal/media"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type imageViewerState struct {
	open           bool
	fileID         string
	conversationID string
	fromTransfer   bool
	fromDraft      bool // images holds the selected draft's local file

	pinned        bool
	menuOpen      bool
	menuAtPointer bool
	menuAt        ui.Rect
	video         *media.Player
	videoPosition time.Duration
	videoFrame    gfx.Bitmap
	videoError    string
	videoEnded    bool
	videoID       uint64
	hover         components.HoverTooltip
	fullscreen    bool
	fit           bool
	zoom          float32
	rotation      int
	panX          float32
	panY          float32
	fitScale      float32
	imageRect     ui.Rect
	drag          bool
	dragX         float32
	dragY         float32

	original     gfx.Bitmap
	originalPath string
	originalTurn int
	originalGen  uint64

	cached             gfx.Bitmap
	cachedSourceID     uint64
	cachedSourceVer    uint32
	cachedRotation     int
	images             []domain.File
	imagesRevision     uint64
	imagesConversation string
}

func (u *UI) imageViewerOpen() bool {
	v := &u.surface.viewer
	if u.presentViewer != nil || !v.open || u.lockVisible() {
		return false
	}
	if v.fromTransfer {
		return u.section == sectionTransfers
	}
	return !u.isPanelSection() && u.selected != "" && v.conversationID == u.selected
}

func (u *UI) openImageViewer(fileID string) {
	if fileID == "" || u.selected == "" || u.isPanelSection() {
		return
	}
	if u.forwardViewer != nil {
		u.forwardViewer(fileID)
		return
	}
	u.openConversationMedia(fileID, u.selected)
}
func (u *UI) openConversationMedia(fileID, conversation string) {
	u.openMediaViewer(imageViewerState{fileID: fileID, conversationID: conversation})
}

func (u *UI) openTransferMedia(file domain.File) {
	u.openMediaViewer(imageViewerState{fileID: file.ID, fromTransfer: true, images: []domain.File{file}})
}

func (u *UI) viewerFile() domain.File {
	v := &u.surface.viewer
	if v.fromTransfer || v.fromDraft {
		return v.images[0]
	}
	return u.state.File(v.fileID)
}

func (u *UI) viewerPath() string {
	v := &u.surface.viewer
	if v.fromTransfer || v.fromDraft {
		return v.images[0].SourcePath
	}
	return u.fileLocalPath[v.fileID]
}

func (u *UI) openMediaViewer(view imageViewerState) {
	if u.lockVisible() {
		return
	}
	u.surface.viewer.video.Stop()
	u.closeChatPickers()
	u.moreOpen = false
	u.memberMenu = ""
	view.open, view.fit, view.zoom, view.fitScale = true, true, 1, 1
	u.surface.viewer = view
	if u.viewerFile().Video() {
		u.startViewerVideo(0)
	} else if path := u.viewerPath(); path != "" {
		u.ensureImage(path)
	}
	u.queueViewerOriginal()
	if u.presentViewer != nil {
		u.presentViewer()
	}
}

func (u *UI) closeImageViewer() {
	surface := u.surface
	if surface.viewer.fromDraft && surface.viewer.conversationID == u.selected {
		u.focusChat = true
		u.invalidator.Invalidate()
	}
	surface.viewer.video.Stop()
	surface.viewer.video = nil
	surface.viewer.videoFrame = gfx.Bitmap{}
	surface.viewer.open = false
	surface.viewer.drag = false
	surface.viewer.cached = gfx.Bitmap{}
	surface.viewer.original = gfx.Bitmap{}
	surface.viewer.originalPath = ""
	surface.viewer.originalGen = 0
	surface.viewer.fileID = ""
	surface.viewer.conversationID = ""
	surface.viewer.fromTransfer = false
	surface.viewer.fromDraft = false
	surface.viewer.images = surface.viewer.images[:0]
	surface.viewer.imagesConversation = ""
	if u.dismissViewer != nil {
		u.dismissViewer()
	}
}

func (u *UI) viewerImageFiles(state *imageViewerState) []domain.File {
	if state.fromTransfer || state.fromDraft {
		return state.images
	}
	if state.imagesConversation == state.conversationID && state.imagesRevision == u.state.Revision {
		return state.images
	}
	state.images = state.images[:0]
	for _, file := range u.state.Files {
		if file.ConversationID == state.conversationID && (fileIsImage(file) || file.Video()) {
			state.images = append(state.images, file)
		}
	}
	sort.SliceStable(state.images, func(i, j int) bool {
		left, right := state.images[i], state.images[j]
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})
	state.imagesConversation = state.conversationID
	state.imagesRevision = u.state.Revision
	return state.images
}

func viewerFileIndex(files []domain.File, id string) int {
	for index := range files {
		if files[index].ID == id {
			return index
		}
	}
	return -1
}

func (u *UI) navigateImageViewer(offset int) {
	state := &u.surface.viewer
	if !state.open {
		return
	}
	files := u.viewerImageFiles(state)
	index := viewerFileIndex(files, state.fileID)
	if index < 0 {
		u.closeImageViewer()
		return
	}
	next := index + offset
	if next < 0 || next >= len(files) {
		return
	}
	u.selectImageViewerFile(files[next].ID)
}

func (u *UI) selectImageViewerFile(fileID string) {
	state := &u.surface.viewer
	state.video.Stop()
	state.video = nil
	state.videoFrame = gfx.Bitmap{}
	state.videoError = ""
	state.videoEnded = false
	state.videoPosition = 0
	state.fileID = fileID
	state.fit = true
	state.zoom = 1
	state.rotation = 0
	state.panX = 0
	state.panY = 0
	state.drag = false
	state.cached = gfx.Bitmap{}
	if u.viewerFile().Video() {
		u.startViewerVideo(0)
		return
	}
	u.queueViewerOriginal()
	if path := u.viewerPath(); path != "" {
		u.ensureImage(path)
		if bitmap := u.fileBitmap(path); bitmapReady(bitmap) {
			u.viewerRotatedBitmap(state, bitmap)
		}
	}
}

func (u *UI) fitImageViewer() {
	state := &u.surface.viewer
	state.fit = true
	state.panX = 0
	state.panY = 0
}

func (u *UI) showImageViewerActualSize() {
	state := &u.surface.viewer
	state.fit = false
	state.zoom = 1
	state.panX = 0
	state.panY = 0
}

func (u *UI) zoomImageViewer(factor float32) {
	state := &u.surface.viewer
	current := state.zoom
	if state.fit && state.fitScale > 0 {
		current = state.fitScale
	}
	if current <= 0 {
		current = 1
	}
	next := min(max(current*factor, 0.05), 8)
	if factor > 1 && next < current {
		next = current
	}
	state.fit = false
	state.zoom = next
}

func (u *UI) rotateImageViewer(direction int) {
	state := &u.surface.viewer
	state.rotation = (state.rotation + direction + 4) % 4
	state.cached = gfx.Bitmap{}
	state.panX = 0
	state.panY = 0
	u.queueViewerOriginal()
	if path := u.viewerPath(); path != "" {
		if bitmap := u.fileBitmap(path); bitmapReady(bitmap) {
			u.viewerRotatedBitmap(state, bitmap)
		}
	}
}

func (u *UI) imageViewerKey(event gfx.KeyEvent) gfx.EventResult {
	state := &u.surface.viewer
	if !state.open || event.Release {
		return gfx.EventResult{}
	}
	if state.menuOpen {
		if event.Key == gfx.KeyEscape {
			state.menuOpen = false
			return gfx.EventResult{Handled: true, Repaint: true, Focus: u.surface.ids.viewerMenu.trigger}
		}
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeySpace && u.viewerFile().Video() {
		if !event.Repeat {
			u.toggleViewerVideo()
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	switch event.Key {
	case gfx.KeyTab:
		return gfx.EventResult{}
	case gfx.KeyEnter, gfx.KeySpace:
		return gfx.EventResult{}
	case gfx.KeyEscape:
		if state.fullscreen && u.fullscreenViewer != nil {
			if u.fullscreenViewer(false) {
				state.fullscreen = false
			}
		} else {
			u.closeImageViewer()
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.KeyLeft, gfx.KeyPageUp:
		u.navigateImageViewer(-1)
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.KeyRight, gfx.KeyPageDown:
		u.navigateImageViewer(1)
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.KeyHome:
		files := u.viewerImageFiles(state)
		if len(files) > 0 {
			u.selectImageViewerFile(files[0].ID)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.KeyEnd:
		files := u.viewerImageFiles(state)
		if len(files) > 0 {
			u.selectImageViewerFile(files[len(files)-1].ID)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0 && strings.EqualFold(event.Text, "s") && u.saveViewer != nil {
		if !event.Repeat {
			u.saveViewer(u.viewerPath())
		}
		return gfx.EventResult{Handled: true}
	}
	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta|gfx.ModifierAlt) != 0 {
		return gfx.EventResult{Handled: true}
	}
	if u.viewerFile().Video() {
		return gfx.EventResult{}
	}
	switch strings.ToLower(event.Text) {
	case "0", "f":
		u.fitImageViewer()
	case "1":
		u.showImageViewerActualSize()
	case "+", "=":
		u.zoomImageViewer(1.25)
	case "-", "_":
		u.zoomImageViewer(0.8)
	case "r":
		direction := 1
		if event.Modifiers&gfx.ModifierShift != 0 {
			direction = -1
		}
		u.rotateImageViewer(direction)
	default:
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (u *UI) imageViewerWheel(event gfx.ScrollEvent) gfx.EventResult {
	if !u.surface.viewer.open || u.viewerFile().Video() || event.DeltaY == 0 {
		return gfx.EventResult{Handled: true}
	}
	factor := float32(math.Exp(-float64(event.DeltaY) / 240))
	u.zoomImageViewer(factor)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (u *UI) drawChatImageViewer(canvas *gfx.Canvas) {
	if u.presentViewer == nil {
		u.drawImageViewer(canvas, u.lastBody, false)
	}
}

func (u *UI) drawImageViewer(canvas *gfx.Canvas, viewport ui.Rect, detached bool) {
	state := &u.surface.viewer
	if !state.open {
		return
	}
	if !detached && !u.imageViewerOpen() {
		u.closeImageViewer()
		return
	}
	files := u.viewerImageFiles(state)
	index := viewerFileIndex(files, state.fileID)
	if index < 0 {
		u.closeImageViewer()
		return
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	ids := u.surface.ids
	canvas.PushLayer(80)
	defer canvas.PopLayer()
	theme := ui.DarkTheme()
	background := ui.RGB(8, 10, 14)
	muted := ui.RGB(167, 174, 186)
	if u.isDark() {
		theme, background, muted = u.theme, u.palette.chat, u.palette.secondaryText
	}
	topHeight, bottomHeight := float32(58), float32(82)
	if detached {
		theme = themeForSettings(u.state.Settings)
		colors := paletteFor(theme, u.state.Settings.ColorScheme)
		background, muted = colors.chat, colors.secondaryText
		topHeight, bottomHeight = viewerTitlebarHeight, 26
	} else {
		canvas.Block(viewport)
	}
	canvas.Fill(viewport, background)
	canvas.PushFocusScope(ids.viewerScope)
	defer canvas.PopFocusScope()

	top := ui.Rect{X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: topHeight}
	stage := ui.Rect{
		X: viewport.X, Y: viewport.Y + top.Height,
		Width: viewport.Width, Height: max(viewport.Height-top.Height-bottomHeight, 1),
	}
	file := files[index]
	path := u.viewerPath()
	source := u.fileBitmap(path)
	bitmap := source
	if detached && state.originalPath == path && state.originalTurn == state.rotation && bitmapReady(state.original) {
		bitmap = state.original
	} else if bitmapReady(source) {
		bitmap = u.viewerRotatedBitmap(state, source)
	}

	if !detached {
		canvas.Fill(top, ui.RGBA(255, 255, 255, 0.035))
		canvas.Label(
			intString(index+1)+" / "+intString(len(files)),
			ui.Rect{X: viewport.X + 20, Y: top.Y, Width: 90, Height: top.Height},
			gfx.TextStyle{Size: u.theme.Metrics.ScaledText(12), Color: ui.RGBA(167, 174, 186, 1)},
		)
		titleWidth := max(viewport.Width-260, 160)
		canvas.LabelCenter(
			file.Name,
			ui.Rect{X: viewport.X + 110, Y: top.Y, Width: titleWidth, Height: top.Height},
			gfx.TextStyle{Size: u.theme.Metrics.ScaledText(14), Weight: gfx.WeightMedium, Color: ui.RGBA(238, 241, 247, 1)},
		)

	}
	if detached {
		canvas.LabelCenter(file.Name, ui.Rect{X: 12, Y: stage.Bottom(), Width: max(viewport.Width-24, 1), Height: bottomHeight}, gfx.TextStyle{Size: u.theme.Metrics.ScaledText(12), Color: muted})
	}
	if file.Video() {
		u.drawViewerVideo(canvas, stage, theme, muted)
	} else if bitmapReady(bitmap) {
		imageRect := u.viewerImageRect(state, stage, bitmap)
		state.imageRect = imageRect
		canvas.Clip(stage)
		canvas.DrawBitmap(bitmap, imageRect)
		canvas.Unclip()
	} else {
		state.imageRect = ui.Rect{}
		message := i18n.T("正在加载图片…")
		if path == "" {
			message = i18n.T("图片尚未保存到本机")
		} else if _, checked := u.imageStamps[path]; checked {
			message = i18n.T("无法读取这张图片")
		}
		canvas.LabelCenter(message, stage, gfx.TextStyle{
			Size: u.theme.Metrics.ScaledText(14), Color: muted,
		})
	}

	cursor := gfx.PointerCursorDefault
	if state.imageRect.Width > stage.Width || state.imageRect.Height > stage.Height {
		cursor = gfx.PointerCursorHand
	}
	canvas.HitWithOptions(ids.viewerStage, stage, i18n.T("拖动查看图片"), gfx.HitOptions{
		Role: gfx.RoleGroup, SkipFocus: true, Cursor: cursor,
	})
	if event, ok := canvas.PointerOn(ids.viewerStage); ok {
		if detached && event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
			state.menuOpen, state.menuAtPointer = true, true
			state.menuAt = ui.Rect{X: event.X, Y: event.Y}
			state.drag = false
			canvas.Consume()
			canvas.RequestFrame()
		} else if !file.Video() {
			u.handleImageViewerPointer(canvas, state, event, stage, detached)
		}
	}

	if file.Video() {
		u.drawVideoControls(canvas, stage, theme)
	}
	if detached {
		u.drawViewerTitlebar(canvas, top, viewport, theme, ids, state, index, len(files), path)
		if !state.menuOpen {
			state.hover.Paint(canvas, theme, viewport, components.TooltipOptions{Layer: 82}, 0)
		}
		return
	}
	previousEnabled := index > 0
	nextEnabled := index+1 < len(files)
	arrowY := stage.Y + stage.Height/2 - 25
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + 22, Y: arrowY, Width: 50, Height: 50},
		ids.viewerPrevious, i18n.T("上一张图片（←）"), u.iconChevronRight,
		gfx.IconTransform{Rotate: float32(math.Pi)}, !previousEnabled, false) {
		u.navigateImageViewer(-1)
	}
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + viewport.Width - 72, Y: arrowY, Width: 50, Height: 50},
		ids.viewerNext, i18n.T("下一张图片（→）"), u.iconChevronRight,
		gfx.IconTransform{}, !nextEnabled, false) {
		u.navigateImageViewer(1)
	}
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + viewport.Width - 58, Y: viewport.Y + 10, Width: 38, Height: 38},
		ids.viewerClose, i18n.T("关闭图片查看器（Esc）"), u.iconX,
		gfx.IconTransform{}, false, false) {
		u.closeImageViewer()
		return
	}

	u.drawImageViewerToolbar(canvas, viewport, ids, state)
	canvas.TrapFocus(ids.viewerScope)
	canvas.SetInitialFocus(ids.viewerClose)
	canvas.SetEscapeTarget(ids.viewerClose)
}

func (u *UI) viewerRotatedBitmap(state *imageViewerState, source gfx.Bitmap) gfx.Bitmap {
	turns := state.rotation % 4
	if turns < 0 {
		turns += 4
	}
	if turns == 0 {
		state.cached = gfx.Bitmap{}
		return source
	}
	if bitmapReady(state.cached) && state.cachedRotation == turns &&
		state.cachedSourceID == source.ID && state.cachedSourceVer == source.Version {
		return state.cached
	}
	rotated := rotateBitmapQuarter(source, turns)
	if bitmapReady(rotated) {
		u.nextImageID++
		rotated.ID = u.nextImageID
		rotated.Version = 1
	}
	state.cached = rotated
	state.cachedRotation = turns
	state.cachedSourceID = source.ID
	state.cachedSourceVer = source.Version
	return rotated
}

func rotateBitmapQuarter(source gfx.Bitmap, turns int) gfx.Bitmap {
	turns = ((turns % 4) + 4) % 4
	if turns == 0 || !bitmapReady(source) {
		return source
	}
	sourceWidth, sourceHeight := source.Width, source.Height
	width, height := sourceWidth, sourceHeight
	if turns%2 != 0 {
		width, height = sourceHeight, sourceWidth
	}
	pixels := make([]byte, width*height*4)
	for y := 0; y < sourceHeight; y++ {
		for x := 0; x < sourceWidth; x++ {
			targetX, targetY := x, y
			switch turns {
			case 1:
				targetX, targetY = sourceHeight-1-y, x
			case 2:
				targetX, targetY = sourceWidth-1-x, sourceHeight-1-y
			case 3:
				targetX, targetY = y, sourceWidth-1-x
			}
			sourceOffset := y*source.Stride + x*4
			targetOffset := targetY*width*4 + targetX*4
			copy(pixels[targetOffset:targetOffset+4], source.Pix[sourceOffset:sourceOffset+4])
		}
	}
	return gfx.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4}
}

func (u *UI) viewerImageRect(state *imageViewerState, stage ui.Rect, bitmap gfx.Bitmap) ui.Rect {
	scale := state.zoom
	if state.fit {
		scale = min(
			(stage.Width-48)/float32(max(bitmap.Width, 1)),
			(stage.Height-36)/float32(max(bitmap.Height, 1)),
		)
		scale = max(scale, 0.01)
		state.fitScale = scale
	}
	if scale <= 0 {
		scale = 1
	}
	width := float32(bitmap.Width) * scale
	height := float32(bitmap.Height) * scale
	panX, panY := state.panX, state.panY
	maxX := max((width-stage.Width)/2, 0)
	maxY := max((height-stage.Height)/2, 0)
	panX = min(max(panX, -maxX), maxX)
	panY = min(max(panY, -maxY), maxY)
	return ui.Rect{
		X:      stage.X + (stage.Width-width)/2 + panX,
		Y:      stage.Y + (stage.Height-height)/2 + panY,
		Width:  width,
		Height: height,
	}
}

func (u *UI) handleImageViewerPointer(canvas *gfx.Canvas, state *imageViewerState,
	event gfx.PointerEvent, stage ui.Rect, detached bool) {
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			canvas.Consume()
			return
		}
		if event.ClickCount >= 2 {
			if pointInside(event.X, event.Y, state.imageRect) {
				if state.fit {
					u.showImageViewerActualSize()
				} else {
					u.fitImageViewer()
				}
			} else if !detached {
				u.closeImageViewer()
			}
			canvas.Consume()
			canvas.RequestFrame()
			return
		}
		if !pointInside(event.X, event.Y, state.imageRect) {
			if !detached {
				u.closeImageViewer()
			}
			canvas.Consume()
			canvas.RequestFrame()
			return
		}
		state.drag = true
		state.dragX, state.dragY = event.X, event.Y
		canvas.Consume()
	case gfx.PointerMove:
		if !state.drag {
			return
		}
		state.panX += event.X - state.dragX
		state.panY += event.Y - state.dragY
		state.dragX, state.dragY = event.X, event.Y
		maxX := max((state.imageRect.Width-stage.Width)/2, 0)
		maxY := max((state.imageRect.Height-stage.Height)/2, 0)
		state.panX = min(max(state.panX, -maxX), maxX)
		state.panY = min(max(state.panY, -maxY), maxY)
		canvas.Consume()
		canvas.RequestFrame()
	case gfx.PointerUp:
		if state.drag {
			state.drag = false
			canvas.Consume()
		}
	}
}

func (u *UI) drawImageViewerToolbar(canvas *gfx.Canvas, viewport ui.Rect,
	ids chatSurfaceIDs, state *imageViewerState) {
	width := min(float32(392), max(viewport.Width-32, 300))
	bar := ui.Rect{
		X:     viewport.X + (viewport.Width-width)/2,
		Y:     viewport.Y + viewport.Height - 64,
		Width: width, Height: 48,
	}
	canvas.Rounded(bar, 18, ui.RGBA(30, 34, 42, 0.92))
	canvas.RoundedBorder(bar, 18, 1, ui.RGBA(255, 255, 255, 0.10), ui.Color{})

	x := bar.X + 8
	buttonY := bar.Y + 5
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 38, Height: 38},
		ids.viewerZoomOut, i18n.T("缩小图片"), "−", false, false) {
		u.zoomImageViewer(0.8)
	}
	x += 40
	percent := intString(int(math.Round(float64(u.viewerScale(state))*100))) + "%"
	canvas.LabelCenter(percent, ui.Rect{X: x, Y: buttonY, Width: 62, Height: 38},
		gfx.TextStyle{Size: u.theme.Metrics.ScaledText(11.5), Color: ui.RGBA(206, 212, 222, 1)})
	x += 64
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 38, Height: 38},
		ids.viewerZoomIn, i18n.T("放大图片"), "+", false, false) {
		u.zoomImageViewer(1.25)
	}
	x += 50
	canvas.Fill(ui.Rect{X: x, Y: bar.Y + 12, Width: 1, Height: 24}, ui.RGBA(255, 255, 255, 0.12))
	x += 9
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 48, Height: 38},
		ids.viewerActual, i18n.T("实际大小 1:1"), "1:1", false, !state.fit && math.Abs(float64(state.zoom-1)) < 0.01) {
		u.showImageViewerActualSize()
	}
	x += 52
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 52, Height: 38},
		ids.viewerFit, i18n.T("适应窗口"), i18n.T("适应"), false, state.fit) {
		u.fitImageViewer()
	}
	x += 56
	if u.viewerIconButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 38, Height: 38},
		ids.viewerRotate, i18n.T("顺时针旋转图片"), u.iconRotate,
		gfx.IconTransform{}, false, false) {
		u.rotateImageViewer(1)
	}
}

func (u *UI) viewerScale(state *imageViewerState) float32 {
	scale := state.zoom
	if state.fit {
		scale = state.fitScale
	}
	if scale <= 0 {
		return 1
	}
	return scale
}

func (u *UI) viewerTextButton(canvas *gfx.Canvas, bounds ui.Rect, id gfx.ID,
	label, text string, disabled, selected bool) bool {
	u.viewerButtonSurface(canvas, bounds, id, label, disabled, selected)
	canvas.LabelCenter(text, bounds, gfx.TextStyle{
		Size: u.theme.Metrics.ScaledText(12), Weight: gfx.WeightMedium,
		Color: u.viewerForeground(disabled, selected),
	})
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, radiusControl, 1, ui.RGBA(255, 255, 255, 0.7), ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{
		Role: gfx.RoleButton, Hint: label, Disabled: disabled, Cursor: gfx.PointerCursorHand,
	})
	return !disabled && canvas.Clicked(id)
}

func (u *UI) viewerIconButton(canvas *gfx.Canvas, bounds ui.Rect, id gfx.ID,
	label string, icon gfx.Icon, transform gfx.IconTransform, disabled, selected bool) bool {
	u.viewerButtonSurface(canvas, bounds, id, label, disabled, selected)
	iconBounds := ui.Rect{
		X: bounds.X + (bounds.Width-18)/2, Y: bounds.Y + (bounds.Height-18)/2,
		Width: 18, Height: 18,
	}
	canvas.DrawIconTransformed(icon, iconBounds, u.viewerForeground(disabled, selected), transform)
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, radiusControl, 1, ui.RGBA(255, 255, 255, 0.7), ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{
		Role: gfx.RoleButton, Hint: label, Disabled: disabled, Cursor: gfx.PointerCursorHand,
	})
	return !disabled && canvas.Clicked(id)
}

func (u *UI) viewerButtonSurface(canvas *gfx.Canvas, bounds ui.Rect, id gfx.ID,
	label string, disabled, selected bool) {
	color := ui.RGBA(255, 255, 255, 0.07)
	switch {
	case disabled:
		color = ui.RGBA(255, 255, 255, 0.03)
	case selected:
		color = ui.RGBA(70, 125, 225, 0.72)
	case canvas.Pressed() == id:
		color = ui.RGBA(255, 255, 255, 0.24)
	case canvas.Hovered() == id:
		color = ui.RGBA(255, 255, 255, 0.15)
	}
	canvas.Rounded(bounds, radiusControl, color)
}

func (u *UI) viewerForeground(disabled, selected bool) ui.Color {
	switch {
	case disabled:
		return ui.RGBA(255, 255, 255, 0.24)
	case selected:
		return ui.RGBA(255, 255, 255, 1)
	default:
		return ui.RGBA(223, 228, 237, 1)
	}
}

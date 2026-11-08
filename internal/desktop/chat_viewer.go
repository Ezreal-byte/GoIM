package desktop

import (
	"math"
	"sort"
	"strings"

	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type imageViewerState struct {
	open           bool
	fileID         string
	conversationID string

	fit       bool
	zoom      float32
	rotation  int
	panX      float32
	panY      float32
	fitScale  float32
	imageRect ui.Rect
	drag      bool
	dragX     float32
	dragY     float32

	cached             ui.Bitmap
	cachedSourceID     uint64
	cachedSourceVer    uint32
	cachedRotation     int
	images             []domain.File
	imagesRevision     uint64
	imagesConversation string
}

func (u *UI) imageViewerOpen() bool {
	return u.chatSurface().viewer.open && !u.lockVisible() && !u.isPanelSection()
}

func (u *UI) openImageViewer(fileID string) {
	if fileID == "" || u.selected == "" || u.isPanelSection() {
		return
	}
	u.closeChatPickers()
	u.moreOpen = false
	u.memberMenu = ""
	surface := u.chatSurface()
	surface.viewer = imageViewerState{
		open:           true,
		fileID:         fileID,
		conversationID: u.selected,
		fit:            true,
		zoom:           1,
		fitScale:       1,
	}
	if path := u.fileLocalPath[fileID]; path != "" {
		u.ensureImage(path)
	}
}

func (u *UI) closeImageViewer() {
	surface := u.chatSurface()
	surface.viewer.open = false
	surface.viewer.drag = false
	surface.viewer.cached = ui.Bitmap{}
	surface.viewer.fileID = ""
	surface.viewer.conversationID = ""
	surface.viewer.images = surface.viewer.images[:0]
}

func (u *UI) viewerImageFiles(state *imageViewerState) []domain.File {
	if state.imagesConversation == u.selected && state.imagesRevision == u.state.Revision {
		return state.images
	}
	state.images = state.images[:0]
	for _, file := range u.files {
		if fileIsImage(file) {
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
	state.imagesConversation = u.selected
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
	state := &u.chatSurface().viewer
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
	state := &u.chatSurface().viewer
	state.fileID = fileID
	state.fit = true
	state.zoom = 1
	state.rotation = 0
	state.panX = 0
	state.panY = 0
	state.drag = false
	state.cached = ui.Bitmap{}
	if path := u.fileLocalPath[fileID]; path != "" {
		u.ensureImage(path)
		if bitmap := u.fileBitmap(path); bitmapReady(bitmap) {
			u.viewerRotatedBitmap(state, bitmap)
		}
	}
}

func (u *UI) fitImageViewer() {
	state := &u.chatSurface().viewer
	state.fit = true
	state.panX = 0
	state.panY = 0
}

func (u *UI) showImageViewerActualSize() {
	state := &u.chatSurface().viewer
	state.fit = false
	state.zoom = 1
	state.panX = 0
	state.panY = 0
}

func (u *UI) zoomImageViewer(factor float32) {
	state := &u.chatSurface().viewer
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
	state := &u.chatSurface().viewer
	state.rotation = (state.rotation + direction + 4) % 4
	state.cached = ui.Bitmap{}
	state.panX = 0
	state.panY = 0
	if path := u.fileLocalPath[state.fileID]; path != "" {
		if bitmap := u.fileBitmap(path); bitmapReady(bitmap) {
			u.viewerRotatedBitmap(state, bitmap)
		}
	}
}

func (u *UI) imageViewerKey(event ui.KeyEvent) ui.EventResult {
	state := &u.chatSurface().viewer
	if !state.open || event.Release {
		return ui.EventResult{}
	}
	switch event.Key {
	case ui.KeyTab:
		return ui.EventResult{}
	case ui.KeyEnter, ui.KeySpace:
		return ui.EventResult{}
	case ui.KeyEscape:
		u.closeImageViewer()
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.KeyLeft, ui.KeyPageUp:
		u.navigateImageViewer(-1)
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.KeyRight, ui.KeyPageDown:
		u.navigateImageViewer(1)
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.KeyHome:
		files := u.viewerImageFiles(state)
		if len(files) > 0 {
			u.selectImageViewerFile(files[0].ID)
		}
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.KeyEnd:
		files := u.viewerImageFiles(state)
		if len(files) > 0 {
			u.selectImageViewerFile(files[len(files)-1].ID)
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if event.Modifiers&(ui.ModifierControl|ui.ModifierMeta|ui.ModifierAlt) != 0 {
		return ui.EventResult{Handled: true}
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
		if event.Modifiers&ui.ModifierShift != 0 {
			direction = -1
		}
		u.rotateImageViewer(direction)
	default:
		return ui.EventResult{Handled: true}
	}
	return ui.EventResult{Handled: true, Repaint: true}
}

func (u *UI) imageViewerWheel(event ui.ScrollEvent) ui.EventResult {
	if !u.imageViewerOpen() || event.DeltaY == 0 {
		return ui.EventResult{Handled: true}
	}
	factor := float32(math.Exp(-float64(event.DeltaY) / 240))
	u.zoomImageViewer(factor)
	return ui.EventResult{Handled: true, Repaint: true}
}

func (u *UI) drawChatImageViewer(canvas *ui.Canvas) {
	state := &u.chatSurface().viewer
	if !state.open {
		return
	}
	if state.conversationID != u.selected || u.selected == "" {
		u.closeImageViewer()
		return
	}
	files := u.viewerImageFiles(state)
	index := viewerFileIndex(files, state.fileID)
	if index < 0 {
		u.closeImageViewer()
		return
	}
	viewport := u.lastBody
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return
	}
	ids := u.chatSurface().ids
	canvas.PushLayer(80)
	defer canvas.PopLayer()
	canvas.Fill(viewport, ui.RGBA(8, 10, 14, 1))
	canvas.Block(viewport)
	canvas.PushFocusScope(ids.viewerScope)
	defer canvas.PopFocusScope()

	top := ui.Rect{X: viewport.X, Y: viewport.Y, Width: viewport.Width, Height: 58}
	bottomHeight := float32(82)
	stage := ui.Rect{
		X: viewport.X, Y: viewport.Y + top.Height,
		Width: viewport.Width, Height: max(viewport.Height-top.Height-bottomHeight, 1),
	}
	file := files[index]
	path := u.fileLocalPath[file.ID]
	source := u.fileBitmap(path)
	bitmap := source
	if bitmapReady(source) {
		bitmap = u.viewerRotatedBitmap(state, source)
	}

	canvas.Fill(top, ui.RGBA(255, 255, 255, 0.035))
	canvas.Label(
		intString(index+1)+" / "+intString(len(files)),
		ui.Rect{X: viewport.X + 20, Y: top.Y, Width: 90, Height: top.Height},
		ui.TextStyle{Size: 12, Color: ui.RGBA(167, 174, 186, 1)},
	)
	titleWidth := max(viewport.Width-260, 160)
	canvas.LabelCenter(
		file.Name,
		ui.Rect{X: viewport.X + 110, Y: top.Y, Width: titleWidth, Height: top.Height},
		ui.TextStyle{Size: 14, Weight: ui.WeightMedium, Color: ui.RGBA(238, 241, 247, 1)},
	)

	if bitmapReady(bitmap) {
		imageRect := u.viewerImageRect(state, stage, bitmap)
		state.imageRect = imageRect
		canvas.Clip(stage)
		canvas.DrawBitmap(bitmap, imageRect)
		canvas.Unclip()
	} else {
		state.imageRect = ui.Rect{}
		message := "正在加载图片…"
		if path == "" {
			message = "图片尚未保存到本机"
		}
		canvas.LabelCenter(message, stage, ui.TextStyle{
			Size: 14, Color: ui.RGBA(167, 174, 186, 1),
		})
	}

	cursor := ui.PointerCursorDefault
	if state.imageRect.Width > stage.Width || state.imageRect.Height > stage.Height {
		cursor = ui.PointerCursorHand
	}
	canvas.HitWithOptions(ids.viewerStage, stage, "拖动查看图片", ui.HitOptions{
		Role: ui.RoleGroup, SkipFocus: true, Cursor: cursor,
	})
	if event, ok := canvas.PointerOn(ids.viewerStage); ok {
		u.handleImageViewerPointer(canvas, state, event, stage)
	}

	previousEnabled := index > 0
	nextEnabled := index+1 < len(files)
	arrowY := stage.Y + stage.Height/2 - 25
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + 22, Y: arrowY, Width: 50, Height: 50},
		ids.viewerPrevious, "上一张图片（←）", u.iconChevronRight,
		ui.IconTransform{Rotate: float32(math.Pi)}, !previousEnabled, false) {
		u.navigateImageViewer(-1)
	}
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + viewport.Width - 72, Y: arrowY, Width: 50, Height: 50},
		ids.viewerNext, "下一张图片（→）", u.iconChevronRight,
		ui.IconTransform{}, !nextEnabled, false) {
		u.navigateImageViewer(1)
	}
	if u.viewerIconButton(canvas, ui.Rect{X: viewport.X + viewport.Width - 58, Y: viewport.Y + 10, Width: 38, Height: 38},
		ids.viewerClose, "关闭图片查看器（Esc）", u.iconX,
		ui.IconTransform{}, false, false) {
		u.closeImageViewer()
		return
	}

	u.drawImageViewerToolbar(canvas, viewport, ids, state)
	canvas.TrapFocus(ids.viewerScope)
	canvas.SetInitialFocus(ids.viewerClose)
	canvas.SetEscapeTarget(ids.viewerClose)
}

func (u *UI) viewerRotatedBitmap(state *imageViewerState, source ui.Bitmap) ui.Bitmap {
	turns := state.rotation % 4
	if turns < 0 {
		turns += 4
	}
	if turns == 0 {
		state.cached = ui.Bitmap{}
		return source
	}
	if bitmapReady(state.cached) && state.cachedRotation == turns &&
		state.cachedSourceID == source.ID && state.cachedSourceVer == source.Version {
		return state.cached
	}
	rotated := rotateBitmapQuarter(source, turns)
	if bitmapReady(rotated) {
		rotated.ID = u.nextImageID
		rotated.Version = 1
		u.nextImageID++
	}
	state.cached = rotated
	state.cachedRotation = turns
	state.cachedSourceID = source.ID
	state.cachedSourceVer = source.Version
	return rotated
}

func rotateBitmapQuarter(source ui.Bitmap, turns int) ui.Bitmap {
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
	return ui.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4}
}

func (u *UI) viewerImageRect(state *imageViewerState, stage ui.Rect, bitmap ui.Bitmap) ui.Rect {
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

func (u *UI) handleImageViewerPointer(canvas *ui.Canvas, state *imageViewerState,
	event ui.PointerEvent, stage ui.Rect) {
	switch event.Kind {
	case ui.PointerDown:
		if event.Button != ui.PointerButtonPrimary {
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
			} else {
				u.closeImageViewer()
			}
			canvas.Consume()
			canvas.RequestFrame()
			return
		}
		if !pointInside(event.X, event.Y, state.imageRect) {
			u.closeImageViewer()
			canvas.Consume()
			canvas.RequestFrame()
			return
		}
		state.drag = true
		state.dragX, state.dragY = event.X, event.Y
		canvas.Consume()
	case ui.PointerMove:
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
	case ui.PointerUp:
		if state.drag {
			state.drag = false
			canvas.Consume()
		}
	}
}

func (u *UI) drawImageViewerToolbar(canvas *ui.Canvas, viewport ui.Rect,
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
		ids.viewerZoomOut, "缩小图片", "−", false, false) {
		u.zoomImageViewer(0.8)
	}
	x += 40
	percent := intString(int(math.Round(float64(u.viewerScale(state))*100))) + "%"
	canvas.LabelCenter(percent, ui.Rect{X: x, Y: buttonY, Width: 62, Height: 38},
		ui.TextStyle{Size: 11.5, Color: ui.RGBA(206, 212, 222, 1)})
	x += 64
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 38, Height: 38},
		ids.viewerZoomIn, "放大图片", "+", false, false) {
		u.zoomImageViewer(1.25)
	}
	x += 50
	canvas.Fill(ui.Rect{X: x, Y: bar.Y + 12, Width: 1, Height: 24}, ui.RGBA(255, 255, 255, 0.12))
	x += 9
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 48, Height: 38},
		ids.viewerActual, "实际大小 1:1", "1:1", false, !state.fit && math.Abs(float64(state.zoom-1)) < 0.01) {
		u.showImageViewerActualSize()
	}
	x += 52
	if u.viewerTextButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 52, Height: 38},
		ids.viewerFit, "适应窗口", "适应", false, state.fit) {
		u.fitImageViewer()
	}
	x += 56
	if u.viewerIconButton(canvas, ui.Rect{X: x, Y: buttonY, Width: 38, Height: 38},
		ids.viewerRotate, "顺时针旋转图片", u.iconRotate,
		ui.IconTransform{}, false, false) {
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

func (u *UI) viewerTextButton(canvas *ui.Canvas, bounds ui.Rect, id ui.ID,
	label, text string, disabled, selected bool) bool {
	u.viewerButtonSurface(canvas, bounds, id, label, disabled, selected)
	canvas.LabelCenter(text, bounds, ui.TextStyle{
		Size: 12, Weight: ui.WeightMedium,
		Color: u.viewerForeground(disabled, selected),
	})
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, 10, 1, ui.RGBA(255, 255, 255, 0.7), ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, ui.HitOptions{
		Role: ui.RoleButton, Disabled: disabled, Cursor: ui.PointerCursorHand,
	})
	return !disabled && canvas.Clicked(id)
}

func (u *UI) viewerIconButton(canvas *ui.Canvas, bounds ui.Rect, id ui.ID,
	label string, icon ui.Icon, transform ui.IconTransform, disabled, selected bool) bool {
	u.viewerButtonSurface(canvas, bounds, id, label, disabled, selected)
	iconBounds := ui.Rect{
		X: bounds.X + (bounds.Width-18)/2, Y: bounds.Y + (bounds.Height-18)/2,
		Width: 18, Height: 18,
	}
	canvas.DrawIconTransformed(icon, iconBounds, u.viewerForeground(disabled, selected), transform)
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, 10, 1, ui.RGBA(255, 255, 255, 0.7), ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, ui.HitOptions{
		Role: ui.RoleButton, Disabled: disabled, Cursor: ui.PointerCursorHand,
	})
	return !disabled && canvas.Clicked(id)
}

func (u *UI) viewerButtonSurface(canvas *ui.Canvas, bounds ui.Rect, id ui.ID,
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
	canvas.Rounded(bounds, 10, color)
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

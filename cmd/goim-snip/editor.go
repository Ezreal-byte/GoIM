package main

import (
	"fmt"
	"image"
	"math"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/capture"
	"goim/internal/i18n"
)

const previewBitmapID uint64 = 0x474F494D534E4950

var (
	tree  = gfx.Tree{}
	page  = tree.Feature("snip")
	tools = tree.Sub(page, tree.Name("tools"))

	canvasID = tree.ID(page, "canvas")
	selectID = tree.ID(tools, "select")
	rectID   = tree.ID(tools, "rectangle")
	arrowID  = tree.ID(tools, "arrow")
	drawID   = tree.ID(tools, "freehand")
	undoID   = tree.ID(page, "undo")
	cancelID = tree.ID(page, "cancel")
	exportID = tree.ID(page, "export")
)

type tool uint8

const (
	toolSelect tool = iota
	toolRectangle
	toolArrow
	toolFreehand
	toolEllipse
	toolText
	toolMosaic
)

func (tool tool) String() string {
	switch tool {
	case toolRectangle:
		return i18n.T("矩形")
	case toolArrow:
		return i18n.T("箭头")
	case toolFreehand:
		return i18n.T("画笔")
	case toolEllipse:
		return i18n.T("椭圆")
	case toolText:
		return i18n.T("文字")
	case toolMosaic:
		return i18n.T("马赛克")
	default:
		return i18n.T("选区")
	}
}

func (tool tool) hint() string {
	switch tool {
	case toolText:
		return i18n.T("单击添加文字")
	case toolSelect:
		return i18n.T("拖动选择截图区域")
	default:
		return i18n.T("拖动标注")
	}
}

type outcome uint8

const (
	outcomePending outcome = iota
	outcomeConfirmed
	outcomeCanceled
	outcomeFailed
	outcomeSaved
)

type gestureKind uint8

const (
	gestureCrop gestureKind = iota
	gestureShape
)

type gesture struct {
	kind   gestureKind
	shape  capture.ShapeKind
	start  capture.Point
	end    capture.Point
	points []capture.Point
}

type editor struct {
	document *capture.Document
	output   string
	info     sourceInfo
	tool     tool
	dark     bool

	followSystem bool
	preview      gfx.Bitmap
	version      uint32
	cropLabel    string
	displayLabel string
	toolLabel    string
	gesture      *gesture
	pressTarget  gfx.ID
	text         snipText
	ink, stroke  int
	notice       string

	windowWidth  int
	windowHeight int
	outcome      outcome
	err          error
	done         bool
	closeWindow  func()
}

func newEditor(document *capture.Document, output string, info sourceInfo, dark bool, width, height int) (*editor, error) {
	if document == nil {
		return nil, capture.ErrNoSource
	}
	editor := &editor{
		document:     document,
		output:       output,
		info:         info,
		dark:         dark,
		windowWidth:  width,
		windowHeight: height,
		displayLabel: sourceDisplayLabel(info),
		toolLabel:    toolSelect.String(),
		stroke:       1,
	}
	editor.text.page = ui.NewPage(ui.PageConfig{Root: "snip-text", Tree: &tree, Theme: &editor.text.theme})
	if err := editor.refreshPreview(); err != nil {
		return nil, err
	}
	return editor, nil
}

func (editor *editor) config(invalidator *app.Invalidator) app.Config {
	return app.Config{
		Tree:        &tree,
		Title:       i18n.T("GoIM 截图"),
		Width:       editor.windowWidth,
		Height:      editor.windowHeight,
		MinWidth:    820,
		MinHeight:   600,
		Invalidator: invalidator,
		Paint:       editor.paint,
		OnPointer:   editor.pointer,
		OnKey:       editor.key,
		OnShortcut:  editor.shortcut,
		TextInput:   editor.textInput,
		OnWake:      editor.wake,
		OnClosed:    editor.closed,
	}
}

func (editor *editor) wake() {
	if !editor.followSystem {
		return
	}
	switch app.SystemAppearance() {
	case app.AppearanceDark:
		editor.dark = true
	case app.AppearanceLight:
		editor.dark = false
	}
}

func (editor *editor) closed() {
	editor.gesture = nil
	editor.done = true
	if editor.outcome == outcomePending {
		editor.outcome = outcomeCanceled
	}
}

func (editor *editor) refreshPreview() error {
	rendered, err := editor.document.Render()
	if err != nil {
		return err
	}
	for index := 0; index < len(rendered.Pix); index += 4 {
		rendered.Pix[index], rendered.Pix[index+2] = rendered.Pix[index+2], rendered.Pix[index]
	}
	editor.version++
	editor.preview = gfx.Bitmap{
		Pix:     rendered.Pix,
		Width:   rendered.Bounds().Dx(),
		Height:  rendered.Bounds().Dy(),
		Stride:  rendered.Stride,
		ID:      previewBitmapID,
		Version: editor.version,
	}
	editor.cropLabel = fmt.Sprintf("%d × %d px", rendered.Bounds().Dx(), rendered.Bounds().Dy())
	return nil
}

func (editor *editor) cancel() bool {
	if editor.done {
		return false
	}
	editor.gesture = nil
	editor.finish(outcomeCanceled)
	return true
}

func (editor *editor) export() bool {
	if editor.done {
		return false
	}
	if editor.output == "" {
		editor.err = fmt.Errorf("export PNG: output path is empty")
		editor.finish(outcomeFailed)
		return true
	}
	if err := editor.document.ExportPNG(editor.output); err != nil {
		editor.err = fmt.Errorf("export PNG to %s: %w", editor.output, err)
		editor.finish(outcomeFailed)
		return true
	}
	editor.finish(outcomeConfirmed)
	return true
}

func (editor *editor) finish(result outcome) {
	if editor.done {
		return
	}
	editor.done = true
	editor.outcome = result
	if editor.closeWindow != nil {
		editor.closeWindow()
	}
}

func (editor *editor) undo() bool {
	if editor.done {
		return false
	}
	editor.gesture = nil
	if !editor.document.Undo() {
		return false
	}
	if err := editor.refreshPreview(); err != nil {
		editor.err = fmt.Errorf("rebuild preview: %w", err)
	}
	return true
}

func (editor *editor) pointer(event gfx.PointerEvent) gfx.EventResult {
	if editor.text.open {
		return editor.text.page.Pointer(event)
	}
	layout := computeLayout(event.Viewport)
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		editor.pressTarget = event.Target
		if event.Target != canvasID {
			return gfx.EventResult{}
		}
		if !pointIn(event.X, event.Y, layout.imageArea()) {
			return gfx.EventResult{Handled: true}
		}
		editor.beginGesture(event, layout)
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.PointerMove:
		if editor.gesture == nil {
			return gfx.EventResult{}
		}
		editor.updateGesture(event, layout)
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.PointerUp:
		if editor.pressTarget.IsZero() {
			return gfx.EventResult{}
		}
		target := editor.pressTarget
		editor.pressTarget = gfx.ID{}
		if editor.gesture != nil {
			editor.updateGesture(event, layout)
			editor.commitGesture(layout)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if event.Button == gfx.PointerButtonPrimary {
			if editor.activatePointer(target, event.X, event.Y, layout) {
				return gfx.EventResult{Handled: true, Repaint: !editor.done}
			}
		}
	}
	return gfx.EventResult{}
}

func (editor *editor) beginGesture(event gfx.PointerEvent, layout editorLayout) {
	crop := editor.document.Crop()
	imageBounds, scale := fitImageRect(layout.imageArea(), crop)
	if imageRectEmpty(imageBounds) {
		return
	}
	start := viewToSource(event.X, event.Y, crop, imageBounds)
	if editor.tool == toolText {
		editor.beginText(start, scale)
		return
	}
	gesture := &gesture{start: start, end: start}
	if editor.tool == toolSelect {
		gesture.kind = gestureCrop
	} else {
		gesture.kind = gestureShape
		gesture.points = []capture.Point{start}
		gesture.shape = editor.tool.shape()
	}
	editor.gesture = gesture
}

func (editor *editor) updateGesture(event gfx.PointerEvent, layout editorLayout) {
	if editor.gesture == nil {
		return
	}
	crop := editor.document.Crop()
	imageBounds, scale := fitImageRect(layout.imageArea(), crop)
	if imageRectEmpty(imageBounds) {
		return
	}
	end := viewToSource(event.X, event.Y, crop, imageBounds)
	editor.gesture.end = end
	if editor.gesture.kind == gestureShape && editor.gesture.shape == capture.Freehand {
		points := editor.gesture.points
		last := points[len(points)-1]
		minimum := math.Max(1.25/scale, 0.5)
		if math.Hypot(end.X-last.X, end.Y-last.Y) >= minimum {
			editor.gesture.points = append(points, end)
		}
	}
}

func (editor *editor) commitGesture(layout editorLayout) {
	gesture := editor.gesture
	editor.gesture = nil
	if gesture == nil {
		return
	}
	crop := editor.document.Crop()
	imageBounds, scale := fitImageRect(layout.imageArea(), crop)
	if imageRectEmpty(imageBounds) {
		return
	}
	switch gesture.kind {
	case gestureCrop:
		if math.Abs(float64(gesture.end.X-gesture.start.X))*scale < 6 ||
			math.Abs(float64(gesture.end.Y-gesture.start.Y))*scale < 6 {
			return
		}
		selected := sourceRectangle(gesture.start, gesture.end)
		if err := editor.document.SetCrop(selected); err != nil {
			return
		}
	case gestureShape:
		shape := editor.annotation(gesture, scale)
		if gesture.shape == capture.Freehand {
			points := gesture.points
			if len(points) == 0 || points[len(points)-1] != gesture.end {
				points = append(points, gesture.end)
			}
			shape.Points = points
		}
		if shape.Validate() != nil {
			return
		}
		if err := editor.document.Add(shape); err != nil {
			return
		}
	}
	if err := editor.refreshPreview(); err != nil {
		editor.err = fmt.Errorf("rebuild preview: %w", err)
	}
}

func (editor *editor) activatePointer(id gfx.ID, x, y float32, layout editorLayout) bool {
	for i, toolID := range toolIDs {
		if id == toolID && pointIn(x, y, layout.toolButtons[i]) {
			editor.selectTool(tool(i))
			return true
		}
	}
	switch id {
	case undoID:
		return pointIn(x, y, layout.undo) && editor.undo()
	case redoID:
		return pointIn(x, y, layout.redo) && editor.redo()
	case cancelID:
		if !pointIn(x, y, layout.cancel) {
			return false
		}
		return editor.cancel()
	case exportID:
		if !pointIn(x, y, layout.export) {
			return false
		}
		return editor.export()
	default:
		return false
	}
}

func (editor *editor) selectTool(next tool) {
	editor.tool = next
	editor.toolLabel = next.String()
}

func (tool tool) shape() capture.ShapeKind {
	switch tool {
	case toolRectangle:
		return capture.Rectangle
	case toolArrow:
		return capture.Arrow
	case toolFreehand:
		return capture.Freehand
	case toolEllipse:
		return capture.Ellipse
	case toolMosaic:
		return capture.Mosaic
	default:
		return 0
	}
}

func (editor *editor) key(event gfx.KeyEvent) gfx.EventResult {
	if editor.text.open {
		return editor.text.page.Key(event)
	}
	if event.Release {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyEscape:
		if !event.Repeat && editor.cancel() {
			return gfx.EventResult{Handled: true}
		}
	case gfx.KeyEnter:
		if !event.Target.IsZero() && event.Target != canvasID {
			if !event.Repeat && editor.activateKeyboard(event.Target) {
				return gfx.EventResult{Handled: true, Repaint: !editor.done}
			}
			return gfx.EventResult{}
		}
		if !event.Repeat && editor.export() {
			return gfx.EventResult{Handled: true}
		}
	case gfx.KeySpace:
		if !event.Repeat && editor.activateKeyboard(event.Target) {
			return gfx.EventResult{Handled: true, Repaint: !editor.done}
		}
	}
	return gfx.EventResult{}
}

func (editor *editor) activateKeyboard(id gfx.ID) bool {
	for i, toolID := range toolIDs {
		if id == toolID {
			editor.selectTool(tool(i))
			return true
		}
	}
	switch id {
	case undoID:
		return editor.undo()
	case redoID:
		return editor.redo()
	case cancelID:
		return editor.cancel()
	case exportID:
		return editor.export()
	default:
		return false
	}
}

func (editor *editor) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || editor.text.open {
		return gfx.EventResult{}
	}
	modifier := gfx.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = gfx.ModifierMeta
	}
	if event.Modifiers&modifier != 0 {
		switch strings.ToLower(event.Text) {
		case "z":
			if event.Modifiers&gfx.ModifierShift != 0 {
				return gfx.EventResult{Handled: true, Repaint: editor.redo()}
			}
			return gfx.EventResult{Handled: true, Repaint: editor.undo()}
		case "y":
			return gfx.EventResult{Handled: true, Repaint: editor.redo()}
		case "c":
			return gfx.EventResult{Handled: editor.export()}
		}
	}
	return gfx.EventResult{}
}

type editorLayout struct {
	body        gfx.Rect
	left        gfx.Rect
	center      gfx.Rect
	right       gfx.Rect
	header      gfx.Rect
	footer      gfx.Rect
	well        gfx.Rect
	toolButtons [len(toolIDs)]gfx.Rect
	undo        gfx.Rect
	redo        gfx.Rect
	style       gfx.Rect
	cancel      gfx.Rect
	save        gfx.Rect
	export      gfx.Rect
}

func computeLayout(size gfx.Size) editorLayout {
	body := gfx.Rect{Width: size.Width, Height: size.Height}
	layout := editorLayout{body: body}
	content := body.Inset(gfx.All(14))
	layout.left, content = content.CutLeft(184)
	content = content.Inset(gfx.Insets{Left: 14})
	layout.right, content = content.CutRight(232)
	layout.center = content.Inset(gfx.Insets{Right: 14})

	leftInner := layout.left.Inset(gfx.All(16))
	y := leftInner.Y
	y += 32
	y += 24
	y += 19
	y += 24
	for index := range layout.toolButtons {
		layout.toolButtons[index] = gfx.Rect{X: leftInner.X, Y: y, Width: leftInner.Width, Height: 32}
		y += 37
	}
	layout.style = gfx.Rect{X: leftInner.X, Y: y + 6, Width: leftInner.Width, Height: 50}
	half := (leftInner.Width - 6) / 2
	layout.undo = gfx.Rect{X: leftInner.X, Y: leftInner.Bottom() - 34, Width: half, Height: 34}
	layout.redo = gfx.Rect{X: layout.undo.Right() + 6, Y: layout.undo.Y, Width: half, Height: 34}

	centerInner := layout.center.Inset(gfx.All(18))
	layout.header, centerInner = centerInner.CutTop(28)
	centerInner = centerInner.Inset(gfx.Insets{Top: 10})
	layout.footer, centerInner = centerInner.CutBottom(18)
	centerInner = centerInner.Inset(gfx.Insets{Bottom: 10})
	layout.well = centerInner

	rightInner := layout.right.Inset(gfx.All(16))
	layout.export = gfx.Rect{X: rightInner.X, Y: rightInner.Bottom() - 44, Width: rightInner.Width, Height: 44}
	layout.cancel = gfx.Rect{X: rightInner.X, Y: layout.export.Y - 8 - 36, Width: rightInner.Width, Height: 36}
	layout.save = gfx.Rect{X: rightInner.X, Y: layout.cancel.Y - 42, Width: rightInner.Width, Height: 34}
	return layout
}

func (layout editorLayout) imageArea() gfx.Rect {
	return layout.well.Inset(gfx.All(18))
}

func fitImageRect(area gfx.Rect, crop image.Rectangle) (gfx.Rect, float64) {
	if area.Width <= 0 || area.Height <= 0 || crop.Empty() {
		return gfx.Rect{}, 1
	}
	scale := math.Min(
		float64(area.Width)/float64(crop.Dx()),
		float64(area.Height)/float64(crop.Dy()),
	)
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return gfx.Rect{}, 1
	}
	width := float32(float64(crop.Dx()) * scale)
	height := float32(float64(crop.Dy()) * scale)
	return gfx.Rect{
		X:      area.X + (area.Width-width)/2,
		Y:      area.Y + (area.Height-height)/2,
		Width:  width,
		Height: height,
	}, scale
}

func sourceToView(point capture.Point, crop image.Rectangle, imageBounds gfx.Rect) gfx.Point {
	sx := imageBounds.Width / float32(crop.Dx())
	sy := imageBounds.Height / float32(crop.Dy())
	return gfx.Point{
		X: imageBounds.X + (float32(point.X)-float32(crop.Min.X))*sx,
		Y: imageBounds.Y + (float32(point.Y)-float32(crop.Min.Y))*sy,
	}
}

func viewToSource(x, y float32, crop image.Rectangle, imageBounds gfx.Rect) capture.Point {
	x = min(max(x, imageBounds.X), imageBounds.Right())
	y = min(max(y, imageBounds.Y), imageBounds.Bottom())
	return capture.Point{
		X: float64(crop.Min.X) + float64(x-imageBounds.X)*float64(crop.Dx())/float64(imageBounds.Width),
		Y: float64(crop.Min.Y) + float64(y-imageBounds.Y)*float64(crop.Dy())/float64(imageBounds.Height),
	}
}

func sourceRectangle(start, end capture.Point) image.Rectangle {
	left := int(math.Floor(math.Min(start.X, end.X)))
	top := int(math.Floor(math.Min(start.Y, end.Y)))
	right := int(math.Ceil(math.Max(start.X, end.X)))
	bottom := int(math.Ceil(math.Max(start.Y, end.Y)))
	return image.Rect(left, top, right, bottom)
}

func annotationWidth(scale float64) float64 {
	if scale <= 0 {
		return capture.DefaultShapeWidth
	}
	return min(max(4/scale, 2), 64)
}

func pointIn(x, y float32, bounds gfx.Rect) bool {
	return x >= bounds.X && x < bounds.Right() && y >= bounds.Y && y < bounds.Bottom()
}

func imageRectEmpty(bounds gfx.Rect) bool {
	return bounds.Width <= 0 || bounds.Height <= 0
}

func intersectRect(a, b gfx.Rect) gfx.Rect {
	left := max(a.X, b.X)
	top := max(a.Y, b.Y)
	right := min(a.Right(), b.Right())
	bottom := min(a.Bottom(), b.Bottom())
	if right <= left || bottom <= top {
		return gfx.Rect{}
	}
	return gfx.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func (editor *editor) paint(canvas *gfx.Canvas, size gfx.Size) {
	theme := snipTheme(editor.dark)
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	layout := computeLayout(size)
	editor.paintTools(canvas, theme, layout)
	editor.paintCanvas(canvas, theme, layout)
	editor.paintExport(canvas, theme, layout)
	editor.paintActions(canvas, theme, layout.body)
}

func (editor *editor) paintTools(canvas *gfx.Canvas, theme gfx.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.left, 14, 1, theme.Border, theme.Card)
	inner := layout.left.Inset(gfx.All(16))

	canvas.Rounded(gfx.Rect{X: inner.X, Y: inner.Y + 4, Width: 3, Height: 18}, 1.5, theme.Primary)
	canvas.Label(i18n.T("GoIM 截图"), gfx.Rect{X: inner.X + 12, Y: inner.Y, Width: inner.Width - 12, Height: 26}, gfx.TextStyle{
		Size: 18, Weight: gfx.WeightSemibold, Color: theme.Foreground,
	})
	y := inner.Y + 32
	canvas.Label(i18n.T("截图编辑器"), gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 18}, gfx.TextStyle{
		Size: 12, Color: theme.MutedForeground,
	})
	y += 24
	canvas.Fill(gfx.Rect{X: inner.X, Y: y + 5, Width: inner.Width, Height: 1}, theme.Border)
	y += 19
	canvas.Label(i18n.T("标注工具"), gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 16}, gfx.TextStyle{
		Size: 10, Weight: gfx.WeightSemibold, Color: theme.MutedForeground,
	})

	for index, id := range toolIDs {
		label := tool(index).String()
		active := int(editor.tool) == index
		button := components.BeginButton(canvas, theme, id, layout.toolButtons[index]).
			Height(layout.toolButtons[index].Height).FontSize(13)
		if active {
			button.Default(label)
		} else {
			button.Outline(label)
		}
	}

	editor.paintStyle(canvas, theme, layout.style, false)
	undo := components.BeginButton(canvas, theme, undoID, layout.undo).
		Height(layout.undo.Height).FontSize(13)
	if !editor.document.CanUndo() {
		undo = undo.Disabled()
	}
	undo.Outline(i18n.T("撤销"))
	redo := components.BeginButton(canvas, theme, redoID, layout.redo).Height(layout.redo.Height).FontSize(13)
	if !editor.document.CanRedo() {
		redo = redo.Disabled()
	}
	redo.Outline(i18n.T("重做"))
}

func (editor *editor) paintCanvas(canvas *gfx.Canvas, theme gfx.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.center, 14, 1, theme.Border, theme.Card)
	crop := editor.document.Crop()
	canvas.Label(i18n.T("预览"), layout.header, gfx.TextStyle{
		Size: 16, Weight: gfx.WeightSemibold, Color: theme.Foreground,
	})
	canvas.LabelAligned(
		editor.cropLabel,
		layout.header, gfx.TextStyle{Size: 12, Color: theme.MutedForeground}, gfx.AlignEnd,
	)

	canvas.RoundedBorder(layout.well, 10, 1, theme.Border, canvasWell(editor.dark))
	area := layout.imageArea()
	imageBounds, scale := fitImageRect(area, crop)
	if !imageRectEmpty(imageBounds) {
		canvas.Elevate(imageBounds, 3, gfx.ShadowSM)
		canvas.Fill(imageBounds, canvasWell(editor.dark))
		canvas.DrawBitmap(editor.preview, imageBounds)
		canvas.RoundedOutline(imageBounds, 3, 1, theme.Border)
		canvas.Clip(imageBounds)
		editor.paintGesture(canvas, imageBounds, crop, scale, theme)
		canvas.Unclip()
	}
	if canvas.Focused() == canvasID && canvas.FocusVisible() == canvasID {
		canvas.RoundedOutline(layout.well, 10, 1.5, theme.Primary)
	}
	canvas.HitWithOptions(canvasID, layout.well, i18n.T("截图区域"), gfx.HitOptions{
		Role:        gfx.RoleGroup,
		Cursor:      gfx.PointerCursorCrosshair,
		Autofocus:   true,
		Description: editor.tool.hint(),
	})

	canvas.Label(editor.tool.hint(), layout.footer, gfx.TextStyle{Size: 11, Color: theme.MutedForeground})
	canvas.LabelAligned(i18n.T("适应窗口"), layout.footer, gfx.TextStyle{Size: 11, Color: theme.MutedForeground}, gfx.AlignEnd)
}

func (editor *editor) paintGesture(canvas *gfx.Canvas, imageBounds gfx.Rect, crop image.Rectangle, scale float64, theme gfx.Theme) {
	if editor.gesture == nil {
		return
	}
	if editor.gesture.kind == gestureCrop {
		start := sourceToView(editor.gesture.start, crop, imageBounds)
		end := sourceToView(editor.gesture.end, crop, imageBounds)
		selection := intersectRect(normalizedPointRect(start, end), imageBounds)
		if imageRectEmpty(selection) {
			return
		}
		dim := gfx.Color{Alpha: 0.38}
		top := gfx.Rect{X: imageBounds.X, Y: imageBounds.Y, Width: imageBounds.Width, Height: selection.Y - imageBounds.Y}
		bottom := gfx.Rect{X: imageBounds.X, Y: selection.Bottom(), Width: imageBounds.Width, Height: imageBounds.Bottom() - selection.Bottom()}
		left := gfx.Rect{X: imageBounds.X, Y: selection.Y, Width: selection.X - imageBounds.X, Height: selection.Height}
		right := gfx.Rect{X: selection.Right(), Y: selection.Y, Width: imageBounds.Right() - selection.Right(), Height: selection.Height}
		for _, fill := range [...]gfx.Rect{top, bottom, left, right} {
			if fill.Width > 0 && fill.Height > 0 {
				canvas.Fill(fill, dim)
			}
		}
		canvas.RoundedOutline(selection, 2, 1.5, theme.Primary)
		for _, corner := range [...]gfx.Point{
			{X: selection.X, Y: selection.Y},
			{X: selection.Right(), Y: selection.Y},
			{X: selection.X, Y: selection.Bottom()},
			{X: selection.Right(), Y: selection.Bottom()},
		} {
			handle := gfx.Rect{X: corner.X - 3, Y: corner.Y - 3, Width: 6, Height: 6}
			canvas.Rounded(handle, 2, gfx.RGB(255, 255, 255))
			canvas.RoundedOutline(handle, 2, 1, theme.Primary)
		}
		return
	}
	shape := editor.annotation(editor.gesture, scale)
	if shape.Kind == capture.Rectangle {
		start := sourceToView(shape.Start, crop, imageBounds)
		end := sourceToView(shape.End, crop, imageBounds)
		if start == end {
			return
		}
	}
	if shape.Kind == capture.Freehand && len(shape.Points) == 0 {
		shape.Points = []capture.Point{shape.Start}
	}
	drawShape(canvas, shape, crop, imageBounds, scale)
}

func drawShape(canvas *gfx.Canvas, shape capture.Shape, crop image.Rectangle, imageBounds gfx.Rect, scale float64) {
	local := func(point capture.Point) gfx.Point {
		view := sourceToView(point, crop, imageBounds)
		return gfx.Point{X: view.X - imageBounds.X, Y: view.Y - imageBounds.Y}
	}
	stroke := gfx.PathStyle{
		Stroke:      inkColor(shape.Color),
		StrokeWidth: max(float32(shape.Width*scale), 1),
		Cap:         gfx.CapRound,
		Join:        gfx.JoinRound,
	}
	switch shape.Kind {
	case capture.Mosaic:
		rect := normalizedPointRect(sourceToView(shape.Start, crop, imageBounds), sourceToView(shape.End, crop, imageBounds))
		canvas.Fill(rect, gfx.RGBA(30, 36, 48, .45))
		canvas.RoundedOutline(rect, 0, 1.5, gfx.RGB(243, 246, 253))
	case capture.Ellipse:
		rect := normalizedPointRect(sourceToView(shape.Start, crop, imageBounds), sourceToView(shape.End, crop, imageBounds))
		if !imageRectEmpty(rect) {
			canvas.DrawPath(snipEllipsePath, rect, stroke)
		}
	case capture.Rectangle:
		start, end := local(shape.Start), local(shape.End)
		path := gfx.NewPath(
			imageBounds.Width, imageBounds.Height,
			gfx.MoveTo(start.X, start.Y),
			gfx.LineTo(end.X, start.Y),
			gfx.LineTo(end.X, end.Y),
			gfx.LineTo(start.X, end.Y),
			gfx.ClosePath(),
		)
		canvas.DrawPath(path, imageBounds, stroke)
	case capture.Arrow:
		start, end := local(shape.Start), local(shape.End)
		shaft := gfx.NewPath(imageBounds.Width, imageBounds.Height, gfx.MoveTo(start.X, start.Y), gfx.LineTo(end.X, end.Y))
		canvas.DrawPath(shaft, imageBounds, stroke)
		head := capture.ArrowHead(shape.Start, shape.End, shape.Width)
		tip, left, right := local(head[0]), local(head[1]), local(head[2])
		headPath := gfx.NewPath(
			imageBounds.Width, imageBounds.Height,
			gfx.MoveTo(tip.X, tip.Y),
			gfx.LineTo(left.X, left.Y),
			gfx.LineTo(right.X, right.Y),
			gfx.ClosePath(),
		)
		canvas.DrawPath(headPath, imageBounds, gfx.PathStyle{Fill: inkColor(shape.Color), FillRule: gfx.PathFillNonZero})
	case capture.Freehand:
		if len(shape.Points) == 0 {
			return
		}
		commands := make([]gfx.PathCommand, 0, len(shape.Points))
		start := local(shape.Points[0])
		commands = append(commands, gfx.MoveTo(start.X, start.Y))
		for _, point := range shape.Points[1:] {
			next := local(point)
			commands = append(commands, gfx.LineTo(next.X, next.Y))
		}
		path := gfx.NewPath(imageBounds.Width, imageBounds.Height, commands...)
		canvas.DrawPath(path, imageBounds, stroke)
	}
}

func (editor *editor) paintExport(canvas *gfx.Canvas, theme gfx.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.right, 14, 1, theme.Border, theme.Card)
	components.BeginButton(canvas, theme, saveID, layout.save).Height(layout.save.Height).Outline(i18n.T("另存为"))
	if canvas.Clicked(saveID) {
		editor.scheduleSaveAs(canvas)
	}
	inner := layout.right.Inset(gfx.All(16))
	y := inner.Y
	canvas.Label(i18n.T("输出截图"), gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 24}, gfx.TextStyle{
		Size: 18, Weight: gfx.WeightSemibold, Color: theme.Foreground,
	})
	y += 31
	detailStyle := gfx.TextStyle{Size: 11, Color: theme.MutedForeground}
	canvas.Label(canvas.FitLine(editor.info.detail, detailStyle.Size, detailStyle.Weight, inner.Width),
		gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 16}, detailStyle)
	y += 24
	canvas.Fill(gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 1}, theme.Border)
	y += 18
	canvas.Label(i18n.T("源图信息"), gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 15}, gfx.TextStyle{
		Size: 10, Weight: gfx.WeightSemibold, Color: theme.MutedForeground,
	})
	y += 22

	rows := [...]struct {
		label string
		value string
	}{
		{i18n.T("来源"), editor.info.title},
		{i18n.T("图片尺寸"), editor.cropLabel},
		{i18n.T("屏幕"), editor.displayLabel},
		{i18n.T("当前工具"), editor.toolLabel},
	}
	for _, row := range rows {
		canvas.Label(row.label, gfx.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 14}, gfx.TextStyle{
			Size: 11, Color: theme.MutedForeground,
		})
		canvas.Label(canvas.FitLine(row.value, 13, gfx.WeightRegular, inner.Width),
			gfx.Rect{X: inner.X, Y: y + 15, Width: inner.Width, Height: 19}, gfx.TextStyle{
				Size: 13, Color: theme.Foreground,
			})
		y += 38
	}

	components.BeginButton(canvas, theme, exportID, layout.export).
		Height(layout.export.Height).FontSize(14).Default(i18n.T("复制截图"))
	components.BeginButton(canvas, theme, cancelID, layout.cancel).
		Height(layout.cancel.Height).FontSize(13).Outline(i18n.T("取消"))
}

func snipTheme(dark bool) gfx.Theme {
	theme := gfx.LightTheme()
	if dark {
		theme = gfx.DarkTheme()
	}
	theme.Primary = gfx.RGB(29, 78, 216)
	theme.PrimaryForeground = gfx.RGB(239, 246, 255)
	theme.Ring = theme.Primary
	if dark {
		theme.Background = gfx.RGB(8, 12, 20)
		theme.Foreground = gfx.RGB(228, 236, 247)
		theme.Card = gfx.RGB(17, 24, 39)
		theme.CardForeground = theme.Foreground
		theme.CardElevated = gfx.RGB(23, 33, 50)
		theme.Popover = theme.Card
		theme.PopoverForeground = theme.Foreground
		theme.Muted = gfx.RGB(26, 36, 53)
		theme.MutedForeground = gfx.RGB(142, 159, 182)
		theme.Border = gfx.RGB(45, 59, 82)
		theme.Hover = gfx.RGB(29, 41, 59)
		theme.Input = theme.Border
		theme.Secondary = gfx.RGB(29, 41, 59)
		theme.SecondaryForeground = theme.Foreground
		theme.Accent = theme.Hover
		theme.AccentForeground = theme.Foreground
		theme.Primary = gfx.RGB(59, 130, 246)
		theme.PrimaryForeground = gfx.RGB(8, 15, 28)
		theme.Selection = gfx.RGB(30, 64, 175)
		theme.SelectionForeground = theme.Foreground
	} else {
		theme.Background = gfx.RGB(244, 247, 252)
		theme.Foreground = gfx.RGB(15, 23, 42)
		theme.Card = gfx.RGB(255, 255, 255)
		theme.CardForeground = theme.Foreground
		theme.CardElevated = gfx.RGB(248, 250, 253)
		theme.Popover = theme.Card
		theme.PopoverForeground = theme.Foreground
		theme.Muted = gfx.RGB(238, 242, 248)
		theme.MutedForeground = gfx.RGB(91, 105, 128)
		theme.Border = gfx.RGB(212, 222, 236)
		theme.Hover = gfx.RGB(241, 245, 250)
		theme.Input = theme.Border
		theme.Secondary = gfx.RGB(241, 245, 250)
		theme.SecondaryForeground = theme.Foreground
		theme.Accent = theme.Hover
		theme.AccentForeground = theme.Foreground
		theme.Selection = gfx.RGB(181, 211, 255)
		theme.SelectionForeground = gfx.RGB(17, 24, 39)
	}
	return theme
}

func canvasWell(dark bool) gfx.Color {
	if dark {
		return gfx.RGB(8, 13, 22)
	}
	return gfx.RGB(232, 238, 247)
}

func sourceDisplayLabel(info sourceInfo) string {
	if info.imported {
		return i18n.T("PNG 文件")
	}
	dpi := i18n.T("未知 DPI")
	if info.dpi != 0 {
		dpi = fmt.Sprintf("%d DPI", info.dpi)
	}
	return fmt.Sprintf("%d · %s", info.displays, dpi)
}

func normalizedPointRect(a, b gfx.Point) gfx.Rect {
	return gfx.Rect{
		X:      min(a.X, b.X),
		Y:      min(a.Y, b.Y),
		Width:  abs(a.X - b.X),
		Height: abs(a.Y - b.Y),
	}
}

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

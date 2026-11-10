package main

import (
	"fmt"
	"image"
	"math"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/capture"
)

const previewBitmapID uint64 = 0x474F494D534E4950

var (
	tree  = ui.Tree{}
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
)

func (tool tool) String() string {
	switch tool {
	case toolRectangle:
		return "Rectangle"
	case toolArrow:
		return "Arrow"
	case toolFreehand:
		return "Freehand"
	default:
		return "Select area"
	}
}

func (tool tool) hint() string {
	switch tool {
	case toolRectangle:
		return "Drag to draw a rectangle"
	case toolArrow:
		return "Drag to point an arrow"
	case toolFreehand:
		return "Drag to draw freehand"
	default:
		return "Drag to select a crop"
	}
}

type outcome uint8

const (
	outcomePending outcome = iota
	outcomeConfirmed
	outcomeCanceled
	outcomeFailed
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
	preview      ui.Bitmap
	version      uint32
	cropLabel    string
	displayLabel string
	toolLabel    string
	undoHint     string
	gesture      *gesture
	pressTarget  ui.ID

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
		undoHint:     primaryShortcut() + "     Undo",
	}
	if err := editor.refreshPreview(); err != nil {
		return nil, err
	}
	return editor, nil
}

func (editor *editor) config(invalidator *app.Invalidator) app.Config {
	return app.Config{
		Tree:        &tree,
		Title:       "GoIM Snip",
		Width:       editor.windowWidth,
		Height:      editor.windowHeight,
		MinWidth:    820,
		MinHeight:   600,
		Invalidator: invalidator,
		Paint:       editor.paint,
		OnPointer:   editor.pointer,
		OnKey:       editor.key,
		OnShortcut:  editor.shortcut,
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
	editor.preview = ui.Bitmap{
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

func (editor *editor) pointer(event ui.PointerEvent) ui.EventResult {
	layout := computeLayout(event.Viewport)
	switch event.Kind {
	case ui.PointerDown:
		if event.Button != ui.PointerButtonPrimary {
			return ui.EventResult{}
		}
		editor.pressTarget = event.Target
		if event.Target != canvasID {
			return ui.EventResult{}
		}
		if !pointIn(event.X, event.Y, layout.imageArea()) {
			return ui.EventResult{Handled: true}
		}
		editor.beginGesture(event, layout)
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.PointerMove:
		if editor.gesture == nil {
			return ui.EventResult{}
		}
		editor.updateGesture(event, layout)
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.PointerUp:
		if editor.pressTarget.IsZero() {
			return ui.EventResult{}
		}
		target := editor.pressTarget
		editor.pressTarget = ui.ID{}
		if editor.gesture != nil {
			editor.updateGesture(event, layout)
			editor.commitGesture(layout)
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if event.Button == ui.PointerButtonPrimary {
			if editor.activatePointer(target, event.X, event.Y, layout) {
				return ui.EventResult{Handled: true, Repaint: !editor.done}
			}
		}
	}
	return ui.EventResult{}
}

func (editor *editor) beginGesture(event ui.PointerEvent, layout editorLayout) {
	crop := editor.document.Crop()
	imageBounds, _ := fitImageRect(layout.imageArea(), crop)
	if imageRectEmpty(imageBounds) {
		return
	}
	start := viewToSource(event.X, event.Y, crop, imageBounds)
	gesture := &gesture{start: start, end: start}
	if editor.tool == toolSelect {
		gesture.kind = gestureCrop
	} else {
		gesture.kind = gestureShape
		gesture.points = []capture.Point{start}
		switch editor.tool {
		case toolRectangle:
			gesture.shape = capture.Rectangle
		case toolArrow:
			gesture.shape = capture.Arrow
		case toolFreehand:
			gesture.shape = capture.Freehand
		}
	}
	editor.gesture = gesture
}

func (editor *editor) updateGesture(event ui.PointerEvent, layout editorLayout) {
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
		width := annotationWidth(scale)
		shape := capture.Shape{
			Kind:   gesture.shape,
			Start:  gesture.start,
			End:    gesture.end,
			Color:  capture.DefaultShapeColor,
			Width:  width,
			Points: nil,
		}
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

func (editor *editor) activatePointer(id ui.ID, x, y float32, layout editorLayout) bool {
	switch id {
	case selectID:
		if !pointIn(x, y, layout.toolButtons[0]) {
			return false
		}
		editor.selectTool(toolSelect)
	case rectID:
		if !pointIn(x, y, layout.toolButtons[1]) {
			return false
		}
		editor.selectTool(toolRectangle)
	case arrowID:
		if !pointIn(x, y, layout.toolButtons[2]) {
			return false
		}
		editor.selectTool(toolArrow)
	case drawID:
		if !pointIn(x, y, layout.toolButtons[3]) {
			return false
		}
		editor.selectTool(toolFreehand)
	case undoID:
		if !pointIn(x, y, layout.undo) {
			return false
		}
		return editor.undo()
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
	return true
}

func (editor *editor) selectTool(next tool) {
	editor.tool = next
	editor.toolLabel = next.String()
}

func (editor *editor) key(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	switch event.Key {
	case ui.KeyEscape:
		if !event.Repeat && editor.cancel() {
			return ui.EventResult{Handled: true}
		}
	case ui.KeyEnter:
		if !event.Repeat && editor.export() {
			return ui.EventResult{Handled: true}
		}
	case ui.KeySpace:
		if !event.Repeat && editor.activateKeyboard(event.Target) {
			return ui.EventResult{Handled: true, Repaint: !editor.done}
		}
	}
	return ui.EventResult{}
}

func (editor *editor) activateKeyboard(id ui.ID) bool {
	switch id {
	case selectID:
		editor.selectTool(toolSelect)
	case rectID:
		editor.selectTool(toolRectangle)
	case arrowID:
		editor.selectTool(toolArrow)
	case drawID:
		editor.selectTool(toolFreehand)
	case undoID:
		return editor.undo()
	case cancelID:
		return editor.cancel()
	case exportID:
		return editor.export()
	default:
		return false
	}
	return true
}

func (editor *editor) shortcut(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	modifier := ui.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = ui.ModifierMeta
	}
	if event.Modifiers&modifier != 0 && strings.EqualFold(event.Text, "z") {
		if editor.undo() {
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return ui.EventResult{Handled: true}
	}
	return ui.EventResult{}
}

type editorLayout struct {
	body        ui.Rect
	left        ui.Rect
	center      ui.Rect
	right       ui.Rect
	header      ui.Rect
	footer      ui.Rect
	well        ui.Rect
	toolButtons [4]ui.Rect
	undo        ui.Rect
	cancel      ui.Rect
	export      ui.Rect
}

func computeLayout(size ui.Size) editorLayout {
	body := ui.Rect{Width: size.Width, Height: size.Height}
	layout := editorLayout{body: body}
	content := body.Inset(ui.All(14))
	layout.left, content = content.CutLeft(184)
	content = content.Inset(ui.Insets{Left: 14, Right: 14})
	layout.right, content = content.CutRight(232)
	layout.center = content

	leftInner := layout.left.Inset(ui.All(16))
	y := leftInner.Y
	y += 32
	y += 24
	y += 19
	y += 24
	for index := range layout.toolButtons {
		layout.toolButtons[index] = ui.Rect{X: leftInner.X, Y: y, Width: leftInner.Width, Height: 38}
		y += 45
	}
	layout.undo = ui.Rect{X: leftInner.X, Y: leftInner.Bottom() - 38, Width: leftInner.Width, Height: 38}

	centerInner := layout.center.Inset(ui.All(18))
	layout.header, centerInner = centerInner.CutTop(28)
	centerInner = centerInner.Inset(ui.Insets{Top: 10})
	layout.footer, centerInner = centerInner.CutBottom(18)
	centerInner = centerInner.Inset(ui.Insets{Bottom: 10})
	layout.well = centerInner

	rightInner := layout.right.Inset(ui.All(16))
	layout.export = ui.Rect{X: rightInner.X, Y: rightInner.Bottom() - 44, Width: rightInner.Width, Height: 44}
	layout.cancel = ui.Rect{X: rightInner.X, Y: layout.export.Y - 8 - 36, Width: rightInner.Width, Height: 36}
	return layout
}

func (layout editorLayout) imageArea() ui.Rect {
	return layout.well.Inset(ui.All(18))
}

func fitImageRect(area ui.Rect, crop image.Rectangle) (ui.Rect, float64) {
	if area.Width <= 0 || area.Height <= 0 || crop.Empty() {
		return ui.Rect{}, 1
	}
	scale := math.Min(
		float64(area.Width)/float64(crop.Dx()),
		float64(area.Height)/float64(crop.Dy()),
	)
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return ui.Rect{}, 1
	}
	width := float32(float64(crop.Dx()) * scale)
	height := float32(float64(crop.Dy()) * scale)
	return ui.Rect{
		X:      area.X + (area.Width-width)/2,
		Y:      area.Y + (area.Height-height)/2,
		Width:  width,
		Height: height,
	}, scale
}

func sourceToView(point capture.Point, crop image.Rectangle, imageBounds ui.Rect) ui.Point {
	sx := imageBounds.Width / float32(crop.Dx())
	sy := imageBounds.Height / float32(crop.Dy())
	return ui.Point{
		X: imageBounds.X + (float32(point.X)-float32(crop.Min.X))*sx,
		Y: imageBounds.Y + (float32(point.Y)-float32(crop.Min.Y))*sy,
	}
}

func viewToSource(x, y float32, crop image.Rectangle, imageBounds ui.Rect) capture.Point {
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

func pointIn(x, y float32, bounds ui.Rect) bool {
	return x >= bounds.X && x < bounds.Right() && y >= bounds.Y && y < bounds.Bottom()
}

func imageRectEmpty(bounds ui.Rect) bool {
	return bounds.Width <= 0 || bounds.Height <= 0
}

func intersectRect(a, b ui.Rect) ui.Rect {
	left := max(a.X, b.X)
	top := max(a.Y, b.Y)
	right := min(a.Right(), b.Right())
	bottom := min(a.Bottom(), b.Bottom())
	if right <= left || bottom <= top {
		return ui.Rect{}
	}
	return ui.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func (editor *editor) paint(canvas *ui.Canvas, size ui.Size) {
	theme := snipTheme(editor.dark)
	canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	layout := computeLayout(size)
	editor.paintTools(canvas, theme, layout)
	editor.paintCanvas(canvas, theme, layout)
	editor.paintExport(canvas, theme, layout)
}

func (editor *editor) paintTools(canvas *ui.Canvas, theme ui.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.left, 14, 1, theme.Border, theme.Card)
	inner := layout.left.Inset(ui.All(16))

	canvas.Rounded(ui.Rect{X: inner.X, Y: inner.Y + 4, Width: 3, Height: 18}, 1.5, theme.Primary)
	canvas.Label("GoIM Snip", ui.Rect{X: inner.X + 12, Y: inner.Y, Width: inner.Width - 12, Height: 26}, ui.TextStyle{
		Size: 18, Weight: ui.WeightSemibold, Color: theme.Foreground,
	})
	y := inner.Y + 32
	canvas.Label("Screenshot editor", ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 18}, ui.TextStyle{
		Size: 12, Color: theme.MutedForeground,
	})
	y += 24
	canvas.Fill(ui.Rect{X: inner.X, Y: y + 5, Width: inner.Width, Height: 1}, theme.Border)
	y += 19
	canvas.Label("TOOLS", ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 16}, ui.TextStyle{
		Size: 10, Weight: ui.WeightSemibold, Color: theme.MutedForeground,
	})

	labels := [...]string{"Select area", "Rectangle", "Arrow", "Freehand"}
	for index, label := range labels {
		active := int(editor.tool) == index
		button := components.BeginButton(canvas, theme, toolID(index), layout.toolButtons[index]).
			Height(layout.toolButtons[index].Height).FontSize(13)
		if active {
			button.Default(label)
		} else {
			button.Outline(label)
		}
	}

	swatchY := layout.undo.Y - 70
	canvas.Rounded(ui.Rect{X: inner.X, Y: swatchY + 2, Width: 12, Height: 12}, 6, shapeColor())
	canvas.Label("Blue ink", ui.Rect{X: inner.X + 20, Y: swatchY - 1, Width: inner.Width - 20, Height: 17}, ui.TextStyle{
		Size: 12, Color: theme.Foreground,
	})
	canvas.Label("Drag on the canvas", ui.Rect{X: inner.X, Y: layout.undo.Y - 32, Width: inner.Width, Height: 16}, ui.TextStyle{
		Size: 11, Color: theme.MutedForeground,
	})
	undo := components.BeginButton(canvas, theme, undoID, layout.undo).
		Height(layout.undo.Height).FontSize(13)
	if !editor.document.CanUndo() {
		undo = undo.Disabled()
	}
	undo.Outline("Undo")
}

func toolID(index int) ui.ID {
	switch index {
	case 0:
		return selectID
	case 1:
		return rectID
	case 2:
		return arrowID
	case 3:
		return drawID
	}
	return ui.ID{}
}

func (editor *editor) paintCanvas(canvas *ui.Canvas, theme ui.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.center, 14, 1, theme.Border, theme.Card)
	crop := editor.document.Crop()
	canvas.Label("Canvas", layout.header, ui.TextStyle{
		Size: 16, Weight: ui.WeightSemibold, Color: theme.Foreground,
	})
	canvas.LabelAligned(
		editor.cropLabel,
		layout.header, ui.TextStyle{Size: 12, Color: theme.MutedForeground}, ui.AlignEnd,
	)

	canvas.RoundedBorder(layout.well, 10, 1, theme.Border, canvasWell(editor.dark))
	area := layout.imageArea()
	imageBounds, scale := fitImageRect(area, crop)
	if !imageRectEmpty(imageBounds) {
		canvas.Elevate(imageBounds, 3, ui.ShadowSM)
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
	canvas.HitWithOptions(canvasID, layout.well, "Screenshot canvas", ui.HitOptions{
		Role:        ui.RoleGroup,
		Cursor:      ui.PointerCursorCrosshair,
		Autofocus:   true,
		Description: "Drag with the selected tool to crop or annotate.",
	})

	canvas.Label(editor.tool.hint(), layout.footer, ui.TextStyle{Size: 11, Color: theme.MutedForeground})
	canvas.LabelAligned("Fit to window", layout.footer, ui.TextStyle{Size: 11, Color: theme.MutedForeground}, ui.AlignEnd)
}

func (editor *editor) paintGesture(canvas *ui.Canvas, imageBounds ui.Rect, crop image.Rectangle, scale float64, theme ui.Theme) {
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
		dim := ui.Color{Alpha: 0.38}
		top := ui.Rect{X: imageBounds.X, Y: imageBounds.Y, Width: imageBounds.Width, Height: selection.Y - imageBounds.Y}
		bottom := ui.Rect{X: imageBounds.X, Y: selection.Bottom(), Width: imageBounds.Width, Height: imageBounds.Bottom() - selection.Bottom()}
		left := ui.Rect{X: imageBounds.X, Y: selection.Y, Width: selection.X - imageBounds.X, Height: selection.Height}
		right := ui.Rect{X: selection.Right(), Y: selection.Y, Width: imageBounds.Right() - selection.Right(), Height: selection.Height}
		for _, fill := range [...]ui.Rect{top, bottom, left, right} {
			if fill.Width > 0 && fill.Height > 0 {
				canvas.Fill(fill, dim)
			}
		}
		canvas.RoundedOutline(selection, 2, 1.5, theme.Primary)
		for _, corner := range [...]ui.Point{
			{X: selection.X, Y: selection.Y},
			{X: selection.Right(), Y: selection.Y},
			{X: selection.X, Y: selection.Bottom()},
			{X: selection.Right(), Y: selection.Bottom()},
		} {
			handle := ui.Rect{X: corner.X - 3, Y: corner.Y - 3, Width: 6, Height: 6}
			canvas.Rounded(handle, 2, ui.RGB(255, 255, 255))
			canvas.RoundedOutline(handle, 2, 1, theme.Primary)
		}
		return
	}
	shape := capture.Shape{
		Kind:   editor.gesture.shape,
		Start:  editor.gesture.start,
		End:    editor.gesture.end,
		Points: editor.gesture.points,
		Color:  capture.DefaultShapeColor,
		Width:  annotationWidth(scale),
	}
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

func drawShape(canvas *ui.Canvas, shape capture.Shape, crop image.Rectangle, imageBounds ui.Rect, scale float64) {
	local := func(point capture.Point) ui.Point {
		view := sourceToView(point, crop, imageBounds)
		return ui.Point{X: view.X - imageBounds.X, Y: view.Y - imageBounds.Y}
	}
	stroke := ui.PathStyle{
		Stroke:      shapeColor(),
		StrokeWidth: max(float32(shape.Width*scale), 1),
		Cap:         ui.CapRound,
		Join:        ui.JoinRound,
	}
	switch shape.Kind {
	case capture.Rectangle:
		start, end := local(shape.Start), local(shape.End)
		path := ui.NewPath(
			imageBounds.Width, imageBounds.Height,
			ui.MoveTo(start.X, start.Y),
			ui.LineTo(end.X, start.Y),
			ui.LineTo(end.X, end.Y),
			ui.LineTo(start.X, end.Y),
			ui.ClosePath(),
		)
		canvas.DrawPath(path, imageBounds, stroke)
	case capture.Arrow:
		start, end := local(shape.Start), local(shape.End)
		shaft := ui.NewPath(imageBounds.Width, imageBounds.Height, ui.MoveTo(start.X, start.Y), ui.LineTo(end.X, end.Y))
		canvas.DrawPath(shaft, imageBounds, stroke)
		head := capture.ArrowHead(shape.Start, shape.End, shape.Width)
		tip, left, right := local(head[0]), local(head[1]), local(head[2])
		headPath := ui.NewPath(
			imageBounds.Width, imageBounds.Height,
			ui.MoveTo(tip.X, tip.Y),
			ui.LineTo(left.X, left.Y),
			ui.LineTo(right.X, right.Y),
			ui.ClosePath(),
		)
		canvas.DrawPath(headPath, imageBounds, ui.PathStyle{Fill: shapeColor(), FillRule: ui.PathFillNonZero})
	case capture.Freehand:
		if len(shape.Points) == 0 {
			return
		}
		commands := make([]ui.PathCommand, 0, len(shape.Points))
		start := local(shape.Points[0])
		commands = append(commands, ui.MoveTo(start.X, start.Y))
		for _, point := range shape.Points[1:] {
			next := local(point)
			commands = append(commands, ui.LineTo(next.X, next.Y))
		}
		path := ui.NewPath(imageBounds.Width, imageBounds.Height, commands...)
		canvas.DrawPath(path, imageBounds, stroke)
	}
}

func (editor *editor) paintExport(canvas *ui.Canvas, theme ui.Theme, layout editorLayout) {
	canvas.RoundedBorder(layout.right, 14, 1, theme.Border, theme.Card)
	inner := layout.right.Inset(ui.All(16))
	y := inner.Y
	canvas.Label("Export", ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 24}, ui.TextStyle{
		Size: 18, Weight: ui.WeightSemibold, Color: theme.Foreground,
	})
	y += 31
	detailStyle := ui.TextStyle{Size: 11, Color: theme.MutedForeground}
	canvas.Label(canvas.FitLine(editor.info.detail, detailStyle.Size, detailStyle.Weight, inner.Width),
		ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 16}, detailStyle)
	y += 24
	canvas.Fill(ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 1}, theme.Border)
	y += 18
	canvas.Label("SOURCE", ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 15}, ui.TextStyle{
		Size: 10, Weight: ui.WeightSemibold, Color: theme.MutedForeground,
	})
	y += 22

	rows := [...]struct {
		label string
		value string
	}{
		{"Source", editor.info.title},
		{"Image", editor.cropLabel},
		{"Displays", editor.displayLabel},
		{"Tool", editor.toolLabel},
	}
	for _, row := range rows {
		canvas.Label(row.label, ui.Rect{X: inner.X, Y: y, Width: inner.Width, Height: 14}, ui.TextStyle{
			Size: 11, Color: theme.MutedForeground,
		})
		canvas.Label(canvas.FitLine(row.value, 13, ui.WeightRegular, inner.Width),
			ui.Rect{X: inner.X, Y: y + 15, Width: inner.Width, Height: 19}, ui.TextStyle{
				Size: 13, Color: theme.Foreground,
			})
		y += 38
	}

	hintStyle := ui.TextStyle{Size: 11, Color: theme.MutedForeground}
	hintY := layout.cancel.Y - 62
	canvas.Label("Enter     Export", ui.Rect{X: inner.X, Y: hintY, Width: inner.Width, Height: 16}, hintStyle)
	canvas.Label(editor.undoHint, ui.Rect{X: inner.X, Y: hintY + 18, Width: inner.Width, Height: 16}, hintStyle)
	canvas.Label("Esc       Cancel", ui.Rect{X: inner.X, Y: hintY + 36, Width: inner.Width, Height: 16}, hintStyle)

	components.BeginButton(canvas, theme, exportID, layout.export).
		Height(layout.export.Height).FontSize(14).Default("Export PNG")
	components.BeginButton(canvas, theme, cancelID, layout.cancel).
		Height(layout.cancel.Height).FontSize(13).Outline("Cancel")
}

func snipTheme(dark bool) ui.Theme {
	theme := ui.LightTheme()
	if dark {
		theme = ui.DarkTheme()
	}
	theme.Primary = ui.RGB(29, 78, 216)
	theme.PrimaryForeground = ui.RGB(239, 246, 255)
	theme.Ring = theme.Primary
	if dark {
		theme.Background = ui.RGB(8, 12, 20)
		theme.Foreground = ui.RGB(228, 236, 247)
		theme.Card = ui.RGB(17, 24, 39)
		theme.CardForeground = theme.Foreground
		theme.CardElevated = ui.RGB(23, 33, 50)
		theme.Popover = theme.Card
		theme.PopoverForeground = theme.Foreground
		theme.Muted = ui.RGB(26, 36, 53)
		theme.MutedForeground = ui.RGB(142, 159, 182)
		theme.Border = ui.RGB(45, 59, 82)
		theme.Hover = ui.RGB(29, 41, 59)
		theme.Input = theme.Border
		theme.Secondary = ui.RGB(29, 41, 59)
		theme.SecondaryForeground = theme.Foreground
		theme.Accent = theme.Hover
		theme.AccentForeground = theme.Foreground
		theme.Primary = ui.RGB(59, 130, 246)
		theme.PrimaryForeground = ui.RGB(8, 15, 28)
		theme.Selection = ui.RGB(30, 64, 175)
		theme.SelectionForeground = theme.Foreground
	} else {
		theme.Background = ui.RGB(244, 247, 252)
		theme.Foreground = ui.RGB(15, 23, 42)
		theme.Card = ui.RGB(255, 255, 255)
		theme.CardForeground = theme.Foreground
		theme.CardElevated = ui.RGB(248, 250, 253)
		theme.Popover = theme.Card
		theme.PopoverForeground = theme.Foreground
		theme.Muted = ui.RGB(238, 242, 248)
		theme.MutedForeground = ui.RGB(91, 105, 128)
		theme.Border = ui.RGB(212, 222, 236)
		theme.Hover = ui.RGB(241, 245, 250)
		theme.Input = theme.Border
		theme.Secondary = ui.RGB(241, 245, 250)
		theme.SecondaryForeground = theme.Foreground
		theme.Accent = theme.Hover
		theme.AccentForeground = theme.Foreground
		theme.Selection = ui.RGB(181, 211, 255)
		theme.SelectionForeground = ui.RGB(17, 24, 39)
	}
	return theme
}

func canvasWell(dark bool) ui.Color {
	if dark {
		return ui.RGB(8, 13, 22)
	}
	return ui.RGB(232, 238, 247)
}

func shapeColor() ui.Color {
	return ui.RGB(capture.DefaultShapeColor.R, capture.DefaultShapeColor.G, capture.DefaultShapeColor.B)
}

func sourceDisplayLabel(info sourceInfo) string {
	if info.imported {
		return "PNG file"
	}
	dpi := "DPI unknown"
	if info.dpi != 0 {
		dpi = fmt.Sprintf("%d DPI", info.dpi)
	}
	return fmt.Sprintf("%d · %s", info.displays, dpi)
}

func primaryShortcut() string {
	if runtime.GOOS == "darwin" {
		return "Cmd+Z"
	}
	return "Ctrl+Z"
}

func normalizedPointRect(a, b ui.Point) ui.Rect {
	return ui.Rect{
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

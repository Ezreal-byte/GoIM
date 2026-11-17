package main

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"goim/internal/capture"
	"goim/internal/i18n"
)

type selectionOverlay struct {
	editor         *editor
	desktop        gfx.Bitmap
	origin         *gfx.Point
	width, height  int
	selected       bool
	selection      image.Rectangle
	drawing        *gesture
	drag           selectionDrag
	targets        []image.Rectangle
	hover          image.Rectangle
	tooltip        components.HoverTooltip
	dimensionRect  image.Rectangle
	dimensionLabel string
}

type selectionDrag struct {
	active, move, moved bool
	edges               selectionEdges
	start, end          capture.Point
	original            image.Rectangle
}

func newSelectionOverlay(e *editor, source sourceData) *selectionOverlay {
	o := &selectionOverlay{editor: e, desktop: e.preview, width: e.windowWidth, height: e.windowHeight}
	o.desktop.ID = previewBitmapID + 1
	if len(source.displays) != 0 {
		var origin gfx.Point
		o.width, o.height, origin = overlayPlacement(source.displays[0])
		o.origin = &origin
	}
	sourcePixels := e.document.SourceBounds()
	for _, window := range source.windows {
		if local := desktopRectToPixels(window, source.bounds, sourcePixels); local.Dx() >= 4 && local.Dy() >= 4 {
			o.targets = append(o.targets, local)
		}
	}
	if source.cursorKnown && source.cursor.In(source.bounds) {
		cursor := desktopRectToPixels(image.Rectangle{Min: source.cursor, Max: source.cursor.Add(image.Pt(1, 1))}, source.bounds, sourcePixels)
		if !cursor.Empty() {
			o.hover = o.targetAt(cursor.Min)
		}
	}
	return o
}

func (o *selectionOverlay) config(invalidator *app.Invalidator) app.Config {
	return app.Config{
		Tree: &tree, Title: i18n.T("GoIM 截图"), Class: "GoIM.Snip",
		Width: o.width, Height: o.height, Origin: o.origin,
		WindowMode: app.WindowFrameless, Invalidator: invalidator,
		Paint: o.paint, OnPointer: o.pointer, OnKey: o.key, OnShortcut: o.shortcut,
		TextInput: o.editor.textInput, OnClosed: o.editor.closed,
	}
}

func (o *selectionOverlay) scene(size gfx.Size) (gfx.Rect, image.Rectangle, float64) {
	return gfx.Rect{Width: size.Width, Height: size.Height}, o.editor.document.SourceBounds(),
		float64(size.Width) / float64(max(o.desktop.Width, 1))
}

func (o *selectionOverlay) pointer(event gfx.PointerEvent) gfx.EventResult {
	if o.editor.done {
		return gfx.EventResult{}
	}
	if o.editor.text.open {
		return o.editor.text.page.Pointer(event)
	}
	bounds, source, scale := o.scene(event.Viewport)
	if imageRectEmpty(bounds) {
		return gfx.EventResult{}
	}
	p := viewToSource(event.X, event.Y, source, bounds)
	pixel := image.Pt(min(int(p.X), source.Max.X-1), min(int(p.Y), source.Max.Y-1))
	handled := gfx.EventResult{Handled: true, Repaint: true}
	switch event.Kind {
	case gfx.PointerExit:
		if !o.selected && !o.drag.active && !o.hover.Empty() {
			o.hover = image.Rectangle{}
			return gfx.EventResult{Repaint: true}
		}
	case gfx.PointerDown:
		edges := resizeEdges(event.Target)
		isScene := event.Target == canvasID || event.Target == moveSelectionID || edges != 0
		if event.Button == gfx.PointerButtonSecondary && isScene {
			o.editor.cancel()
			return gfx.EventResult{Handled: true}
		}
		if event.Button != gfx.PointerButtonPrimary || !isScene {
			return gfx.EventResult{}
		}
		if edges != 0 || event.Target == moveSelectionID {
			o.drag = selectionDrag{active: true, edges: edges, move: edges == 0, start: p, end: p, original: o.selection}
			return handled
		}
		if o.selected && o.editor.tool != toolSelect {
			if !pixel.In(o.selection) {
				return handled
			}
			if o.editor.tool == toolText {
				o.editor.beginText(p, scale)
				return handled
			}
			o.drawing = &gesture{kind: gestureShape, shape: o.editor.tool.shape(), start: p, end: p, points: []capture.Point{p}}
		} else {
			o.drag = selectionDrag{active: true, start: p, end: p}
		}
		return handled
	case gfx.PointerMove, gfx.PointerUp:
		if o.drag.active {
			o.drag.end = p
			o.drag.moved = o.drag.moved || math.Hypot(p.X-o.drag.start.X, p.Y-o.drag.start.Y)*scale >= 3
			if event.Kind == gfx.PointerUp {
				drag := o.drag
				o.drag = selectionDrag{}
				switch {
				case drag.moved:
					o.selectRegion(drag.rect(source))
				case drag.move && event.ClickCount == 2:
					o.editor.export()
					return gfx.EventResult{Handled: true}
				case drag.original.Empty():
					o.selectRegion(o.targetAt(pixel))
				}
			}
			return handled
		}
		if g := o.drawing; g != nil {
			g.end = clampCapturePoint(p, o.selection)
			if g.shape == capture.Freehand {
				last := g.points[len(g.points)-1]
				if math.Hypot(g.end.X-last.X, g.end.Y-last.Y)*scale >= 1.3 || event.Kind == gfx.PointerUp {
					g.points = append(g.points, g.end)
				}
			}
			if event.Kind == gfx.PointerUp {
				if o.editor.document.Add(o.activeShape(scale)) == nil {
					o.editor.rebuildPreview()
				}
				o.drawing = nil
			}
			return handled
		}
		if !o.selected && event.Kind == gfx.PointerMove {
			next := image.Rectangle{}
			if event.Target == canvasID {
				next = o.targetAt(pixel)
			}
			if next != o.hover {
				o.hover = next
				return gfx.EventResult{Repaint: true}
			}
		}
	}
	return gfx.EventResult{}
}

func (o *selectionOverlay) targetAt(pixel image.Point) image.Rectangle {
	for _, target := range o.targets {
		if pixel.In(target) {
			return target
		}
	}
	return o.editor.document.SourceBounds()
}

func (o *selectionOverlay) selectRegion(rect image.Rectangle) bool {
	rect = rect.Intersect(o.editor.document.SourceBounds())
	if rect.Dx() < 4 || rect.Dy() < 4 || (o.selected && rect == o.selection) {
		return false
	}
	setCrop := o.editor.document.SetCrop
	if !o.selected {
		setCrop = o.editor.document.SelectCrop
	}
	if setCrop(rect) != nil {
		return false
	}
	o.selected, o.selection = true, rect
	o.editor.rebuildPreview()
	return true
}

func clampCapturePoint(p capture.Point, r image.Rectangle) capture.Point {
	return capture.Point{X: min(max(p.X, float64(r.Min.X)), float64(r.Max.X-1)),
		Y: min(max(p.Y, float64(r.Min.Y)), float64(r.Max.Y-1))}
}

func (d selectionDrag) rect(source image.Rectangle) image.Rectangle {
	if d.original.Empty() {
		return sourceRectangle(d.start, d.end).Intersect(source)
	}
	r := d.original
	dx, dy := int(math.Round(d.end.X-d.start.X)), int(math.Round(d.end.Y-d.start.Y))
	if d.move {
		dx = min(max(dx, source.Min.X-r.Min.X), source.Max.X-r.Max.X)
		dy = min(max(dy, source.Min.Y-r.Min.Y), source.Max.Y-r.Max.Y)
		return r.Add(image.Pt(dx, dy))
	}
	if d.edges&edgeLeft != 0 {
		r.Min.X = min(max(source.Min.X, r.Min.X+dx), r.Max.X-4)
	}
	if d.edges&edgeRight != 0 {
		r.Max.X = max(min(source.Max.X, r.Max.X+dx), r.Min.X+4)
	}
	if d.edges&edgeTop != 0 {
		r.Min.Y = min(max(source.Min.Y, r.Min.Y+dy), r.Max.Y-4)
	}
	if d.edges&edgeBottom != 0 {
		r.Max.Y = max(min(source.Max.Y, r.Max.Y+dy), r.Min.Y+4)
	}
	return r
}

func (o *selectionOverlay) activeShape(scale float64) capture.Shape {
	return o.editor.annotation(o.drawing, scale)
}

func (o *selectionOverlay) key(event gfx.KeyEvent) gfx.EventResult {
	if o.editor.text.open {
		return o.editor.text.page.Key(event)
	}
	if event.Release || event.Repeat {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyEscape:
		o.editor.cancel()
		return gfx.EventResult{Handled: true}
	case gfx.KeyEnter:
		if !event.Target.IsZero() && event.Target != canvasID {
			if o.activate(event.Target) {
				return gfx.EventResult{Handled: true, Repaint: !o.editor.done}
			}
			return gfx.EventResult{}
		}
		if !o.selected && !o.hover.Empty() {
			o.selectRegion(o.hover)
		}
		if o.selected {
			o.editor.export()
			return gfx.EventResult{Handled: true}
		}
	case gfx.KeySpace:
		if o.activate(event.Target) {
			return gfx.EventResult{Handled: true, Repaint: !o.editor.done}
		}
	}
	return gfx.EventResult{}
}

func (o *selectionOverlay) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || o.editor.text.open || event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 {
		return gfx.EventResult{}
	}
	switch strings.ToLower(event.Text) {
	case "a":
		return gfx.EventResult{Handled: true, Repaint: o.activate(screenSelectionID)}
	case "z":
		if event.Modifiers&gfx.ModifierShift != 0 {
			return gfx.EventResult{Handled: true, Repaint: o.activate(redoID)}
		}
		return gfx.EventResult{Handled: true, Repaint: o.activate(undoID)}
	case "y":
		return gfx.EventResult{Handled: true, Repaint: o.activate(redoID)}
	case "c":
		return gfx.EventResult{Handled: o.activate(exportID)}
	}
	return gfx.EventResult{}
}

func (o *selectionOverlay) activate(id gfx.ID) bool {
	if o.editor.done {
		return false
	}
	if id == screenSelectionID {
		changed := o.editor.tool != toolSelect
		o.editor.tool = toolSelect
		return o.selectRegion(o.editor.document.SourceBounds()) || changed
	}
	if id == redoID || id == undoID {
		change := o.editor.redo
		if id == undoID {
			change = o.editor.undo
		}
		if !change() {
			return false
		}
		o.selection, o.selected = o.editor.document.Crop(), o.editor.document.CanUndo()
		if !o.selected {
			o.selection = image.Rectangle{}
			o.editor.selectTool(toolSelect)
		}
		return true
	}
	if !o.selected {
		return false
	}
	for i, candidate := range toolIDs {
		if id == candidate {
			o.editor.selectTool(tool(i))
			return true
		}
	}
	switch id {
	case cancelID:
		return o.editor.cancel()
	case exportID:
		return o.editor.export()
	default:
		return false
	}
}

func (o *selectionOverlay) paint(c *gfx.Canvas, size gfx.Size) {
	bounds, source, scale := o.scene(size)
	c.DrawBitmap(o.desktop, bounds)
	// Bitmap batches render above flat geometry on the same layer.
	c.PushLayer(1)
	defer c.PopLayer()
	cursor := gfx.PointerCursorCrosshair
	c.HitWithOptions(canvasID, bounds, i18n.T("截图区域"), gfx.HitOptions{Role: gfx.RoleGroup, Cursor: cursor, SkipFocus: true})
	selection := o.selection
	if !o.selected {
		selection = o.hover
	}
	if o.drag.active {
		if o.drag.moved {
			selection = o.drag.rect(source)
		} else if !o.selected {
			selection = o.hover
		}
	}
	dim := gfx.RGBA(10, 14, 23, 0.53)
	if selection.Empty() {
		c.Fill(bounds, dim)
		o.paintStartHint(c, bounds)
		o.tooltip.Paint(c, gfx.DarkTheme(), bounds, components.TooltipOptions{Layer: 3}, 0)
		return
	}
	a := sourceToView(capture.Point{X: float64(selection.Min.X), Y: float64(selection.Min.Y)}, source, bounds)
	b := sourceToView(capture.Point{X: float64(selection.Max.X), Y: float64(selection.Max.Y)}, source, bounds)
	rect := normalizedPointRect(a, b)
	for _, outside := range [...]gfx.Rect{
		{Width: bounds.Width, Height: rect.Y},
		{Y: rect.Bottom(), Width: bounds.Width, Height: bounds.Bottom() - rect.Bottom()},
		{Y: rect.Y, Width: rect.X, Height: rect.Height},
		{X: rect.Right(), Y: rect.Y, Width: bounds.Right() - rect.Right(), Height: rect.Height},
	} {
		if !imageRectEmpty(outside) {
			c.Fill(outside, dim)
		}
	}
	c.Clip(rect)
	if o.selected {
		old := o.editor.document.Crop()
		c.DrawBitmap(o.editor.preview, normalizedPointRect(
			sourceToView(capture.Point{X: float64(old.Min.X), Y: float64(old.Min.Y)}, source, bounds),
			sourceToView(capture.Point{X: float64(old.Max.X), Y: float64(old.Max.Y)}, source, bounds),
		))
	}
	c.PushLayer(2)
	if o.drawing != nil {
		drawShape(c, o.activeShape(scale), source, bounds, scale)
	}
	c.PopLayer()
	c.Unclip()
	accent := gfx.RGB(67, 180, 255)
	c.RoundedOutline(rect, 0, 2, accent)
	labelWidth := min(float32(116), max(bounds.Width-8, 1))
	label := gfx.Rect{X: min(max(rect.X+10, 4), max(bounds.Right()-labelWidth-4, 4)), Y: max(rect.Y-26, 3), Width: labelWidth, Height: 22}
	c.Rounded(label, 4, gfx.RGB(25, 30, 40))
	if selection != o.dimensionRect {
		o.dimensionRect = selection
		o.dimensionLabel = fmt.Sprintf("%d × %d", selection.Dx(), selection.Dy())
	}
	c.LabelCenter(o.dimensionLabel, label, gfx.TextStyle{Size: 11, Color: gfx.RGB(248, 250, 252)})
	if o.selected && (!o.drag.active || !o.drag.original.Empty()) {
		o.paintSelectionHandles(c, bounds, rect, accent)
	}
	if o.selected && !o.drag.active && o.drawing == nil {
		o.paintToolbar(c, bounds, rect)
	} else if !o.selected && !o.drag.active {
		o.paintStartHint(c, bounds)
	}
	o.tooltip.Paint(c, gfx.DarkTheme(), bounds, components.TooltipOptions{Layer: 3}, 0)
	o.editor.paintActions(c, snipTheme(o.editor.dark), bounds)
}

func desktopRectToPixels(rect, desktop, pixels image.Rectangle) image.Rectangle {
	rect = rect.Intersect(desktop)
	if rect.Empty() || desktop.Empty() {
		return image.Rectangle{}
	}
	// Backing pixels can be denser than desktop coordinates (e.g. Retina).
	return image.Rect(
		pixels.Min.X+(rect.Min.X-desktop.Min.X)*pixels.Dx()/desktop.Dx(),
		pixels.Min.Y+(rect.Min.Y-desktop.Min.Y)*pixels.Dy()/desktop.Dy(),
		pixels.Min.X+((rect.Max.X-desktop.Min.X)*pixels.Dx()+desktop.Dx()-1)/desktop.Dx(),
		pixels.Min.Y+((rect.Max.Y-desktop.Min.Y)*pixels.Dy()+desktop.Dy()-1)/desktop.Dy(),
	).Intersect(pixels)
}

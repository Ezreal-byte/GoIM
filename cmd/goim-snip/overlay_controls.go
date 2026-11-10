package main

import (
	"github.com/vivaui/viva/ui"
	"goim/internal/i18n"
)

type selectionEdges uint8

const (
	edgeLeft selectionEdges = 1 << iota
	edgeRight
	edgeTop
	edgeBottom
)

var (
	moveSelectionID   = tree.ID(page, "move-selection")
	screenSelectionID = tree.ID(tools, "select-screen")
	selectionGrips    = [...]struct {
		id     ui.ID
		edges  selectionEdges
		cursor ui.PointerCursor
	}{
		{tree.ID(page, "resize-top"), edgeTop, ui.PointerCursorResizeVertical},
		{tree.ID(page, "resize-right"), edgeRight, ui.PointerCursorResizeHorizontal},
		{tree.ID(page, "resize-bottom"), edgeBottom, ui.PointerCursorResizeVertical},
		{tree.ID(page, "resize-left"), edgeLeft, ui.PointerCursorResizeHorizontal},
		{tree.ID(page, "resize-top-left"), edgeTop | edgeLeft, ui.PointerCursorResizeNWSE},
		{tree.ID(page, "resize-top-right"), edgeTop | edgeRight, ui.PointerCursorResizeNESW},
		{tree.ID(page, "resize-bottom-right"), edgeBottom | edgeRight, ui.PointerCursorResizeNWSE},
		{tree.ID(page, "resize-bottom-left"), edgeBottom | edgeLeft, ui.PointerCursorResizeNESW},
	}
)

func resizeEdges(id ui.ID) selectionEdges {
	for _, grip := range selectionGrips {
		if id == grip.id {
			return grip.edges
		}
	}
	return 0
}

func (o *selectionOverlay) paintSelectionHandles(c *ui.Canvas, screen, rect ui.Rect, accent ui.Color) {
	if o.editor.tool == toolSelect {
		c.HitWithOptions(moveSelectionID, rect, i18n.T("拖动移动选区"), ui.HitOptions{
			Role: ui.RoleGroup, SkipFocus: true, Cursor: ui.PointerCursorHand,
		})
	}
	cx, cy := rect.X+rect.Width/2, rect.Y+rect.Height/2
	const reach float32 = 8
	for _, grip := range selectionGrips {
		x, y := cx, cy
		if grip.edges&edgeLeft != 0 {
			x = rect.X
		}
		if grip.edges&edgeRight != 0 {
			x = rect.Right()
		}
		if grip.edges&edgeTop != 0 {
			y = rect.Y
		}
		if grip.edges&edgeBottom != 0 {
			y = rect.Bottom()
		}
		hit := ui.Rect{X: x - reach, Y: y - reach, Width: 2 * reach, Height: 2 * reach}
		if x == cx {
			hit.X, hit.Width = rect.X, rect.Width
		}
		if y == cy {
			hit.Y, hit.Height = rect.Y, rect.Height
		}
		hit = intersectRect(screen, hit)
		if !imageRectEmpty(hit) {
			c.HitWithOptions(grip.id, hit, i18n.T("拖动调整选区"), ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Cursor: grip.cursor})
		}
		// Inset a few pixels at the monitor boundary so the whole handle stays visible.
		handleX := min(max(x, screen.X+4), screen.Right()-4)
		handleY := min(max(y, screen.Y+4), screen.Bottom()-4)
		r := ui.Rect{X: handleX - 3, Y: handleY - 3, Width: 6, Height: 6}
		c.RoundedBorder(r, 1, 1, accent, ui.RGB(247, 251, 255))
	}
}

func (o *selectionOverlay) paintStartHint(c *ui.Canvas, screen ui.Rect) {
	width := min(float32(446), max(screen.Width-16, 1))
	hint := ui.Rect{X: (screen.Width - width) / 2, Y: 18, Width: width, Height: 42}
	c.PushLayer(2)
	defer c.PopLayer()
	c.RoundedBorder(hint, 10, 1, ui.RGB(64, 73, 91), ui.RGB(24, 29, 39))
	c.Block(hint)
	c.LabelCenter(i18n.T("单击选窗口 · 拖动框选 · Esc 取消"), ui.Rect{X: hint.X + 10, Y: hint.Y, Width: max(hint.Width-54, 1), Height: hint.Height}, ui.TextStyle{Size: 13, Color: ui.RGB(244, 247, 252)})
	o.paintToolButton(c, ui.Rect{X: hint.Right() - 39, Y: hint.Y + 5, Width: 34, Height: 32},
		screenSelectionID, i18n.T("选择整屏"), snipMonitorIcon, false, false, false)
}

func (o *selectionOverlay) paintToolbar(c *ui.Canvas, screen, selection ui.Rect) {
	const h float32 = 40
	actions := [...]struct {
		id    ui.ID
		label string
		icon  ui.Icon
		tool  tool
	}{
		{selectID, "选区", snipRegionIcon, toolSelect},
		{screenSelectionID, "选择整屏", snipMonitorIcon, toolSelect},
		{rectID, "矩形", snipRectangleIcon, toolRectangle},
		{arrowID, "箭头", snipArrowIcon, toolArrow},
		{drawID, "画笔", snipPenIcon, toolFreehand},
		{undoID, "撤销", snipUndoIcon, toolSelect},
		{cancelID, "取消", snipCancelIcon, toolSelect},
		{exportID, "完成", snipCheckIcon, toolSelect},
	}
	step := min(float32(35), max((screen.Width-40)/float32(len(actions)), 1))
	width := float32(len(actions))*step + 16
	x := min(max(selection.Right()-width, 10), max(screen.Right()-width-10, 10))
	y := selection.Bottom() + 10
	if y+h+8 > screen.Bottom() {
		y = selection.Y - h - 10
	}
	if y < screen.Y+8 {
		// On a maximized selection keep controls inside the crop and away from
		// every screen edge, leaving the resize handles available.
		x = min(x, screen.Right()-width-18)
		y = max(screen.Bottom()-h-20, screen.Y+8)
	}
	bar := ui.Rect{X: x, Y: y, Width: width, Height: h}
	c.PushLayer(2)
	defer c.PopLayer()
	c.RoundedBorder(bar, 10, 1, ui.RGB(64, 73, 91), ui.RGB(24, 29, 39))
	c.Block(bar)
	x += 5
	for _, action := range actions {
		if action.id == undoID || action.id == cancelID {
			c.Fill(ui.Rect{X: x + 1, Y: y + 10, Width: 1, Height: 20}, ui.RGB(71, 80, 95))
			x += 3
		}
		r := ui.Rect{X: x, Y: y + 4, Width: max(step-3, 1), Height: 32}
		x += step
		active := (action.id == selectID || action.id == rectID || action.id == arrowID || action.id == drawID) && action.tool == o.editor.tool
		o.paintToolButton(c, r, action.id, i18n.T(action.label), action.icon, active, action.id == exportID, action.id == undoID && len(o.history) == 0)
	}
}

func (o *selectionOverlay) paintToolButton(c *ui.Canvas, r ui.Rect, id ui.ID, label string, icon ui.Icon, active, primary, disabled bool) {
	color := ui.RGB(241, 245, 252)
	switch {
	case disabled:
		color = ui.RGB(136, 148, 165)
	case primary:
		bg := ui.RGB(27, 103, 186)
		if c.Hovered() == id {
			bg = ui.RGB(36, 122, 214)
		}
		c.Rounded(r, 6, bg)
	case c.Pressed() == id || active:
		c.Rounded(r, 6, ui.RGB(56, 69, 89))
	case c.Hovered() == id:
		c.Rounded(r, 6, ui.RGB(43, 53, 71))
	}
	if c.FocusVisible() == id {
		c.RoundedOutline(r, 6, 1, ui.RGB(105, 180, 250))
	}
	edge := min(19, r.Width, r.Height)
	c.DrawIcon(icon, ui.Rect{X: r.X + (r.Width-edge)/2, Y: r.Y + (r.Height-edge)/2, Width: edge, Height: edge}, color)
	c.HitWithOptions(id, r, label, ui.HitOptions{Role: ui.RoleButton, Hint: label, Selected: active, Disabled: disabled, Cursor: ui.PointerCursorHand})
	if !disabled && c.Clicked(id) && o.activate(id) && !o.editor.done {
		c.RequestFrame()
	}
}

func snipIcon(commands ...ui.PathCommand) ui.Icon {
	return ui.Icon{ViewBox: 24, Stroke: 1.75, Cap: ui.CapRound, Join: ui.JoinRound, Commands: commands}
}

var (
	snipRegionIcon = snipIcon(
		ui.MoveTo(3, 8), ui.LineTo(3, 3), ui.LineTo(8, 3), ui.MoveTo(16, 3), ui.LineTo(21, 3), ui.LineTo(21, 8),
		ui.MoveTo(21, 16), ui.LineTo(21, 21), ui.LineTo(16, 21), ui.MoveTo(8, 21), ui.LineTo(3, 21), ui.LineTo(3, 16),
		ui.MoveTo(8, 12), ui.LineTo(16, 12), ui.MoveTo(12, 8), ui.LineTo(12, 16))
	snipMonitorIcon = snipIcon(
		ui.MoveTo(3, 4), ui.LineTo(21, 4), ui.LineTo(21, 17), ui.LineTo(3, 17), ui.ClosePath(),
		ui.MoveTo(12, 17), ui.LineTo(12, 21), ui.MoveTo(8, 21), ui.LineTo(16, 21))
	snipRectangleIcon = snipIcon(ui.MoveTo(4, 5), ui.LineTo(20, 5), ui.LineTo(20, 19), ui.LineTo(4, 19), ui.ClosePath())
	snipArrowIcon     = snipIcon(ui.MoveTo(4, 20), ui.LineTo(20, 4), ui.MoveTo(10, 4), ui.LineTo(20, 4), ui.LineTo(20, 14))
	snipPenIcon       = snipIcon(ui.MoveTo(4, 20), ui.LineTo(5, 15), ui.LineTo(16, 4), ui.QuadTo(17, 3, 18, 4), ui.LineTo(20, 6),
		ui.QuadTo(21, 7, 20, 8), ui.LineTo(9, 19), ui.ClosePath(), ui.MoveTo(14, 6), ui.LineTo(18, 10))
	snipUndoIcon = snipIcon(ui.MoveTo(9, 4), ui.LineTo(4, 9), ui.LineTo(9, 14), ui.MoveTo(4, 9), ui.LineTo(15, 9),
		ui.CubicTo(22, 9, 22, 20, 15, 20), ui.LineTo(11, 20))
	snipCancelIcon = snipIcon(ui.MoveTo(6, 6), ui.LineTo(18, 18), ui.MoveTo(18, 6), ui.LineTo(6, 18))
	snipCheckIcon  = snipIcon(ui.MoveTo(4, 12), ui.LineTo(9, 17), ui.LineTo(20, 6))
)

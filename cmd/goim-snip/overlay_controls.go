package main

import (
	"github.com/vivaui/viva/gfx"
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
		id     gfx.ID
		edges  selectionEdges
		cursor gfx.PointerCursor
	}{
		{tree.ID(page, "resize-top"), edgeTop, gfx.PointerCursorResizeVertical},
		{tree.ID(page, "resize-right"), edgeRight, gfx.PointerCursorResizeHorizontal},
		{tree.ID(page, "resize-bottom"), edgeBottom, gfx.PointerCursorResizeVertical},
		{tree.ID(page, "resize-left"), edgeLeft, gfx.PointerCursorResizeHorizontal},
		{tree.ID(page, "resize-top-left"), edgeTop | edgeLeft, gfx.PointerCursorResizeNWSE},
		{tree.ID(page, "resize-top-right"), edgeTop | edgeRight, gfx.PointerCursorResizeNESW},
		{tree.ID(page, "resize-bottom-right"), edgeBottom | edgeRight, gfx.PointerCursorResizeNWSE},
		{tree.ID(page, "resize-bottom-left"), edgeBottom | edgeLeft, gfx.PointerCursorResizeNESW},
	}
)

func resizeEdges(id gfx.ID) selectionEdges {
	for _, grip := range selectionGrips {
		if id == grip.id {
			return grip.edges
		}
	}
	return 0
}

func (o *selectionOverlay) paintSelectionHandles(c *gfx.Canvas, screen, rect gfx.Rect, accent gfx.Color) {
	if o.editor.tool == toolSelect {
		c.HitWithOptions(moveSelectionID, rect, i18n.T("拖动移动选区"), gfx.HitOptions{
			Role: gfx.RoleGroup, SkipFocus: true, Cursor: gfx.PointerCursorHand,
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
		hit := gfx.Rect{X: x - reach, Y: y - reach, Width: 2 * reach, Height: 2 * reach}
		if x == cx {
			hit.X, hit.Width = rect.X, rect.Width
		}
		if y == cy {
			hit.Y, hit.Height = rect.Y, rect.Height
		}
		hit = intersectRect(screen, hit)
		if !imageRectEmpty(hit) {
			c.HitWithOptions(grip.id, hit, i18n.T("拖动调整选区"), gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Cursor: grip.cursor})
		}
		// Inset a few pixels at the monitor boundary so the whole handle stays visible.
		handleX := min(max(x, screen.X+4), screen.Right()-4)
		handleY := min(max(y, screen.Y+4), screen.Bottom()-4)
		r := gfx.Rect{X: handleX - 3, Y: handleY - 3, Width: 6, Height: 6}
		c.RoundedBorder(r, 1, 1, accent, gfx.RGB(247, 251, 255))
	}
}

func (o *selectionOverlay) paintStartHint(c *gfx.Canvas, screen gfx.Rect) {
	width := min(float32(446), max(screen.Width-16, 1))
	hint := gfx.Rect{X: (screen.Width - width) / 2, Y: 18, Width: width, Height: 42}
	c.PushLayer(2)
	defer c.PopLayer()
	c.RoundedBorder(hint, 10, 1, gfx.RGB(64, 73, 91), gfx.RGB(24, 29, 39))
	c.Block(hint)
	c.LabelCenter(i18n.T("单击选窗口 · 拖动框选 · Esc 取消"), gfx.Rect{X: hint.X + 10, Y: hint.Y, Width: max(hint.Width-54, 1), Height: hint.Height}, gfx.TextStyle{Size: 13, Color: gfx.RGB(244, 247, 252)})
	o.paintToolButton(c, gfx.Rect{X: hint.Right() - 39, Y: hint.Y + 5, Width: 34, Height: 32},
		screenSelectionID, i18n.T("选择整屏"), snipMonitorIcon, false, false, false)
}

func (o *selectionOverlay) paintToolbar(c *gfx.Canvas, screen, selection gfx.Rect) {
	const h float32 = 40
	actions := [...]struct {
		id    gfx.ID
		label string
		icon  gfx.Icon
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
	bar := gfx.Rect{X: x, Y: y, Width: width, Height: h}
	c.PushLayer(2)
	defer c.PopLayer()
	c.RoundedBorder(bar, 10, 1, gfx.RGB(64, 73, 91), gfx.RGB(24, 29, 39))
	c.Block(bar)
	x += 5
	for _, action := range actions {
		if action.id == undoID || action.id == cancelID {
			c.Fill(gfx.Rect{X: x + 1, Y: y + 10, Width: 1, Height: 20}, gfx.RGB(71, 80, 95))
			x += 3
		}
		r := gfx.Rect{X: x, Y: y + 4, Width: max(step-3, 1), Height: 32}
		x += step
		active := (action.id == selectID || action.id == rectID || action.id == arrowID || action.id == drawID) && action.tool == o.editor.tool
		o.paintToolButton(c, r, action.id, i18n.T(action.label), action.icon, active, action.id == exportID, action.id == undoID && len(o.history) == 0)
	}
}

func (o *selectionOverlay) paintToolButton(c *gfx.Canvas, r gfx.Rect, id gfx.ID, label string, icon gfx.Icon, active, primary, disabled bool) {
	color := gfx.RGB(241, 245, 252)
	switch {
	case disabled:
		color = gfx.RGB(136, 148, 165)
	case primary:
		bg := gfx.RGB(27, 103, 186)
		if c.Hovered() == id {
			bg = gfx.RGB(36, 122, 214)
		}
		c.Rounded(r, 6, bg)
	case c.Pressed() == id || active:
		c.Rounded(r, 6, gfx.RGB(56, 69, 89))
	case c.Hovered() == id:
		c.Rounded(r, 6, gfx.RGB(43, 53, 71))
	}
	if c.FocusVisible() == id {
		c.RoundedOutline(r, 6, 1, gfx.RGB(105, 180, 250))
	}
	edge := min(19, r.Width, r.Height)
	c.DrawIcon(icon, gfx.Rect{X: r.X + (r.Width-edge)/2, Y: r.Y + (r.Height-edge)/2, Width: edge, Height: edge}, color)
	c.HitWithOptions(id, r, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Selected: active, Disabled: disabled, Cursor: gfx.PointerCursorHand})
	if !disabled && c.Clicked(id) && o.activate(id) && !o.editor.done {
		c.RequestFrame()
	}
}

func snipIcon(commands ...gfx.PathCommand) gfx.Icon {
	return gfx.Icon{ViewBox: 24, Stroke: 1.75, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: commands}
}

var (
	snipRegionIcon = snipIcon(
		gfx.MoveTo(3, 8), gfx.LineTo(3, 3), gfx.LineTo(8, 3), gfx.MoveTo(16, 3), gfx.LineTo(21, 3), gfx.LineTo(21, 8),
		gfx.MoveTo(21, 16), gfx.LineTo(21, 21), gfx.LineTo(16, 21), gfx.MoveTo(8, 21), gfx.LineTo(3, 21), gfx.LineTo(3, 16),
		gfx.MoveTo(8, 12), gfx.LineTo(16, 12), gfx.MoveTo(12, 8), gfx.LineTo(12, 16))
	snipMonitorIcon = snipIcon(
		gfx.MoveTo(3, 4), gfx.LineTo(21, 4), gfx.LineTo(21, 17), gfx.LineTo(3, 17), gfx.ClosePath(),
		gfx.MoveTo(12, 17), gfx.LineTo(12, 21), gfx.MoveTo(8, 21), gfx.LineTo(16, 21))
	snipRectangleIcon = snipIcon(gfx.MoveTo(4, 5), gfx.LineTo(20, 5), gfx.LineTo(20, 19), gfx.LineTo(4, 19), gfx.ClosePath())
	snipArrowIcon     = snipIcon(gfx.MoveTo(4, 20), gfx.LineTo(20, 4), gfx.MoveTo(10, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 14))
	snipPenIcon       = snipIcon(gfx.MoveTo(4, 20), gfx.LineTo(5, 15), gfx.LineTo(16, 4), gfx.QuadTo(17, 3, 18, 4), gfx.LineTo(20, 6),
		gfx.QuadTo(21, 7, 20, 8), gfx.LineTo(9, 19), gfx.ClosePath(), gfx.MoveTo(14, 6), gfx.LineTo(18, 10))
	snipUndoIcon = snipIcon(gfx.MoveTo(9, 4), gfx.LineTo(4, 9), gfx.LineTo(9, 14), gfx.MoveTo(4, 9), gfx.LineTo(15, 9),
		gfx.CubicTo(22, 9, 22, 20, 15, 20), gfx.LineTo(11, 20))
	snipCancelIcon = snipIcon(gfx.MoveTo(6, 6), gfx.LineTo(18, 18), gfx.MoveTo(18, 6), gfx.LineTo(6, 18))
	snipCheckIcon  = snipIcon(gfx.MoveTo(4, 12), gfx.LineTo(9, 17), gfx.LineTo(20, 6))
)

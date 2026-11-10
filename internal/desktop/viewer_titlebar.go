package desktop

import (
	"math"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

const viewerTitlebarHeight = 40

type viewerMenuIDs struct {
	trigger, scope, dismiss, fit, actual, rotateLeft ui.ID
}

func newViewerMenuIDs(tree *ui.Tree, dialog ui.Scope) viewerMenuIDs {
	return viewerMenuIDs{
		trigger:    tree.ID(dialog, "image-viewer-more"),
		scope:      tree.ID(dialog, "image-viewer-menu"),
		dismiss:    tree.ID(dialog, "image-viewer-menu-dismiss"),
		fit:        tree.ID(dialog, "image-viewer-menu-fit"),
		actual:     tree.ID(dialog, "image-viewer-menu-actual"),
		rotateLeft: tree.ID(dialog, "image-viewer-menu-rotate-left"),
	}
}

func (u *UI) imageViewerConfig() app.Config {
	colors := paletteFor(themeForName(u.state.Settings.Theme))
	return app.Config{
		Tree: &u.tree, Title: i18n.T("GoIM · 图片查看"), Class: "GoIM.Desktop", Icon: windowIcon(),
		Width: 920, Height: 680, MinWidth: 640, MinHeight: 310,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: viewerTitlebarHeight,
		ClearColor: colorToApp(colors.chat),
		WindowControls: func() app.WindowControlsStyle {
			return windowControlsFor(paletteFor(themeForName(u.state.Settings.Theme)))
		},
		Paint: func(c *ui.Canvas, size ui.Size) {
			u.drawImageViewer(c, ui.Rect{Width: size.Width, Height: size.Height}, true)
		},
		OnKey: u.imageViewerKey, OnScroll: u.imageViewerWheel,
		OnFullscreen: func(active bool) { u.chatSurface().viewer.fullscreen = active },
	}
}

func (u *UI) drawViewerTitlebar(c *ui.Canvas, bar, viewport ui.Rect, theme ui.Theme, ids chatSurfaceIDs,
	state *imageViewerState, index, total int, path string) {
	colors := paletteFor(theme)
	c.Fill(bar, colors.rail)
	c.Fill(ui.Rect{X: bar.X, Y: bar.Bottom() - 1, Width: bar.Width, Height: 1}, colors.border)

	leading, trailing := app.TitlebarInsets()
	if state.fullscreen {
		leading, trailing = 0, 0
	}
	right := bar.Right() - trailing
	more := ui.Rect{X: right - 40, Y: bar.Y + 4, Width: 34, Height: bar.Height - 8}
	divider := func(x float32) {
		c.Fill(ui.Rect{X: x, Y: bar.Y + 11, Width: 1, Height: bar.Height - 22}, colors.border)
	}
	if trailing > 0 {
		divider(right - 18)
		more.X = right - 63
	}

	x := bar.X + leading + 6
	// Groups preserve the reference spacing at ordinary sizes. Fit the slots
	// when the OS constrains the window below its requested minimum.
	const groupSpace = float32(13)
	step := min(float32(40), max((more.X-x-4*groupSpace-6)/9, 0))
	button := func(id ui.ID, label string, icon ui.Icon, transform ui.IconTransform, disabled, selected bool) bool {
		bounds := ui.Rect{X: x + 3, Y: bar.Y + 4, Width: max(step-6, 0), Height: bar.Height - 8}
		x += step
		return viewerTitleButton(c, theme, colors, bounds, id, label, icon, transform, disabled, selected)
	}
	group := func() { divider(x + 6); x += groupSpace }

	if button(ids.viewerPin, i18n.T("置顶图片窗口"), viewerPinIcon, ui.IconTransform{}, false, state.pinned) && u.pinViewer != nil {
		if u.pinViewer(!state.pinned) {
			state.pinned = !state.pinned
		}
	}
	group()
	if button(ids.viewerPrevious, i18n.T("上一张图片（←）"), viewerBackIcon, ui.IconTransform{}, index == 0, false) {
		u.navigateImageViewer(-1)
	}
	if button(ids.viewerNext, i18n.T("下一张图片（→）"), viewerBackIcon, ui.IconTransform{Rotate: float32(math.Pi)}, index+1 >= total, false) {
		u.navigateImageViewer(1)
	}
	group()
	if button(ids.viewerZoomIn, i18n.T("放大图片"), viewerZoomInIcon, ui.IconTransform{}, false, false) {
		u.zoomImageViewer(1.25)
	}
	if button(ids.viewerZoomOut, i18n.T("缩小图片"), viewerZoomOutIcon, ui.IconTransform{}, false, false) {
		u.zoomImageViewer(0.8)
	}
	actual := !state.fit && state.zoom == 1
	sizeAction := i18n.T("实际大小 1:1")
	if actual {
		sizeAction = i18n.T("适应窗口")
	}
	if button(ids.viewerActual, sizeAction, viewerActualIcon, ui.IconTransform{}, false, actual) {
		if actual {
			u.fitImageViewer()
		} else {
			u.showImageViewerActualSize()
		}
	}
	group()
	if button(ids.viewerRotate, i18n.T("顺时针旋转图片"), viewerRotateIcon, ui.IconTransform{}, false, false) {
		u.rotateImageViewer(1)
	}
	group()
	if button(ids.viewerFullscreen, i18n.T("切换全屏"), viewerFullscreenIcon, ui.IconTransform{}, false, state.fullscreen) && u.fullscreenViewer != nil {
		if u.fullscreenViewer(!state.fullscreen) {
			state.fullscreen = !state.fullscreen
		}
	}
	if button(ids.viewerSave, i18n.T("保存原图"), viewerSaveIcon, ui.IconTransform{}, path == "", false) && u.saveViewer != nil {
		u.saveViewer(path)
	}

	if !state.menuOpen && !state.fullscreen && more.X > x {
		c.MoveRegion(ui.Rect{X: x, Y: bar.Y, Width: more.X - x, Height: bar.Height})
	}
	if viewerTitleButton(c, theme, colors, more, ids.viewerMenu.trigger, i18n.T("图片查看选项"), MoreHorizontal, ui.IconTransform{}, false, state.menuOpen) {
		state.menuOpen = !state.menuOpen
	}
	if state.menuOpen {
		u.drawViewerOptions(c, viewport, more, theme, ids.viewerMenu, state)
	}
}

func viewerTitleButton(c *ui.Canvas, theme ui.Theme, colors palette, bounds ui.Rect, id ui.ID, label string,
	icon ui.Icon, transform ui.IconTransform, disabled, selected bool) bool {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return false
	}
	tint := colors.icon
	switch {
	case disabled:
		tint = tint.Mix(colors.rail, 0.55)
	case c.Pressed() == id:
		c.Rounded(bounds, 5, colors.selected)
	case selected:
		c.Rounded(bounds, 5, colors.selected)
		tint = colors.selectedText
	case c.Hovered() == id:
		c.Rounded(bounds, 5, colors.hover)
	}
	if c.FocusVisible() == id {
		c.RoundedOutline(bounds, 5, 1, theme.Ring)
	}
	edge := min(18, bounds.Width-2, bounds.Height-2)
	if edge > 0 {
		c.DrawIconTransformed(icon, ui.Rect{X: bounds.X + (bounds.Width-edge)/2, Y: bounds.Y + (bounds.Height-edge)/2, Width: edge, Height: edge}, tint, transform)
	}
	c.HitWithOptions(id, bounds, label, ui.HitOptions{
		Role: ui.RoleButton, Hint: label, Disabled: disabled, Selected: selected,
	})
	return !disabled && c.Clicked(id)
}

func (u *UI) drawViewerOptions(c *ui.Canvas, viewport, anchor ui.Rect, theme ui.Theme, ids viewerMenuIDs, state *imageViewerState) {
	bounds, side := components.PlaceOverlay(anchor, ui.Size{Width: 218, Height: 108}, viewport,
		components.OverlayPlacement{Align: components.OverlayAlignEnd, SideOffset: 6, Margin: 6})
	menu := components.BeginContextMenu(c, theme, bounds, components.ContextMenuOptions{
		Layer: 83, Scope: ids.scope, DismissID: ids.dismiss, Side: side, Viewport: viewport,
	})
	menu.AddItem(components.ContextMenuItem{ID: ids.fit, Label: i18n.T("适应窗口")})
	menu.AddItem(components.ContextMenuItem{ID: ids.actual, Label: i18n.T("实际大小 1:1")})
	menu.AddItem(components.ContextMenuItem{ID: ids.rotateLeft, Label: i18n.T("逆时针旋转图片")})
	menu.End()
	switch {
	case c.Clicked(ids.fit):
		u.fitImageViewer()
	case c.Clicked(ids.actual):
		u.showImageViewerActualSize()
	case c.Clicked(ids.rotateLeft):
		u.rotateImageViewer(-1)
	case c.Clicked(ids.dismiss):
	default:
		return
	}
	state.menuOpen = false
	c.RequestFocus(ids.trigger)
}

func viewerLineIcon(commands ...ui.PathCommand) ui.Icon {
	return ui.Icon{ViewBox: 24, Stroke: 1.55, Cap: ui.CapRound, Join: ui.JoinRound, Commands: commands}
}

func viewerMagnifier(plus bool) ui.Icon {
	commands := []ui.PathCommand{
		ui.MoveTo(18, 10), ui.CubicTo(18, 14.4, 14.4, 18, 10, 18), ui.CubicTo(5.6, 18, 2, 14.4, 2, 10),
		ui.CubicTo(2, 5.6, 5.6, 2, 10, 2), ui.CubicTo(14.4, 2, 18, 5.6, 18, 10), ui.ClosePath(),
		ui.MoveTo(16, 16), ui.LineTo(22, 22), ui.MoveTo(6.5, 10), ui.LineTo(13.5, 10),
	}
	if plus {
		commands = append(commands, ui.MoveTo(10, 6.5), ui.LineTo(10, 13.5))
	}
	return viewerLineIcon(commands...)
}

var (
	viewerPinIcon     = viewerLineIcon(Pin.Commands...)
	viewerBackIcon    = viewerLineIcon(ui.MoveTo(21, 12), ui.LineTo(3, 12), ui.MoveTo(10.5, 4.5), ui.LineTo(3, 12), ui.LineTo(10.5, 19.5))
	viewerZoomInIcon  = viewerMagnifier(true)
	viewerZoomOutIcon = viewerMagnifier(false)
	viewerActualIcon  = viewerLineIcon(
		ui.MoveTo(1, 3), ui.LineTo(23, 3), ui.LineTo(23, 21), ui.LineTo(1, 21), ui.ClosePath(),
		ui.MoveTo(7, 9), ui.LineTo(7, 15), ui.MoveTo(12, 10), ui.LineTo(12, 10.4),
		ui.MoveTo(12, 13.6), ui.LineTo(12, 14), ui.MoveTo(17, 9), ui.LineTo(17, 15))
	viewerRotateIcon = viewerLineIcon(
		ui.MoveTo(6, 8), ui.LineTo(22, 8), ui.LineTo(22, 20), ui.LineTo(6, 20), ui.ClosePath(),
		ui.MoveTo(1, 8), ui.CubicTo(1, 4.8, 3, 2.5, 8.5, 2.5), ui.LineTo(10, 2.5),
		ui.MoveTo(7.4, 0.4), ui.LineTo(10.4, 2.5), ui.LineTo(7.4, 5))
	viewerFullscreenIcon = viewerLineIcon(
		ui.MoveTo(8, 2), ui.LineTo(2, 2), ui.LineTo(2, 8), ui.MoveTo(16, 2), ui.LineTo(22, 2), ui.LineTo(22, 8),
		ui.MoveTo(2, 16), ui.LineTo(2, 22), ui.LineTo(8, 22), ui.MoveTo(16, 22), ui.LineTo(22, 22), ui.LineTo(22, 16),
		ui.MoveTo(7, 17), ui.LineTo(17, 7), ui.MoveTo(13, 7), ui.LineTo(17, 7), ui.LineTo(17, 11),
		ui.MoveTo(7, 13), ui.LineTo(7, 17), ui.LineTo(11, 17))
	viewerSaveIcon = viewerLineIcon(
		ui.MoveTo(7, 5), ui.LineTo(4.2, 5), ui.QuadTo(3, 5, 3, 6.2), ui.LineTo(3, 20),
		ui.QuadTo(3, 21.4, 4.4, 21.4), ui.LineTo(19.6, 21.4), ui.QuadTo(21, 21.4, 21, 20), ui.LineTo(21, 6.2),
		ui.QuadTo(21, 5, 19.8, 5), ui.LineTo(17, 5),
		ui.MoveTo(12, 1), ui.LineTo(12, 16), ui.MoveTo(7.5, 11.5), ui.LineTo(12, 16), ui.LineTo(16.5, 11.5))
)

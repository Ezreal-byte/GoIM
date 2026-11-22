package desktop

import (
	"math"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/i18n"
)

const trayPanelVisibleRows = 4

type unreadTrayPanel struct {
	shell   *UI
	window  *app.Window
	entries []unreadTrayChat
	page    *ui.Page
	tree    gfx.Tree
	rows    gfx.Scope
	wake    app.Invalidator
	onChat  func(string)
	dismiss func()
}

func newUnreadTrayPanel(shell *UI, entries []unreadTrayChat, onChat func(string), dismiss func()) *unreadTrayPanel {
	v := &unreadTrayPanel{shell: shell, entries: entries, onChat: onChat, dismiss: dismiss}
	v.page = ui.NewPage(ui.PageConfig{Root: "unread-tray", Tree: &v.tree, Theme: &shell.theme})
	v.rows = v.tree.Feature("unread-tray-rows")
	return v
}

func (r *desktopRuntime) showUnreadTrayPanel(anchor app.StatusAnchor) *app.Window {
	entries := r.ui.unreadTrayChats()
	if len(entries) == 0 {
		return nil
	}
	r.closeUnreadTrayPanel()
	r.ui.applyMediaResults()
	view := newUnreadTrayPanel(r.ui, entries, func(id string) {
		r.closeUnreadTrayPanel()
		r.openUnreadChat(id)
	}, r.closeUnreadTrayPanel)
	config := view.config(anchor)
	config.OnClosed = func() {
		view.page.Close()
		if r.trayPanel == view {
			r.trayPanel = nil
		}
	}
	window, err := r.application.OpenWindow(config, nil)
	if err != nil {
		view.page.Close()
		return nil
	}
	view.window, r.trayPanel = window, view
	return window
}

func (r *desktopRuntime) closeUnreadTrayPanel() {
	if v := r.trayPanel; v != nil {
		r.trayPanel = nil
		v.window.Close()
	}
}

func (r *desktopRuntime) updateUnreadTrayPanel(entries []unreadTrayChat) {
	v := r.trayPanel
	if v == nil {
		return
	}
	if len(entries) == 0 {
		r.closeUnreadTrayPanel()
		return
	}
	if slices.Equal(v.entries, entries) {
		return
	}
	oldWidth, oldHeight := v.size()
	v.entries = entries
	width, height := v.size()
	if width != oldWidth || height != oldHeight {
		_ = v.window.SetSize(width, height)
	}
	v.wake.Invalidate()
}

func (v *unreadTrayPanel) rowHeight() float32    { return v.shell.textExtent(66, 30) }
func (v *unreadTrayPanel) footerHeight() float32 { return v.shell.textExtent(48, 16) }

func (v *unreadTrayPanel) size() (width, height int) {
	width = int(math.Ceil(float64(v.shell.textExtent(304, 76))))
	height = int(math.Ceil(float64(float32(min(trayPanelVisibleRows, len(v.entries)))*v.rowHeight() + v.footerHeight() + 15)))
	return
}

func (v *unreadTrayPanel) config(anchor app.StatusAnchor) app.Config {
	width, height := v.size()
	return app.Config{
		Title: i18n.T("未读消息") + " · GoIM", Class: "GoIM.UnreadTray",
		Width: width, Height: height, Tree: &v.tree, Invalidator: &v.wake,
		WindowMode: app.WindowNotification, Anchor: anchor, ClearColor: colorToApp(v.shell.theme.Card),
		Paint: v.paint, OnWake: v.shell.applyMediaResults,
		OnPointer: v.page.Pointer, OnScroll: v.page.Wheel, OnKey: v.page.Key,
	}
}

func (v *unreadTrayPanel) paint(canvas *gfx.Canvas, size gfx.Size) {
	u := v.shell
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.theme.Card)
	if u.lockVisible() || u.state.Settings.HideNotificationText {
		return
	}
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Radius(10).Border(1, u.palette.border).Children(func() {
			list := ui.Scroll(f.Part("conversations")).Grow(1).MinHeight(0).FillWidth().PadSides(7, 7, 7, 7)
			if len(v.entries) > trayPanelVisibleRows {
				list = list.Scrollbars()
			}
			list.Children(func() {
				for _, entry := range v.entries {
					v.row(f, entry)
				}
			})
			ui.Divider(f)
			ui.Row(f).FillWidth().Height(v.footerHeight()).PadX(12).Items(ui.Center).Justify(ui.End).Children(func() {
				label := i18n.T("暂不处理")
				if ui.Button(f.Part("dismiss"), label).Ghost().Clicked() {
					canvas.AfterPaint(v.dismiss)
				}
			})
		})
	})
}

func (v *unreadTrayPanel) row(f ui.C, entry unreadTrayChat) {
	u := v.shell
	row := ui.Row(f.Key(entry.id)).FillWidth().Height(v.rowHeight()).Radius(8).PadX(10).Gap(14).Items(ui.Center).
		Hover(ui.Look{}.Background(u.palette.hover)).Pressed(ui.Look{}.Background(u.palette.selected)).
		HitWith(v.tree.ID(v.rows, entry.id), entry.label, gfx.HitOptions{Role: gfx.RoleButton})
	if row.Children(func() {
		v.avatar(f, entry)
		ui.Column(f).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(f, entry.title).FillWidth().FontSize(textSize(f, 15)).Weight(gfx.WeightSemibold).Ellipsis()
			if entry.sender != "" {
				ui.Text(f, entry.sender).FillWidth().FontSize(textSize(f, 12)).Muted().Ellipsis()
			}
		})
	}).Clicked() {
		f.Canvas().AfterPaint(func() { v.onChat(entry.id) })
	}
}

func (v *unreadTrayPanel) avatar(f ui.C, entry unreadTrayChat) {
	const size = 44
	u := v.shell
	ui.Stack(f).Size(size+6, size+6).Shrink(0).Children(func() {
		ui.Stack(f).Size(size, size).Absolute().Left(0).Bottom(0).Radius(9).Clip().Background(u.palette.mutedAvatar).
			Children(func() {
				if bitmap := u.avatarBitmap(entry.avatar); bitmapReady(bitmap) {
					ui.Image(f, bitmap).Fill()
					return
				}
				ui.Row(f).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
					u.avatarInitial(f, entry.title, size)
				})
			})
		width := max(19, float32(len(entry.count))*6+9)
		ui.Stack(f).Size(width, 19).Absolute().Right(0).Top(0).Radius(10).
			Background(ui.RGB(215, 52, 67)).Border(1, u.theme.Card).Children(func() {
			ui.Text(f, entry.count).FontSize(10.5).Weight(gfx.WeightSemibold).
				Color(ui.RGB(255, 255, 255)).TextCenter()
		})
	})
}

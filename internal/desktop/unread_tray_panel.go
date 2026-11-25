package desktop

import (
	"math"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

const (
	trayPanelVisibleRows = 4
	trayPanelListPadding = 3
)

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

func (r *desktopRuntime) unreadTrayPreview() app.StatusPreview {
	view, config := r.prepareUnreadTrayPanel(app.StatusAnchor{})
	if view == nil {
		return app.StatusPreview{}
	}
	return app.StatusPreview{Config: config, Opened: func(window *app.Window, err error) {
		r.bindUnreadTrayPanel(view, window, err)
	}}
}

func (r *desktopRuntime) openUnreadTrayPanel(anchor app.StatusAnchor, mode app.WindowMode) *app.Window {
	view, config := r.prepareUnreadTrayPanel(anchor)
	if view == nil {
		return nil
	}
	config.WindowMode = mode
	window, err := r.application.OpenWindow(config, nil)
	r.bindUnreadTrayPanel(view, window, err)
	return window
}

func (r *desktopRuntime) prepareUnreadTrayPanel(anchor app.StatusAnchor) (*unreadTrayPanel, app.Config) {
	if r.trayReminderPaused {
		return nil, app.Config{}
	}
	entries := r.ui.unreadTrayChats()
	if len(entries) == 0 {
		return nil, app.Config{}
	}
	r.closeUnreadTrayPanel()
	r.ui.applyMediaResults()
	view := newUnreadTrayPanel(r.ui, entries, func(id string) {
		r.closeUnreadTrayPanel()
		r.openUnreadChat(id)
	}, r.dismissUnreadTrayReminder)
	config := view.config(anchor)
	config.OnClosed = func() {
		view.page.Close()
		if r.trayPanel == view {
			r.trayPanel = nil
		}
	}
	return view, config
}

func (r *desktopRuntime) bindUnreadTrayPanel(view *unreadTrayPanel, window *app.Window, err error) {
	if window == nil {
		view.page.Close()
		if err != nil {
			r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("无法打开未读消息窗口：") + err.Error()})
		}
		return
	}
	view.window, r.trayPanel = window, view
	view.wake.Invalidate()
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

func (v *unreadTrayPanel) rowHeight() float32 {
	if len(v.entries) == 1 && v.entries[0].sender == "" {
		return v.shell.textExtent(36, 28)
	}
	return v.shell.textExtent(38, 28)
}

func (v *unreadTrayPanel) footerHeight() float32 { return v.shell.textExtent(23, 16) }

func (v *unreadTrayPanel) size() (width, height int) {
	baseWidth := float32(212)
	if len(v.entries) == 1 {
		baseWidth = 180
	}
	width = int(math.Ceil(float64(v.shell.textExtent(baseWidth, 68))))
	rowCount := float32(min(trayPanelVisibleRows, len(v.entries)))
	height = int(math.Ceil(float64(rowCount*v.rowHeight() + v.footerHeight() + 2*trayPanelListPadding + 1)))
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
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Radius(9).Border(1, u.palette.border).Children(func() {
			list := ui.Scroll(f.Part("conversations")).Grow(1).MinHeight(0).FillWidth().Pad(trayPanelListPadding)
			if len(v.entries) > trayPanelVisibleRows {
				list = list.Scrollbars()
			}
			list.Children(func() {
				for _, entry := range v.entries {
					v.row(f, entry)
				}
			})
			ui.Divider(f)
			ui.Row(f).FillWidth().Height(v.footerHeight()).PadX(6).Items(ui.Center).Justify(ui.End).Children(func() {
				label := i18n.T("暂不处理")
				if ui.Button(f.Part("dismiss"), label).Small().Ghost().FontSize(textSize(f, 11)).
					Height(v.footerHeight() - 2).Clicked() {
					canvas.AfterPaint(v.dismiss)
				}
			})
		})
	})
}

func (v *unreadTrayPanel) row(f ui.C, entry unreadTrayChat) {
	u := v.shell
	// Also mask an already-open panel before its cached entries are refreshed.
	if u.lockVisible() || u.state.Settings.HideNotificationText {
		entry.title, entry.label, entry.sender, entry.avatar = i18n.T("新消息"), i18n.T("新消息"), "", ""
	}
	row := ui.Row(f.Key(entry.id)).FillWidth().Height(v.rowHeight()).Radius(6).PadX(6).Gap(7).Items(ui.Center).
		Hover(ui.Look{}.Background(u.palette.hover)).Pressed(ui.Look{}.Background(u.palette.selected)).
		HitWith(v.tree.ID(v.rows, entry.id), entry.label, gfx.HitOptions{Role: gfx.RoleButton})
	if row.Children(func() {
		v.avatar(f, entry)
		ui.Column(f).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(f, entry.title).FillWidth().FontSize(textSize(f, 13)).Weight(gfx.WeightSemibold).Ellipsis()
			if entry.sender != "" {
				ui.Text(f, entry.sender).FillWidth().FontSize(textSize(f, 10.5)).Muted().Ellipsis()
			}
		})
	}).Clicked() {
		f.Canvas().AfterPaint(func() { v.onChat(entry.id) })
	}
}

func (v *unreadTrayPanel) avatar(f ui.C, entry unreadTrayChat) {
	const size = 26
	u := v.shell
	badgeWidth := max(14, float32(len(entry.count))*5+6)
	ui.Stack(f).Size(size+badgeWidth-5, size+4).Shrink(0).Children(func() {
		ui.Stack(f).Size(size, size).Absolute().Left(0).Bottom(0).Radius(6).Clip().Background(u.palette.mutedAvatar).
			Children(func() {
				if bitmap := u.avatarBitmap(entry.avatar); bitmapReady(bitmap) {
					ui.Image(f, bitmap).Fill()
					return
				}
				ui.Row(f).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
					u.avatarInitial(f, entry.title, size)
				})
			})
		ui.Stack(f).Size(badgeWidth, 14).Absolute().Right(0).Top(0).Radius(7).
			Background(ui.RGB(215, 52, 67)).Border(1, u.theme.Card).Children(func() {
			ui.Text(f, entry.count).FontSize(9).Weight(gfx.WeightSemibold).
				Color(ui.RGB(255, 255, 255)).TextCenter()
		})
	})
}

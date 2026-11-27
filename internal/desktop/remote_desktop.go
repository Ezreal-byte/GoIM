package desktop

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

var remoteDesktopIcon = gfx.Icon{
	ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 16), gfx.LineTo(4, 16), gfx.ClosePath(),
		gfx.MoveTo(12, 16), gfx.LineTo(12, 20), gfx.MoveTo(8, 20), gfx.LineTo(16, 20),
	},
}

func remoteDesktopUnavailable(state domain.State, peer domain.Peer) string {
	switch {
	case !state.RemoteDesktopAvailable:
		return i18n.T("当前构建未提供远程桌面助手")
	case state.Locked:
		return i18n.T("请先解锁 GoIM")
	case remoteDesktopWindowFor(state.RemoteDesktop) != remoteDesktopWindowNone:
		return i18n.T("请先结束当前远程协助")
	case peer.ID == "" || peer.ID == state.Self.ID || !peer.VisibleOnline():
		return i18n.T("联系人上线后即可请求远程协助")
	case peer.Transport != domain.TransportGoIM || !peer.RemoteDesktopSupported:
		return i18n.T("对方客户端不支持远程桌面")
	default:
		return ""
	}
}

func (u *UI) drawRemoteDesktopButton(f ui.C, peer domain.Peer) {
	label := i18n.T("远程桌面")
	reason := remoteDesktopUnavailable(u.state, peer)
	if reason != "" {
		u.remoteDesktopMenuOpen = false
	}
	ui.Row(f).Items(ui.Center).Gap(0).Children(func() {
		if u.composerActionButton(f, u.ids.chatRemoteDesktop, remoteDesktopIcon, label, reason).
			Disabled(reason != "").Tooltip(defaultIfEmpty(reason, label)).Clicked() && reason == "" {
			u.remoteDesktopMenuOpen = false
			u.submit(domain.Command{Kind: domain.StartRemoteDesktop, PeerID: peer.ID})
		}
		moreLabel := i18n.T("更多远程协助选项")
		moreBg := ui.Color{}
		if u.remoteDesktopMenuOpen {
			moreBg = u.palette.selected
		}
		more := ui.Row(f).Size(20, 34).Radius(radiusControl).Background(moreBg).Center().
			Hover(ui.Look{}.Background(u.palette.chat.Mix(u.theme.Foreground, 0.09))).
			Focus(ui.Look{}.Ring(1.5, u.theme.Ring)).
			Disabled(reason != "").
			HitWith(u.ids.chatRemoteDesktopMore, moreLabel, gfx.HitOptions{
				Role: gfx.RoleButton, Selected: u.remoteDesktopMenuOpen, Hint: defaultIfEmpty(reason, moreLabel),
			}).
			Tooltip(defaultIfEmpty(reason, moreLabel)).
			Children(func() {
				ui.Icon(f, ChevronDown).Size(14, 14).Color(u.theme.MutedForeground)
			})
		if more.Clicked() && reason == "" {
			u.remoteDesktopMenuOpen = !u.remoteDesktopMenuOpen
			if u.remoteDesktopMenuOpen {
				u.emojiOpen = false
			}
		}
		spec := ui.PopoverSpec{
			Side: components.PopoverTop, Align: ui.End,
			Width:               max(float32(236), textSize(f, 210)),
			Padding:             8,
			Offset:              6,
			Instant:             true,
			ConsumeOutsidePress: true,
		}
		ui.Popover(f, more, &u.remoteDesktopMenuOpen, spec, func() {
			u.drawRemoteDesktopPopover(f, peer)
		})
	})
}

func (u *UI) drawRemoteDesktopPopover(f ui.C, peer domain.Peer) {
	assistDisabled := !peer.RemoteDesktopAssistSupported
	assistHint := i18n.T("允许对方控制当前电脑")
	if assistDisabled {
		assistHint = i18n.T("对方客户端版本不支持请求远程协助")
	}
	ui.Column(f).FillWidth().Gap(4).Children(func() {
		if u.remoteDesktopMenuItem(f.Part("control-peer"), remoteDesktopIcon, i18n.T("控制对方电脑"), i18n.T("请求查看并控制对方屏幕"), false) {
			u.remoteDesktopMenuOpen = false
			u.submit(domain.Command{Kind: domain.StartRemoteDesktop, PeerID: peer.ID})
		}
		if u.remoteDesktopMenuItem(f.Part("request-assist"), remoteDesktopIcon, i18n.T("请求对方协助"), assistHint, assistDisabled) {
			u.remoteDesktopMenuOpen = false
			u.submit(domain.Command{Kind: domain.RequestRemoteAssistance, PeerID: peer.ID})
		}
	})
}

func (u *UI) remoteDesktopMenuItem(f ui.C, icon gfx.Icon, title, subtitle string, disabled bool) bool {
	item := ui.Button(f, title).Ghost().FillWidth().PadSides(8, 10, 8, 10).Gap(10).
		Items(ui.Center).Justify(ui.Start).Disabled(disabled).Children(func() {
		ui.Icon(f, icon).Size(16, 16).Color(u.theme.MutedForeground)
		ui.Column(f).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(f, title).FillWidth().FontSize(textSize(f, 13)).Weight(gfx.WeightMedium).Ellipsis()
			ui.Text(f, subtitle).FillWidth().FontSize(textSize(f, 11.5)).Muted().Ellipsis()
		})
	})
	return item.Clicked() && !disabled
}

type remoteDesktopWindowMode uint8

const (
	remoteDesktopWindowNone remoteDesktopWindowMode = iota
	remoteDesktopWindowConsent
	remoteDesktopWindowProgress
	remoteDesktopWindowSharing
	remoteDesktopWindowViewing
)

func remoteDesktopWindowFor(session domain.RemoteDesktopSession) remoteDesktopWindowMode {
	if session.ID == "" {
		return remoteDesktopWindowNone
	}
	switch session.Status {
	case domain.RemoteDesktopPending:
		if session.Incoming {
			return remoteDesktopWindowConsent
		}
		return remoteDesktopWindowProgress
	case domain.RemoteDesktopConnecting:
		return remoteDesktopWindowProgress
	case domain.RemoteDesktopActive:
		if session.Host {
			return remoteDesktopWindowSharing
		}
		return remoteDesktopWindowViewing
	default:
		return remoteDesktopWindowNone
	}
}

type remoteDesktopView struct {
	session      domain.RemoteDesktopSession
	mode         remoteDesktopWindowMode
	peerName     string
	peerAvatar   string
	avatarID     uint64
	avatarShell  *UI
	theme        ui.Theme
	textMenu     gfx.TextMenuLabels
	tree         gfx.Tree
	page         *ui.Page
	wake         app.Invalidator
	stopID       gfx.ID
	acceptID     gfx.ID
	collapseID   gfx.ID
	collapsed    bool
	resize       func(width, height int)
	handle       *app.Window
	accept       func()
	stop         func()
	decisionSent bool
	viewer       *remoteDesktopViewer
}

func newRemoteDesktopView(state domain.State) *remoteDesktopView {
	view := &remoteDesktopView{
		session: state.RemoteDesktop,
		mode:    remoteDesktopWindowFor(state.RemoteDesktop),
	}
	view.update(state)
	view.page = ui.NewPage(ui.PageConfig{Root: "remote-desktop", Tree: &view.tree, Theme: &view.theme, EditMenu: &view.textMenu})
	actions := view.tree.Feature("remote-desktop-actions")
	view.stopID = view.tree.ID(actions, "stop")
	if view.mode == remoteDesktopWindowConsent {
		view.acceptID = view.tree.ID(actions, "accept-control")
	}
	if view.mode == remoteDesktopWindowSharing {
		view.collapseID = view.tree.ID(actions, "toggle-collapse")
	}
	return view
}

func (v *remoteDesktopView) update(state domain.State) {
	v.session = state.RemoteDesktop
	name := v.session.PeerName
	v.peerAvatar = ""
	if peer, ok := state.LookupPeer(v.session.PeerID); ok {
		name = defaultIfEmpty(peer.DisplayName(), name)
		v.peerAvatar = peer.Avatar
	}
	if v.avatarShell != nil {
		v.avatarID = v.avatarShell.avatarBitmap(v.peerAvatar).ID
	}
	v.peerName = defaultIfEmpty(name, i18n.T("未知联系人"))
	v.theme = themeForSettings(state.Settings)
	v.textMenu = localizedTextMenuLabels()
}

func (v *remoteDesktopView) config() app.Config {
	switch v.mode {
	case remoteDesktopWindowConsent:
		return v.incomingConfig()
	case remoteDesktopWindowSharing:
		return v.sharingConfig()
	}
	config := app.Config{
		Title: i18n.T("远程桌面") + " · GoIM", Class: "GoIM.RemoteDesktop", Icon: windowIcon(),
		Width: 490, Height: 310, MinWidth: 420, MinHeight: 280,
		WindowMode: app.WindowTitled, Tree: &v.tree, Invalidator: &v.wake,
		ClearColor: colorToApp(v.theme.Background),
		OnPointer:  v.page.Pointer, OnScroll: v.page.Wheel, OnKey: v.page.Key,
		TextInput: v.page.TextInput, TextMenu: &v.textMenu, Paint: v.paintConnecting,
	}
	switch v.mode {
	case remoteDesktopWindowProgress:
		config.Height, config.MinHeight = 260, 240
		if !v.session.Incoming {
			config.Width, config.MinWidth = 460, 410
			config.Height, config.MinHeight = 278, 252
			config.Paint = v.paintWaiting
		}
	case remoteDesktopWindowViewing:
		v.viewer.configure(&config)
		return config
	}
	growth := float32(max(0, v.theme.Metrics.ScaledText(14)/14-1))
	config.MinWidth += int(110 * growth)
	config.MinHeight += int(95 * growth)
	config.Width = max(config.Width, config.MinWidth)
	config.Height = max(config.Height, config.MinHeight)
	return config
}

func (v *remoteDesktopView) paintConnecting(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, v.theme.Background)
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Pad(22).Gap(10).Children(func() {
			ui.Row(f).FillWidth().Gap(9).Items(ui.Center).Children(func() {
				ui.Icon(f, remoteDesktopIcon).Size(20, 20).Color(v.theme.Primary)
				ui.Text(f, i18n.T("远程协助")).Grow(1).Weight(gfx.WeightSemibold).FontSize(textSize(f, 17)).Ellipsis()
			})
			ui.Text(f, v.peerName).FillWidth().Weight(gfx.WeightSemibold).Ellipsis()
			ui.Paragraph(f, i18n.T("正在建立远程协助…"), 2).Muted()
			ui.Spacer(f)
			ui.Row(f).FillWidth().Justify(ui.End).Children(func() {
				if namedButton(f, v.stopID, i18n.T("取消")).Outline().Clicked() {
					f.After(v.stop)
				}
			})
		})
		canvas.SetInitialFocus(v.stopID)
	})
}

func (r *desktopRuntime) syncRemoteDesktop(state domain.State, locked bool) {
	session := state.RemoteDesktop
	mode := remoteDesktopWindowFor(session)
	if view := r.remoteDesktopView; view != nil && (locked || view.session.ID != session.ID || view.mode != mode) {
		r.closeRemoteDesktop()
	}
	if mode == remoteDesktopWindowNone {
		r.remoteDesktopDismissedID = ""
		if session.Status == domain.RemoteDesktopEnded && session.ID != "" && r.remoteDesktopNotifiedID != session.ID {
			r.remoteDesktopNotifiedID = session.ID
			if reason := remoteDesktopSessionNotice(session.Reason, session.Incoming); reason != "" && !locked {
				r.ui.showToast(i18n.T("远程桌面"), reason, components.ToastWarning)
			}
		}
		return
	}
	if locked || r.application == nil || r.remoteDesktopDismissedID == session.ID {
		return
	}
	if r.remoteDesktopView != nil {
		view := r.remoteDesktopView
		oldSession, oldName, oldTheme := view.session, view.peerName, view.theme
		oldAvatar := view.avatarID
		view.update(state)
		devicesChanged := !slices.Equal(oldSession.OutputDevices, view.session.OutputDevices)
		if view.viewer != nil && devicesChanged {
			view.viewer.setDevices(view.session.OutputDevices)
		}
		if oldSession.Status != view.session.Status || oldSession.Control != view.session.Control || oldSession.Audio != view.session.Audio ||
			oldName != view.peerName || oldTheme != view.theme || oldAvatar != view.avatarID || devicesChanged {
			view.wake.Invalidate()
		}
		return
	}

	if r.screenshotConcealed {
		return
	}
	view := newRemoteDesktopView(state)
	if mode == remoteDesktopWindowConsent {
		view.accept = func() { r.acceptRemoteDesktopInvitation(session.ID) }
	}
	view.stop = func() { r.dismissRemoteDesktop(view) }
	if mode == remoteDesktopWindowViewing {
		backend, ok := r.backend.(domain.RemoteDesktopBackend)
		if !ok {
			r.failRemoteDesktopWindow(view, i18n.T("当前构建未提供远程桌面助手"))
			return
		}
		view.viewer = newRemoteDesktopViewer(view, backend)
	}
	config := view.config()
	if config.WindowMode == app.WindowNotification || mode == remoteDesktopWindowViewing {
		view.avatarShell = r.ui
		view.avatarID = r.ui.avatarBitmap(view.peerAvatar).ID
	}
	config.OnCloseRequest = func(allow func()) {
		if view.viewer != nil {
			view.viewer.releaseInput()
		}
		r.endRemoteDesktopView(view)
		allow()
	}
	config.OnClosed = func() {
		if r.remoteDesktopView == view {
			r.remoteDesktopView = nil
			r.endRemoteDesktopView(view)
		}
		view.dispose()
	}
	if mode == remoteDesktopWindowProgress && !session.Incoming {
		centerRemoteDesktopWaiting(&config, r.window)
	}
	handle, err := r.application.OpenWindow(config, nil)
	if err != nil {
		view.dispose()
		r.failRemoteDesktopWindow(view, err.Error())
		return
	}
	view.handle, r.remoteDesktopView = handle, view
	if config.WindowMode == app.WindowNotification {
		// Viva maps desktop notifications above normal windows without activating GoIM.
		view.wake.Invalidate()
	} else {
		handle.Show()
	}
	if view.viewer != nil {
		view.viewer.start()
	}
}

func (r *desktopRuntime) acceptRemoteDesktopInvitation(id string) {
	current := r.backend.Snapshot()
	if current.Locked || r.ui.lockVisible() || current.RemoteDesktop.ID != id ||
		remoteDesktopWindowFor(current.RemoteDesktop) != remoteDesktopWindowConsent {
		return
	}
	r.backend.Submit(domain.Command{
		Kind: domain.AcceptRemoteDesktop, RemoteDesktopID: id, AllowRemoteControl: true,
	})
}

func (r *desktopRuntime) failRemoteDesktopWindow(view *remoteDesktopView, detail string) {
	r.endRemoteDesktopView(view)
	r.ui.showToast(i18n.T("无法打开远程桌面窗口"), detail, components.ToastError)
}

func (r *desktopRuntime) closeRemoteDesktop() {
	if view := r.remoteDesktopView; view != nil {
		r.remoteDesktopView = nil
		if view.viewer != nil {
			view.viewer.releaseInput()
		}
		view.handle.Close()
	}
}

func (r *desktopRuntime) dismissRemoteDesktop(view *remoteDesktopView) {
	if r.remoteDesktopView != view {
		return
	}
	r.endRemoteDesktopView(view)
	r.closeRemoteDesktop()
}

func (r *desktopRuntime) endRemoteDesktopView(view *remoteDesktopView) {
	id := view.session.ID
	if r.remoteDesktopDismissedID == id {
		return
	}
	r.remoteDesktopDismissedID = id
	current := r.backend.Snapshot().RemoteDesktop
	if current.ID != id || remoteDesktopWindowFor(current) == remoteDesktopWindowNone {
		return
	}
	kind := domain.EndRemoteDesktop
	if remoteDesktopWindowFor(current) == remoteDesktopWindowConsent && !view.decisionSent {
		kind = domain.RejectRemoteDesktop
	}
	r.backend.Submit(domain.Command{Kind: kind, RemoteDesktopID: id})
}

func (r *desktopRuntime) remoteDesktopTrayItems() []app.MenuItem {
	session := r.backend.Snapshot().RemoteDesktop
	if remoteDesktopWindowFor(session) != remoteDesktopWindowSharing {
		return nil
	}
	return []app.MenuItem{
		{ID: "remote-stop-" + session.ID, Title: i18n.T("停止共享"), OnSelect: func() {
			if current := r.backend.Snapshot().RemoteDesktop; current.ID == session.ID && current.Status == domain.RemoteDesktopActive && current.Host {
				r.backend.Submit(domain.Command{Kind: domain.EndRemoteDesktop, RemoteDesktopID: session.ID})
			}
		}},
		{Separator: true},
	}
}

func (v *remoteDesktopView) dispose() {
	if v.viewer != nil {
		v.viewer.close()
	}
	v.page.Close()
}

func remoteDesktopSessionNotice(reason string, incoming bool) string {
	switch reason {
	case "ended", "cancelled", "":
		return ""
	case "declined":
		if incoming {
			return ""
		}
		return i18n.T("对方已拒绝远程协助")
	case "no_answer":
		return i18n.T("远程协助请求已过期")
	case "disconnected":
		return i18n.T("远程桌面连接已断开")
	case "permission":
		return i18n.T("系统未授予屏幕或控制权限")
	case "unsupported":
		return i18n.T("当前桌面环境不支持远程协助")
	case "busy":
		return i18n.T("对方正在进行远程协助")
	case "locked":
		return i18n.T("应用锁定，远程协助已结束")
	default:
		return i18n.T("未能建立远程桌面连接")
	}
}

func (v *remoteDesktopView) viewerTitle() string {
	return fmt.Sprintf(i18n.T("远程桌面 · %s"), v.peerName)
}

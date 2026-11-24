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
	if u.composerActionButton(f, u.ids.chatRemoteDesktop, remoteDesktopIcon, label, reason).
		Disabled(reason != "").Tooltip(defaultIfEmpty(reason, label)).Clicked() && reason == "" {
		u.submit(domain.Command{Kind: domain.StartRemoteDesktop, PeerID: peer.ID})
	}
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
		if session.Incoming {
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
	theme        ui.Theme
	tree         gfx.Tree
	page         *ui.Page
	wake         app.Invalidator
	stopID       gfx.ID
	handle       *app.Window
	accept       func(control, audio bool)
	stop         func()
	consentAudio bool
	decisionSent bool
	viewer       *remoteDesktopViewer
}

func newRemoteDesktopView(state domain.State) *remoteDesktopView {
	view := &remoteDesktopView{
		session: state.RemoteDesktop,
		mode:    remoteDesktopWindowFor(state.RemoteDesktop),
	}
	view.update(state)
	view.page = ui.NewPage(ui.PageConfig{Root: "remote-desktop", Tree: &view.tree, Theme: &view.theme})
	view.stopID = view.tree.ID(view.tree.Feature("remote-desktop-actions"), "stop")
	return view
}

func (v *remoteDesktopView) update(state domain.State) {
	v.session = state.RemoteDesktop
	name := v.session.PeerName
	if peer, ok := state.LookupPeer(v.session.PeerID); ok {
		name = defaultIfEmpty(peer.DisplayName(), name)
	}
	v.peerName = defaultIfEmpty(name, i18n.T("未知联系人"))
	v.theme = themeForSettings(state.Settings)
}

func (v *remoteDesktopView) config() app.Config {
	if v.mode == remoteDesktopWindowConsent {
		return v.incomingConfig()
	}
	config := app.Config{
		Title: i18n.T("远程桌面") + " · GoIM", Class: "GoIM.RemoteDesktop", Icon: windowIcon(),
		Width: 490, Height: 310, MinWidth: 420, MinHeight: 280,
		WindowMode: app.WindowTitled, Tree: &v.tree, Invalidator: &v.wake,
		ClearColor: colorToApp(v.theme.Background),
		OnPointer:  v.page.Pointer, OnScroll: v.page.Wheel, OnKey: v.page.Key,
		TextInput: v.page.TextInput, Paint: v.paint,
	}
	switch v.mode {
	case remoteDesktopWindowProgress:
		config.Height, config.MinHeight = 260, 240
		if !v.session.Incoming {
			config.Width, config.MinWidth = 460, 410
			config.Height, config.MinHeight = 278, 252
			config.Paint = v.paintWaiting
		}
	case remoteDesktopWindowSharing:
		config.Height, config.MinHeight = 220, 200
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

func (v *remoteDesktopView) paint(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, v.theme.Background)
	v.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().Pad(22).Gap(10).Children(func() {
			ui.Row(f).FillWidth().Gap(9).Items(ui.Center).Children(func() {
				ui.Icon(f, remoteDesktopIcon).Size(20, 20).Color(v.theme.Primary)
				ui.Text(f, v.windowHeading()).Grow(1).Weight(gfx.WeightSemibold).FontSize(textSize(f, 17)).Ellipsis()
			})
			ui.Text(f, v.peerName).FillWidth().Weight(gfx.WeightSemibold).Ellipsis()
			switch v.mode {
			case remoteDesktopWindowProgress:
				ui.Paragraph(f, i18n.T("正在建立远程协助…"), 2).Muted()
				ui.Spacer(f)
				v.drawStop(f, i18n.T("取消"))
			case remoteDesktopWindowSharing:
				message := i18n.T("对方正在查看你的屏幕")
				if v.session.Control {
					message = i18n.T("对方可以操作本机的键盘和鼠标")
				}
				ui.Paragraph(f, message, 2).Muted()
				if v.session.Audio {
					ui.Text(f, i18n.T("正在共享系统声音")).Muted()
				}
				ui.Spacer(f)
				v.drawStop(f, i18n.T("停止共享"))
			}
		})
		canvas.SetInitialFocus(v.stopID)
	})
}

func (v *remoteDesktopView) windowHeading() string {
	switch v.mode {
	case remoteDesktopWindowSharing:
		return i18n.T("正在共享屏幕")
	default:
		return i18n.T("远程协助")
	}
}

func (v *remoteDesktopView) drawConsentActions(f ui.C) {
	ui.Row(f).FillWidth().Justify(ui.End).Items(ui.Center).Gap(8).Children(func() {
		if namedButton(f, v.stopID, i18n.T("拒绝")).Outline().Clicked() {
			f.After(v.stop)
		}
		if ui.Button(f.Part("view-only"), i18n.T("仅查看")).Outline().Disabled(v.decisionSent).Clicked() {
			v.allow(f, false)
		}
		if ui.Button(f.Part("allow-control"), i18n.T("允许控制")).Disabled(v.decisionSent).Clicked() {
			v.allow(f, true)
		}
	})
}

func (v *remoteDesktopView) allow(f ui.C, control bool) {
	if v.decisionSent || v.accept == nil {
		return
	}
	v.decisionSent = true
	audio := v.consentAudio
	f.After(func() { v.accept(control, audio) })
}

func (v *remoteDesktopView) drawStop(f ui.C, label string) {
	ui.Row(f).FillWidth().Justify(ui.End).Children(func() {
		if namedButton(f, v.stopID, label).Outline().Clicked() {
			f.After(v.stop)
		}
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
		view.update(state)
		devicesChanged := !slices.Equal(oldSession.OutputDevices, view.session.OutputDevices)
		if view.viewer != nil && devicesChanged {
			view.viewer.setDevices(view.session.OutputDevices)
		}
		if oldSession.Status != view.session.Status || oldSession.Control != view.session.Control || oldSession.Audio != view.session.Audio ||
			oldName != view.peerName || oldTheme != view.theme || devicesChanged {
			view.wake.Invalidate()
		}
		return
	}

	view := newRemoteDesktopView(state)
	view.accept = func(control, audio bool) {
		current := r.backend.Snapshot()
		if current.Locked || r.ui.lockVisible() || current.RemoteDesktop.ID != session.ID ||
			remoteDesktopWindowFor(current.RemoteDesktop) != remoteDesktopWindowConsent {
			return
		}
		r.backend.Submit(domain.Command{
			Kind: domain.AcceptRemoteDesktop, RemoteDesktopID: session.ID,
			AllowRemoteControl: control, ShareRemoteAudio: audio,
		})
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
	if mode == remoteDesktopWindowSharing {
		// On compositors that reserve stacking decisions, the tray also keeps
		// the Stop sharing action available.
		_ = handle.SetAlwaysOnTop(true)
	}
	if mode == remoteDesktopWindowConsent {
		// Viva maps incoming notifications without activating GoIM.
		view.wake.Invalidate()
	} else {
		handle.Show()
	}
	if view.viewer != nil {
		view.viewer.start()
	}
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
			if current := r.backend.Snapshot().RemoteDesktop; current.ID == session.ID && current.Status == domain.RemoteDesktopActive && current.Incoming {
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

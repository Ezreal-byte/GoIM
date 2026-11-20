package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type membersWindow struct {
	view            *UI
	window          *app.Window
	restoreOnUnlock bool
}

func activeMemberGroup(s domain.State, conversationID string) (domain.Group, bool) {
	conv := s.Conversation(conversationID)
	group := s.Group(conv.GroupID)
	return group, conv.Kind == "group" && group.ID != "" && !group.Dissolved && !group.Left
}

func (r *desktopRuntime) openMembersWindow(id string) {
	if r.application == nil || r.ui.lockVisible() {
		return
	}
	state := r.ui.state
	if _, active := activeMemberGroup(state, id); !active || state.Locked {
		return
	}
	if existing := r.memberWindows[id]; existing != nil {
		existing.window.Show()
		return
	}
	u := newUIShell(nil, false)
	u.memberWindow, u.detachedWindow = true, true
	u.backend, u.selected, u.section = r.backend, id, sectionMessages
	u.presentMemberChat = r.showPeerTarget
	u.setMemberState(state)
	entry := &membersWindow{view: u}
	config := u.membersConfig()
	config.OnWake = func() {
		u.applyMediaResults()
		u.queueMediaJobs()
	}
	config.OnKey = func(event gfx.KeyEvent) gfx.EventResult {
		if event.Key == gfx.KeyEscape && !event.Release && !u.modalOpen() && u.memberMenu == "" && entry.window != nil {
			if u.guardGroupRenameNavigation(entry.window.Close) {
				entry.window.Close()
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return u.onKey(event)
	}
	config.OnCloseRequest = func(allow func()) {
		if u.lockVisible() || u.guardGroupRenameNavigation(allow) {
			allow()
		} else {
			u.invalidator.Invalidate()
		}
	}
	config.OnClosed = func() {
		if r.memberWindows[id] == entry {
			delete(r.memberWindows, id)
		}
		u.Close()
	}
	r.geometry.remember("members", &config)
	var canvas gfx.Canvas
	window, err := r.application.OpenWindow(config, &canvas)
	if err != nil {
		u.Close()
		r.backend.Submit(domain.Command{Kind: domain.ReportError, Text: i18n.T("群聊成员") + " · " + err.Error()})
		return
	}
	entry.window = window
	if r.memberWindows == nil {
		r.memberWindows = make(map[string]*membersWindow)
	}
	r.memberWindows[id] = entry
	window.Show()
}

func (entry *membersWindow) sync(s domain.State, locked, changed bool) {
	u := entry.view
	if _, active := activeMemberGroup(s, u.selected); !active {
		entry.window.Close()
		return
	}
	if changed {
		u.setMemberState(s)
	}
	if locked != u.detachedConcealed {
		u.detachedConcealed = locked
		if locked {
			entry.restoreOnUnlock = !entry.window.Hidden()
			entry.window.Hide()
		} else if entry.restoreOnUnlock {
			entry.restoreOnUnlock = false
			entry.window.Show()
		}
		changed = true
	}
	if changed {
		_ = entry.window.SetTitle(u.membersTitle())
		u.invalidator.Invalidate()
	}
}

func (u *UI) setMemberState(s domain.State) {
	oldError := u.state.Error
	u.state = s
	u.rebuildPresentation(oldError)
	u.queueMediaJobs()
}

func (u *UI) membersTitle() string {
	group, ok := activeMemberGroup(u.state, u.selected)
	if !ok || u.lockVisible() {
		return "GoIM"
	}
	return i18n.T("群聊成员") + " · " + group.Name
}

func (u *UI) membersConfig() app.Config {
	return app.Config{
		Title: u.membersTitle(), Class: "GoIM.GroupMembers", Icon: windowIcon(), Width: 560, Height: 740, MinWidth: 440, MinHeight: 520,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: 44, WindowControls: u.windowControls,
		Tree: &u.tree, Invalidator: &u.invalidator, ClearColor: colorToApp(u.palette.sidebar),
		Paint: u.paintMembersWindow, TextInput: u.textInput, OnKey: u.onKey, OnPointer: u.onPointer, OnScroll: u.onScroll,
	}
}

func (u *UI) paintMembersWindow(canvas *gfx.Canvas, size gfx.Size) {
	bounds := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(bounds, u.palette.sidebar)
	group, active := activeMemberGroup(u.state, u.selected)
	if !active || u.lockVisible() {
		return
	}
	u.focusedID = canvas.Focused()
	u.lastBody, u.lastChat = bounds, bounds
	title := ui.Rect{Width: size.Width, Height: 44}
	leading, trailing := app.TitlebarInsets()
	u.titlePage.Draw(canvas, title, func(f ui.C) {
		ui.Row(f).Fill().MovesWindow().PadSides(8, trailing+16, 8, leading+20).Items(ui.Center).Children(func() {
			ui.Text(f, u.membersTitle()).FontSize(14).Weight(gfx.WeightMedium).Grow(1).Ellipsis()
		})
	})
	body := ui.Rect{X: 18, Y: 56, Width: max(size.Width-36, 0), Height: max(size.Height-124, 0)}
	footer := ui.Rect{X: 18, Y: size.Height - 60, Width: body.Width, Height: 56}
	owner := group.OwnerID != "" && group.OwnerID == u.state.Self.ID
	anchor, avatar := u.drawMemberContent(canvas, body, u.state.Conversation(u.selected), group, owner)
	paintDialogActions(canvas, u.theme, footer, u.membershipAction(owner))
	u.handleMemberActions(canvas, bounds, group, owner, anchor, avatar)
	u.drawToasts(canvas, bounds)
	u.drawOverlays(canvas, bounds)
}

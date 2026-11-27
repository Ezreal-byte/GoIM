package desktop

import (
	"errors"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
	"goim/internal/systemauth"
)

var lockRequests atomic.Uint64

func nextLockRequest() uint64 { return lockRequests.Add(1) }

type lockUIState struct {
	scope                             gfx.Scope
	page                              *ui.Page
	theme                             ui.Theme
	focus, confirmID, submit, cancel  gfx.ID
	systemButton, revealButton        gfx.ID
	field, confirmation               ui.Field
	setup, pending, focused, revealed bool
	restoreMotion                     bool
	request                           uint64
	err                               string
	systemBusy                        bool
	systemRequest                     uint64
	systemErr                         string
	systemResults                     chan systemAuthResult
	systemAuth                        func(string) error
	method                            systemauth.Kind
}

type systemAuthResult struct {
	request uint64
	err     error
}

func (u *UI) initSystemUnlock() {
	s := &u.lockUI
	s.systemAuth = systemauth.Verify
	s.systemResults = make(chan systemAuthResult, 2)
	s.method = systemauth.Method()
	s.page = ui.NewPage(ui.PageConfig{Root: "lock-screen", Tree: &u.tree, Theme: &s.theme})
}

func (u *UI) requestLock() {
	if u.state.Locked || u.lockUI.pending || u.lockUI.setup {
		return
	}
	u.saveCurrentDraft()
	u.closeChatPickers()
	u.panels.settingsExtra.resetSecrets()
	u.panels.closeShareInteractions()
	s := &u.lockUI
	s.setup, s.pending, s.focused, s.err = !u.state.Settings.HasLockPassword, u.state.Settings.HasLockPassword, false, ""
	s.revealed = false
	s.field, s.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
	if s.systemBusy {
		s.systemRequest++
	}
	s.systemBusy, s.systemErr = false, ""
	if u.state.Settings.HasLockPassword {
		s.request = nextLockRequest()
		u.submit(domain.Command{Kind: domain.LockApp, Request: s.request})
	}
}

func (u *UI) lockVisible() bool {
	s := &u.lockUI
	if s.pending && u.state.LockResult.Request == s.request {
		s.pending = false
		s.err = i18n.T(u.state.LockResult.Error)
		if s.err != "" && !u.state.Locked && !s.setup {
			u.submit(domain.Command{Kind: domain.ReportError, Text: s.err})
		}
	}
	if u.state.Locked {
		s.setup = false
	}
	if !u.state.Locked && s.systemBusy {
		s.systemRequest++
		s.systemBusy, s.systemErr = false, ""
	}
	if !u.state.Locked && !s.setup && !s.pending {
		s.focused = false
		s.hidePassword()
	}
	return u.lockConcealsShell()
}

func (u *UI) lockConcealsShell() bool {
	return u.state.Locked || u.lockUI.setup || u.lockUI.pending || u.detachedConcealed
}

func (u *UI) drawLocked(canvas *gfx.Canvas, bounds ui.Rect) {
	s := &u.lockUI
	if s.scope.IsZero() {
		s.scope = u.tree.Feature("lock")
		s.focus = u.tree.ID(s.scope, "password")
		s.confirmID = u.tree.ID(s.scope, "confirm-password")
		s.submit = u.tree.ID(s.scope, "submit")
		s.cancel = u.tree.ID(s.scope, "cancel")
		s.systemButton = u.tree.ID(s.scope, "system-verify")
	}
	s.theme = u.theme
	s.theme.Background = u.palette.chat
	s.theme.Border = s.theme.Background.Mix(s.theme.Foreground, .42)
	canvas.Clip(bounds)
	defer canvas.Unclip()
	canvas.Fill(bounds, s.theme.Background)
	title, description := i18n.T("GoIM 已锁定"), i18n.T("验证身份，返回会话")
	if s.setup {
		title = i18n.T("设置锁定密码")
		description = i18n.T("密码至少 6 个字符。可在设置 → 安全中更改。")
	}
	busy := s.pending || s.systemBusy
	s.field.Secure, s.confirmation.Secure = !s.revealed || busy || s.setup, true
	// At the design viewport (1487×1058), the mark starts 112 pixels below the
	// titlebar and the form is 596 pixels wide. Shrink the layout on short screens.
	scale := min(float32(1), bounds.Height/1016)
	zoom := min(float32(1.55), s.theme.Metrics.ScaledText(1))
	s.theme.Metrics.PadControlX = 26*scale + 1
	width := min(596*scale*max(1, min(zoom, 1.24)), max(0, bounds.Width-48))
	top := 112*scale + max(0, bounds.Height-1016)*.35
	markSize := 116 * scale
	mark := ui.Rect{X: bounds.X + (bounds.Width-markSize)/2, Y: bounds.Y + top, Width: markSize, Height: markSize}
	canvas.Shadow(mark, 36*scale, gfx.ShadowStyle{Blur: 98 * scale, Spread: 41 * scale},
		s.theme.Primary.WithAlpha(.16))
	s.page.Draw(canvas, bounds, func(f ui.C) {
		ui.Column(f).Fill().PadTop(top).Items(ui.Center).Children(func() {
			ui.Column(f).Width(width).Items(ui.Center).Children(func() {
				ui.Slot(f).Size(markSize, markSize)
				ui.Row(f).Height(31 * scale)
				ui.Row(f).FillWidth().Height(max(64, 61*zoom) * scale).Center().Children(func() {
					ui.Text(f, title).FontSize(min(50*zoom, 65) * scale).Weight(gfx.WeightSemibold)
				})
				ui.Row(f).Height(12 * scale)
				ui.Paragraph(f, description, 2).FillWidth().TextCenter().FontSize(24 * scale * zoom).Muted()
				ui.Row(f).Height(48 * scale)
				u.drawLockForm(f, scale, zoom, busy)
			})
			if !s.setup {
				ui.Row(f).Absolute().Left(0).Right(0).Bottom(65 * scale).Height(42 * scale).Gap(14 * scale).Center().Children(func() {
					ui.Row(f).Size(15*scale, 15*scale).Radius(8 * scale).Background(s.theme.Primary)
					ui.Text(f, i18n.T("锁定期间仍可接收消息和文件")).FontSize(21 * scale * zoom).Muted()
				})
			}
		})
	})
	canvas.RoundedBorder(mark, 27*scale, 1.5*scale, s.theme.Border, ui.RGB(0, 0, 0))
	paintGoIMMark(canvas, mark.Inset(gfx.Insets{Top: -6 * scale, Right: -6 * scale, Bottom: -6 * scale, Left: -6 * scale}), lockMark)
	if !s.focused && !busy {
		s.focused = canvas.Focused() == s.focus || canvas.Focused() == s.confirmID
		if !s.focused {
			canvas.RequestFocus(s.focus)
		}
	}
}

func (u *UI) drawLockForm(f ui.C, scale, zoom float32, busy bool) {
	s := &u.lockUI
	label, placeholder, action := i18n.T("锁定密码"), i18n.T("请输入锁定密码"), i18n.T("使用密码解锁")
	if s.setup {
		label, placeholder, action = i18n.T("新密码"), i18n.T("至少 6 个字符"), i18n.T("设置并锁定")
	}
	if s.pending {
		action = i18n.T("正在验证…")
	}
	hasSystem := !s.setup && u.state.Settings.SystemUnlock && s.method != systemauth.Unsupported
	if hasSystem {
		u.drawLockSystemAction(f, scale, zoom, busy)
		ui.Row(f).FillWidth().Height(96 * scale).Gap(26 * scale).Items(ui.Center).Children(func() {
			ui.Row(f).Grow(1).Height(1).Background(u.palette.border)
			ui.Text(f, i18n.T("或使用锁定密码")).FontSize(21 * scale * zoom).Muted()
			ui.Row(f).Grow(1).Height(1).Background(u.palette.border)
		})
	}
	field := drawLockField(f, s.focus, &s.field, label, placeholder, scale, zoom, busy)
	submitted := field.Submitted()
	if !s.setup {
		icon, reveal := Eye, i18n.T("显示密码")
		if s.revealed {
			icon, reveal = lockPasswordHiddenIcon, i18n.T("隐藏密码")
		}
		s.revealButton = field.ActionID()
		field.Addons().SetEnd(components.InputGroupSlot{
			Kind: components.InputGroupSlotButton, ID: s.revealButton, Icon: icon, Label: reveal,
			ButtonSize: components.InputGroupButtonIconSM, Width: 72 * scale,
			IconTransform: gfx.IconTransform{Scale: 1.65 * scale},
		})
		if field.ActionClicked() {
			s.revealed = !s.revealed
			u.invalidator.Invalidate()
		}
	} else {
		ui.Row(f).Height(16 * scale)
		submitted = drawLockField(f, s.confirmID, &s.confirmation, i18n.T("确认新密码"),
			i18n.T("再输入一次新密码"), scale, zoom, busy).Submitted() || submitted
	}
	ui.Row(f).Height(24 * scale)
	ui.Row(f).FillWidth().Gap(12 * scale).Children(func() {
		height := 65 * scale
		if s.setup && namedButton(f, s.cancel, i18n.T("取消")).Outline().Height(height).
			FontSize(23*scale*zoom).Radius(13*scale).ButtonRecipe(lockButtonLook).Disabled(busy).Clicked() {
			u.cancelLockSetup()
		}
		button := namedButton(f, s.submit, action).Grow(1).Height(height).FontSize(24 * scale * zoom).
			Weight(gfx.WeightSemibold).Radius(13 * scale).ButtonRecipe(lockButtonLook).Disabled(busy)
		if hasSystem {
			button = button.Outline()
		}
		if !busy && (button.Clicked() || submitted) {
			u.submitLock()
		}
	})
	for _, errorText := range [...]string{s.err, s.systemErr} {
		if errorText != "" {
			ui.Row(f).Height(10 * scale)
			ui.Paragraph(f, errorText, 2).FillWidth().FontSize(18 * scale * zoom).Color(s.theme.Danger)
		}
	}
}

func drawLockField(f ui.C, id gfx.ID, field *ui.Field, label, placeholder string, scale, zoom float32, disabled bool) ui.Box {
	ui.Row(f).FillWidth().Height(max(37, 32*zoom) * scale).Items(ui.Start).Children(func() {
		ui.Text(f, label).FontSize(22 * scale * zoom).Weight(gfx.WeightSemibold)
	})
	theme := f.Theme()
	return ui.TextField(f, field, label, placeholder, ui.WithID(id)).FillWidth().Height(66*scale).
		FontSize(23*scale*zoom).Radius(13*scale).Background(theme.Card).
		Border(1.5*scale, theme.Border).Disabled(disabled)
}

func lockSystemAction(method systemauth.Kind) (string, gfx.Icon) {
	switch method {
	case systemauth.TouchID:
		return i18n.T("使用 Touch ID 解锁"), lockTouchIDIcon
	case systemauth.WindowsHello:
		return i18n.T("使用 Windows Hello 解锁"), lockWindowsHelloIcon
	default:
		return "", gfx.Icon{}
	}
}

func (u *UI) drawLockSystemAction(f ui.C, scale, zoom float32, busy bool) {
	s := &u.lockUI
	action, icon := lockSystemAction(s.method)
	if s.systemBusy {
		action = i18n.T("正在等待系统验证…")
	}
	button := namedButton(f, s.systemButton, action).FillWidth().Height(75 * scale).Radius(14 * scale).
		ButtonRecipe(lockButtonLook).Disabled(busy)
	button.Center().Children(func() {
		ui.Row(f).Gap(23 * scale).Center().Children(func() {
			ink := s.theme.PrimaryForeground
			if busy {
				ink = ink.WithAlpha(.5)
			}
			ui.Icon(f, icon).Size(39*scale, 39*scale).Color(ink)
			ui.Text(f, action).FontSize(28 * scale * zoom).Weight(gfx.WeightSemibold).Color(ink)
		})
	})
	if button.Clicked() {
		u.beginSystemUnlock()
	}
}

func lockButtonLook(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	look := components.ButtonRecipe(theme, variant, state)
	look.Shadow = gfx.ShadowNone
	if variant == components.ButtonOutline {
		look.Background, look.Filled = theme.Background, true
		if !state.FocusVisible {
			look.Border = theme.Border
		}
		if state.Disabled {
			look.Border = look.Border.WithAlpha(.5)
		} else if state.Hovered || state.Pressed {
			look.Background = theme.Hover
		}
	}
	return look
}

func (s *lockUIState) hidePassword() bool {
	revealed := s.revealed
	s.revealed, s.field.Secure = false, true
	return revealed
}

func (u *UI) submitLock() {
	s := &u.lockUI
	if s.systemBusy {
		return
	}
	password := s.field.Text()
	if s.setup {
		if err := validatePasswordPair(password, s.confirmation.Text()); err != "" {
			s.err = err
			return
		}
	}
	if password == "" {
		s.err = i18n.T("请输入密码")
		return
	}
	s.pending, s.err = true, ""
	s.systemErr = ""
	s.hidePassword()
	s.request = nextLockRequest()
	s.field.SetText("")
	s.confirmation.SetText("")
	s.focused = false
	if s.setup {
		u.submit(domain.Command{Kind: domain.SetLockPassword, Text: password, Bool: true, Request: s.request})
	} else {
		u.submit(domain.Command{Kind: domain.UnlockApp, Password: password, Request: s.request})
	}
}

func (u *UI) beginSystemUnlock() {
	s := &u.lockUI
	if s.setup || s.pending || s.systemBusy || !u.state.Locked || !u.state.Settings.SystemUnlock || s.method == systemauth.Unsupported {
		return
	}
	auth := s.systemAuth
	if auth == nil {
		auth = systemauth.Verify
	}
	results := s.systemResults
	if results == nil {
		return
	}
	s.systemRequest++
	request := s.systemRequest
	s.systemBusy, s.systemErr, s.err, s.focused = true, "", "", false
	s.hidePassword()
	reason := i18n.T("验证身份以解锁 GoIM")
	go func() {
		results <- systemAuthResult{request: request, err: auth(reason)}
		u.invalidator.Invalidate()
	}()
}

func (u *UI) collectSystemUnlock() {
	s := &u.lockUI
	if s.systemResults == nil {
		return
	}
	for {
		select {
		case result := <-s.systemResults:
			if result.request != s.systemRequest || !u.state.Locked {
				continue
			}
			s.systemBusy = false
			if result.err != nil {
				s.systemErr = systemUnlockMessage(result.err)
				s.focused = false
				continue
			}
			s.systemErr = ""
			s.pending = true
			s.request = nextLockRequest()
			u.submit(domain.Command{Kind: domain.UnlockAppSystem, Request: s.request})
		default:
			return
		}
	}
}

func systemUnlockMessage(err error) string {
	switch {
	case errors.Is(err, systemauth.ErrCanceled):
		return i18n.T("已取消系统验证")
	case errors.Is(err, systemauth.ErrNotConfigured):
		return i18n.T("系统尚未设置可用的验证方式")
	case errors.Is(err, systemauth.ErrDisabledByPolicy):
		return i18n.T("系统验证已被管理员禁用")
	case errors.Is(err, systemauth.ErrBusy):
		return i18n.T("验证设备正忙，请稍后重试")
	case errors.Is(err, systemauth.ErrRetriesExhausted):
		return i18n.T("系统验证失败次数过多，请稍后重试")
	case errors.Is(err, systemauth.ErrFailed):
		return i18n.T("系统验证未通过")
	case errors.Is(err, systemauth.ErrUnavailable):
		return i18n.T("当前系统无法完成验证")
	default:
		return i18n.T("系统验证失败，请改用 GoIM 密码")
	}
}

func validatePasswordPair(password, confirmation string) string {
	if strings.TrimSpace(password) == "" || utf8.RuneCountInString(password) < 6 || len(password) > 256 {
		return i18n.T("密码至少 6 个字符，且不能超过 256 字节")
	}
	if password != confirmation {
		return i18n.T("两次输入的密码不一致")
	}
	return ""
}

func (u *UI) cancelLockSetup() {
	if !u.lockUI.setup || u.lockUI.pending {
		return
	}
	u.lockUI.setup, u.lockUI.focused, u.lockUI.err = false, false, ""
	u.lockUI.field, u.lockUI.confirmation = ui.Field{}, ui.Field{}
}

func (u *UI) lockKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Key == gfx.KeyEscape && !event.Release && u.lockUI.setup {
		u.cancelLockSetup()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (u *UI) drawChatBackground(canvas *gfx.Canvas, bounds ui.Rect) bool {
	path := u.state.Settings.Background
	if path == "" || u.state.Settings.BackgroundTransparency >= 100 || bounds.Width <= 0 || bounds.Height <= 0 {
		return false
	}
	bitmap := u.fileBitmap(path)
	if bitmap.Width <= 0 || bitmap.Height <= 0 {
		return false
	}
	w, h := float32(bitmap.Width), float32(bitmap.Height)
	ratio := bounds.Width / bounds.Height
	crop := ui.Rect{Width: w, Height: h}
	if w/h > ratio {
		crop.Width = h * ratio
		crop.X = (w - crop.Width) / 2
	} else {
		crop.Height = w / ratio
		crop.Y = (h - crop.Height) / 2
	}
	canvas.DrawBitmapRegion(bitmap, crop, bounds)
	return true
}

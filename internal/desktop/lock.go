package desktop

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type lockUIState struct {
	scope                            ui.Scope
	focus, confirmID, submit, cancel ui.ID
	field, confirmation              flex.Field
	setup, pending, focused          bool
	request                          uint64
	err                              string
}

func (u *UI) requestLock() {
	if u.state.Locked || u.lockUI.pending || u.lockUI.setup {
		return
	}
	u.saveCurrentDraft()
	u.panels.settingsExtra.resetSecrets()
	u.panels.closeShareInteractions()
	s := &u.lockUI
	s.setup, s.pending, s.focused, s.err = !u.state.Settings.HasLockPassword, u.state.Settings.HasLockPassword, false, ""
	s.field, s.confirmation = flex.Field{Secure: true}, flex.Field{Secure: true}
	if u.state.Settings.HasLockPassword {
		s.request++
		u.submit(domain.Command{Kind: domain.LockApp, Request: s.request})
	}
}

func (u *UI) lockVisible() bool {
	s := &u.lockUI
	if s.pending && u.state.LockResult.Request == s.request {
		s.pending = false
		s.err = u.state.LockResult.Error
		if s.err != "" && !u.state.Locked && !s.setup {
			u.submit(domain.Command{Kind: domain.ReportError, Text: s.err})
		}
	}
	if u.state.Locked {
		s.setup = false
	}
	if !u.state.Locked && !s.setup && !s.pending {
		s.focused = false
	}
	return u.state.Locked || s.setup || s.pending
}

func (u *UI) drawLocked(canvas *ui.Canvas, bounds ui.Rect) {
	s := &u.lockUI
	if s.scope.IsZero() {
		s.scope = u.tree.Feature("lock")
		s.focus = u.tree.ID(s.scope, "password")
		s.confirmID = u.tree.ID(s.scope, "confirm-password")
		s.submit = u.tree.ID(s.scope, "submit")
		s.cancel = u.tree.ID(s.scope, "cancel")
	}
	canvas.Fill(bounds, u.palette.chat)
	width := min(454, max(0, bounds.Width-28))
	height := min(350, max(0, bounds.Height-28))
	if s.setup {
		height = min(430, max(0, bounds.Height-28))
	}
	panel := ui.Rect{X: bounds.X + (bounds.Width-width)/2, Y: bounds.Y + (bounds.Height-height)/2, Width: width, Height: height}
	s.field.Secure, s.confirmation.Secure = true, true
	var logo flex.Box
	u.dialogView.Draw(canvas, u.theme, panel, func(f *flex.Frame) {
		f.Column().Fill().Pad(30).Gap(12).Radius(20).Border(1, u.theme.Border).Background(u.theme.Background).Children(func() {
			title, description, action, label := "GoIM 已锁定", "输入密码以恢复你的会话。", "解锁", "锁定密码"
			if s.setup {
				title, description, action, label = "设置锁定密码", "密码至少 6 个字符。可在设置 → 安全中更改。", "设置并锁定", "新密码"
			}
			logo = f.Slot().Size(44, 44)
			f.Text(title).FontSize(26).Weight(ui.WeightSemibold)
			f.Paragraph(description, 2).Muted().FontSize(13)
			field := f.Field(&s.field, s.focus, label, label).Height(52).FillWidth().Disabled(s.pending)
			submitted := field.Submitted()
			if s.setup {
				submitted = f.Field(&s.confirmation, s.confirmID, "确认新密码", "再输入一次新密码").
					Height(52).FillWidth().Disabled(s.pending).Submitted() || submitted
			}
			if s.err != "" {
				f.Paragraph(s.err, 2).FontSize(12).Color(u.theme.Danger)
			}
			f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
				if s.setup && f.Button(s.cancel, "取消").Height(48).Outline().Disabled(s.pending).Clicked() {
					u.cancelLockSetup()
				}
				if s.pending {
					action = "正在验证…"
				}
				if f.Button(s.submit, action).Grow(1).Height(48).Disabled(s.pending).Clicked() || submitted && !s.pending {
					u.submitLock()
				}
			})
		})
	})
	PaintGoIMMark(canvas, logo.Rect())
	if !s.focused && !s.pending {
		s.focused = canvas.Focused() == s.focus || canvas.Focused() == s.confirmID
		if !s.focused {
			canvas.RequestFocus(s.focus)
		}
	}
}

func (u *UI) submitLock() {
	s := &u.lockUI
	password := s.field.Text()
	if s.setup {
		if err := validatePasswordPair(password, s.confirmation.Text()); err != "" {
			s.err = err
			return
		}
	}
	if password == "" {
		s.err = "请输入密码"
		return
	}
	s.pending, s.err = true, ""
	s.request++
	s.field.SetText("")
	s.confirmation.SetText("")
	s.focused = false
	if s.setup {
		u.submit(domain.Command{Kind: domain.SetLockPassword, Text: password, Bool: true, Request: s.request})
	} else {
		u.submit(domain.Command{Kind: domain.UnlockApp, Password: password, Request: s.request})
	}
}

func validatePasswordPair(password, confirmation string) string {
	if strings.TrimSpace(password) == "" || utf8.RuneCountInString(password) < 6 || len(password) > 256 {
		return "密码至少 6 个字符，且不能超过 256 字节"
	}
	if password != confirmation {
		return "两次输入的密码不一致"
	}
	return ""
}

func (u *UI) cancelLockSetup() {
	if !u.lockUI.setup || u.lockUI.pending {
		return
	}
	u.lockUI.setup, u.lockUI.focused, u.lockUI.err = false, false, ""
	u.lockUI.field, u.lockUI.confirmation = flex.Field{}, flex.Field{}
}

func (u *UI) lockTextInput(context app.TextInputContext) *app.TextInput {
	return flex.Fields(&u.lockUI.field, &u.lockUI.confirmation)(context)
}

func (u *UI) lockKey(event ui.KeyEvent) ui.EventResult {
	if event.Key == ui.KeyEscape && !event.Release && u.lockUI.setup {
		u.cancelLockSetup()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (u *UI) drawChatBackground(canvas *ui.Canvas, bounds ui.Rect) bool {
	path := u.state.Settings.Background
	if path == "" || bounds.Width <= 0 || bounds.Height <= 0 {
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

func (u *UI) chatBackgroundTint() ui.Color {
	shade := float32(u.state.Settings.BackgroundShade) / 100
	tint := u.palette.chat
	if u.isDark() {
		tint.Alpha = 0.63 + 0.32*shade
	} else {
		// Maintain readable labels in both themes even at the least shaded value.
		tint = tint.Mix(ui.RGB(111, 120, 140), 0.46*shade)
		tint.Alpha = 0.60 + 0.11*shade
	}
	return tint
}

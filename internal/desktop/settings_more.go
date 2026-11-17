package desktop

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type panelSettingsExtra struct {
	background, clearBackground, notificationPrivacy, offline, discovery, acceptImages                    gfx.ID
	profile, connect, connectAddress, lockStartup, passwordOld, passwordNew, passwordRepeat, passwordSave gfx.ID
	shade                                                                                                 ui.SliderState
	shadeDirty                                                                                            bool
	connectRoute                                                                                          connectionRoute
	compat                                                                                                [2]compatibilityEditor
	address, old, password, repeat                                                                        ui.Field
	passwordError                                                                                         string
	passwordOpen, passwordPending, passwordRefocus                                                        bool
	passwordRequest                                                                                       uint64
	passwordTrigger, passwordScope, passwordDismiss, passwordClose, passwordCancel                        gfx.ID
	openProfile                                                                                           func()
	chooseBackground                                                                                      func(*gfx.Canvas)
}

var panelIconGeneral = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(3, 6), gfx.LineTo(7, 6), gfx.MoveTo(11, 6), gfx.LineTo(21, 6), gfx.MoveTo(7, 3), gfx.LineTo(7, 9),
	gfx.MoveTo(3, 12), gfx.LineTo(15, 12), gfx.MoveTo(19, 12), gfx.LineTo(21, 12), gfx.MoveTo(15, 9), gfx.LineTo(15, 15),
	gfx.MoveTo(3, 18), gfx.LineTo(9, 18), gfx.MoveTo(13, 18), gfx.LineTo(21, 18), gfx.MoveTo(9, 15), gfx.LineTo(9, 21),
}}

var panelSettingsLockIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(7, 10), gfx.LineTo(7, 7), gfx.CubicTo(7, 1, 17, 1, 17, 7), gfx.LineTo(17, 10),
	gfx.MoveTo(4, 10), gfx.LineTo(20, 10), gfx.LineTo(20, 21), gfx.LineTo(4, 21), gfx.ClosePath(),
	gfx.MoveTo(12, 14), gfx.LineTo(12, 17),
}}

var panelSettingsBellIcon = gfx.Icon{ViewBox: 24, Stroke: 1.8, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(5, 17), gfx.LineTo(19, 17), gfx.LineTo(17, 14), gfx.LineTo(17, 8), gfx.CubicTo(17, 2, 7, 2, 7, 8),
	gfx.LineTo(7, 14), gfx.LineTo(5, 17), gfx.ClosePath(), gfx.MoveTo(10, 21), gfx.QuadTo(12, 23, 14, 21),
}}

func (s *panelSettingsExtra) init(tree *gfx.Tree, scope gfx.Scope) {
	s.background = tree.ID(scope, "background-choose")
	s.clearBackground = tree.ID(scope, "background-clear")
	s.notificationPrivacy = tree.ID(scope, "notification-private")
	s.offline = tree.ID(scope, "offline-contacts")
	s.discovery = tree.ID(scope, "auto-discovery")
	s.acceptImages = tree.ID(scope, "auto-accept-images")
	s.profile = tree.ID(scope, "open-profile")
	s.connect = tree.ID(scope, "manual-connect")
	s.connectAddress = tree.ID(scope, "manual-address")
	s.compat[0].init(tree, tree.Sub(scope, tree.Name("neiwangtong")))
	s.compat[1].init(tree, tree.Sub(scope, tree.Name("ipmsg")))
	s.lockStartup = tree.ID(scope, "lock-startup")
	s.passwordOld = tree.ID(scope, "password-old")
	s.passwordNew = tree.ID(scope, "password-new")
	s.passwordRepeat = tree.ID(scope, "password-repeat")
	s.passwordSave = tree.ID(scope, "password-save")
	s.passwordTrigger = tree.ID(scope, "password-trigger")
	s.passwordScope = tree.ID(scope, "password-dialog")
	s.passwordDismiss = tree.ID(scope, "password-dismiss")
	s.passwordClose = tree.ID(scope, "password-close")
	s.passwordCancel = tree.ID(scope, "password-cancel")
	s.resetSecrets()
}

func (s *panelSettingsExtra) resetSecrets() {
	s.old, s.password, s.repeat = ui.Field{Secure: true}, ui.Field{Secure: true}, ui.Field{Secure: true}
	s.passwordError = ""
	s.passwordOpen, s.passwordPending, s.passwordRefocus = false, false, false
	s.passwordRequest = 0
}

func (s *panelSettingsExtra) isPasswordField(id gfx.ID) bool {
	return !id.IsZero() && (id == s.passwordOld || id == s.passwordNew || id == s.passwordRepeat)
}

func (p *Panels) drawBackgroundSettings(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	settings := state.Settings
	p.settingsRow(f, theme, i18n.T("自定义背景"), i18n.T("用于聊天区域，图片仅保存在本机"), func() {
		p.settingsControl(f, 240, func() {
			ui.Row(f).Gap(8).Items(ui.Center).Children(func() {
				if namedButton(f, s.background, i18n.T("选择图片…")).Small().Outline().Clicked() {
					if s.chooseBackground != nil {
						s.chooseBackground(f.Canvas())
					}
				}
				if settings.Background != "" && namedButton(f, s.clearBackground, i18n.T("恢复默认")).Small().Ghost().Clicked() {
					submit(domain.Command{Kind: domain.SetSetting, Setting: "background", Value: ""})
				}
			})
		})
	})
	p.settingsRow(f, theme, i18n.T("背景深浅"), i18n.T("调整图片上的底色，保持聊天文字清晰"), func() {
		p.settingsControl(f, 240, func() {
			if !s.shade.Dragging() && !s.shadeDirty && (p.pending == nil || p.pending.setting != "background_shade") {
				s.shade.Value = float32(settings.BackgroundShade)
			}
			ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				slider := ui.Slider(f, &s.shade).Step(5).Grow(1).
					Label(i18n.T("背景深浅")).Disabled(settings.Background == "")
				if slider.Changed() {
					s.shadeDirty = true
				}
				if s.shadeDirty && !s.shade.Dragging() {
					p.submitString(submit, state, "background_shade", strconv.Itoa(int(s.shade.Value)))
					s.shadeDirty = false
				}
				ui.Text(f, strconv.Itoa(int(s.shade.Value))+"%").FontSize(12).Muted().Width(40)
			})
		})
	})
}

func (p *Panels) drawSettingsProfile(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	panelSection(f, i18n.T("个人资料"), "", func() {
		p.settingsRow(f, theme, state.Self.Name, i18n.T("本机昵称和头像"), func() {
			if namedButton(f, s.profile, i18n.T("编辑资料…")).Small().Outline().Clicked() && s.openProfile != nil {
				s.openProfile()
			}
		})
		p.settingsRow(f, theme, i18n.T("隐藏离线联系人"), i18n.T("联系人列表仅显示自己和在线联系人"), func() {
			p.settingsControl(f, 80, func() {
				hidden := ui.State[bool](f, "hide-offline")
				*hidden = state.Settings.HideOfflineContacts
				if ui.Switch(f, hidden, "").Hit(s.offline, i18n.T("隐藏离线联系人")).Changed() {
					p.submitBool(submit, state, "hide_offline_contacts", *hidden)
				}
			})
		})
	})
}

func (p *Panels) drawDiscoverySettings(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	p.settingsRow(f, theme, i18n.T("自动发现联系人"), i18n.T("搜索同一局域网内运行 GoIM 的设备"), func() {
		p.settingsControl(f, 80, func() {
			discovery := ui.State[bool](f, "auto-discovery")
			*discovery = !state.Settings.DisableAutoDiscovery
			if ui.Switch(f, discovery, "").Hit(s.discovery, i18n.T("自动发现联系人")).Changed() {
				p.submitBool(submit, state, "disable_auto_discovery", !*discovery)
			}
		})
	})
	p.settingsRow(f, theme, i18n.T("手动连接"), i18n.T("先选择连接方式，再输入对方设备地址。"), func() {
		p.settingsControl(f, 300, func() {
			submitted := connectionFields(f, state, panelConnectionFields{
				route:   &s.connectRoute,
				address: &s.address, addressID: s.connectAddress, connectID: s.connect,
			})
			if !submitted {
				return
			}
			address := strings.TrimSpace(s.address.Text())
			if address == "" {
				p.localError = i18n.T("请输入对方 IP 地址")
				return
			}
			p.localError = ""
			if p.toast != nil {
				p.toast(i18n.T("已发起连接"), "", components.ToastInfo)
			}
			submit(domain.Command{Kind: domain.Connect, Address: address, Transport: s.connectRoute.transport()})
		})
	})
}

func (p *Panels) drawSettingsSecurity(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	settings := state.Settings
	panelSection(f, i18n.T("应用锁定"), i18n.T("锁定后需要输入密码才能查看会话。消息与文件仍在后台接收。"), func() {
		p.settingsRow(f, theme, i18n.T("启动时锁定"), i18n.T("下次启动 GoIM 时先验证密码"), func() {
			p.settingsControl(f, 80, func() {
				enabled := ui.State[bool](f, "lock-on-startup")
				*enabled = settings.LockOnStartup
				if ui.Switch(f, enabled, "").Hit(s.lockStartup, i18n.T("启动时锁定")).Disabled(!settings.HasLockPassword).Changed() {
					p.submitBool(submit, state, "lock_on_startup", *enabled)
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("锁定密码"), i18n.T("用于验证 GoIM 的应用锁定。"), func() {
			if namedButton(f, s.passwordTrigger, lockPasswordAction(settings.HasLockPassword)).Small().Outline().Clicked() {
				s.resetSecrets()
				s.passwordOpen = true
			}
		})
	})
}

func lockPasswordAction(hasPassword bool) string {
	if hasPassword {
		return i18n.T("修改密码")
	}
	return i18n.T("设置密码")
}

func (p *Panels) drawPasswordDialog(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	if !s.passwordOpen {
		return
	}
	hasPassword := state.Settings.HasLockPassword
	focus := s.passwordNew
	if hasPassword {
		focus = s.passwordOld
	}
	dialog := ui.Dialog(f, ui.DialogSpec{
		Title: lockPasswordAction(hasPassword), Width: 420,
		Scope: s.passwordScope, DismissID: s.passwordDismiss, CloseID: s.passwordClose,
		InitialFocusID: focus, NoClose: s.passwordPending,
	})
	if s.passwordRefocus {
		f.Canvas().RequestFocus(focus)
		s.passwordRefocus = false
	}
	var save, cancel bool
	dialog.Body(func() {
		ui.Column(f).FillWidth().Gap(10).Children(func() {
			field := func(id gfx.ID, editor *ui.Field, label, hint string) ui.Box {
				var result ui.Box
				ui.Column(f).FillWidth().Gap(4).Children(func() {
					ui.Text(f, i18n.T(label)).FontSize(13)
					result = ui.TextFieldID(f, id, editor, i18n.T(label), i18n.T(hint)).FillWidth().Disabled(s.passwordPending)
				})
				return result
			}
			if hasPassword {
				field(s.passwordOld, &s.old, "当前密码", "输入当前密码")
			}
			field(s.passwordNew, &s.password, "新密码", "至少 6 个字符")
			save = field(s.passwordRepeat, &s.repeat, "确认新密码", "再输入一次新密码").Submitted()
			if s.passwordError != "" {
				ui.Paragraph(f, s.passwordError, 2).Color(theme.Danger).FontSize(12)
			}
		})
	})
	dialog.Footer(func() {
		cancel = namedButton(f, s.passwordCancel, i18n.T("取消")).Small().Outline().Disabled(s.passwordPending).Clicked()
		save = namedButton(f, s.passwordSave, i18n.T("保存密码")).Small().Disabled(s.passwordPending).Clicked() || save
	})
	if s.passwordPending {
		return
	}
	if cancel || dialog.Dismissed() {
		s.resetSecrets()
		f.Canvas().RequestFocus(s.passwordTrigger)
		return
	}
	if save {
		password := s.password.Text()
		s.passwordError = validatePasswordPair(password, s.repeat.Text())
		if s.passwordError == "" {
			s.passwordRequest = nextLockRequest()
			s.passwordPending = true
			submit(domain.Command{Kind: domain.SetLockPassword, Password: s.old.Text(), Text: password, Request: s.passwordRequest})
		}
	}
}

func (s *panelSettingsExtra) observePassword(state domain.State) {
	if state.Locked {
		s.resetSecrets()
		return
	}
	result := state.LockResult
	if !s.passwordPending || result.Request != s.passwordRequest {
		return
	}
	if result.Error == "" {
		s.resetSecrets()
		return
	}
	s.passwordPending, s.passwordRefocus = false, true
	s.passwordError = i18n.T(result.Error)
	s.old.SetText("")
}

func (p *Panels) drawAutoAcceptImages(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	p.settingsRow(f, theme, i18n.T("自动接收聊天图片"), i18n.T("接收后可直接在聊天中预览"), func() {
		p.settingsControl(f, 80, func() {
			enabled := ui.State[bool](f, "accept-images")
			*enabled = settings.AutoAcceptImages || settings.AutoAccept
			if ui.Switch(f, enabled, "").Hit(p.settingsExtra.acceptImages, i18n.T("自动接收聊天图片")).Disabled(settings.AutoAccept).Changed() {
				p.submitBool(submit, state, "auto_accept_images", *enabled)
			}
		})
	})
}

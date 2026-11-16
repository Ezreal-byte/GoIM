package desktop

import (
	"errors"
	"github.com/vivaui/viva/gfx"
	"goim/internal/i18n"
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
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
	openProfile                                                                                           func()
}

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
	s.resetSecrets()
}

func (s *panelSettingsExtra) resetSecrets() {
	s.old, s.password, s.repeat = ui.Field{Secure: true}, ui.Field{Secure: true}, ui.Field{Secure: true}
	s.passwordError = ""
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
					paths, err := app.OpenFiles(app.OpenFileOptions{Title: i18n.T("选择聊天背景"), Filters: []app.FileFilter{
						{Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif"},
					}})
					if err == nil && len(paths) > 0 {
						submit(domain.Command{Kind: domain.SetSetting, Setting: "background", Value: paths[0]})
					} else if err != nil && !errors.Is(err, app.ErrCanceled) && !errors.Is(err, app.ErrNoDriverResponse) {
						p.localError = i18n.T("无法选择背景：") + err.Error()
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
			address, transport, submitted := panelConnectionForm(f, state, panelConnectionFields{
				route:   &s.connectRoute,
				address: &s.address, addressID: s.connectAddress, connectID: s.connect,
			})
			if !submitted {
				return
			}
			if address == "" {
				p.localError = i18n.T("请输入对方 IP 地址")
				return
			}
			p.localError, p.localNotice = "", i18n.T("已发起连接")
			submit(domain.Command{Kind: domain.Connect, Address: address, Transport: transport})
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
		ui.Row(f).FillWidth().Justify(ui.End).Children(func() {
			p.settingsControl(f, 360, func() {
				ui.Column(f).FillWidth().Gap(8).PadY(10).Children(func() {
					if settings.HasLockPassword {
						ui.Text(f, i18n.T("当前密码")).FontSize(13)
						ui.TextFieldID(f, s.passwordOld, &s.old, i18n.T("当前密码"), i18n.T("输入当前密码")).FillWidth()
					}
					ui.Text(f, i18n.T("新密码")).FontSize(13)
					ui.TextFieldID(f, s.passwordNew, &s.password, i18n.T("新密码"), i18n.T("至少 6 个字符")).FillWidth()
					ui.Text(f, i18n.T("确认新密码")).FontSize(13)
					last := ui.TextFieldID(f, s.passwordRepeat, &s.repeat, i18n.T("确认新密码"), i18n.T("再输入一次新密码")).FillWidth()
					if s.passwordError != "" {
						ui.Paragraph(f, s.passwordError, 2).Color(theme.Danger).FontSize(12)
					}
					if namedButton(f, s.passwordSave, i18n.T("保存密码")).Small().Clicked() || last.Submitted() {
						password := s.password.Text()
						if err := validatePasswordPair(password, s.repeat.Text()); err != "" {
							s.passwordError = err
							return
						}
						submit(domain.Command{Kind: domain.SetLockPassword, Password: s.old.Text(), Text: password})
						s.resetSecrets()
					}
				})
			})
		})
	})
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

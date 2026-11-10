package desktop

import (
	"errors"
	"goim/internal/i18n"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

type panelSettingsExtra struct {
	scroll                                                                                                ui.Scope
	background, clearBackground, backgroundShade, notificationPrivacy, offline, discovery, acceptImages   ui.ID
	profile, connect, connectAddress, lockStartup, passwordOld, passwordNew, passwordRepeat, passwordSave ui.ID
	shade                                                                                                 flex.SliderState
	shadeDirty                                                                                            bool
	address, old, password, repeat                                                                        flex.Field
	passwordError                                                                                         string
	openProfile                                                                                           func()
}

var panelSettingsLockIcon = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(7, 10), ui.LineTo(7, 7), ui.CubicTo(7, 1, 17, 1, 17, 7), ui.LineTo(17, 10),
	ui.MoveTo(4, 10), ui.LineTo(20, 10), ui.LineTo(20, 21), ui.LineTo(4, 21), ui.ClosePath(),
	ui.MoveTo(12, 14), ui.LineTo(12, 17),
}}

var panelSettingsBellIcon = ui.Icon{ViewBox: 24, Stroke: 1.8, Cap: ui.CapRound, Join: ui.JoinRound, Commands: []ui.PathCommand{
	ui.MoveTo(5, 17), ui.LineTo(19, 17), ui.LineTo(17, 14), ui.LineTo(17, 8), ui.CubicTo(17, 2, 7, 2, 7, 8),
	ui.LineTo(7, 14), ui.LineTo(5, 17), ui.ClosePath(), ui.MoveTo(10, 21), ui.QuadTo(12, 23, 14, 21),
}}

func (s *panelSettingsExtra) init(tree *ui.Tree, scope ui.Scope) {
	s.scroll = tree.Sub(scope, tree.Name("category-scroll"))
	s.background = tree.ID(scope, "background-choose")
	s.clearBackground = tree.ID(scope, "background-clear")
	s.backgroundShade = tree.ID(scope, "background-shade")
	s.notificationPrivacy = tree.ID(scope, "notification-private")
	s.offline = tree.ID(scope, "offline-contacts")
	s.discovery = tree.ID(scope, "auto-discovery")
	s.acceptImages = tree.ID(scope, "auto-accept-images")
	s.profile = tree.ID(scope, "open-profile")
	s.connect = tree.ID(scope, "manual-connect")
	s.connectAddress = tree.ID(scope, "manual-address")
	s.lockStartup = tree.ID(scope, "lock-startup")
	s.passwordOld = tree.ID(scope, "password-old")
	s.passwordNew = tree.ID(scope, "password-new")
	s.passwordRepeat = tree.ID(scope, "password-repeat")
	s.passwordSave = tree.ID(scope, "password-save")
	s.resetSecrets()
}

func (s *panelSettingsExtra) resetSecrets() {
	s.old, s.password, s.repeat = flex.Field{Secure: true}, flex.Field{Secure: true}, flex.Field{Secure: true}
	s.passwordError = ""
}

func (s *panelSettingsExtra) textInput(context app.TextInputContext) *app.TextInput {
	return flex.Fields(&s.address, &s.old, &s.password, &s.repeat)(context)
}

func (p *Panels) drawBackgroundSettings(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	settings := state.Settings
	panelSettingRow(f, theme, i18n.T("自定义背景"), i18n.T("用于聊天区域，图片仅保存在本机"), func() {
		panelControlColumn(f, 240, func() {
			f.Row().Gap(8).Items(flex.Center).Children(func() {
				if f.Button(s.background, i18n.T("选择图片…")).Small().Outline().Clicked() {
					paths, err := app.OpenFiles(app.OpenFileOptions{Title: i18n.T("选择聊天背景"), Filters: []app.FileFilter{
						{Name: i18n.T("图片"), Pattern: "*.png;*.jpg;*.jpeg;*.gif"},
					}})
					if err == nil && len(paths) > 0 {
						submit(domain.Command{Kind: domain.SetSetting, Setting: "background", Value: paths[0]})
					} else if err != nil && !errors.Is(err, app.ErrCanceled) && !errors.Is(err, app.ErrNoDriverResponse) {
						p.localError = i18n.T("无法选择背景：") + err.Error()
					}
				}
				if settings.Background != "" && f.Button(s.clearBackground, i18n.T("恢复默认")).Small().Ghost().Clicked() {
					submit(domain.Command{Kind: domain.SetSetting, Setting: "background", Value: ""})
				}
			})
		})
	})
	panelSettingRow(f, theme, i18n.T("背景深浅"), i18n.T("调整图片上的底色，保持聊天文字清晰"), func() {
		panelControlColumn(f, 240, func() {
			if !s.shade.Dragging() && !s.shadeDirty && (p.pending == nil || p.pending.setting != "background_shade") {
				s.shade.Value = float32(settings.BackgroundShade)
			}
			f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
				slider := f.Slider(s.backgroundShade, &s.shade).Step(5).Grow(1).
					Label(i18n.T("背景深浅")).Disabled(settings.Background == "")
				if slider.Changed() {
					s.shadeDirty = true
				}
				if s.shadeDirty && !s.shade.Dragging() {
					p.submitString(submit, state, "background_shade", strconv.Itoa(int(s.shade.Value)))
					s.shadeDirty = false
				}
				f.Text(strconv.Itoa(int(s.shade.Value)) + "%").FontSize(12).Muted().Width(40)
			})
		})
	})
}

func (p *Panels) drawSettingsProfile(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	panelSection(f, i18n.T("个人资料"), "", func() {
		panelSettingRow(f, theme, state.Self.Name, i18n.T("本机昵称和头像"), func() {
			if f.Button(s.profile, i18n.T("编辑资料…")).Small().Outline().Clicked() && s.openProfile != nil {
				s.openProfile()
			}
		})
		panelSettingRow(f, theme, i18n.T("隐藏离线联系人"), i18n.T("联系人列表仅显示自己和在线联系人"), func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(s.offline, state.Settings.HideOfflineContacts).Label(i18n.T("隐藏离线联系人")).Clicked() {
					p.submitBool(submit, state, "hide_offline_contacts", !state.Settings.HideOfflineContacts)
				}
			})
		})
	})
}

func (p *Panels) drawDiscoverySettings(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	panelSettingRow(f, theme, i18n.T("自动发现联系人"), i18n.T("搜索同一局域网内运行 GoIM 的设备"), func() {
		panelControlColumn(f, 80, func() {
			if f.Switch(s.discovery, !state.Settings.DisableAutoDiscovery).Label(i18n.T("自动发现联系人")).Clicked() {
				p.submitBool(submit, state, "disable_auto_discovery", !state.Settings.DisableAutoDiscovery)
			}
		})
	})
	panelSettingRow(f, theme, i18n.T("手动连接"), i18n.T("可用 IP 或 IP:端口连接可达的设备"), func() {
		panelControlColumn(f, 260, func() {
			f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
				field := f.Field(&s.address, s.connectAddress, i18n.T("对方 IP 地址"), i18n.T("IP 或 IP:端口")).Grow(1)
				if f.Button(s.connect, i18n.T("连接")).Small().Outline().Clicked() || field.Submitted() {
					address := strings.TrimSpace(s.address.Text())
					if address == "" {
						p.localError = i18n.T("请输入对方 IP 地址")
						return
					}
					p.localError, p.localNotice = "", i18n.T("已发起连接")
					submit(domain.Command{Kind: domain.Connect, Address: address})
				}
			})
		})
	})
}

func (p *Panels) drawSettingsSecurity(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	s := &p.settingsExtra
	settings := state.Settings
	panelSection(f, i18n.T("应用锁定"), i18n.T("锁定后需要输入密码才能查看会话。消息与文件仍在后台接收。"), func() {
		panelSettingRow(f, theme, i18n.T("启动时锁定"), i18n.T("下次启动 GoIM 时先验证密码"), func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(s.lockStartup, settings.LockOnStartup).Label(i18n.T("启动时锁定")).Disabled(!settings.HasLockPassword).Clicked() {
					p.submitBool(submit, state, "lock_on_startup", !settings.LockOnStartup)
				}
			})
		})
		f.Column().MaxWidth(360).Gap(8).PadY(10).Children(func() {
			if settings.HasLockPassword {
				f.Text(i18n.T("当前密码")).FontSize(13)
				f.Field(&s.old, s.passwordOld, i18n.T("当前密码"), i18n.T("输入当前密码")).FillWidth()
			}
			f.Text(i18n.T("新密码")).FontSize(13)
			f.Field(&s.password, s.passwordNew, i18n.T("新密码"), i18n.T("至少 6 个字符")).FillWidth()
			f.Text(i18n.T("确认新密码")).FontSize(13)
			last := f.Field(&s.repeat, s.passwordRepeat, i18n.T("确认新密码"), i18n.T("再输入一次新密码")).FillWidth()
			if s.passwordError != "" {
				f.Paragraph(s.passwordError, 2).Color(theme.Danger).FontSize(12)
			}
			if f.Button(s.passwordSave, i18n.T("保存密码")).Small().Clicked() || last.Submitted() {
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
}

func (p *Panels) drawAutoAcceptImages(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSettingRow(f, theme, i18n.T("自动接收聊天图片"), i18n.T("接收后可直接在聊天中预览"), func() {
		panelControlColumn(f, 80, func() {
			if f.Switch(p.settingsExtra.acceptImages, settings.AutoAcceptImages || settings.AutoAccept).
				Label(i18n.T("自动接收聊天图片")).Disabled(settings.AutoAccept).Clicked() {
				p.submitBool(submit, state, "auto_accept_images", !settings.AutoAcceptImages)
			}
		})
	})
}

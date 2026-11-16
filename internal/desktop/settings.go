package desktop

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/gfx"
	"os"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"goim/internal/flex"

	"goim/internal/domain"
	"goim/internal/i18n"
)

var (
	panelThemeLabels   = []string{"明亮", "暗黑"}
	panelThemeValues   = []string{"light", "dark"}
	panelSendKeyLabels = []string{"Enter 发送", "Ctrl+Enter 发送"}
	panelSendKeyValues = []string{"Enter", "Ctrl+Enter"}
)

const (
	panelSettingsContentMax = float32(760)
	panelSettingsNarrowAt   = float32(620)
	panelSettingsControlH   = float32(38)
)

func (p *Panels) drawSettings(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	available := f.Bounds().Width - panelSettingsWidth - 1 - 32 - 32
	p.settingsWidth = min(max(available, 0), panelSettingsContentMax)
	p.settingsNarrow = p.settingsWidth > 0 && p.settingsWidth < panelSettingsNarrowAt
	contentTheme := theme.WithControlHeight(panelSettingsControlH)
	f.Row().Fill().Children(func() {
		p.drawSettingsCategories(f, theme)
		f.Divider().FillHeight()
		f.Column().Grow(1).FillHeight().PadSides(24, 32, 10, 32).
			Items(flex.Center).Children(func() {
			f.Column().FillWidth().MaxWidth(panelSettingsContentMax).FillHeight().Gap(14).Children(func() {
				p.drawSettingsHeader(f, contentTheme, state)
				feedback := state
				if feedback.Notice != "锁定密码已保存" && feedback.Notice != "已切换局域网接口" {
					feedback.Notice = ""
				}
				p.panelFeedback(f, contentTheme, feedback, submit, p.ids.settingsDismissLocal, p.ids.settingsClearError)
				scroll := p.settingsScrollArea(f, p.settingsTab)
				scroll.Grow(1).FillWidth().Children(func() {
					f.Column().FillWidth().PadSides(0, 0, 22, 0).Children(func() {
						switch p.settingsTab {
						case panelSettingsGeneral:
							p.drawSettingsGeneral(f, contentTheme, state, submit)
						case panelSettingsShortcuts:
							p.drawSettingsShortcuts(f, contentTheme, state, submit)
						case panelSettingsAbout:
							p.drawSettingsAbout(f, theme, state, submit)
						case panelSettingsProfile:
							p.drawSettingsProfile(f, contentTheme, state, submit)
						case panelSettingsNotifications:
							p.drawSettingsNotifications(f, contentTheme, state, submit)
						case panelSettingsNetwork:
							p.drawSettingsNetwork(f, contentTheme, state, submit)
						case panelSettingsTransfers:
							p.drawSettingsTransfers(f, contentTheme, state, submit)
						case panelSettingsSecurity:
							p.drawSettingsSecurity(f, contentTheme, state, submit)
						}
					})
				})
				panelScrollFooter(f, contentTheme, "", p.settingsFooterText())
			})
		})
	})
}

func (p *Panels) settingsRow(f *flex.Frame, theme ui.Theme, title, description string, control func()) {
	labels := func() {
		f.Column().Gap(3).Children(func() {
			f.Text(title).FontSize(theme.Metrics.FontSizeM + 1).Weight(gfx.WeightMedium)
			if description != "" {
				f.Paragraph(description, 3).Muted().FontSize(theme.Metrics.FontSizeS)
			}
		})
	}
	if p.settingsNarrow {
		f.Column().FillWidth().Gap(9).PadY(13).Children(func() {
			f.Column().FillWidth().Children(labels)
			if control != nil {
				f.Row().FillWidth().Justify(flex.End).Children(control)
			}
		})
	} else {
		f.Row().FillWidth().Gap(24).PadY(13).Items(flex.Center).Children(func() {
			f.Column().Grow(1).Children(labels)
			if control != nil {
				control()
			}
		})
	}
	f.Divider().FillWidth()
}

func (p *Panels) settingsControl(f *flex.Frame, width float32, body func()) {
	if p.settingsWidth > 0 {
		width = min(width, p.settingsWidth)
	}
	f.Column().Width(width).Shrink(0).Items(flex.End).Justify(flex.Center).Children(body)
}

func (p *Panels) drawSettingsCategories(f *flex.Frame, theme ui.Theme) {
	f.Column().Width(panelSettingsWidth).FillHeight().PadSides(22, 20, 14, 20).Gap(3).Children(func() {
		f.Text(i18n.T("设置")).FontSize(26).Weight(gfx.WeightSemibold)
		f.Slot().Height(18)
		for _, tab := range panelSettingsOrder {
			label := i18n.T(panelSettingsLabels[tab])
			id := p.ids.settingsCategories.At(int(tab))
			active := p.settingsTab == tab
			icon := panelIconGear
			switch tab {
			case panelSettingsProfile:
				icon = Users
			case panelSettingsNotifications:
				icon = panelSettingsBellIcon
			case panelSettingsNetwork:
				icon = Wifi
			case panelSettingsTransfers:
				icon = ArrowRightLeft
			case panelSettingsShortcuts:
				icon = panelIconKeyboard
			case panelSettingsAbout:
				icon = panelIconInfo
			case panelSettingsSecurity:
				icon = panelSettingsLockIcon
			}
			foreground := theme.Foreground
			if active {
				foreground = panelAccentText(theme)
			}
			row := f.Row().FillWidth().Height(44).PadX(12).Gap(12).Radius(theme.Metrics.RadiusControl).
				Items(flex.Center).HitWith(id, label, gfx.HitOptions{Role: gfx.RoleTab, Selected: active})
			if active {
				row = row.Background(panelAccentSurface(theme)).
					Hover(flex.Look{}.Background(theme.Background.Mix(panelAccentText(theme), 0.12)))
			} else {
				row = row.Hover(flex.Look{}.Background(theme.Hover))
			}
			if f.Canvas().FocusVisible() == id {
				row = row.Stroke(1.5, theme.Ring)
			}
			if row.Children(func() {
				f.Icon(icon).Size(18, 18).Color(foreground)
				f.Text(label).FontSize(15).Weight(gfx.WeightMedium).Color(foreground)
			}).Clicked() {
				if p.settingsTab != tab {
					p.cancelShortcutEdit()
					p.aboutLicense = false
					p.resetShortcuts = false
					p.localError = ""
					p.localNotice = ""
					p.duplicateSelect.Open = false
					p.networkSelect.Open = false
					p.settingsExtra.resetSecrets()
				}
				p.settingsTab = tab
			}
		}
		f.Spacer()
		p.drawSettingsSidebarFooter(f, theme)
	})
}

func (p *Panels) drawSettingsSidebarFooter(f *flex.Frame, theme ui.Theme) {
	f.Divider().FillWidth()
	f.Text(i18n.T("设置自动保存在本机")).Muted().FontSize(theme.Metrics.FontSizeS)
}

func (p *Panels) drawSettingsHeader(f *flex.Frame, theme ui.Theme, state domain.State) {
	title, description := i18n.T(panelSettingsLabels[p.settingsTab]), i18n.T("管理外观、消息发送与窗口行为")
	switch p.settingsTab {
	case panelSettingsShortcuts:
		title, description = i18n.T("快捷键"), i18n.T("设置组合键，并检查应用内和系统快捷键冲突")
	case panelSettingsAbout:
		title, description = i18n.T("关于"), i18n.T("版本、构建信息与开源许可")
	case panelSettingsProfile:
		description = i18n.T("在局域网内展示的昵称与头像")
	case panelSettingsNotifications:
		description = i18n.T("设置消息提醒、声音和预览")
	case panelSettingsNetwork:
		description = i18n.T("局域网发现、网卡和手动连接")
	case panelSettingsTransfers:
		description = i18n.T("设置接收方式和文件保存位置")
	case panelSettingsSecurity:
		description = i18n.T("设置锁定密码与启动保护")
	}
	panelPageHeader(f, theme, title, description, func() {
		if p.settingsTab != panelSettingsAbout {
			p.drawSettingsSaveStatus(f, theme)
		}
	})
}

func (p *Panels) drawSettingsSaveStatus(f *flex.Frame, theme ui.Theme) {
	text := i18n.T("已保存")
	tone := theme.Success
	showCheck := true
	switch {
	case p.pending != nil:
		text, tone, showCheck = i18n.T("保存中"), theme.MutedForeground, false
	case p.saveFailed:
		text, tone, showCheck = i18n.T("保存失败"), theme.Danger, false
	}
	f.Row().Gap(6).Items(flex.Center).Children(func() {
		if showCheck {
			f.Icon(panelIconCheck).Size(15, 15).Color(tone)
		}
		f.Text(text).FontSize(theme.Metrics.FontSizeS + 1).Color(tone)
	})
}

func (p *Panels) settingsScrollArea(f *flex.Frame, tab panelSettingsTab) flex.Box {
	id := p.settingsScrollID(tab)
	area := f.ScrollArea(id)
	if !p.scrollTabSet || p.scrollTab != tab {
		p.view.ScrollState(id).ScrollTo(p.settingsScroll[tab])
		p.scrollTab = tab
		p.scrollTabSet = true
	}
	return area
}

func (p *Panels) settingsFooterText() string {
	switch p.settingsTab {
	case panelSettingsNetwork:
		return i18n.T("互通设置修改后请点击保存")
	case panelSettingsShortcuts:
		return i18n.T("录制后点击保存")
	case panelSettingsAbout:
		return i18n.T("此页面可离线查看")
	default:
		return i18n.T("修改后自动保存")
	}
}

func (p *Panels) drawSettingsGeneral(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSection(f, i18n.T("外观"), "", func() {
		p.settingsRow(f, theme, i18n.T("主题"), i18n.T("仅提供明亮与暗黑两种外观"), func() {
			selected := -1
			for index, value := range panelThemeValues {
				if settings.Theme == value {
					selected = index
					break
				}
			}
			p.settingsControl(f, 260, func() {
				choice := f.Choice(p.ids.themeChoice, i18n.Labels(panelThemeLabels), selected).FillWidth()
				if index, ok := choice.ChosenIndex(); ok {
					p.submitString(submit, state, "theme", panelThemeValues[index])
				}
			})
		})
	})
	p.settingsRow(f, theme, i18n.T("界面语言"), i18n.T("立即切换 GoIM 的界面语言"), func() {
		selected := 0
		if i18n.Normalize(settings.Language) == i18n.English {
			selected = 1
		}
		p.settingsControl(f, 260, func() {
			if index, ok := f.Choice(p.ids.languageChoice, []string{"简体中文", "English"}, selected).FillWidth().ChosenIndex(); ok {
				p.submitString(submit, state, "language", [...]string{i18n.Chinese, i18n.English}[index])
			}
		})
	})
	p.drawBackgroundSettings(f, theme, state, submit)
	panelSectionGap(f)
	panelSection(f, i18n.T("发送"), "", func() {
		p.settingsRow(f, theme, i18n.T("发送消息"), i18n.T("Shift+Enter 始终换行"), func() {
			selected := 0
			for index, value := range panelSendKeyValues {
				if settings.SendKey == value {
					selected = index
					break
				}
			}
			p.settingsControl(f, 250, func() {
				choice := f.Choice(p.ids.sendKeyChoice, i18n.Labels(panelSendKeyLabels), selected).FillWidth()
				if index, ok := choice.ChosenIndex(); ok {
					p.submitString(submit, state, "send_key", panelSendKeyValues[index])
				}
			})
		})
	})
	panelSectionGap(f)
	panelSection(f, i18n.T("启动与关闭"), "", func() {
		description := i18n.T("关闭主窗口时退出应用")
		if settings.CloseToTray {
			description = i18n.T("关闭主窗口后继续在托盘接收消息")
		}
		p.settingsRow(f, theme, i18n.T("关闭主窗口"), description, func() {
			p.settingsControl(f, 80, func() {
				if f.Switch(p.ids.settingsCloseToTray, settings.CloseToTray).
					Label(i18n.T("关闭主窗口时最小化到托盘")).Clicked() {
					p.submitBool(submit, state, "close_to_tray", !settings.CloseToTray)
				}
			})
		})
	})
}

func (p *Panels) drawSettingsNotifications(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSection(f, i18n.T("消息通知"), "", func() {
		p.settingsRow(f, theme, i18n.T("桌面通知"), i18n.T("收到新消息时提醒"), func() {
			p.settingsControl(f, 80, func() {
				if f.Switch(p.ids.settingsNotification, settings.DesktopNotifications).
					Label(i18n.T("桌面通知")).Clicked() {
					p.submitBool(submit, state, "desktop_notifications", !settings.DesktopNotifications)
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("提示音"), i18n.T("消息免打扰的会话不播放提示音"), func() {
			p.settingsControl(f, 80, func() {
				if f.Switch(p.ids.settingsSound, settings.Sound).Label(i18n.T("提示音")).Clicked() {
					p.submitBool(submit, state, "sound", !settings.Sound)
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("隐藏消息预览"), i18n.T("通知仅提示收到新消息"), func() {
			p.settingsControl(f, 80, func() {
				if f.Switch(p.settingsExtra.notificationPrivacy, settings.HideNotificationText).Label(i18n.T("隐藏消息预览")).Clicked() {
					p.submitBool(submit, state, "hide_notification_text", !settings.HideNotificationText)
				}
			})
		})
	})
}

func (p *Panels) drawSettingsTransfers(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSection(f, i18n.T("文件传输"), "", func() {
		description := i18n.T("仅影响之后接收的文件；正在进行的传输继续使用原目录")
		p.settingsRow(f, theme, i18n.T("默认保存位置"), description, func() {
			p.settingsControl(f, 420, func() {
				path := settings.DownloadDir
				f.Row().FillWidth().Gap(8).Children(func() {
					display := path
					if strings.TrimSpace(display) == "" {
						display = i18n.T("尚未选择保存位置")
					}
					f.Row().Grow(1).Height(theme.Metrics.ControlHeight).PadX(10).
						Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
						Clip().Items(flex.Center).Children(func() {
						f.Text(display).FontSize(theme.Metrics.FontSizeS + 1).PadX(4).Grow(1).Ellipsis()
					})
					if f.Button(p.ids.settingsChooseDir, i18n.T("更改…")).Small().Outline().Clicked() {
						p.chooseDownloadDirectory(f.Canvas(), submit, state)
					}
				})
				open := f.Button(p.ids.settingsOpenDir, i18n.T("打开文件夹")).Small().Link().
					Disabled(strings.TrimSpace(path) == "")
				if open.Clicked() {
					panelOpenLocal(f.Canvas(), p, path, true)
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("同名文件"), i18n.T("不覆盖已有文件"), func() {
			selected := -1
			for index, value := range panelDuplicateValues {
				if settings.DuplicatePolicy == value {
					selected = index
					break
				}
			}
			p.settingsControl(f, 250, func() {
				selectBox := f.Select(p.ids.duplicateChoice, &p.duplicateSelect, i18n.Labels(panelDuplicateLabels), selected).
					FillWidth().
					Label(i18n.T("同名文件"))
				if index, ok := selectBox.Picked(); ok && index < len(panelDuplicateValues) {
					p.submitString(submit, state, "duplicate_policy", panelDuplicateValues[index])
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("接收方式"), i18n.T("聊天图片自动显示；其他文件在你确认后才下载"), func() {
			f.Text(i18n.T("文件需确认")).FontSize(12).Muted()
		})
	})
}

func (p *Panels) drawSettingsNetwork(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSection(f, i18n.T("局域网"), "", func() {
		description := i18n.T("进行中的传输不会因网络切换而中断")
		if settings.PendingNetwork != "" {
			description = i18n.T("传输结束后将切换至 ") + p.networkName(settings.PendingNetwork)
		}
		if p.networkReadError != "" {
			description += i18n.T("；无法读取网络接口：") + p.networkReadError
		} else if state.ListenAddress != "" {
			description += i18n.T("；当前监听 ") + state.ListenAddress
		}
		labels, values, selected := p.networkChoicesFor(settings.Network)
		p.settingsRow(f, theme, i18n.T("使用的网络"), description, func() {
			p.settingsControl(f, 280, func() {
				selectBox := f.Select(p.ids.networkChoice, &p.networkSelect, labels, selected).
					FillWidth().
					Label(i18n.T("使用的网络"))
				if index, ok := selectBox.Picked(); ok && index < len(values) {
					value := values[index]
					if value == "" {
						value = "auto"
					}
					p.chooseNetwork(submit, state, value, labels[index])
				}
				if f.Button(p.ids.settingsRefreshNet, i18n.T("刷新网络适配器")).Small().Ghost().Clicked() {
					p.networkSelect.Open = false
					p.networkChange = panelNetworkChange{}
					p.refreshNetworks()
				}
			})
		})
		if p.networkChange.value != "" {
			p.drawNetworkChangeConfirmation(f, theme, submit, state)
		}
		p.drawDiscoverySettings(f, theme, state, submit)
	})
	p.drawCompatibility(f, theme, state, submit)
}

func (p *Panels) drawNetworkChangeConfirmation(f *flex.Frame, theme ui.Theme, submit func(domain.Command), state domain.State) {
	change := p.networkChange
	text := fmt.Sprintf(
		i18n.T("有 %d 个传输正在进行。切换到“%s”将等待这些传输结束后生效；首期不中断当前任务，如需立即切换请先取消相关传输。"),
		change.active, change.label,
	)
	if p.settingsNarrow {
		f.Column().FillWidth().PadSides(10, 12, 10, 12).Gap(10).
			Background(panelTint(theme.Primary, 0.06, theme.Background)).
			Border(1, panelTint(theme.Primary, 0.20, theme.Background)).Radius(8).Children(func() {
			f.Paragraph(text, 6).FontSize(theme.Metrics.FontSizeS + 1).
				Width(max(p.settingsWidth-24, 160))
			f.Row().FillWidth().Gap(8).Justify(flex.End).Children(func() {
				if f.Button(p.ids.networkCancel, i18n.T("取消更改")).Small().Ghost().Clicked() {
					p.networkChange = panelNetworkChange{}
				}
				if f.Button(p.ids.networkDefer, i18n.T("传输结束后切换")).Small().Clicked() {
					value := change.value
					p.networkChange = panelNetworkChange{}
					p.submitString(submit, state, "network", value)
				}
			})
		})
		return
	}
	f.Row().FillWidth().PadSides(10, 12, 10, 12).Gap(12).Items(flex.Center).
		Background(panelTint(theme.Primary, 0.06, theme.Background)).
		Border(1, panelTint(theme.Primary, 0.20, theme.Background)).Radius(8).Children(func() {
		f.Paragraph(text, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1)
		if f.Button(p.ids.networkCancel, i18n.T("取消更改")).Small().Ghost().Clicked() {
			p.networkChange = panelNetworkChange{}
		}
		if f.Button(p.ids.networkDefer, i18n.T("传输结束后切换")).Small().Clicked() {
			value := change.value
			p.networkChange = panelNetworkChange{}
			p.submitString(submit, state, "network", value)
		}
	})
}

func (p *Panels) drawSettingsShortcuts(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	f.Row().FillWidth().PadY(8).Items(flex.Center).Gap(12).Children(func() {
		f.Text(i18n.T("应用内快捷键会在当前窗口生效；全局快捷键只有注册成功后才会生效。")).Muted().
			FontSize(theme.Metrics.FontSizeS).Grow(1)
		if f.Button(p.ids.shortcutReset, i18n.T("恢复默认")).Small().Outline().Clicked() {
			p.cancelShortcutEdit()
			p.resetShortcuts = true
		}
	})
	if p.resetShortcuts {
		panelKeyboardConfirm(f, theme, i18n.T("恢复默认只会重置快捷键，不影响其他设置。"),
			p.ids.shortcutCancelReset, p.ids.shortcutConfirmReset,
			func() { p.resetShortcuts = false },
			func() {
				p.submitResetShortcuts(submit, state)
				p.resetShortcuts = false
				p.cancelShortcutEdit()
			})
	}
	f.Divider().FillWidth()
	groups := [...]struct {
		title, description, scope string
	}{
		{title: "应用内", description: "仅在 GoIM 获得焦点时生效", scope: "应用内"},
		{title: "全局", description: "在系统中全局注册，可显示或隐藏 GoIM", scope: "全局"},
		{title: "其他", description: "由现有用户设置保留", scope: ""},
	}
	inGroup := func(shortcut domain.Shortcut, group int) bool {
		if group < len(groups)-1 {
			return shortcut.Scope == groups[group].scope
		}
		return shortcut.Scope != groups[0].scope && shortcut.Scope != groups[1].scope
	}
	for groupIndex, group := range groups {
		count := 0
		for _, shortcut := range state.Settings.Shortcuts {
			if inGroup(shortcut, groupIndex) {
				count++
			}
		}
		if count == 0 {
			continue
		}
		f.Column().FillWidth().PadY(10).Gap(2).Children(func() {
			f.Text(i18n.T(group.title)).FontSize(theme.Metrics.FontSizeM + 1).Weight(gfx.WeightSemibold)
			f.Text(i18n.T(group.description)).Muted().FontSize(theme.Metrics.FontSizeS)
		})
		if !p.settingsNarrow {
			f.Row().FillWidth().PadY(6).Gap(12).Items(flex.Center).Children(func() {
				f.Text(i18n.T("操作")).Muted().FontSize(theme.Metrics.FontSizeS).Grow(1)
				f.Text(i18n.T("点击键帽录制")).Muted().FontSize(theme.Metrics.FontSizeS).Width(220)
			})
		}
		for index, shortcut := range state.Settings.Shortcuts {
			if !inGroup(shortcut, groupIndex) {
				continue
			}
			f.Column().FillWidth().PadY(11).Gap(7).Children(func() {
				if p.settingsNarrow {
					f.Column().FillWidth().Gap(2).Children(func() {
						f.Text(i18n.T(shortcut.Action)).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium)
						f.Text(i18n.T("点击键帽录制组合键")).Muted().FontSize(theme.Metrics.FontSizeS)
					})
					f.Row().FillWidth().Justify(flex.End).Children(func() {
						if panelShortcutKeycap(f, p.ids.shortcutRecord.At(index), shortcut.Keys, min(240, p.settingsWidth)) {
							p.beginShortcutEdit(index)
						}
					})
				} else {
					f.Row().FillWidth().Gap(12).Items(flex.Center).Children(func() {
						f.Column().Grow(1).Gap(2).Children(func() {
							f.Text(i18n.T(shortcut.Action)).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium)
							f.Text(i18n.T("点击键帽录制组合键")).Muted().FontSize(theme.Metrics.FontSizeS)
						})
						if panelShortcutKeycap(f, p.ids.shortcutRecord.At(index), shortcut.Keys, 220) {
							p.beginShortcutEdit(index)
						}
					})
				}
			})
			f.Divider().FillWidth()
		}
	}
	f.Text(i18n.T("发送消息使用 Enter 或 Ctrl+Enter，可在“常规”中修改。")).Muted().
		FontSize(theme.Metrics.FontSizeS).PadY(12)
	p.drawShortcutDialog(f, theme, state, submit)
}

func (p *Panels) beginShortcutEdit(index int) {
	p.cancelShortcutEdit()
	p.shortcutEdit = index
	p.shortcutKeys = ""
	p.shortcutError = ""
}

func (p *Panels) drawShortcutDialog(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	index := p.shortcutEdit
	if index < 0 || index >= len(state.Settings.Shortcuts) {
		if index >= 0 {
			p.cancelShortcutEdit()
		}
		return
	}
	shortcut := state.Settings.Shortcuts[index]
	problem := panelShortcutProblem(state.Settings.Shortcuts, index, p.shortcutKeys, p.shortcutError)
	dialog := f.Dialog(flex.DialogSpec{
		Title:       i18n.T("录制快捷键"),
		Description: fmt.Sprintf(i18n.T("为“%s”按下组合键。Tab 在弹窗内移动焦点，Esc 取消。"), i18n.T(shortcut.Action)),
		Width:       460, Scope: p.ids.shortcutDialog,
		DismissID: p.ids.shortcutDialogDismiss, CloseID: p.ids.shortcutDialogClose,
		InitialFocusID: p.ids.shortcutDialogSave.At(index),
	})
	dialog.Body(func() {
		f.Column().Fill().Gap(10).Children(func() {
			f.Text(i18n.T(shortcut.Action)).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium)
			panelShortcutChord(f, theme, p.shortcutKeys)
			message := problem
			tone := theme.Danger
			if message == "" && strings.TrimSpace(p.shortcutKeys) == "" {
				message, tone = i18n.T("请按下组合键…"), theme.MutedForeground
			}
			if message != "" {
				f.Text(message).FontSize(theme.Metrics.FontSizeS).Color(tone)
			}
		})
	})
	dialog.Footer(func() {
		if f.Button(p.ids.shortcutDialogReset.At(index), i18n.T("使用默认")).Small().Outline().Clicked() {
			p.shortcutKeys = panelShortcutDefaultKeys(state.Settings, shortcut.Action)
			p.shortcutError = ""
		}
		if f.Button(p.ids.shortcutDialogClear.At(index), i18n.T("清除")).Small().Ghost().Clicked() {
			p.shortcutKeys = ""
			p.shortcutError = ""
		}
		if f.Button(p.ids.shortcutDialogCancel.At(index), i18n.T("取消")).Small().Ghost().Clicked() {
			p.cancelShortcutEdit()
			return
		}
		save := f.Button(p.ids.shortcutDialogSave.At(index), i18n.T("保存")).Small()
		if problem != "" {
			save = save.Disabled(true)
		}
		if save.Clicked() && problem == "" {
			shortcuts := append([]domain.Shortcut(nil), state.Settings.Shortcuts...)
			shortcuts[index].Keys = p.shortcutKeys
			p.submitShortcuts(submit, state, shortcuts)
			p.cancelShortcutEdit()
		}
	})
	if dialog.Dismissed() {
		p.cancelShortcutEdit()
	}
}

func panelShortcutProblem(shortcuts []domain.Shortcut, index int, keys, recordingError string) string {
	switch {
	case recordingError != "":
		return recordingError
	case keys != "" && !panelShortcutValid(keys):
		return i18n.T("快捷键需要 Ctrl、Alt、Meta 或功能键")
	default:
		return panelShortcutConflict(shortcuts, index, keys)
	}
}

func panelShortcutDefaultKeys(settings domain.Settings, action string) string {
	for _, shortcut := range domain.DefaultSettings(settings.DownloadDir).Shortcuts {
		if shortcut.Action == action {
			return shortcut.Keys
		}
	}
	return ""
}

func panelShortcutKeycap(f *flex.Frame, id gfx.ID, keys string, width float32) bool {
	display := keys
	if strings.TrimSpace(display) == "" {
		display = i18n.T("未设置")
	}
	return f.Button(id, display).Small().Outline().Width(width).Clicked()
}

func panelShortcutChord(f *flex.Frame, theme ui.Theme, keys string) {
	display := keys
	if strings.TrimSpace(display) == "" {
		display = i18n.T("请按下组合键…")
	}
	f.Row().FillWidth().Height(38).PadX(12).Border(1, panelAccentText(theme)).
		Background(panelAccentSurface(theme)).
		Radius(theme.Metrics.RadiusControl).Items(flex.Center).Children(func() {
		f.Text(display).Grow(1).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).Ellipsis()
	})
}

func panelKeyboardConfirm(f *flex.Frame, theme ui.Theme, text string, cancelID, confirmID gfx.ID, cancel, confirm func()) {
	f.Row().FillWidth().PadSides(10, 12, 10, 12).Gap(12).Items(flex.Center).
		Background(panelTint(theme.Primary, 0.06, theme.Background)).
		Border(1, panelTint(theme.Primary, 0.20, theme.Background)).Radius(8).Children(func() {
		f.Text(text).FontSize(theme.Metrics.FontSizeS + 1).Grow(1)
		if f.Button(cancelID, i18n.T("取消")).Small().Ghost().Clicked() {
			cancel()
		}
		if f.Button(confirmID, i18n.T("恢复默认")).Small().DestructiveOutline().Clicked() {
			confirm()
		}
	})
}

func (p *Panels) chooseDownloadDirectory(canvas *gfx.Canvas, submit func(domain.Command), state domain.State) {
	canvas.AfterPaint(func() {
		path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: i18n.T("选择默认保存位置")})
		switch {
		case errors.Is(err, app.ErrCanceled):
			p.localError = ""
			return
		case err != nil:
			p.localError = i18n.T("无法选择保存位置：") + err.Error()
			return
		}
		if err := panelWritableDirectory(path); err != nil {
			p.localError = i18n.T("保存位置不可写：") + err.Error()
			return
		}
		p.localError = ""
		p.localNotice = ""
		p.submitString(submit, state, "download_dir", path)
	})
}

func panelWritableDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf(i18n.T("%q 不是目录"), path)
	}
	file, err := os.CreateTemp(path, ".goim-write-check-*")
	if err != nil {
		return err
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

const panelLicenseText = `GoIM 应用许可证

GoIM — MIT License，Copyright (c) 2026 Ezreal-byte。完整许可与第三方声明随应用压缩包提供。

第三方组件

Viva — Apache License 2.0
Go 标准库 — BSD 3-Clause
bbolt — MIT
pelletier/go-toml/v2 — MIT
purego — Apache License 2.0
Lucide 图标 — ISC
shadcn-svelte 组件 — MIT

完整许可证与第三方声明随相应依赖及其分发材料提供。Viva 的 LICENSE 与 THIRD_PARTY_NOTICES.md 包含其完整许可文本和图标来源说明。

该页面为只读内容，可离线查看。`

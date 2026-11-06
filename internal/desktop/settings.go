package desktop

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
)

var (
	panelThemeLabels   = []string{"明亮", "暗黑"}
	panelThemeValues   = []string{"light", "dark"}
	panelSendKeyLabels = []string{"Enter 发送", "Ctrl+Enter 发送"}
	panelSendKeyValues = []string{"Enter", "Ctrl+Enter"}
)

func (p *Panels) drawSettings(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	f.Row().Fill().Children(func() {
		p.drawSettingsCategories(f, theme)
		f.Divider().FillHeight()
		f.Column().Grow(1).FillHeight().PadSides(24, 32, 10, 32).Gap(14).Children(func() {
			p.drawSettingsHeader(f, theme, state)
			p.panelFeedback(f, theme, state, submit, p.ids.settingsDismissLocal, p.ids.settingsClearError)
			scroll := p.settingsScrollArea(f, p.settingsTab)
			scroll.Grow(1).FillWidth().Children(func() {
				f.Column().FillWidth().PadSides(0, 0, 22, 0).Children(func() {
					switch p.settingsTab {
					case panelSettingsGeneral:
						p.drawSettingsGeneral(f, theme, state, submit)
					case panelSettingsShortcuts:
						p.drawSettingsShortcuts(f, theme, state, submit)
					case panelSettingsAbout:
						p.drawSettingsAbout(f, theme, state, submit)
					}
				})
			})
			panelScrollFooter(f, theme, "", p.settingsFooterText())
		})
	})
}

func (p *Panels) drawSettingsCategories(f *flex.Frame, theme ui.Theme) {
	f.Column().Width(panelSettingsWidth).FillHeight().PadSides(22, 20, 14, 20).Gap(3).Children(func() {
		f.Text("设置").FontSize(26).Weight(ui.WeightSemibold)
		f.Slot().Height(18)
		for index, label := range panelSettingsLabels {
			id := p.ids.settingsCategories.At(index)
			active := int(p.settingsTab) == index
			icon := panelIconGear
			switch index {
			case int(panelSettingsShortcuts):
				icon = panelIconKeyboard
			case int(panelSettingsAbout):
				icon = panelIconInfo
			}
			foreground := theme.Foreground
			if active {
				foreground = panelAccentText(theme)
			}
			row := f.Row().FillWidth().Height(44).PadX(12).Gap(12).Radius(theme.Metrics.RadiusControl).
				Items(flex.Center).HitWith(id, label, ui.HitOptions{Role: ui.RoleTab, Selected: active})
			if active {
				row = row.Background(panelAccentSurface(theme)).
					HoverBackground(theme.Background.Mix(panelAccentText(theme), 0.12))
			} else {
				row = row.HoverBackground(theme.Hover)
			}
			if f.Canvas().FocusVisible() == id {
				row = row.Stroke(1.5, theme.Ring)
			}
			if row.Children(func() {
				f.Icon(icon).Size(18, 18).Color(foreground)
				f.Text(label).FontSize(15).Weight(ui.WeightMedium).Color(foreground)
			}).Clicked() {
				if p.settingsTab != panelSettingsTab(index) {
					p.cancelShortcutEdit()
					p.aboutLicense = false
					p.resetShortcuts = false
					p.localError = ""
					p.localNotice = ""
					p.duplicateSelect.Open = false
					p.networkSelect.Open = false
				}
				p.settingsTab = panelSettingsTab(index)
			}
		}
		f.Spacer()
		p.drawSettingsSidebarFooter(f, theme)
	})
}

func (p *Panels) drawSettingsSidebarFooter(f *flex.Frame, theme ui.Theme) {
	f.Divider().FillWidth()
	f.Text("设置自动保存在本机").Muted().FontSize(theme.Metrics.FontSizeS)
}

func (p *Panels) drawSettingsHeader(f *flex.Frame, theme ui.Theme, state domain.State) {
	title, description := "常规", "管理外观、通知与文件传输"
	switch p.settingsTab {
	case panelSettingsShortcuts:
		title, description = "快捷键", "录制应用内快捷键，并检查作用范围内的冲突"
	case panelSettingsAbout:
		title, description = "关于", "版本、构建信息与开源许可"
	}
	panelPageHeader(f, theme, title, description, func() {
		if p.settingsTab != panelSettingsAbout {
			p.drawSettingsSaveStatus(f, theme)
		}
	})
}

func (p *Panels) drawSettingsSaveStatus(f *flex.Frame, theme ui.Theme) {
	text := "已保存"
	tone := theme.Success
	showCheck := true
	switch {
	case p.pending != nil:
		text, tone, showCheck = "保存中", theme.MutedForeground, false
	case p.saveFailed:
		text, tone, showCheck = "保存失败", theme.Danger, false
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
	if !p.scrollTabSet || p.scrollTab != tab {
		p.view.ScrollState(id).ScrollTo(p.settingsScroll[tab])
		p.scrollTab = tab
		p.scrollTabSet = true
	}
	return f.ScrollArea(id)
}

func (p *Panels) settingsFooterText() string {
	switch p.settingsTab {
	case panelSettingsGeneral:
		return "修改后自动保存"
	case panelSettingsShortcuts:
		return "录制后点击保存"
	default:
		return "此页面可离线查看"
	}
}

func (p *Panels) drawSettingsGeneral(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	settings := state.Settings
	panelSection(f, "外观", "", func() {
		panelSettingRow(f, theme, "主题", "仅提供明亮与暗黑两种外观", func() {
			selected := -1
			for index, value := range panelThemeValues {
				if settings.Theme == value {
					selected = index
					break
				}
			}
			panelControlColumn(f, 260, func() {
				choice := f.Choice(p.ids.themeChoice, panelThemeLabels, selected)
				if index, ok := choice.ChosenIndex(); ok {
					p.submitString(submit, state, "theme", panelThemeValues[index])
				}
			})
		})
	})

	panelSectionGap(f)
	panelSection(f, "消息通知", "", func() {
		panelSettingRow(f, theme, "桌面通知", "收到新消息时提醒", func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(p.ids.settingsNotification, settings.DesktopNotifications).
					Label("桌面通知").Clicked() {
					p.submitBool(submit, state, "desktop_notifications", !settings.DesktopNotifications)
				}
			})
		})
		panelSettingRow(f, theme, "提示音", "消息免打扰的会话不播放提示音", func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(p.ids.settingsSound, settings.Sound).Label("提示音").Clicked() {
					p.submitBool(submit, state, "sound", !settings.Sound)
				}
			})
		})
	})

	panelSectionGap(f)
	panelSection(f, "发送", "", func() {
		panelSettingRow(f, theme, "发送消息", "Shift+Enter 始终换行", func() {
			selected := 0
			for index, value := range panelSendKeyValues {
				if settings.SendKey == value {
					selected = index
					break
				}
			}
			panelControlColumn(f, 250, func() {
				choice := f.Choice(p.ids.sendKeyChoice, panelSendKeyLabels, selected)
				if index, ok := choice.ChosenIndex(); ok {
					p.submitString(submit, state, "send_key", panelSendKeyValues[index])
				}
			})
		})
	})

	panelSectionGap(f)
	panelSection(f, "文件传输", "", func() {
		description := "仅影响之后接收的文件；正在进行的传输继续使用原目录"
		panelSettingRow(f, theme, "默认保存位置", description, func() {
			panelControlColumn(f, 420, func() {
				path := settings.DownloadDir
				f.Row().FillWidth().Gap(8).Children(func() {
					display := path
					if strings.TrimSpace(display) == "" {
						display = "尚未选择保存位置"
					}
					f.Row().Grow(1).Height(theme.Metrics.ControlHeight).PadX(10).
						Background(theme.Muted).Border(1, theme.Border).Radius(theme.Metrics.RadiusControl).
						Clip().Items(flex.Center).Children(func() {
						f.Text(display).FontSize(theme.Metrics.FontSizeS + 1).Ellipsis()
					})
					if f.Button(p.ids.settingsChooseDir, "更改…").Small().Outline().Clicked() {
						p.chooseDownloadDirectory(f.Canvas(), submit, state)
					}
				})
				open := f.Button(p.ids.settingsOpenDir, "打开文件夹").Small().Link().
					Disabled(strings.TrimSpace(path) == "")
				if open.Clicked() {
					panelOpenLocal(f.Canvas(), p, path, true)
				}
			})
		})
		panelSettingRow(f, theme, "同名文件", "不覆盖已有文件", func() {
			selected := -1
			for index, value := range panelDuplicateValues {
				if settings.DuplicatePolicy == value {
					selected = index
					break
				}
			}
			panelControlColumn(f, 250, func() {
				selectBox := f.Select(p.ids.duplicateChoice, &p.duplicateSelect, panelDuplicateLabels, selected).
					Label("同名文件")
				if index, ok := selectBox.Picked(); ok && index < len(panelDuplicateValues) {
					p.submitString(submit, state, "duplicate_policy", panelDuplicateValues[index])
				}
			})
		})
		panelSettingRow(f, theme, "自动接收文件", "开启后仅自动保存来自已加入会话的文件，不会自动执行或打开", func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(p.ids.settingsAutoAccept, settings.AutoAccept).
					Label("自动接收文件").Clicked() {
					p.submitBool(submit, state, "auto_accept", !settings.AutoAccept)
				}
			})
		})
	})

	panelSectionGap(f)
	panelSection(f, "局域网", "", func() {
		description := "切换网络可能影响进行中的传输"
		if p.networkReadError != "" {
			description += "；无法读取网络接口：" + p.networkReadError
		} else if state.ListenAddress != "" {
			description += "；当前监听 " + state.ListenAddress
		}
		labels, values, selected := p.networkChoicesFor(settings.Network)
		panelSettingRow(f, theme, "使用的网络", description, func() {
			panelControlColumn(f, 280, func() {
				selectBox := f.Select(p.ids.networkChoice, &p.networkSelect, labels, selected).
					Label("使用的网络")
				if index, ok := selectBox.Picked(); ok && index < len(values) {
					value := values[index]
					if value == "" {
						value = "auto"
					}
					p.submitString(submit, state, "network", value)
				}
			})
		})
	})

	panelSectionGap(f)
	panelSection(f, "启动与关闭", "", func() {
		description := "关闭主窗口时退出应用"
		if settings.CloseToTray {
			description = "关闭主窗口后继续在托盘接收消息"
		}
		panelSettingRow(f, theme, "关闭主窗口", description, func() {
			panelControlColumn(f, 80, func() {
				if f.Switch(p.ids.settingsCloseToTray, settings.CloseToTray).
					Label("关闭主窗口时最小化到托盘").Clicked() {
					p.submitBool(submit, state, "close_to_tray", !settings.CloseToTray)
				}
			})
		})
	})
}

func (p *Panels) drawSettingsShortcuts(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	f.Row().FillWidth().PadY(8).Items(flex.Center).Gap(12).Children(func() {
		f.Text("应用内快捷键会在当前窗口生效；全局快捷键只有注册成功后才会生效。").Muted().
			FontSize(theme.Metrics.FontSizeS).Grow(1)
		if f.Button(p.ids.shortcutReset, "恢复默认").Small().Outline().Clicked() {
			p.cancelShortcutEdit()
			p.resetShortcuts = true
		}
	})
	if p.resetShortcuts {
		panelKeyboardConfirm(f, theme, "恢复默认只会重置快捷键，不影响其他设置。",
			p.ids.shortcutCancelReset, p.ids.shortcutConfirmReset,
			func() { p.resetShortcuts = false },
			func() {
				p.submitResetShortcuts(submit, state)
				p.resetShortcuts = false
				p.cancelShortcutEdit()
			})
	}
	f.Divider().FillWidth()
	f.Row().FillWidth().PadY(8).Gap(12).Items(flex.Center).Children(func() {
		f.Text("操作").Muted().FontSize(theme.Metrics.FontSizeS).Grow(1)
		f.Text("按键").Muted().FontSize(theme.Metrics.FontSizeS).Width(190)
		f.Text("作用范围").Muted().FontSize(theme.Metrics.FontSizeS).Width(80)
		f.Slot().Width(64)
	})
	f.Divider().FillWidth()

	for index, shortcut := range state.Settings.Shortcuts {
		editing := p.shortcutEdit == index
		keys := shortcut.Keys
		conflict := ""
		problem := ""
		if editing {
			keys = p.shortcutKeys
			conflict = panelShortcutConflict(state.Settings.Shortcuts, index, keys)
			switch {
			case p.shortcutError != "":
				problem = p.shortcutError
			case keys != "" && !panelShortcutValid(keys):
				problem = "快捷键需要 Ctrl、Alt、Meta 或功能键"
			case conflict != "":
				problem = conflict
			}
		}
		f.Column().FillWidth().PadY(11).Gap(8).Children(func() {
			f.Row().FillWidth().Gap(12).Items(flex.Center).Children(func() {
				f.Column().Grow(1).Gap(2).Children(func() {
					f.Text(shortcut.Action).FontSize(theme.Metrics.FontSizeM).Weight(ui.WeightMedium)
					if !editing {
						f.Text("点击更改后录制组合键").Muted().FontSize(theme.Metrics.FontSizeS)
					}
				})
				panelKeycap(f, theme, keys, 190, editing)
				f.Text(shortcut.Scope).Muted().FontSize(theme.Metrics.FontSizeS).Width(80)
				if editing {
					f.Slot().Width(64)
				} else if f.Button(p.ids.shortcutRecord.At(index), "更改").Small().Outline().Clicked() {
					p.cancelShortcutEdit()
					p.shortcutEdit = index
					p.shortcutKeys = ""
					p.shortcutError = ""
				}
			})
			if editing {
				f.Row().FillWidth().Gap(8).Items(flex.Center).Children(func() {
					message := problem
					if message == "" && keys == "" {
						message = "请按下组合键"
					}
					if message != "" {
						tone := theme.MutedForeground
						if problem != "" {
							tone = theme.Danger
						}
						f.Text(message).FontSize(theme.Metrics.FontSizeS).Color(tone).Grow(1)
					} else {
						f.Spacer()
					}
					if f.Button(p.ids.shortcutCancel.At(index), "取消").Small().Ghost().Clicked() {
						p.cancelShortcutEdit()
					}
					if f.Button(p.ids.shortcutRecord.At(index), "清除").Small().Ghost().Clicked() {
						p.shortcutKeys = ""
						p.shortcutError = ""
					}
					save := f.Button(p.ids.shortcutSave.At(index), "保存").Small()
					if problem != "" {
						save = save.Disabled(true)
					}
					if save.Clicked() {
						shortcuts := append([]domain.Shortcut(nil), state.Settings.Shortcuts...)
						shortcuts[index].Keys = p.shortcutKeys
						p.submitShortcuts(submit, state, shortcuts)
						p.cancelShortcutEdit()
					}
				})
			}
		})
		f.Divider().FillWidth()
	}
	f.Text("发送消息使用 Enter 或 Ctrl+Enter，可在“常规”中修改。").Muted().
		FontSize(theme.Metrics.FontSizeS).PadY(12)
}

func panelKeycap(f *flex.Frame, theme ui.Theme, keys string, width float32, active bool) {
	display := keys
	if strings.TrimSpace(display) == "" {
		display = "未设置"
	}
	foreground := theme.Foreground
	border := theme.Border
	background := theme.Background
	if strings.TrimSpace(keys) == "" {
		foreground = theme.MutedForeground
	}
	if active {
		border = panelAccentText(theme)
		background = panelAccentSurface(theme)
		foreground = panelAccentText(theme)
		if strings.TrimSpace(keys) == "" {
			display = "请按下组合键…"
		}
	}
	f.Row().Width(width).Height(28).PadX(9).Border(1, border).Background(background).
		Radius(6).Clip().Items(flex.Center).Children(func() {
		f.Text(display).FontSize(theme.Metrics.FontSizeS + 1).Color(foreground).Ellipsis()
	})
}

func panelKeyboardConfirm(f *flex.Frame, theme ui.Theme, text string, cancelID, confirmID ui.ID, cancel, confirm func()) {
	f.Row().FillWidth().PadSides(10, 12, 10, 12).Gap(12).Items(flex.Center).
		Background(panelTint(theme.Primary, 0.06, theme.Background)).
		Border(1, panelTint(theme.Primary, 0.20, theme.Background)).Radius(8).Children(func() {
		f.Text(text).FontSize(theme.Metrics.FontSizeS + 1).Grow(1)
		if f.Button(cancelID, "取消").Small().Ghost().Clicked() {
			cancel()
		}
		if f.Button(confirmID, "恢复默认").Small().DestructiveOutline().Clicked() {
			confirm()
		}
	})
}

func (p *Panels) drawSettingsAbout(f *flex.Frame, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	if p.aboutLicense {
		p.drawLicense(f, theme)
		return
	}
	f.Row().FillWidth().Gap(16).PadY(12).Items(flex.Center).Children(func() {
		f.Row().Width(54).Height(54).Radius(13).Background(theme.Primary).Items(flex.Center).
			Justify(flex.Center).Children(func() {
			f.Icon(panelIconInfo).Size(26, 26).Color(theme.PrimaryForeground)
		})
		f.Column().Grow(1).Gap(3).Children(func() {
			f.Text("GoIM").FontSize(22).Weight(ui.WeightSemibold)
			f.Paragraph("局域网内的原生即时通讯与文件传输工具。", 2).Muted().
				FontSize(theme.Metrics.FontSizeS + 1)
		})
	})

	panelSectionGap(f)
	panelSection(f, "构建", "", func() {
		version := state.Version
		if strings.TrimSpace(version) == "" {
			version = "未提供"
		}
		build := p.build.revision
		if build == "" {
			build = "未提供"
		}
		if p.build.buildTime != "" {
			build += " · " + p.build.buildTime
		}
		panelInfoRow(f, theme, "应用版本", version)
		panelInfoRow(f, theme, "构建", build)
		panelInfoRow(f, theme, "Go", p.build.goVersion)
		panelInfoRow(f, theme, "Viva", p.build.vivaVersion)
	})
	f.Row().FillWidth().PadY(16).Gap(10).Items(flex.Center).Children(func() {
		if f.Button(p.ids.aboutCopyVersion, "复制版本信息").Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelVersionReport(state, p.build))
		}
		if f.Button(p.ids.aboutLicense, "开源许可").Small().Outline().Clicked() {
			p.aboutLicense = true
			p.localError = ""
			p.localNotice = ""
		}
	})
	f.Paragraph("复制的内容只包含版本与构建信息，不包含聊天内容、联系人、文件路径或本机地址。", 3).
		Muted().FontSize(theme.Metrics.FontSizeS)
}

func panelInfoRow(f *flex.Frame, theme ui.Theme, label, value string) {
	f.Row().FillWidth().PadY(11).Gap(20).Items(flex.Start).Children(func() {
		f.Text(label).Muted().FontSize(theme.Metrics.FontSizeS + 1).Width(130)
		f.Paragraph(value, 3).FontSize(theme.Metrics.FontSizeS + 1).Grow(1)
	})
	f.Divider().FillWidth()
}

func (p *Panels) drawLicense(f *flex.Frame, theme ui.Theme) {
	f.Row().FillWidth().Gap(8).PadY(8).Items(flex.Center).Children(func() {
		if f.Button(p.ids.aboutBack, "返回关于").Small().Ghost().Clicked() {
			p.aboutLicense = false
		}
		f.Spacer()
		if f.Button(p.ids.aboutCopyLicense, "复制许可摘要").Small().Outline().Clicked() {
			panelCopy(f.Canvas(), p, panelLicenseText)
		}
	})
	f.Divider().FillWidth()
	for _, paragraph := range strings.Split(panelLicenseText, "\n\n") {
		f.Paragraph(paragraph, 40).FontSize(theme.Metrics.FontSizeS + 1).PadY(10)
	}
}

func panelVersionReport(state domain.State, build panelBuildInfo) string {
	version := state.Version
	if strings.TrimSpace(version) == "" {
		version = "未提供"
	}
	revision := build.revision
	if revision == "" {
		revision = "未提供"
	}
	line := fmt.Sprintf("GoIM %s\nGo %s\nViva %s\n构建 %s", version, build.goVersion, build.vivaVersion, revision)
	if build.buildTime != "" {
		line += "\n构建时间 " + build.buildTime
	}
	return line
}

func (p *Panels) chooseDownloadDirectory(canvas *ui.Canvas, submit func(domain.Command), state domain.State) {
	canvas.AfterPaint(func() {
		path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: "选择默认保存位置"})
		switch {
		case errors.Is(err, app.ErrCanceled):
			p.localError = ""
			return
		case err != nil:
			p.localError = "无法选择保存位置：" + err.Error()
			return
		}
		if err := panelWritableDirectory(path); err != nil {
			p.localError = "保存位置不可写：" + err.Error()
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
		return fmt.Errorf("%q 不是目录", path)
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

本源码树尚未包含 GoIM 应用自身的 LICENSE 文件，因此当前构建不声明应用级开源许可证。正式分发前应由项目所有者补充应用许可证。

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

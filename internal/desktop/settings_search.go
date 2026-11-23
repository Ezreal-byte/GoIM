package desktop

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"goim/internal/domain"
	"goim/internal/i18n"
)

type settingSearchItem struct {
	tab             panelSettingsTab
	title, keywords string
}

// Keys are also the labels used by the destination controls. Add aliases for
// terms users type that differ from the displayed setting name.
var searchableSettings = []settingSearchItem{
	{panelSettingsProfile, "编辑资料", "昵称 头像 签名 nickname avatar profile"},
	{panelSettingsProfile, "隐藏离线联系人", "offline"},
	{panelSettingsGeneral, "主题配色", "经典 松青 陶土 鸢尾 palette classic pine clay iris"},
	{panelSettingsGeneral, "主题", "外观 明亮 暗黑 dark light"},
	{panelSettingsGeneral, "显示 GitHub 图标", "顶栏 GoIM 项目 主页 仓库 title bar project repository"},
	{panelSettingsGeneral, "界面语言", "language English 简体中文"},
	{panelSettingsGeneral, "界面字号", "字体 大小 font size"},
	{panelSettingsGeneral, "界面字体", "font family"},
	{panelSettingsGeneral, "自定义背景", "聊天 壁纸 wallpaper"},
	{panelSettingsGeneral, "背景透明度", "背景深浅 opacity transparency"},
	{panelSettingsGeneral, "发送消息", "Enter Ctrl+Enter Shift+Enter 换行"},
	{panelSettingsGeneral, "开机自启", "启动 autostart launch"},
	{panelSettingsGeneral, "关闭主窗口", "托盘 tray"},
	{panelSettingsNotifications, "桌面通知", "消息提醒 notification"},
	{panelSettingsNotifications, "提示音", "铃声 声音 静音 mute sound"},
	{panelSettingsNotifications, "打开消息预览", "隐藏 隐私 通知 托盘 preview privacy"},
	{panelSettingsVoice, "麦克风", "输入设备 microphone input"},
	{panelSettingsVoice, "麦克风音量", "输入音量 input volume"},
	{panelSettingsVoice, "扬声器", "输出设备 speaker output"},
	{panelSettingsVoice, "扬声器音量", "输出音量 output volume"},
	{panelSettingsVoice, "设备列表", "刷新设备 refresh devices"},
	{panelSettingsNetwork, "使用的网络", "网卡 适配器 接口 adapter interface"},
	{panelSettingsNetwork, "自动发现联系人", "局域网 LAN discovery"},
	{panelSettingsNetwork, "手动连接", "地址 IP address"},
	{panelSettingsNetwork, "内网通互通", "协议 端口 UDP TCP neiwangtong port"},
	{panelSettingsNetwork, "飞鸽／飞秋互通", "协议 端口 UDP TCP IPMSG FeiQ port"},
	{panelSettingsNetwork, "DNS 服务器", "高级网络设置 跨网段发现 discovery server"},
	{panelSettingsNetwork, "发现域名", "高级网络设置 跨网段发现 DNS SRV domain"},
	{panelSettingsNetwork, "发现状态", "高级网络设置 跨网段发现 DNS status"},
	{panelSettingsTransfers, "默认保存位置", "文件接收 下载目录 download folder"},
	{panelSettingsTransfers, "同名文件", "重命名 duplicate rename"},
	{panelSettingsTransfers, "网页上传需要确认", "网页接收 临时免确认 web upload approval"},
	{panelSettingsTransfers, "自动接收聊天图片", "图片下载 image photo"},
	{panelSettingsTransfers, "自动接收聊天视频", "视频下载 video"},
	{panelSettingsSecurity, "启动时锁定", "启动保护 lock"},
	{panelSettingsSecurity, "使用系统验证解锁", "指纹 Windows Hello Touch ID biometrics"},
	{panelSettingsSecurity, "锁定密码", "修改密码 password"},
	{panelSettingsShortcuts, "恢复默认", "快捷键 重置 reset shortcuts"},
	{panelSettingsAbout, "开源许可", "license"},
	{panelSettingsAbout, "检查更新", "软件更新 升级 update"},
	{panelSettingsAbout, "自动检查更新", "软件更新 升级 update"},
	{panelSettingsAbout, "诊断与反馈", "日志 导出报告 diagnostics logs"},
	{panelSettingsAbout, "清除所有用户数据", "重置 reset data"},
	{panelSettingsAbout, "应用版本", "构建信息 版本号 version build"},
	{panelSettingsAbout, "平台与架构", "构建信息 architecture platform"},
	{panelSettingsAbout, "源码提交", "构建信息 commit"},
	{panelSettingsAbout, "Go", "构建信息 Go version"},
	{panelSettingsAbout, "Viva", "构建信息 Viva version"},
	{panelSettingsAbout, "构建来源", "build source"},
	{panelSettingsAbout, "构建时间", "build time"},
	{panelSettingsAbout, "QQ 群", "交流 community"},
	{panelSettingsAbout, "微信群", "交流 wechat"},
	{panelSettingsAbout, "项目官网", "下载 website"},
	{panelSettingsAbout, "GitHub 仓库", "源码 repository"},
	{panelSettingsAbout, "问题反馈", "报告问题 issues"},
}

type settingsSearch struct {
	field           ui.Field
	query, language string
	resetScroll     bool
	shortcuts       []domain.Shortcut
	results         []settingSearchItem
	target          string
	anchor          ui.Box
}

func (s *settingsSearch) update(state domain.State) {
	query := strings.TrimSpace(s.field.Text())
	lang := i18n.Language()
	if query == s.query && (query == "" || s.language == lang && slices.Equal(s.shortcuts, state.Settings.Shortcuts)) {
		return
	}
	s.resetScroll = s.query != query
	s.query, s.language = query, lang
	s.results = s.results[:0]
	if query == "" {
		return
	}
	s.shortcuts = append(s.shortcuts[:0], state.Settings.Shortcuts...)
	terms := strings.Fields(strings.ToLower(query))
	add := func(item settingSearchItem) {
		category := panelSettingsLabels[item.tab]
		text := strings.ToLower(category + " " + i18n.T(category) + " " + item.title + " " + i18n.T(item.title) + " " + item.keywords)
		for _, word := range terms {
			if !strings.Contains(text, word) {
				return
			}
		}
		s.results = append(s.results, item)
	}
	for _, item := range searchableSettings {
		add(item)
	}
	for _, shortcut := range state.Settings.Shortcuts {
		add(settingSearchItem{panelSettingsShortcuts, shortcut.Action, shortcut.Keys + " " + i18n.T(shortcut.Scope)})
	}
}
func (p *Panels) selectSettingsTab(f ui.C, tab panelSettingsTab) {
	p.search.field.SetText("")
	p.search.query = ""
	p.search.target = ""
	if p.settingsTab != tab {
		p.cancelShortcutEdit()
		p.aboutLicense = false
		p.resetShortcuts = false
		p.localError = ""
		p.settingsExtra.resetSecrets()
	}
	p.settingsTab = tab
	if tab == panelSettingsVoice && !p.settingsExtra.voiceDevices.known && !p.settingsExtra.voiceDevices.refreshing {
		p.settingsExtra.voiceDevices.refreshing = true
		f.After(p.refreshVoiceDevices)
	}
}

func (p *Panels) openSettingResult(f ui.C, item settingSearchItem, state domain.State) {
	p.selectSettingsTab(f, item.tab)
	p.aboutLicense = item.title == "开源许可"
	p.scrollTabSet = false
	p.settingsScroll[item.tab] = 0
	p.search.target = i18n.T(item.title)
	switch item.title {
	case "DNS 服务器", "发现域名", "发现状态":
		p.settingsExtra.dns.advanced.Open = true
	case "内网通互通", "飞鸽／飞秋互通":
		p.search.target = i18n.T("启用") + i18n.T(item.title)
	case "编辑资料":
		p.search.target = state.Self.Name
	}
	f.Canvas().RequestFrame()
}

func (p *Panels) drawSettingResults(f ui.C, state domain.State) {
	panelPageHeader(f, *f.Theme(), i18n.T("搜索结果"), "", nil)
	area := ui.Scroll(f.Part("setting-results")).Grow(1).FillWidth().AlwaysScrollbars()
	if p.search.resetScroll {
		area.ScrollState().ScrollTo(0)
		p.search.resetScroll = false
	}
	area.Children(func() {
		if len(p.search.results) == 0 {
			ui.Text(f, i18n.T("没有匹配的设置")).Muted()
			return
		}
		for _, item := range p.search.results {
			label, category := i18n.T(item.title), i18n.T(panelSettingsLabels[item.tab])
			id := ui.ID(f.KeyInt(int(item.tab)).Key(item.title))
			row := ui.Row(f).FillWidth().MinHeight(44).Items(ui.Center).PadX(8).Gap(12).Radius(radiusControl).
				Hover(ui.Look{}.Background(f.Theme().Hover)).HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: category})
			if f.Canvas().FocusVisible() == id {
				row.Stroke(1, f.Theme().Ring)
			}
			row.Children(func() {
				ui.Text(f, label).Grow(1).Ellipsis()
				ui.Text(f, category).Muted().FontSize(textSize(f, 12))
			})
			if row.Clicked() {
				p.openSettingResult(f, item, state)
			}
		}
	})
}

func (p *Panels) settingAnchor(label string, box ui.Box) {
	if p.search.target != "" && p.search.target == label {
		p.search.anchor = box
	}
}

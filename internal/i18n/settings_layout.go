package i18n

// settingsEnglish is kept separate from the main catalog so settings layout
// copy can grow without making the shared locale table harder to scan. init
// runs after package variables, so these entries are merged into en in a
// deterministic order regardless of file declaration order.
var settingsEnglish = [...]struct {
	source, translated string
}{
	{"自动", "Automatic"},
	{"例如 192.168.1.12 或 192.168.1.12:47842", "e.g. 192.168.1.12 or 192.168.1.12:47842"},
	{"先选择连接方式，再输入对方设备地址。", "Choose a connection method, then enter the peer's address."},
	{"应用内", "In GoIM"},
	{"全局", "Global"},
	{"其他", "Other"},
	{"仅在 GoIM 获得焦点时生效", "Active only while GoIM has focus"},
	{"在系统中全局注册，可显示或隐藏 GoIM", "Registered with the system to show or hide GoIM"},
	{"由现有用户设置保留", "Preserved from your existing settings"},
	{"点击键帽录制", "Click a keycap to record"},
	{"点击键帽录制组合键", "Click the keycap to record a shortcut"},
	{"录制快捷键", "Record shortcut"},
	{"为“%s”按下组合键。Tab 在弹窗内移动焦点，Esc 取消。", "Press a key combination for “%s”. Tab moves focus in this dialog; Esc cancels."},
	{"使用默认", "Use default"},
	{"界面字号", "Interface font size"},
	{"调整 GoIM 全窗口文字大小", "Change text size across GoIM windows"},
	{"界面字体", "Interface font"},
	{"更改 GoIM 窗口、菜单和设置页使用的字体", "Choose the font used by GoIM windows, menus, and settings"},
	{"选择字体", "Choose font"},
	{"当前：%s", "Current: %s"},
	{"系统默认", "System default"},
	{"搜索已安装的字体", "Search installed fonts"},
	{"正在读取已安装的字体…", "Loading installed fonts…"},
	{"未找到匹配的字体。", "No matching fonts."},
	{"当前平台没有可枚举的系统字体。", "System fonts cannot be listed on this platform."},
	{"应用所选字体", "Apply selected font"},
	{"当前", "Current"},
	{"已保存的字体未安装，暂时使用系统字体", "The saved font is not installed; the system font is being used"},
	{"当前平台不支持更改界面字体", "This platform cannot change the interface font"},
	{"Aa Bb Cc 你好，世界！ 0123456789", "Aa Bb Cc Hello, world! 0123456789"},
}

func init() {
	for _, entry := range settingsEnglish {
		en[entry.source] = entry.translated
	}
}

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
}

func init() {
	for _, entry := range settingsEnglish {
		en[entry.source] = entry.translated
	}
}

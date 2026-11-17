package i18n

// desktopFeaturesEnglish covers desktop features that are not part of the
// settings layout catalog, keeping the main locale table easy to scan.
func init() {
	for label, english := range map[string]string{
		"开机自启":             "Launch at login",
		"无法读取开机自启状态：":      "Could not read the launch-at-login setting: ",
		"无法修改开机自启设置：":      "Could not change the launch-at-login setting: ",
		"正在更新系统登录项…":       "Updating the system login item…",
		"当前系统或运行环境不支持开机自启": "Launch at login is not supported on this system or runtime",
		"正在读取系统登录项…":       "Reading the system login item…",
		"登录系统后自动启动 GoIM":   "Start GoIM automatically after you sign in",
		"诊断与反馈":            "Diagnostics and feedback",
		"遇到问题时导出脱敏报告，仅保留版本、协议状态、适配器和数据计数，不含聊天内容、联系人、完整地址、文件路径或凭据。": "Export a redacted report for troubleshooting. It keeps only versions, protocol status, adapters and counts, without chats, contacts, full addresses, paths or credentials.",
		"导出脱敏报告":             "Export redacted report",
		"导出脱敏诊断报告":           "Export redacted diagnostic report",
		"文本文件":               "Text file",
		"无法创建诊断报告：":          "Could not create the diagnostic report: ",
		"无法保存诊断报告：":          "Could not save the diagnostic report: ",
		"诊断报告已保存":            "Diagnostic report saved",
		"可发送给开发者协助排查问题。":     "You can send it to the developer to help diagnose the problem.",
		"自动检查更新":             "Check for updates automatically",
		"启动 GoIM 时在后台检查新版本":  "Check for new versions in the background when GoIM starts",
		"自动检查已关闭，可随时手动检查更新。": "Automatic checks are off. You can check for updates manually at any time.",
		"更新说明":               "Release notes",
		"正在加载发布说明…":          "Loading release notes…",
		"暂时无法加载发布说明":         "Release notes could not be loaded",
		"无法打开独立会话窗口":         "Could not open the detached chat window",
		"无法打开独立会话窗口：":        "Could not open the detached chat window: ",
		"请稍后重试。":             "Please try again.",
		"置顶会话窗口":             "Keep chat window on top",
		"拖到窗口外打开独立会话":        "Drop outside the window to open a separate chat",
		"无法更改会话窗口置顶":         "Could not change the chat window pin state",
		"当前系统不支持窗口置顶。":       "This system does not support always-on-top windows.",
	} {
		if _, exists := en[label]; !exists {
			en[label] = english
		}
	}
}

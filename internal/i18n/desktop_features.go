package i18n

// desktopFeaturesEnglish covers desktop features that are not part of the
// settings layout catalog, keeping the main locale table easy to scan.
func init() {
	for label, english := range map[string]string{
		"开机自启":                "Launch at login",
		"无法读取开机自启状态：":         "Could not read the launch-at-login setting: ",
		"无法修改开机自启设置：":         "Could not change the launch-at-login setting: ",
		"正在更新系统登录项…":          "Updating the system login item…",
		"当前系统或运行环境不支持开机自启":    "Launch at login is not supported on this system or runtime",
		"正在读取系统登录项…":          "Reading the system login item…",
		"登录系统后自动启动 GoIM":      "Start GoIM automatically after you sign in",
		"诊断与反馈":               "Diagnostics and feedback",
		"遇到问题时导出报告，发送给开发者排查。": "Export a report when you encounter a problem and send it to the developer.",
		"系统诊断报告":              "System diagnostic report",
		"包含版本、协议状态、适配器和数据计数；不包含聊天内容、联系人、完整地址或文件路径。": "Includes versions, protocol status, adapters and data counts. It excludes chat content, contacts, full addresses and file paths.",
		"导出报告":             "Export report",
		"保存系统诊断报告":         "Save system diagnostic report",
		"文本文件":             "Text file",
		"无法创建诊断报告：":        "Could not create the diagnostic report: ",
		"无法保存诊断报告：":        "Could not save the diagnostic report: ",
		"诊断报告已保存":          "Diagnostic report saved",
		"可发送给开发者协助排查问题。":   "You can send it to the developer to help diagnose the problem.",
		"无法打开独立会话窗口":       "Could not open the detached chat window",
		"无法打开独立会话窗口：":      "Could not open the detached chat window: ",
		"请稍后重试。":           "Please try again.",
		"会话已在独立窗口打开":       "Chats are open in detached windows",
		"关闭独立窗口后，会话会回到这里。": "Close a detached window to return its chat here.",
	} {
		if _, exists := en[label]; !exists {
			en[label] = english
		}
	}
}

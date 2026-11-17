package i18n

func init() {
	for label, english := range map[string]string{
		"构建信息": "Build information",
		"点击任一卡片可复制完整的构建信息。": "Select any card to copy the complete build information.",
		"平台与架构":            "Platform & architecture",
		"构建来源":             "Build source",
		"源码提交":             "Source commit",
		"构建时间":             "Build time",
		"加入交流":             "Community",
		"项目":               "Project",
		"项目官网":             "Project website",
		"了解和下载 GoIM":       "Learn about and download GoIM",
		"查看入群方式":           "See how to join",
		"复制群号":             "Copy group number",
		"GitHub 仓库":        "GitHub repository",
		"查看源码、版本与发布记录":     "Browse source, versions and release history",
		"问题反馈":             "Report an issue",
		"报告问题并跟进修复":        "Report a problem and follow its fix",
		"QQ 群":             "QQ group",
		"微信群":              "WeChat group",
		"复制构建信息":           "Copy build information",
		"在浏览器中打开":          "Open in browser",
		"无法打开链接：":          "Could not open the link: ",
		"平台":               "Platform",
		"提交日期":             "Commit date",
		"本地构建":             "Local build",
		"修改密码":             "Change password",
		"设置密码":             "Set password",
		"锁定密码":             "Lock password",
		"用于验证 GoIM 的应用锁定。": "Protect access to GoIM when it is locked.",
		"无法注册全局快捷键":        "Could not register a global shortcut",
		"原快捷键仍可用":          "The previous shortcut still works",
	} {
		if _, exists := en[label]; !exists {
			en[label] = english
		}
	}
}

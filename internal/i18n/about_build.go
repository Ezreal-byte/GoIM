package i18n

func init() {
	for label, english := range map[string]string{
		"构建信息": "Build information",
		"点击任一卡片可复制完整的构建信息。": "Select any card to copy the complete build information.",
		"平台与架构": "Platform & architecture",
		"构建来源":  "Build source",
		"源码提交":  "Source commit",
		"构建时间":  "Build time",
		"CI 运行": "CI run",
		"项目与支持": "Project & support",
		"源码与问题跟踪使用 GitHub；群聊入口确认后再开放。": "Source and issues use GitHub. Chat groups will be listed after they are confirmed.",
		"GitHub 仓库":    "GitHub repository",
		"查看源码、版本与发布记录": "Browse source, versions and release history",
		"问题反馈":         "Report an issue",
		"报告问题并跟进修复":    "Report a problem and follow its fix",
		"QQ 群":         "QQ group",
		"微信群":          "WeChat group",
		"待开放":          "Coming soon",
		"复制构建信息":       "Copy build information",
		"在浏览器中打开":      "Open in browser",
		"无法打开链接：":      "Could not open the link: ",
		"平台":           "Platform",
		"提交日期":         "Commit date",
		"CI 链接":        "CI link",
		"本地构建":         "Local build",
		"打开 CI 运行":     "Open CI run",
	} {
		if _, exists := en[label]; !exists {
			en[label] = english
		}
	}
}

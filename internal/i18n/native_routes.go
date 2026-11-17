package i18n

func init() {
	for label, english := range map[string]string{
		"刷新头像": "Refresh avatar",
		"联系资料": "Contact information",
		"设备信息": "Device information",
		"从左侧选择一项，查看资料或开始聊天。": "Choose a contact or group on the left to view details or start a chat.",
		"移除头像": "Remove avatar",
		"已填写的资料对已连接的 GoIM 和内网通联系人可见。": "Your profile is visible to connected GoIM and Neiwangtong contacts.",
	} {
		en[label] = english
	}
}

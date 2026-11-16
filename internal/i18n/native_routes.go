package i18n

func init() {
	for label, english := range map[string]string{
		"刷新头像":   "Refresh avatar",
		"关联兼容连接": "Link compatibility routes",
		"请选择同一台内网通设备。关联后优先使用原生连接，现有聊天记录会保留。": "Choose a route for the same Neiwangtong device. The native route will take priority and existing chats will be kept.",
		"选择同一设备的连接": "Select a route for the same device",
		"确认关联":      "Link routes",
		"移除头像":      "Remove avatar",
		"已填写的资料对已连接的 GoIM 和内网通联系人可见。": "Your profile is visible to connected GoIM and Neiwangtong contacts.",
	} {
		en[label] = english
	}
}

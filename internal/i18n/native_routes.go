package i18n

func init() {
	for label, english := range map[string]string{
		"刷新头像": "Refresh avatar",
		"联系资料": "Contact information",
		"设备信息": "Device information",
		"从左侧选择一项，查看资料或开始聊天。": "Choose a contact or group on the left to view details or start a chat.",
		"移除头像":  "Remove avatar",
		"内网通群聊": "Neiwangtong group",
		"文件夹传输已中断，请让对方重新发送": "Folder transfer was interrupted. Ask the sender to send it again.",
		"添加成员":                        "Add members",
		"请选择在线的内网通联系人":                "Choose online Neiwangtong contacts",
		"新成员会在内网通看到这个群聊。":             "New members will see this group in Neiwangtong.",
		"选择使用同一种客户端的在线联系人。":           "Choose online contacts using the same app.",
		"内网通群至少选 1 人，GoIM 群至少选 2 人。":  "Choose at least one Neiwangtong contact or at least two GoIM contacts.",
		"此联系人不支持群聊":                   "This contact does not support group chat",
		"此群需要同一种客户端":                  "This group needs contacts using the same app",
		"一个群只能选择同一种客户端的联系人":           "A group can only include contacts using the same app",
		"内网通群聊已创建":                    "Neiwangtong group created",
		"内网通只能撤回两分钟内已发送的文字，且对方需要在线":   "You can recall sent Neiwangtong text for two minutes while the recipient is online",
		"内网通群暂不支持在 GoIM 中管理成员":        "Member management for Neiwangtong groups is unavailable in GoIM",
		"内网通群暂只支持发送文字和聊天图片":           "Neiwangtong groups currently support only text and inline images",
		"已填写的资料对已连接的 GoIM 和内网通联系人可见。": "Your profile is visible to connected GoIM and Neiwangtong contacts.",
	} {
		en[label] = english
	}
}

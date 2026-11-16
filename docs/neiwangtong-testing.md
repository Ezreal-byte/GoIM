# 内网通互通验证

测试分为本地回环和原版实机两套。两套都直接判断通信结果，不依赖 Wireshark 或手工读包。

| 模式 | 验证内容 | 结果 |
| --- | --- | --- |
| 回环 | GoIM 后端和使用 GoIM 协议实现的测试对端双向发现、中文文字、文件收发和 SHA-256、拒收状态、头像变更和清除。协议测试另检查 ENet 分片及应用层回执。 | 测试退出码与 `wire.jsonl`、`core.jsonl`。CI 会随普通 Go 测试运行。 |
| 原版 | 对指定的隔离测试地址验证 UDP 发现、控制连接和资料握手，并核对原版报告的版本。可要求核验真实头像，或向指定测试昵称发送一条样例并等待匹配的应用层回执。 | `reference.json` 给出每项结果和失败原因。回执只证明原版协议端已回应，不代表界面显示正确或用户已读。 |

在 Windows PowerShell 7.2 或更新版本中运行本地回归：

```powershell
pwsh -File scripts/test-neiwangtong.ps1
```

原版测试需要运行中的内网通测试实例，默认版本为 `3.4.3055`。指定运行中的 VMware 测试机即可由 VMware Tools 解析来宾 IP；默认用 VMnet8，使用其他测试网卡时给出 `-Adapter` 或 `-Bind`。

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Reference -Vmx 'D:\VMs\Windows Test.vmx' -Vmrun 'D:\VMware\vmrun.exe' -Avatar
```

也可以指定本机 IP 和对端端口：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Reference -Bind 192.168.100.1 -Target 192.168.100.2:9011 -Avatar
```

- `-Avatar` 要求测试档案已设置头像。报告只包含图片尺寸和 SHA-256。
- `-TextReceipt -Peer '原版测试机昵称'` 会发送一条自动生成的测试文字。只有握手、版本和昵称都匹配才发送。ENet 传输回执和应用回执分别检查，缺少应用回执时会判失败。
- 原版测试绑定指定本地地址，只联系指定对端，关闭广播，不启用 IPMSG。若原生端口已被占用，测试会在绑定阶段失败，已有实例继续运行。
- 本地报告默认保存在忽略的 `artifacts/neiwangtong/` 下，不包含原始报文、聊天正文、头像文件或数据库。

原版主动发消息、发送文件和点击接收需要一个能操作其已登录测试桌面的来宾端测试入口。当前参考命令只验证表中列出的协议行为；它不会把回环结果算作原版双向收发通过。

# 飞鸽／飞秋适配进度

更新于 2026-11-18。本轮开始于 `0.1.7` 发布之后，新增回执等待等改动属于后续开发，**不包含在 0.1.7 安装包中**。

兼容目标以[内网通官方说明](https://51nwt.com/help/g2.htm)中的飞鸽／飞秋联系人发现和文字互通为基础，保留并验证标准 IPMSG 文件传输。各客户端的私有群聊、语音、视频、远程控制不属于本轮目标。

| 功能 | 进度 | 验证范围 |
| --- | --- | --- |
| 自动发现、协议选择 | 原版 IP Messenger 通过 | 无需手输地址，真实 GoIM 后端发现原版；原版命令行也列出 GoIM。GoIM 对端仍走自身协议，内网通仍走原生协议。 |
| 双向中文文字 | 原版 IP Messenger 通过 | 原版 `5.9.1` 的命令行返回 GoIM 发出的完整中文正文；原版回复进入同一个 GoIM 会话。 |
| GBK、GB18030、UTF-8 和飞秋扩展头 | 协议回归通过 | 保留完整报文号，兼容扩展版本头；普通正文保留前后空白和末尾换行。具体飞秋版本仍需原版验收。 |
| 文字送达回执 | 原版回执、丢包及重启回归通过 | 收到对应设备、端口和报文号的 `RECVMSG` 才标为已发送。重发沿用原号和相同内容；其他设备、端口或报文号不能误完成。回执不是已读。 |
| 关闭客户端时的待发送消息 | 回归通过 | 回执到来前关闭，消息仍在持久发件箱；重启后用原报文号补投并等待确认。 |
| 昵称与登录标识 | 回归通过 | 登录标识来自 GoIM 持久身份，修改昵称和重启不会改变报头身份；昵称仍通过资料广播更新。 |
| 普通文件双向收发 | 原版 IP Messenger 通过 | 两个方向均使用真实 GoIM 后端和原版 `5.9.1`，各保存 98,321 字节、中文文件名并核对 SHA-256。GoIM 接收需要明确接受附件。 |
| 文件断点续传 | 标准 TCP 协议回归通过 | 测试对端验证非零偏移及完整内容；原版中断重试专项尚未完成。 |
| 状态变化 | 原版 IP Messenger 通过 | 原版切换离开、恢复在线并退出时，GoIM 同一联系人记录随之更新。 |
| 离线文字补投 | GoIM→原版 IP Messenger 通过 | 关闭接收端后发送中文，GoIM 重启仍保留待发送消息。原版重开后自动发现、收到对应回执，同一会话内保留一条已发送消息。 |
| 飞秋 2013（3.0.0.2） | 待 Windows 原版验收 | 原版在 Wine 8.0 和 11.0 的新档案中均停在 `CryptDecrypt test512 error = 80090005`，未处理发现流量。后续在 Windows 上验证广播发现、双向中文和普通文件。 |
| 其他中文飞鸽版本 | 待原版验收 | 当前真实客户端报告覆盖 IP Messenger 5.9.1。 |

## 自动验收

普通回归：

```powershell
pwsh -File scripts/test-ipmsg.ps1
```

原版沙箱复用[内网通测试文档](neiwangtong-testing.md)中的专用 WSL 发行版及 Wine／Xvfb 环境，使用独立的临时网络命名空间、Wine 档案和 GoIM 数据目录。两个参与者只有一条私有虚拟链路，没有连接宿主机或外网的路由，不操作 Windows 用户桌面。

当前参考程序来自 [IP Messenger 官网](https://ipmsg.org/)的 `5.9.1` 安装程序，安装包的 Authenticode 签名为 FastCopy Lab, LLC。使用安装程序的 `/SILENT /EXTRACT32 /NOSUBDIR /DIR=...` 在上述隔离环境中提取到 `/opt/goim-lab/ipmsg5.9.1`；目录中需要 `IPMsg.exe`、`ipcmd.exe` 及随附依赖。不要在沙箱中启用 Windows 驱动器挂载或 Windows interop。

```powershell
pwsh -File scripts/test-ipmsg.ps1 -Mode Sandbox
```

非默认程序位置可以传 `-SandboxOriginalDir`。每次使用全新档案，报告保存在忽略的 `artifacts/ipmsg/sandbox.json`。测试通过原版 `ipcmd` 收发和读取正文、切换状态以及关闭原版，随后用同一个临时档案重开。临时原版的自动保存目录用于核对 GoIM 发出的文件。GoIM 侧仍需明确接受文件。

协议依据为原作者的 [IPMSG 通信规范](https://github.com/shirouzu/ipmsg/blob/master/protocol.txt)和[命令行帮助](https://ipmsg.org/help/ipmsghlp_eng.htm)。接下来的原版验收集中在飞秋的 Windows 版本和普通文件中断续传。

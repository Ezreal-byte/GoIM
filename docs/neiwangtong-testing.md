# 内网通互通验证

测试直接判断通信结果，不依赖抓包或手工判读。日常 CI 跑回环；需要验证原版的行为时，运行隔离沙箱或检查现有参考实例。

功能覆盖、未验收项和后续顺序见[内网通适配进度](neiwangtong-compatibility-progress.md)。

| 模式 | 验证内容 | 结果 |
| --- | --- | --- |
| 回环 | GoIM 后端与原生协议测试对端验证发现、中文文字、双向文件和目录、拒收与撤回、头像变更、普通文件中断和重试、群图片，以及 GoIM 重启后补发离线文字和文件。 | 测试退出码与 `wire.jsonl`、`core.jsonl`。CI 会随普通 Go 测试运行。 |
| 原版沙箱 | 自动启动原版 3.4.3055，验证握手、双向中文文字及应用回执、文件收发和取消、单聊及原版群里的双向 PNG／JPEG／GIF 图片、原版群成员和双向群文字、含空子目录及 0 字节文件的中文目录树、原版发送时从指定偏移续传。 | `sandbox.json` 给出逐项结果。发给原版的图片从其缓存核对字节，群文字从原版会话核对正文，目录逐文件核对 SHA-256。不需要登录来宾桌面。 |
| 原版离线补投给 GoIM | 断开 GoIM 后，用原版排入文字和文件。用同一身份重连，核对文字和原发送时间、文件 SHA-256，以及原版对 GoIM 带原发送时间消息的应用回执。 | 独立的 `sandbox.json`。 |
| GoIM 离线补投给原版 | GoIM 自动发现原版。关闭原版，确认仍在运行的 GoIM 标记同一联系人离线；排队发送后重启 GoIM。原版重开时自动恢复在线，核对补投的回执、原版会话的文字和文件的 SHA-256。 | 独立的 `sandbox.json`；整个过程没有手动连接命令。 |
| 三方内网通群 | 在同一隔离网段启动两台原版和真实 GoIM 后端。核对建群、添加成员、群消息和改名后回复；让一台原版实际退群，再核对 GoIM 的名单。 | `sandbox.json` 单独记录创建、添加、群消息、改名和原版主动退出。 |
| 单聊和群聊文字撤回 | 让原版和 GoIM 分别发送并撤回一条文字。检查被撤回的正文消失，且相邻的对照消息仍在。 | 单聊和群聊分别有一份 `sandbox.json`。 |
| 解散与移出群成员 | 让 GoIM 操作，原版检查对应群窗口关闭；另用全新的原版群验证原版发出的群主操作与群 ID。Go 侧回归检查成员权限和历史记录。 | 三方群、群撤回和两个原版群主操作的报告分别保存。 |
| 群内普通文件 | GoIM 与原版各从群内发送文件，由对方从同一会话接受。核验原版保存目录和 GoIM 下载文件的长度与 SHA-256。 | `group-file/sandbox.json`，也可单独运行 `-GroupFile`。 |
| 群内文件夹 | 双向发送含多层中文路径、空子目录和零字节文件的目录，核对结构、长度和 SHA-256。 | `group-folder/sandbox.json`，也可单独运行 `-GroupFolder`。 |
| 内置表情 | 原版选择并发送“微笑”，GoIM 核对含义；GoIM 发送正文夹带微笑，原版回执并核对其记录中保留前后正文和内联对象。 | `emoticons/sandbox.json`，也可单独运行 `-Emoticons`。 |
| 原版接收端续传专项 | 让原版真实落盘第一块，再断开并重启 GoIM 服务。复发同一任务，验证原版请求的非零偏移及最终 SHA-256。 | 独立的 `sandbox.json`。 |
| 原版参考实例 | 对指定的隔离测试地址验证 UDP 发现、控制连接和资料握手，并核对原版报告的版本。可要求核验真实头像，或向指定测试昵称发送一条样例并等待匹配的应用层回执。 | `reference.json` 给出逐项结果。回执证明原版协议端已回应，不代表用户已读。 |

在 Windows PowerShell 7.2 或更新版本中运行本地回归：

```powershell
pwsh -File scripts/test-neiwangtong.ps1
```

## 原版沙箱

用一个命令串行复验常规互通（含原版忙碌、离开和在线状态）、两个方向的离线补投、三方群聊、普通文件断点续传、头像更新、单聊及群聊文字撤回、群主操作、群文件、群文件夹、内置表情，以及两个方向的目录重传。每个场景都从全新档案启动，分别输出 `sandbox.json`：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -SandboxSuite
```

只跑普通互通场景：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox
```

离线投递使用独立的原版档案和连接，避免其他聊天和传输场景影响重连：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -OfflineReplay
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -OfflineSend
```

单独验收 GoIM 创建的三方群：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -MultiGroup
```

单独验收单聊和群聊的双向文字撤回：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -TextRecall
```

验收三方群里的退群、移出成员和群主解散：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -GroupManagement
```

验收群内普通文件：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -GroupFile
```

验收群内文件夹，包括多层中文路径、空子目录、零字节文件和内容 SHA-256：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -GroupFolder
```

单独验收原版作为收件人时的断点续传：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -ResumeReceive
```

大文件专项以 4,295,032,849 字节为样本。两个方向分别启动原版并使用全新档案，检查完整文件和 SHA-256。每个方向最多运行 18 分钟；GoIM 发送时若 1 分钟没有新进度会提前失败。建议为 WSL 数据盘预留至少 12 GiB 可用空间：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -LargeFile
```

原版发给 GoIM 的大文件已通过。反向失败时，报告会记录连接错误、原版落盘长度和原版回报的已收长度。反向未通过时，专项整体返回失败。

用原版对原版作 Wine 环境对照时，小文件和同样大小的边界文件各用两份新的原版档案。对照会先检查聊天能互通，再检查实际写出的文件和 SHA-256；如果小文件没有通过，同一轮的大文件失败不能用来归因：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -StockLargeFile
```

单独验收头像时，GoIM 发出两张不同的测试 PNG，在原版缓存中核对完整字节。原版也会实际更换两张照片，再恢复默认；GoIM 根据图像内容和更新顺序核对头像，确认恢复默认时清除旧图：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -AvatarUpdate
```

目录续传专项也分别启动两个原版档案。它在真实接收首块后断开连接，分别检查原版能否重新打开同一份邀请，以及原版接收时是否请求保留的偏移；从头重传即使哈希正确也不会计为续传通过：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -DirectoryRetry
```

实际恢复路径使用新邀请从头重传。专项分别中断两个方向，再让原版和 GoIM 使用新邀请重发，核对目录内容的 SHA-256：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -DirectoryResend
```

每个参与者都使用独立的 Linux 网络命名空间。双端用一对虚拟网卡，三方群聊用命名空间内的虚拟交换网段；都没有通往宿主机或外网的路由。每个原版在独立的 Wine 档案和 Xvfb 私有显示中运行；测试夹具只操作本次启动的原版窗口。脚本结束后清理进程、虚拟网卡和临时文件。原版程序与测试数据无需进 Git。

中途取消用例在第一块文件数据发出后暂停发送，等待原版取消，再验证 GoIM 的状态和原版落盘结果。原版可能保留部分文件，报告会记录保留了多少字节。

在新机器上只需准备一次：使用 x64 WSL 2 导入一个专用的 Debian 12 发行版，命名为 `GoIM-Neiwangtong-Lab`。在该发行版以 root 安装依赖和创建普通用户：

```sh
dpkg --add-architecture i386
apt-get update
apt-get install wine wine32:i386 wine64 xvfb xauth openbox fonts-noto-cjk iproute2 gcc-mingw-w64-i686-posix
useradd --create-home nwtlab
```

在这个专用发行版的 `/etc/wsl.conf` 关闭 Windows 驱动器挂载及互操作，然后执行 `wsl --terminate GoIM-Neiwangtong-Lab` 使设置生效：

```ini
[automount]
enabled=false

[interop]
enabled=false
appendWindowsPath=false

[user]
default=nwtlab
```

将原版 3.4.3055 的完整程序目录复制到发行版的 `/opt/goim-lab/original`，确保 `nwtlab` 可以读取；只需程序文件，无需复制 Windows 用户档案。可以通过 `\\wsl.localhost\GoIM-Neiwangtong-Lab` 访问发行版文件。目录不同时，用 `-SandboxOriginalDir` 传入 Linux 路径。夹具使用该版本在 96 dpi 的默认布局；版本或布局不符会让检查失败。沙箱使用空白头像，原版头像验证使用下方参考实例模式。

## 现有原版参考实例

指定运行中的 VMware 测试机即可由 VMware Tools 解析来宾 IP；默认用 VMnet8，使用其他测试网卡时给出 `-Adapter` 或 `-Bind`。

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

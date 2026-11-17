# 内网通互通验证

测试直接判断通信结果，不依赖抓包或手工判读。日常 CI 跑回环；需要验证原版的行为时，运行隔离沙箱或检查现有参考实例。

功能覆盖、未验收项和后续顺序见[内网通适配进度](neiwangtong-compatibility-progress.md)。

| 模式 | 验证内容 | 结果 |
| --- | --- | --- |
| 回环 | GoIM 后端与原生协议测试对端验证发现、中文文字、双向文件和目录、拒收与撤回、头像变更、断点续传，以及 GoIM 重启后补发离线文字和文件。 | 测试退出码与 `wire.jsonl`、`core.jsonl`。CI 会随普通 Go 测试运行。 |
| 原版沙箱 | 自动启动原版 3.4.3055，验证握手、双向中文文字及应用回执、文件收发和取消、双向 PNG／JPEG／GIF 聊天图片、含空子目录和 0 字节文件的中文目录树、原版发送时从指定偏移续传、原版群成员和双向群文字。 | `sandbox.json` 给出逐项结果。反向图片从原版缓存核对字节，反向群文字从原版会话核对正文，目录逐文件核对 SHA-256。不需要登录来宾桌面。 |
| 原版离线补投给 GoIM | 断开 GoIM 后，用原版排入文字和文件。用同一身份重连，核对文字和原发送时间、文件 SHA-256，以及原版对 GoIM 带原发送时间消息的应用回执。 | 独立的 `sandbox.json`。 |
| GoIM 离线补投给原版 | 关闭原版，在 GoIM 后端排队发送并重启 GoIM。原版重新上线后，核对应用回执、会话中的补投文字、落盘文件的 SHA-256。 | 独立的 `sandbox.json`。 |
| 原版接收端续传专项 | 让原版真实落盘第一块，再断开并重启 GoIM 服务。复发同一任务，验证原版请求的非零偏移及最终 SHA-256。 | 独立的 `sandbox.json`。 |
| 原版参考实例 | 对指定的隔离测试地址验证 UDP 发现、控制连接和资料握手，并核对原版报告的版本。可要求核验真实头像，或向指定测试昵称发送一条样例并等待匹配的应用层回执。 | `reference.json` 给出逐项结果。回执证明原版协议端已回应，不代表用户已读。 |

在 Windows PowerShell 7.2 或更新版本中运行本地回归：

```powershell
pwsh -File scripts/test-neiwangtong.ps1
```

## 原版沙箱

用一个命令串行复验常规互通、两个方向的离线补投，以及原版接收端断点续传。四个场景各用一个全新档案，分别输出 `sandbox.json`：

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

单独验收原版作为收件人时的断点续传：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -ResumeReceive
```

大文件专项以 4,295,032,849 字节为样本，单独检查两端收到的完整文件和 SHA-256。每个方向最多运行 4 分钟，建议为 WSL 数据盘预留至少 12 GiB 可用空间：

```powershell
pwsh -File scripts/test-neiwangtong.ps1 -Mode Sandbox -LargeFile
```

专项直接验证原生协议。当前原版发给 GoIM 的大文件已通过，反向未通过，专项整体仍会返回失败。

每次运行都创建独立的 Wine 档案和两个 Linux 网络命名空间。两端只连一对虚拟网卡，没有通往宿主机或外网的路由。原版在 Xvfb 私有显示中运行；测试夹具只向本次启动的原版窗口投递操作。脚本结束后清理进程、虚拟网卡和临时文件。原版程序与测试数据无需进 Git。

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

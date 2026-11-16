<!-- The gallery uses GoIM's native headless renderer with synthetic demo data. -->
<div align="center">
  <p><a href="README.md">English</a> · 简体中文</p>
  <img src="website/assets/goim.png" width="84" height="84" alt="GoIM 标志" />
  <h1>GoIM</h1>
  <p><strong>局域网里的即时通讯与文件共享</strong></p>
  <p>
    <a href="https://ezreal-byte.github.io/GoIM/">官网</a> ·
    <a href="https://github.com/Ezreal-byte/GoIM/releases/latest">下载</a> ·
    <a href="#快速开始">快速开始</a> ·
    <a href="#旧版客户端兼容">兼容进度</a>
  </p>
  <a href="website/assets/screenshots/chat-light.png">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="website/assets/screenshots/chat-dark.png" />
      <img src="website/assets/screenshots/chat-light.png" alt="GoIM 群聊、图片消息与聊天文件" width="100%" />
    </picture>
  </a>
  <p><sub>GoIM 原生界面，由无头脚本使用演示数据渲染。</sub></p>
</div>

GoIM 是基于 Go 和 [Viva](https://github.com/vivaui/viva) 的桌面即时通讯工具。同一局域网内的设备可以直接发现彼此、聊天和传文件，聊天记录保存在本机。浏览器也可以打开分享链接，无需安装客户端。

<table>
  <tr>
    <td width="50%"><a href="website/assets/screenshots/contacts-light.png"><img src="website/assets/screenshots/contacts-light.png" alt="联系人分组与在线状态" /></a><strong>联系人与分组</strong></td>
    <td width="50%"><a href="website/assets/screenshots/transfers-dark.png"><img src="website/assets/screenshots/transfers-dark.png" alt="暗色外观下的待接收与传输队列" /></a><strong>文件传输</strong></td>
  </tr>
  <tr>
    <td width="50%"><a href="website/assets/screenshots/shares-light.png"><img src="website/assets/screenshots/shares-light.png" alt="文件分享列表及密码保护状态" /></a><strong>文件共享</strong></td>
    <td width="50%"><a href="website/assets/screenshots/chat-dark.png"><img src="website/assets/screenshots/chat-dark.png" alt="暗色外观下的群聊和聊天文件" /></a><strong>暗色外观</strong></td>
  </tr>
</table>

## 功能

| 聊天 | 传输与分享 |
| --- | --- |
| 单聊、群聊、邀请和成员管理 | 普通附件由接收方确认，支持另存、取消、重试和续传 |
| 文字、表情和聊天图片，图片可在独立窗口缩放、旋转和保存 | GoIM 设备之间使用 TLS，并校验文件的 SHA-256 |
| 本地记录、会话搜索、未读、置顶与离线补发 | 文件和文件夹可生成链接及二维码，按项目设置访问密码 |
| 联系人分组、个人资料、应用锁、两种主题与中英界面 | 设备分享主页可展示公开分享项目，并接收浏览器上传 |

截图支持自由框选、移动和缩放选区、标注与原分辨率复制。默认快捷键为 `Alt+A`，也可使用聊天输入框里的剪刀图标。

## 安装

从 [GitHub Releases](https://github.com/Ezreal-byte/GoIM/releases/latest) 选择系统和架构。v0.1.1 提供以下 14 个安装包和压缩包。

| 系统 | 架构 | 下载格式 |
| --- | --- | --- |
| Windows | x64、ARM64 | `Setup.exe`、`.msi`、`.zip` |
| macOS 13 或更新版本 | Intel、Apple 芯片 | `.dmg`、`.zip` |
| Linux（X11 或 XWayland） | x64、ARM64 | `.deb`、`.tar.gz` |

Windows 的 Setup 和 MSI 使用同一套安装内容，任选一种。压缩包需完整解压，保留 GoIM 与截图工具的相对位置。Windows 包未使用商业证书签名，macOS 应用使用 ad-hoc 签名且未经 Apple 公证。可用发布附带的 `SHA256SUMS` 校验下载。

## 快速开始

1. 两台设备连入同一个可互通的局域网并运行 GoIM。
2. 在「联系人」选中发现的设备。自动发现受限时，使用「手动连接」，输入对方 IP 或 `IP:47842`。
3. 发送文字或图片。普通文件可在聊天或「传输」中接收。
4. 要让手机或其他电脑用浏览器获取文件，在「文件共享」里添加文件或文件夹，然后复制项目链接或二维码。设备分享主页启用后也可接收上传。

原生通信默认使用 TCP 47842，自动发现使用 IPv4 多播 `239.192.77.77:47841`。网页分享通过局域网 HTTP 提供，适用于可信网络。

## 旧版客户端兼容

| 客户端 | 已验证的能力 | 边界 |
| --- | --- | --- |
| 内网通 3.4.3055 | Windows 与 VMware 客户端之间初步验证了发现、双向文字和普通单文件传输。修复版通过了 TCP 拉取报文和 GoIM 聊天文件卡片回归。 | 图片可按普通附件发送。内网通的内嵌图片、截图、表情、原生文件共享及群聊未实现互通。 |
| 飞秋 | 自动化测试覆盖扩展报头、中文编码、发现和标准 IPMSG 文件路径。 | 真实飞秋客户端及其不同版本仍待验证，私有扩展未纳入兼容范围。 |

GoIM 之间使用原生 TLS。新配置默认启用「内网通互通」（UDP 9011 发现、ENet 9012 控制和 TCP 9013 头像及文件传输）与「飞鸽／飞秋互通」（端口 `2425`）。可在「设置 → 网络」分别管理两套互通，也可用 `--no-compatibility` 启动仅使用 GoIM 协议的实例。

无需抓包的回环回归和原版实机验证，见[内网通互通验证](docs/neiwangtong-testing.md)。

## 从源码构建

使用 CI 固定的 Go 工具链（当前为 1.27.3）。Go 会按 [`go.mod`](go.mod) 下载固定版本的 Viva。如果需要同时修改 Viva，可在本地创建包含两个仓库的 Go 工作区。

```sh
go test ./... -count=1
go vet ./...
go run ./cmd/goim
```

Windows 上，运行 `pwsh -File scripts/start-temp-dev.ps1` 可构建并启动使用独立配置和下载目录的开发客户端。加上 `-Loopback` 可关闭局域网发现和旧协议互通；运行 `scripts/start-dual.ps1` 可启动通过本机回环连接的两个客户端。

`pwsh -File scripts/smoke-headless.ps1` 会重新构建无头客户端，使用三个独立进程验证消息、文件传输和网页分享。`scripts/build.ps1` 只构建桌面程序和截图工具，`scripts/promo-headless.ps1` 可重新生成演示截图。发布构建及各平台校验见 [Actions](https://github.com/Ezreal-byte/GoIM/actions/workflows/build.yml)。

## 许可证

[MIT](LICENSE) · [第三方许可](THIRD_PARTY_NOTICES.md)

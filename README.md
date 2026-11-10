# GoIM

Go + Viva 原生局域网即时通讯。无中心服务器；设备通过 IPv4 多播发现，通过 TCP/TLS 直连。[下载 GoIM 0.1.0](https://github.com/Ezreal-byte/GoIM/releases/tag/v0.1.0)，支持 Windows、Linux 和 macOS 的 x86-64 与 ARM64。具体构建号见应用「关于」和包内 `build-info.json`。

支持单聊、确认入群、成员管理与群主转让、文字/图片/表情/文件、离线补发、本地记录、文件确认/取消/重试/续传/SHA256、文件与文件夹的只读分享、分组搜索、会话搜索、两种外观及设置、托盘和通知。

在「文件共享 → 新增分享」中选择文件或文件夹，GoIM 会打开系统选择器。可在创建时添加访问密码；列表行上可直接复制链接，也可从「…」菜单查看二维码、管理密码或取消分享。收件人用同一局域网的电脑或手机浏览器打开链接，可以逐级进入文件夹并下载文件，无需安装 GoIM。文件夹链接只包含创建分享时已有的文件，不包含符号链接或后续新增的文件。下载期间请保持分享人的 GoIM 运行；网页分享通过局域网 HTTP 提供，请只在可信网络使用。

在「文件共享 → 设备分享主页」菜单可启用主页、复制链接或查看二维码。主页列出该设备已发布的分享，访客也可以上传文件到「下载目录/网页接收」。上传文件不会自动发布，同名文件会另存，单文件最多 256 MB。每个项目的密码独立生效。关闭主页会停用主页及上传入口，已有项目直达链接仍可使用。

在「传输」页顶部处理待接收文件，可以接收、另存为，或从「…」菜单拒绝。下面的「传输中」展示实时进度，「已结束」可打开收到的文件或重试失败任务。右上角的「接收目录」可以在系统文件管理器中找到默认保存位置。

点击外观上方的锁图标可锁定应用，密码在「设置 → 安全」中更改。锁定时隐藏会话和通知内容，接收继续在后台运行。应用锁保护界面访问；数据目录仍应使用系统账户权限保护。自定义聊天背景和深浅位于「设置 → 常规」。

GoIM 之间发送 PNG、JPEG 或 GIF 时，符合尺寸限制的图片直接显示，其他文件等待接收确认。点击图片会打开独立窗口，可以缩放、旋转、切换前后图片或保存原图。设置 → 常规 → 界面语言可即时切换简体中文和英语。聊天框左下角的剪刀或默认 `Alt+A` 可打开截图。悬停可识别窗口，单击选择；也可拖动框选，或按 `Ctrl+A`（macOS：`⌘A`）选整屏。拖动边线或角点调整尺寸，拖动选区内部可移动。标注后点完成图标，按原像素复制到剪贴板，再粘贴到聊天。全局快捷键可在「设置 → 快捷键」更改。请保留包内的 `goim-snip`，macOS 需授权屏幕录制，Linux X11 复制图片需 `xclip`。

基础 IPMSG 兼容可发现旧版联系人并进行单聊文字和文件传输，使用 UDP/TCP 2425；具体客户端和版本须分别验证。旧版链路不使用 GoIM 的 TLS，可信内网以外请用 `--no-ipmsg` 关闭。原生 GoIM 的图片自动接收规则不适用于未经验证的 IPMSG 附件。

## 启动

Windows 解压双击 GoIM.exe；开发目录运行 scripts/start.ps1。默认监听 TCP 47842，发现使用 UDP 47841 / 239.192.77.77。首次 Windows 网络访问提示由用户处理；不要放开公共网络。两个设备须在可互通的同一局域网，客户端隔离和多播禁用时可使用界面里的手动 IP:端口连接。

在 Windows 开发目录可以双击 `Local-Pair.cmd` 体验两端聊天。它启动两个使用独立数据目录的客户端，通过回环地址通信，昵称为 Loopback A/B；聊天、图片和文件均经过实际传输。

Linux 解压运行 ./GoIM，需要 X11（Wayland 使用 XWayland）。macOS 解压 GoIM.app，需要 macOS 13 或更新版本；首版仅 ad-hoc 签名，没有 Apple 公证，下载来源检查可能要求用户在系统设置中允许打开。Windows 无商业证书签名。

Linux 中文界面需要系统 CJK 字体。Ubuntu/Debian 可安装 `fonts-noto-cjk`，以及 `libx11-6 libx11-xcb1 libxcb1 libxkbcommon0 libxkbcommon-x11-0 libvulkan1 mesa-vulkan-drivers`。

Linux 包含应用菜单图标。需要加入应用菜单时，将 `GoIM` 和 `goim-snip` 一起放入 `PATH` 上的同一目录，并将包内 `share/` 的内容复制到 `~/.local/share/`。

默认数据在系统用户配置目录的 GoIM，文件在 Downloads/GoIM，可在设置中更改。设备身份私钥与本地聊天数据库保存在数据目录；备份时一起保存。

```powershell
./GoIM.exe --data-dir .runtime/A --download-dir .runtime/A-downloads --listen 127.0.0.1:47901 --no-discovery --no-ipmsg --name 测试端A
./GoIM.exe --data-dir .runtime/B --listen 127.0.0.1:47902 --no-discovery --no-ipmsg --name 测试端B --connect 127.0.0.1:47901
```

## 开发与验证

固定 Go 1.27.3。Viva 上游基准和可审查补丁见 framework/；将 Viva 放在 ../viva，检出基准并应用补丁后：

```text
go mod download
go test ./... -count=1
go vet ./...
go build -tags vivahead -o build/goim-headless.exe ./cmd/goim
```

Windows 三独立进程端到端：scripts/smoke-headless.ps1。截图回归：设置 GOIM_QA_SCREENSHOTS 为输出目录后运行 go test ./internal/desktop -run TestShellScreenshotQA -count=1。CI 的 Linux 核心另外执行 race。

使用 `pwsh -NoProfile -File scripts/promo-headless.ps1` 可生成四个关键页面的明亮和暗黑截图，保存到 `artifacts/promo/screenshots/`。截图使用虚构数据和真实界面渲染，不需要启动桌面客户端。

黑白 Logo 母图和各尺寸位于 `internal/brand/assets/`。更新母图后，从仓库根目录运行 `go run ./cmd/assets` 生成 PNG、Windows ICO 和 macOS ICNS。

## 首版边界

消息送达确认不表示已读。群主离线时现有成员继续聊天，群管理待群主上线处理。文件由原发送方提供，离线时等待；源文件变化须重新发送。分享只公开手动选定的文件和文件夹内容。下载目录须支持硬链接，用于完整校验后原子安装文件并避免覆盖；不支持的文件系统会报告错误，可更换目录。全局显示／隐藏快捷键支持 Windows、macOS 和 Linux X11；原生 Wayland 可使用应用内快捷键。

TLS 设备身份校验和群签名可防止已有设备身份被替换；首版局域网设备首次见面自动建联，尚未提供人工指纹核对或组织身份认证。仅用于可信局域网。

实际验证结果、未验证的平台行为和上游贡献记录在 docs/implementation。Linux/macOS 桌面及跨机器网络验证不能由 CI 构建代替。

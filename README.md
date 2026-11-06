# GoIM

Go + Viva 原生局域网即时通讯。无中心服务器；设备通过 IPv4 多播发现，通过 TCP/TLS 直连。首版为 0.1.0-alpha.1。

支持单聊、确认入群、成员管理与群主转让、文字/图片/表情/文件、离线补发、本地记录、文件确认/取消/重试/续传/SHA256、只读选定文件分享、分组搜索、会话搜索、两种外观及设置、托盘和通知。

## 启动

Windows 解压双击 GoIM.exe；开发目录运行 scripts/start.ps1。默认监听 TCP 47842，发现使用 UDP 47841 / 239.192.77.77。首次 Windows 网络访问提示由用户处理；不要放开公共网络。两个设备须在可互通的同一局域网，客户端隔离和多播禁用时可使用界面里的手动 IP:端口连接。

Linux 解压运行 ./GoIM，需要 X11（Wayland 使用 XWayland）。macOS 解压 GoIM.app，需要 macOS 13 或更新版本；首版仅 ad-hoc 签名，没有 Apple 公证，下载来源检查可能要求用户在系统设置中允许打开。Windows 无商业证书签名。

默认数据在系统用户配置目录的 GoIM，文件在 Downloads/GoIM，可在设置中更改。设备身份私钥与本地聊天数据库保存在数据目录；备份时一起保存。

```powershell
./GoIM.exe --data-dir .runtime/A --download-dir .runtime/A-downloads --listen 127.0.0.1:47901 --no-discovery --name 测试端A
./GoIM.exe --data-dir .runtime/B --listen 127.0.0.1:47902 --no-discovery --name 测试端B --connect 127.0.0.1:47901
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

## 首版边界

消息送达确认不表示已读。群主离线时现有成员继续聊天，群管理待群主上线处理。文件由原发送方提供，离线时等待；源文件变化须重新发送。分享只公开手动选定文件。下载目录须支持硬链接，用于完整校验后原子安装文件并避免覆盖；不支持的文件系统会报告错误，可更换目录。全局显示/隐藏快捷键当前支持 Windows，其他系统可使用应用内快捷键。

TLS 设备身份校验和群签名可防止已有设备身份被替换；首版局域网设备首次见面自动建联，尚未提供人工指纹核对或组织身份认证。仅用于可信局域网。

实际验证结果、未验证的平台行为和上游贡献记录在 docs/implementation。Linux/macOS 桌面及跨机器网络验证不能由 CI 构建代替。

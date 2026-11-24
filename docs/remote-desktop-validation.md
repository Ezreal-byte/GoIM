# 远程桌面验证

远程桌面验证版从私聊工具栏、语音通话按钮右侧发起。两端需要能通过 GoIM 原生 TLS 直连。被控端可在右下角浮窗拒绝或允许控制。允许后共享屏幕和键鼠控制，系统声音保持关闭。共享期间可通过右下角的状态卡或托盘菜单停止共享。控制窗口的顶栏提供快捷键、全屏和结束协助。协助结束后需要重新邀请。

GoIM 和 RustDesk 助手分别构建。RustDesk fork 的验证流水线生成指定系统、架构和提交的助手、清单、许可证及对应源码。GoIM 构建固定清单的 SHA-256，打包时再核对助手和源码归档的 SHA-256。版本以 `-rd-preview.N` 标记的构建用于内部验证。助手通过 GoIM 加密直连传输，协助时才启动。正式分发需要单独确认组合发行的许可证条件。

## 本机验证

1. 在同平台和架构上构建 RustDesk fork 的 `goim-rd`。Windows 文件名是 `goim-rd.exe`。Linux 验证使用 `goim-remote,linux-pkg-config`。macOS 的验证构建启用 `goim-remote,screencapturekit`，被控端需要屏幕录制权限，控制模式还需辅助功能权限。
2. 开发时可将 `GOIM_REMOTE_DESKTOP_HELPER` 设为该助手的绝对路径，再启动本地 GoIM。两端都使用独立的数据目录进行测试。
3. 使用 fork 的 `res/goim-stage-helper.py` 生成验证包输入目录。确认清单摘要后，在同平台的 GoIM 仓库执行：

```sh
go run ./cmd/package --version 0.2.5-rd-preview.3 \
  --remote-desktop-bundle /absolute/path/to/goim-rd-bundle \
  --remote-desktop-manifest-sha256 <manifest-sha256>
```

Linux 安装包要求助手兼容 GLIBC 2.31。内部验证包的 amd64 助手在 Ubuntu 20.04 构建，静态链接 VPX、AV1、YUV 和 Opus，仍使用系统的 PulseAudio、GStreamer 等运行库。麒麟和 UOS 应核对实际系统的运行库。Wayland 还需要桌面的 ScreenCast 和 RemoteDesktop Portal。

在安装了 Xvfb、`xev` 和 `xdotool` 的测试环境中，用真实助手测试端到端的授权、画面和键鼠：

```sh
env -u WAYLAND_DISPLAY XDG_SESSION_TYPE=x11 RUSTDESK_FORCED_DISPLAY_SERVER=x11 \
  GOIM_REMOTE_DESKTOP_HELPER=/absolute/path/to/goim-rd GOIM_TEST_REMOTE_DESKTOP_X11=1 \
  dbus-run-session -- xvfb-run -a -s '-screen 0 1280x720x24 -noreset' \
  go test ./internal/core -run '^TestRemoteDesktopNativeX11$' -v -count=1 -timeout=60s
```

验证完成应记录实际系统、架构和桌面版本，以及画面、键鼠、系统声音、权限撤销、进程退出、压缩包增量和 30 分钟会话的内存变化。Xvfb 验证覆盖 X11 基础链路，Linux 系统声音可用隔离的 PulseAudio 虚拟输出验证。实体桌面上的输入法、系统权限和音频设备需分别实测。

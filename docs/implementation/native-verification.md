# Windows 原生验收记录

2026-11-07，Windows amd64，Go 1.27.3，Viva fee2ecb + global shortcut 补丁。

首次默认地址启动成功创建原生窗口、托盘、可访问控件；Windows 防火墙弹出了网络许可提示。未操作系统安全提示或修改防火墙。随后结束本次自有测试进程，以仅监听 loopback、关闭自动发现、独立数据目录的实例继续本机交互验收。跨机器自动发现仍需要用户在首次启动时处理系统网络许可。

三个独立进程的真实 TCP/TLS 文字、文件确认接收与 SHA256、建群邀请确认、群消息和设置布局已通过 scripts/smoke-headless.ps1，使用真正业务和 painter，非演示数据。

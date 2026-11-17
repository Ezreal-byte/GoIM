# GoIM 在线更新发布

正式版本从 GitHub Release 的稳定入口读取 `goim-update.json` 和 `goim-update.json.sig`。Viva 校验 Ed25519 签名、应用身份和渠道，下载后再校验软件包的长度和 SHA-256。Windows 当前用户 Viva Setup 安装版和可替换的 macOS 应用可原位安装；MSI、DEB、ZIP 和 Linux tar.gz 只提供官方下载。

1. 使用当前锁定版本的 Viva CLI `viva update-keygen -private <私钥文件> -public <公钥文件>` 创建 GoIM 专用密钥。私钥文件放在仓库之外。将 CLI 输出的 **Base64 公钥**设为仓库 Actions 变量 `GOIM_UPDATE_PUBLIC_KEY`；有权限的维护者将对应私钥设为 `goim-release` 环境的 secret `GOIM_UPDATE_PRIVATE_KEY`。保护该环境，只允许正式版本 tag 使用，并限制发布权限。
2. 推送新的 `v主.次.补丁` tag。确认发布构建通过六个平台的打包和校验，且 `GoIM-signed-update-feed` 产物生成。日常分支不会调用签名步骤；没有生产公钥时照常构建，但客户端不做在线检查。
3. 将 14 个包、`SHA256SUMS` 和两份更新文件一并上传到该 tag 的 GitHub Release 草稿。核对附件与 CI 产物后再将草稿设为 Latest。不要把签名清单上传到不同版本或预发布版本。
4. 首个包含公钥和更新器的版本需手动安装。用该版本对下一次稳定发布验证在线升级，覆盖 Windows Setup 和 macOS 应用，确认重启后显示新版本、同一数据目录可读且截图工具仍存在。旧版 MSI、DEB 与便携版应提示官网下载。

没有签名、签名无法验证、清单地址不符合发布约定或软件包校验失败时，GoIM 不启动安装。失败可在「关于」重新检查或使用官网发布页；下载中断可继续。

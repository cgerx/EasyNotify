通过 curl 或 MCP，把服务器和 AI Agent 的任务结果发送到 Mac。

- macOS 13+ 菜单栏客户端，支持 Markdown、消息历史、自动重连、登录时启动和 ⌘W 关闭窗口。
- Linux 服务端支持 AMD64、ARM64、ARMv7、ARMv6；安装包内含 MCP 程序和 systemd 安装脚本。
- 客户端离线时消息保留在内存队列，接收确认后移除。服务端重启会清空待送达消息。

Mac 下载 `EasyNotify-macos-universal.zip`，兼容 Apple Silicon 和 Intel。客户端尚未经过 Apple Developer ID 签名和公证。

Linux 按系统架构选择 `easynotify-server-linux-*.tar.gz`。具体安装和 curl / MCP 用法见 [README](https://github.com/cgerx/EasyNotify#readme)。

`SHA256SUMS` 提供文件校验值。

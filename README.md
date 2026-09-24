# EasyNotify

服务器上的任务跑完了，不用一直盯着终端。用一条 curl，把结果发到 Mac 上。

EasyNotify 由一个自托管中转服务和一个 Mac 菜单栏应用组成。适合构建完成、备份失败、脚本运行结束，或让 AI Agent 在任务完成时提醒你。描述支持 Markdown，也提供 MCP 接口。

服务端是独立的 Go 程序，树莓派和普通 Linux 服务器都能运行，不需要 Node.js、Docker 或数据库。

## 下载

到 [Releases](https://github.com/cgerx/EasyNotify/releases/latest) 下载：

| 文件 | 适用设备 |
| --- | --- |
| `EasyNotify-macos-universal.zip` | macOS 13 及以上，Apple Silicon 和 Intel Mac |
| `easynotify-server-linux-amd64.tar.gz` | 常见 x86_64 云服务器、PC |
| `easynotify-server-linux-arm64.tar.gz` | ARM64 服务器、安装 64 位系统的树莓派 |
| `easynotify-server-linux-armv7.tar.gz` | 安装 32 位 ARMv7 系统的树莓派等设备 |
| `easynotify-server-linux-armv6.tar.gz` | 初代树莓派、Pi Zero 等 ARMv6 设备 |

服务器上运行 `uname -m` 可以查看系统架构。按操作系统架构选择，不只看设备型号。每个服务端包还包含 MCP 程序和 systemd 安装脚本。`SHA256SUMS` 可用于校验下载文件。

## 先把服务跑起来

下面以 Linux AMD64 为例，其他架构换成对应的文件名。

```sh
tar -xzf easynotify-server-linux-amd64.tar.gz
cd easynotify-server-linux-amd64

# 生成接收密钥，终端会显示密钥，稍后填到 Mac 客户端
./easynotify-server key --config ./config.json

# 先在前台运行
./easynotify-server start --config ./config.json --host 0.0.0.0 --port 8787
```

让 Mac 和发送通知的机器能够访问服务器的 TCP 8787 端口。如果服务器在路由器后面，需要使用局域网、VPN 或配置端口转发。

要作为系统服务长期运行，先用 Ctrl+C 停止前台进程，再在解压目录执行：

```sh
sudo sh install.sh
sudo systemctl status easynotify
```

安装后会自动启动，开机启动，异常退出后重启。配置保存在 `/etc/easynotify/config.json`，升级时保留已有密钥。查看日志：

```sh
sudo journalctl -u easynotify -n 50 --no-pager
```

需要换端口时，编辑 `/etc/systemd/system/easynotify.service` 中的 `--port`，然后运行 `sudo systemctl daemon-reload` 和 `sudo systemctl restart easynotify`。

## 在 Mac 上接收

1. 解压客户端，把 `EasyNotify.app` 放入“应用程序”后打开。
2. 打开设置，填写服务器地址，例如 `http://你的服务器地址:8787`，再填入刚才生成的接收密钥。
3. 点击“保存并连接”，右上角显示“已连接”后即可接收消息。首次使用时允许系统通知。

窗口里可以查看、删除消息，点击消息查看 Markdown 详情。关闭窗口或按 ⌘W 后，应用继续在菜单栏接收通知，Dock 图标隐藏；点击菜单栏铃铛可以重新打开。设置中的“登录时启动”开关控制自动启动。

收到的消息保存在本机，退出应用或重启 Mac 后仍在。配置和消息位于 `~/Library/Application Support/EasyNotify/`，以本地 JSON 文件保存。

客户端目前使用临时签名，尚未经过 Apple Developer ID 签名和公证。如果 macOS 拦截启动，请在确认下载来源后，到“系统设置 → 隐私与安全性”中选择“仍要打开”。

## 发一条通知

把示例里的 `localhost:8787` 换成你的服务器地址。发送接口只需要标题和描述：

```sh
curl --fail http://localhost:8787/notify \
  -H 'Content-Type: application/json' \
  --data-binary @- <<'JSON'
{
  "title": "构建完成",
  "description": "**测试通过**\n\n可以开始部署了。"
}
JSON
```

- `title`：1–30 字。
- `description`：1–500 字，支持 Markdown。
- 两个字段都必填，不能全是空白；长度按 Unicode 码点计算，组合 emoji 可能占多个字符。

成功返回 HTTP 200：

```json
{"id":"消息 UUID","accepted":true}
```

这表示服务端已接收。客户端暂时离线时，消息会在服务端内存中等待，客户端收到并保存后才移出队列。队列最多 10,000 条，满时返回 HTTP 503；**服务端进程重启会清空尚未送达的消息**。多台客户端共享同一个队列，任意一台确认即算消费完成。

发送接口无需密钥，任何能访问它的人都可以发送通知；接收连接需要密钥。HTTP 默认明文传输，请按使用范围限制网络访问，需要加密时在前面配置 HTTPS 反向代理，客户端填写 `https://你的域名`。

## 在 iPhone 上接收（Bark）

从 App Store 安装 Bark 并允许通知，将 Bark 的 Device Key 填入服务器的私有 `config.json`。Mac 客户端无需升级，同一条 `/notify` 请求会分别投递到 Mac 和 Bark。

```json
{
  "key": "你的现有接收密钥",
  "bark": {
    "enabled": true,
    "endpoint": "https://api.day.app/push",
    "device_key": "你的 Bark Device Key",
    "group": "EasyNotify"
  }
}
```

`endpoint`、`group` 可省略，默认值如上；自建 Bark 时，将 `endpoint` 改成自己的 `/push` 地址。未配置 Bark 或 `enabled` 为 `false` 时，仅使用原有 Mac 投递。修改后重启服务：`sudo systemctl restart easynotify`。配置文件应仅允许管理员与服务账号读取，真实密钥不要提交到仓库。

标题发送到 Bark 的 `title`，描述发送到 `markdown`，并启用消息归档。Bark 在后台使用 Apple APNs 接收通知，不需要手机常驻应用，也不需要你购买 Apple 开发者会员。

Bark 使用独立的内存队列，Mac 的确认不会取消 Bark 推送。网络错误、HTTP 429/5xx 等暂时故障最多尝试 3 次，每次请求超时 10 秒，重试间隔为 1、2 秒；永久错误或重试耗尽会记录不含密钥的失败日志。两条队列各最多等待 10,000 条，任一队列满时整个请求返回 503，不接受本条消息。Bark 请求不会阻塞 Mac 投递。

`accepted: true` 只表示已进入队列，Bark 返回成功也不代表手机已展示通知。服务重启会丢失内存中的待投递消息；网络超时重试可能重复投递。可通过 `journalctl -u easynotify` 查看 Bark 接收或失败记录。修改接收密钥的 `key` 命令会保留 Bark 配置。

## 给 AI Agent 配置 MCP

MCP 使用 stdio，由 Agent 启动。在运行 Agent 的机器上放好对应平台的 `easynotify-mcp`，配置如下：

```json
{
  "mcpServers": {
    "easynotify": {
      "command": "/absolute/path/to/easynotify-mcp",
      "env": {
        "EASYNOTIFY_URL": "http://localhost:8787"
      }
    }
  }
}
```

将 `command` 和服务器地址改为实际值。Linux 包内附带 MCP；Mac 的 MCP 可以按下文从源码构建。工具名为 `notify`，参数同样只有 `title` 和 `description`。配置好后，可以告诉 Agent：“任务完成或失败时，用 notify 通知我。”

如果 Agent 支持执行命令，也可以直接把上面的 curl 示例写进你的 Skill 或任务说明。

## 从源码构建

目录分为 `server/`、`client/`、`mcp/`。服务端和 MCP 需要 Go 1.25 或以上，具体工具链由 `go.mod` 指定；Mac 客户端需要 macOS、Xcode Command Line Tools 和 Node.js 22 或以上。Node.js 只用于构建和测试，运行客户端不需要它。

```sh
# 服务端和 MCP
bash server/build.sh

# Linux 四种架构
bash server/build.sh --linux

# Mac 客户端
npm ci
npm run build:client

# Mac 通用版
bash client/build.sh --universal

# 完整发布包，输出到 dist/（在 Mac 上执行）
bash scripts/release.sh
```

本地服务默认监听 `127.0.0.1:8787`：

```sh
./server/build/easynotify-server key
./server/build/easynotify-server start
```

测试：

```sh
npm test
npm run test:race
npm run test:native  # 需要 macOS
```

推送 `v*` 标签会触发 GitHub Actions，测试、构建并发布相应版本。

## API 与配置

| 接口 | 用途 |
| --- | --- |
| `POST /notify` | 发送通知，JSON 包含 `title` 和 `description` |
| `GET /ws` | 接收通知，连接时携带 `Authorization: Bearer <接收密钥>` |
| `GET /health` | 健康检查，返回 `{"ok":true}` |

发送失败时，400 表示字段不合法，413 表示请求超过 16 KB，415 表示 Content-Type 错误，503 表示队列已满或服务正在关闭。请求超时后重试可能产生重复消息。

WebSocket 消息包含 `id`、`title`、`description`、`createdAt`。接收端保存成功后发送 `{"type":"ack","id":"消息 UUID"}`，服务端才删除对应消息；未确认消息会重试，自定义客户端需按 ID 去重。

CLI 支持 `--config`、`--host`、`--port`。也可以通过 `EASYNOTIFY_CONFIG`、`HOST`、`PORT` 设置；`EASYNOTIFY_KEY` 可覆盖配置文件里的接收密钥。用 `key --stdin` 可从标准输入设置密钥，修改后需重启服务并更新客户端。

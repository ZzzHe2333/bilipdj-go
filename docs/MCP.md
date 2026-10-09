# BiliPDJ-Go MCP（AI 工具接口）

BiliPDJ-Go 内置 Model Context Protocol (MCP) 工具服务器，可供支持 MCP 的 AI 客户端调用。**MCP 不会直接调用语言模型或对用户弹幕自动作出决定**，只有已连接的 AI 客户端才能按自己的会话触发工具调用。

## 运行方式

首先正常运行 BiliPDJ-Go（默认监听 `127.0.0.1:9816`）。HTTP MCP 端点是 `http://127.0.0.1:9816/mcp`；Web/OBS 和两路直播监听照常工作。

本机 AI 客户端可以直接使用 HTTP MCP（Streamable HTTP），也可以通过 **stdio 桥接**连接已有服务。stdio 模式只是把 JSON-RPC 转发给后台服务，**不会启动第二套排队服务，也不会与已运行的 Go 争抢 state.json**。

例如 Claude Desktop / Cursor / 支持命令式 MCP 的 Agent 可按其配置格式加入如下服务器：

```json
{
  "mcpServers": {
    "bilipdj-go": {
      "command": "C:\\path\\to\\bilipdj-go-mcp.exe",
      "args": ["--mcp-stdio"]
    }
  }
}
```

Windows 打包内提供两个可执行文件：`bilipdj-go.exe` 为无控制台窗口的主程序；`bilipdj-go-mcp.exe` 为专门提供标准输入/标准输出管道的命令行程序，AI 客户端请使用后者的 **绝对路径**并传入 `--mcp-stdio`。macOS/Linux 将 command 换成本机 `bilipdj-go` 二进制的**绝对路径**。若 BiliPDJ-Go 不是默认端口，可给 stdio 桥接配置环境变量 `BILIPDJ_MCP_URL=http://127.0.0.1:9817/mcp`。客户端不应尝试使用 `go run . --mcp-stdio` 代替正式路径（启动器可能将诊断写入 stdout）。

兼容 MCP 2025-03-26、2025-06-18、2025-11-25（传统 initialize）及 2026-07-28（无状态 per-request metadata + `server/discover`），HTTP 为 JSON-RPC 单次响应，不提供 GET/SSE 订阅；对于只需要 tools/list 和 tools/call 的客户端无需长连接。

## 安全和权限

MCP 端口默认沿用 BiliPDJ-Go 的监听地址。读操作在本机 loopback 且 Host 为 localhost/127.0.0.1 时不需要 MCP token。Docker/远程访问必须提供 token，**不要开放该 HTTP 端口到公网**。

写权限**默认关闭**。只有明确配置 `BILIPDJ_MCP_WRITE_TOKEN` 且请求使用匹配的 `Authorization: Bearer ...` 才允许变动排队；写 token 也允许读取。可另设只读 `BILIPDJ_MCP_READ_TOKEN` 供 Docker/远程客户端。

```sh
# 示例（不要把实际 token 写入仓库）
export BILIPDJ_MCP_READ_TOKEN="替换为强随机只读令牌"
export BILIPDJ_MCP_WRITE_TOKEN="替换为不同的强随机写入令牌"
./bilipdj-go
```

Windows 可通过启动脚本的进程环境变量设置这些值。Docker Compose 通过 `.env` 定义变量再传入容器（默认 compose 只发布宿主机回环接口）。**HTTP 远程接入请使用 HTTPS 反向代理/私网 VPN**，避免明文传输 Bearer token。若将第三方 AI 接入写权限，应在 AI 客户端侧限制授权范围并对删除/清空操作要求人工确认。

stdio 桥接**默认不继承服务端写令牌**：如果 AI 客户端进程设置了 `BILIPDJ_MCP_READ_TOKEN`，桥接默认只会使用该只读令牌（本地无令牌时也能读取）；需要允许 AI 执行管理操作时，**必须在 AI 客户端的 MCP 环境变量中单独指定 `BILIPDJ_MCP_CLIENT_TOKEN`，值为服务端配置的写令牌**。这避免因服务进程和 AI 客户端意外共享环境变量而将写权限默认交给模型。若使用非本机 URL，必须是 HTTPS。

## 可供 AI 使用的工具

| 工具名 | 权限 | 说明 |
| --- | --- | --- |
| `bilipdj_status` | 读 | B站/抖音状态、版本、队列人数 |
| `bilipdj_queue` | 读 | 当前统一队列，包括来源平台及用户 ID |
| `bilipdj_slots` | 读 | 10 个槽位人数与当前槽位 |
| `bilipdj_messages` | 读 | 最近收到的弹幕 |
| `bilipdj_add` | 写 | 增加手工排队成员 |
| `bilipdj_edit` | 写 | 编辑成员备注（需队列 key） |
| `bilipdj_remove` | 写 | 删除指定成员（需队列 key） |
| `bilipdj_move` | 写 | 移动成员到指定零基序号 |
| `bilipdj_switch_slot` | 写 | 切换 1–10 号存档 |
| `bilipdj_clear` | 写 | 清空当前队列，必须明确传 `confirm=true` |

工具调用直接复用现有 BiliPDJ-Go Web API 逻辑：保留管理员验证、队列持久化与 SSE 通知。MCP 不提供访问 Cookie、修改管理员、执行命令、读写任意文件、直接发送直播弹幕的接口。

## HTTP 请求示例

传统 2025 MCP：

```sh
curl -s -X POST http://127.0.0.1:9816/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

新版 2026 MCP：

```sh
curl -s -X POST http://127.0.0.1:9816/mcp \
  -H 'Content-Type: application/json' \
  -H 'MCP-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: tools/list' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"example","version":"1.0"}}}}'
```

写入示例（要求预先设置服务端 `BILIPDJ_MCP_WRITE_TOKEN`）：

```sh
curl -s -X POST http://127.0.0.1:9816/mcp \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer 替换成实际写令牌' \
  --data '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bilipdj_add","arguments":{"name":"测试主播","note":"AI 添加"}}}'
```

## 已知边界

- 只实现 MCP tool 调用，不提供工具主动唤醒模型、订阅推送或 AI 对话窗口。
- 支持本机 stdio 或经鉴权的 HTTP，云端 AI 需要对本机建立可信网络连接；不能只填写一个私有 `127.0.0.1` 地址就让云端服务接入。
- 工具返回最近消息与队列包含直播用户公开昵称和用户 ID，请按数据用途管理第三方模型权限。
- 生产部署建议为写令牌配置较高随机强度并定期轮换；丢失令牌不能通过 MCP 找回。


## 预览版打包（尚非正式发行）

MCP PR #3 在 GitHub Actions 的 **Build and Release** 检查通过后，会于该次运行的 **Artifacts** 区提供六平台 ZIP 包：Windows/Linux/macOS 各 amd64、arm64。所有预览 ZIP 都包含 README、许可证、NOTICE、第三方声明及 `docs/MCP.md`；Windows 包额外附带不隐藏标准输入输出管道的 `bilipdj-go-mcp.exe`。压缩包附有对应的 `*.zip.sha256` 校验文件；GitHub Actions 产物保留 14 天。预览包附带 `PREVIEW_BUILD.txt`，不能将其视为正式 GitHub Release；打包不会创建 tag 或 Release。

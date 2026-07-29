# cyber-code 能力矩阵

状态含义：`implemented` 表示有运行时代码和自动化测试；`partial` 表示存在明确边界或平台限制；`out-of-scope` 表示不复制私有实现细节。

| 能力 | 状态 | 入口/验证 |
| --- | --- | --- |
| 唯一产品入口 | implemented | `cmd/cli`；`scripts/check-entrypoint-reachability.sh` 拒绝旧 CLI、工具、语音和 ChatModel 产品路径 |
| Anthropic Messages API | implemented | `internal/provider/anthropic`，provider 契约测试 |
| OpenAI-compatible API | implemented | `internal/provider/openai`，DeepSeek 配置示例 |
| 流式文本、思考、工具调用、usage | implemented | provider 与 agent 测试 |
| Print 文本/JSON/verbose | implemented | 真实 CLI smoke 与 `internal/frontend` 测试；stdout/stderr 分离 |
| TUI Markdown、diff、多行、补全、历史搜索 | implemented | `internal/ui` 交互和渲染测试 |
| 文件读写、编辑、搜索、shell | implemented | builtin 工具和权限测试 |
| PNG/JPEG/GIF/WebP 图片输入 | implemented | `--image`、附件边界和 Anthropic/OpenAI codec 测试 |
| 权限 Broker、审计、交互确认 | implemented | `internal/permissions`、CLI/UI 测试 |
| 会话恢复、checkpoint、rewind、branch | implemented | `internal/session`、集成测试 |
| Compact、Context Pipeline、Memory | implemented | control plane 与 context 测试 |
| MCP HTTP/stdio、重连、幂等重试 | implemented | `internal/mcp` 测试 |
| MCP OAuth/token 生命周期 | partial | 私有存储、过期刷新、CLI 和运行时接线已实现；浏览器授权发现/回调仍由外部宿主提供 |
| 插件、LSP、Hooks、Tasks/子 Agent | implemented | 各模块测试与集成测试 |
| Claude-compatible 插件市场 | implemented | 本地/HTTPS Git catalog、固定 revision/digest、公开仓库 smoke |
| Linux sandbox | partial | bubblewrap 强隔离；缺失时 best-effort 降级 |
| Windows sandbox | partial | Job Object 进程树回收；`required` 对完整边界 fail-closed |
| 本机结构化 SDK 协议 | implemented | `cyber-code serve`、`internal/protocol` |
| MCP Server 有限会话工具 | implemented | `internal/bridge.MCPServer` 集成测试 |
| IDE 文件焦点/选择/诊断/diff 消息模型 | implemented | `cyber-code serve` 转发文件工具执行后的 canonical diff；协议和客户端测试 |
| VS Code 扩展 | partial | 命令入口、托管进程、流式/取消/权限客户端测试，协议 diff 转发测试及只读预览实现通过 TypeScript 编译；尚未运行 Extension Host 原生 UI smoke |
| Claude Code 私有服务、账号和内部提示词 | out-of-scope | 不复制私有实现 |

## 发布门禁

- `go test ./... -count=1`：通过
- `go test -race ./internal/protocol ./internal/bridge ./internal/cli ./internal/ui ./tests/integration -count=1`：通过
- `go vet ./...`、`git diff --check`：通过
- `scripts/check-placeholders.sh`、`scripts/check-brand.sh`、`scripts/check-entrypoint-reachability.sh`：通过
- `scripts/check-coverage.sh`：通过，`internal/security` 90.1%，所有设定门槛均满足
- `npm test --prefix editors/vscode`、`npm run compile --prefix editors/vscode`：通过
- Linux/Windows CLI 交叉构建：通过

macOS 原生 CLI smoke（config、doctor、marketplace、Print、sessions/resume、protocol）通过；经用户显式配置的 `deepseek-v4-pro` smoke 通过且未记录响应正文。Linux/Windows 原生 TUI、Windows 原生沙箱和 VS Code Extension Host UI 尚未在对应宿主运行，交叉构建不等同于原生验收。

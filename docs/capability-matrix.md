# cyber-code 能力矩阵

状态含义：`implemented` 表示有运行时代码和自动化测试；`partial` 表示存在明确边界或平台限制；`out-of-scope` 表示不复制私有实现细节。

| 能力 | 状态 | 入口/验证 |
| --- | --- | --- |
| Anthropic Messages API | implemented | `internal/provider/anthropic`，provider 契约测试 |
| OpenAI-compatible API | implemented | `internal/provider/openai`，DeepSeek 配置示例 |
| 流式文本、思考、工具调用、usage | implemented | provider 与 agent 测试 |
| 文件读写、编辑、搜索、shell | implemented | builtin 工具和权限测试 |
| 权限 Broker、审计、交互确认 | implemented | `internal/permissions`、CLI/UI 测试 |
| 会话恢复、checkpoint、rewind、branch | implemented | `internal/session`、集成测试 |
| Compact、Context Pipeline、Memory | implemented | control plane 与 context 测试 |
| MCP HTTP/stdio、重连、幂等重试 | implemented | `internal/mcp` 测试 |
| MCP OAuth/token 生命周期 | partial | 私有存储、过期刷新、CLI 和运行时接线已实现；浏览器授权发现/回调仍由外部宿主提供 |
| 插件、LSP、Hooks、Tasks/子 Agent | implemented | 各模块测试与集成测试 |
| Linux sandbox | partial | bubblewrap 强隔离；缺失时 best-effort 降级 |
| Windows sandbox | partial | Job Object 进程树回收；`required` 对完整边界 fail-closed |
| 本机结构化 SDK 协议 | implemented | `cyber-code serve`、`internal/protocol` |
| MCP Server 有限会话工具 | implemented | `internal/bridge.MCPServer` 集成测试 |
| IDE 文件焦点/选择/诊断/diff 消息模型 | partial | 稳定数据模型已提供，未绑定具体 IDE 插件 |
| Claude Code 私有服务、账号和内部提示词 | out-of-scope | 不复制私有实现 |

## 发布门禁

- `go test ./... -count=1`：通过
- `go test -race ./internal/protocol ./internal/bridge ./internal/cli ./tests/integration -count=1`：通过
- `go vet ./...`、`git diff --check`：通过
- `scripts/check-placeholders.sh`、`scripts/check-brand.sh`：通过
- `govulncheck ./...`：未发现代码可达漏洞
- `scripts/check-coverage.sh`：通过，`internal/session` 80.1%，`internal/tool/builtin` 80.2%
- Linux/Windows CLI 交叉构建：通过

真实 DeepSeek smoke、Windows 原生 TUI/沙箱和具体 IDE 插件验收需要对应用户环境，默认 CI 不读取或保存 API 密钥。

<div align="center">

# cyber-code

**面向真实开发工作流的本地 Go 编码 Agent**

统一支持 Anthropic 原生 API 与 OpenAI-compatible API，内置流式 TUI、安全工具链、可恢复会话和本机集成协议。

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-4C8BF5?style=flat-square)
![Provider](https://img.shields.io/badge/API-Anthropic%20%7C%20OpenAI--compatible-7B61FF?style=flat-square)
![Interface](https://img.shields.io/badge/Interface-TUI%20%7C%20Print%20%7C%20SDK-18A957?style=flat-square)

[快速开始](#快速开始) · [配置模型](#配置模型) · [常用命令](#常用命令) · [安全边界](#安全边界) · [能力矩阵](docs/capability-matrix.md) · [发行指南](docs/releases.md)

</div>

> [!IMPORTANT]
> cyber-code 是基于公开发布物和 Source Map 进行研究性还原的非官方项目，不代表 Anthropic 或任何模型厂商的官方实现。重新分发或商业使用前，请自行完成许可证与权利审查。

## 为什么选择 cyber-code

| | 能力 | 说明 |
| --- | --- | --- |
| 🧭 | **完整 Agent 工作流** | 流式对话、工具调用、任务/子 Agent、图片输入、上下文压缩、项目指令和 Vim 输入模式。 |
| 🔌 | **多 Provider 接口** | 支持 Anthropic Messages API、OpenAI-compatible Chat Completions，以及 DeepSeek 自定义 Base URL。 |
| 🧰 | **本地开发工具链** | 文件读写、精确编辑、Grep、Glob、Shell、Git、LSP、Hooks、MCP 和插件市场。 |
| 🛡️ | **默认安全边界** | Permission Broker、路径逃逸防护、敏感信息脱敏、原子状态写入和平台沙箱降级提示。 |
| 💾 | **可恢复会话** | 事件日志、原子快照、摘要列表、恢复、导出、checkpoint、rewind 与 branch。 |
| 🖥️ | **多种使用入口** | Bubble Tea TUI、纯文本/JSONL Print、本机 SDK 协议和 VS Code 扩展。 |

## 快速开始

### 1. 构建

需要 Go `1.26.5` 或更高版本。

```bash
go mod download
go build -o cyber-code ./cmd/cli
./cyber-code --version
```

Windows PowerShell：

```powershell
go mod download
go build -o cyber-code.exe ./cmd/cli
.\cyber-code.exe --version
```

### 2. 配置 Provider

凭据只从配置指定的环境变量读取，不会写入 YAML。下面以 DeepSeek 为例：

<details open>
<summary><strong>Linux / macOS</strong></summary>

```bash
export DEEPSEEK_API_KEY="your-key"

./cyber-code config profile set deepseek \
  --provider openai-compatible \
  --base-url https://api.deepseek.com \
  --model deepseek-v4-pro \
  --api-key-env DEEPSEEK_API_KEY \
  --activate
```

</details>

<details>
<summary><strong>Windows PowerShell</strong></summary>

```powershell
$env:DEEPSEEK_API_KEY = "your-key"

.\cyber-code.exe config profile set deepseek `
  --provider openai-compatible `
  --base-url https://api.deepseek.com `
  --model deepseek-v4-pro `
  --api-key-env DEEPSEEK_API_KEY `
  --activate
```

</details>

验证配置：

```bash
./cyber-code config validate
./cyber-code doctor
```

Anthropic、Bedrock、Vertex、Azure 与更多 OpenAI-compatible 配置见 [Provider 指南](docs/providers.md) 和 [配置参考](docs/configuration.md)。

### 3. 开始使用

```bash
# Phase 3 默认入口：Tactical Ops 场景演示，不需要 API key
./cyber-code "评估 juice-shop.lab"

# 完整编码 Agent（保留一个弃用周期的经典界面）
./cyber-code --ui=classic
```

Tactical Ops 当前明确标记为 `DEMO`，只运行确定性的 `juice-shop.lab` 安全场景，不会调用 Provider，也不会在本地失败时回退远端。快捷键、恢复语义和迁移说明见 [Tactical Ops 指南](docs/tactical-ops.md)。

真实 Runtime Source 默认关闭，必须由可信宿主或操作者显式启用。本机 Tactical Ops 运行时使用：

```bash
./cyber-code --enable-real-sources --source local
```

Web 宿主需要在启动前设置 `__CYBER_RUNTIME_CONFIG__.realSourcesEnabled = true`，并提供可信的 loopback bridge 或 HTTPS Remote 配置；Desktop 构建需要设置 `VITE_CYBER_REAL_SOURCES_ENABLED=true`。真实来源握手、凭据与失败回退边界见 [Phase 4 Runtime 威胁模型](docs/security/phase4-runtime-threat-model.md)。

也可以跳过 TUI，直接获得适合脚本和管道处理的输出：

```bash
./cyber-code --print "检查这个项目并告诉我如何运行"
./cyber-code --print --json "分析当前 Git 变更"
```

## 核心能力

### Agent 与上下文

- 每轮请求统一加载用户指令、根目录 `CYBER.md` 和逐级 `.cyber-code/instructions.md`。
- 接近 context window 时发出预警，并在阈值跨越时进行可取消的自动压缩。
- 支持 `/init`、`/cost`、`/stats`、`/clear`、`/checkpoint`、`/rewind` 和 `/branch`。
- `task_run`、`task_status`、`task_cancel` 在父级权限和 turn budget 内运行子 Agent。

### 工作区工具

- `read_file`、`write_file`、`edit_file` 与 Notebook 编辑。
- 独立的 `grep_files` 和 `glob_files`，支持递归匹配、结果上限与取消。
- 单次及批量精确编辑，支持 SHA-256 前置条件、冲突检测和 canonical diff。
- Shell、Git workflow、LSP、Hooks 和图片输入统一经过 Runtime 与 Permission Broker。

### 扩展生态

- MCP HTTP/stdio、OAuth token 生命周期、健康检查与运行时重连。
- 独立进程插件、项目/用户 Skills，以及 Claude-compatible 插件市场内容转换。
- `cyber-code serve` 提供 newline-delimited JSON 本机协议。
- VS Code 扩展支持上下文、流式事件、取消、权限确认和只读 diff 预览。

## 常用命令

| 场景 | 命令 |
| --- | --- |
| 启动 Tactical Ops Demo | `cyber-code "评估 juice-shop.lab"` |
| 启动完整编码 Agent | `cyber-code --ui=classic` |
| 非交互文本输出 | `cyber-code --print "PROMPT"` |
| JSON Lines 输出 | `cyber-code --print --json "PROMPT"` |
| 显示工具进度和 Token | `cyber-code --print --verbose "PROMPT"` |
| 附加首轮图片 | `cyber-code --image screenshot.png "PROMPT"` |
| 恢复会话 | `cyber-code --resume SESSION_ID` |
| 查看和验证配置 | `cyber-code config list` / `cyber-code config validate` |
| 环境诊断 | `cyber-code doctor` / `cyber-code doctor --json` |
| 管理会话 | `cyber-code sessions list|resume|export|delete` |
| 管理 MCP | `cyber-code mcp list|status|test|enable|disable` |
| 管理插件 | `cyber-code plugins list|enable|disable` |
| 搜索插件市场 | `cyber-code plugins marketplace search QUERY` |
| 启动本机协议 | `cyber-code serve` |
| 查看终端设置建议 | `cyber-code terminal-setup` |
| 显式检查版本 | `cyber-code version-check` |

经典交互 TUI 中还可以使用：

```text
/help           /status          /config
/init           /cost            /stats
/clear          /compact         /vim
/checkpoint     /rewind          /branch
/diff           /review          /commit
/mcp            /memory          /bug
```

运行 `/help` 可查看当前版本实际注册的命令和参数。

经典 TUI 快捷键：输入 `/` 会显示斜杠命令候选，使用 `↑/↓` 选择、`Tab` 或 `Enter` 接受；`PgUp/PgDn` 或鼠标滚轮滚动对话，普通 `↑/↓` 仍用于输入历史。按 `Ctrl+T` 打开子 Agent 任务列表，`Enter` 进入选中任务，`[` / `]` 切换任务，`Esc` 或再次按 `Ctrl+T` 返回父会话。模型生成期间按 `Ctrl+C` 只取消当前生成，再次处于空闲状态时按 `Ctrl+C` 退出。权限窗口中的 `Allow Always` 仅记住当前进程会话内相同工具和动作的批准，不会写入配置，也不会绕过显式 deny 或沙箱边界。

## 会话与项目指令

cyber-code 会持久化 canonical 事件日志和原子快照。使用 `sessions list` 浏览按更新时间倒序排列的脱敏摘要，使用 `--resume` 继续工作。单个会话采用单写者 lease，避免多个进程同时覆盖状态。

自动上下文压缩在 context 使用比例或绝对 Token 阈值任一越界时触发；两个阈值都回落后才允许下一次自动尝试。手动 `/compact` 只遵循绝对 Token 阈值。

项目指令按以下顺序组合：

1. cyber-code 内置身份与安全边界；
2. 用户状态目录中的 `instructions.md`；
3. Git 项目根目录的 `CYBER.md`；
4. 从项目根到当前目录逐级发现的 `.cyber-code/instructions.md`。

在 TUI 中执行 `/init` 可以安全创建一个不会覆盖现有文件的 `CYBER.md` 模板。完整加载规则与大小限制见 [配置参考](docs/configuration.md)。

## 安全边界

> [!WARNING]
> `--permission-mode bypass` 会跳过交互确认。它只适用于隔离环境和完全可信的输入，不应作为日常默认配置。

| 模式 | 行为 |
| --- | --- |
| `default` | 自动允许只读文件操作；写入和执行需要确认。Headless 无确认 UI 时拒绝相关操作。 |
| `plan` | 只允许分析和规划，禁止修改工作区。 |
| `accept-edits` | 允许工作区内文件编辑，其他高风险操作仍受限制。 |
| `bypass` | 仅能通过显式 CLI 参数启用，跳过交互确认。 |

路径解析、Shell 重定向、MCP、插件、LSP、Hooks 和子 Agent 都不能绕过父级权限。Provider 错误、Doctor、审计、会话摘要和导出会经过集中脱敏。详细威胁边界见 [安全模型](docs/security.md)。

## 平台支持

| 平台 | 状态 | 边界 |
| --- | --- | --- |
| Linux | 支持 | 可使用 bubblewrap 隔离文件系统、网络和进程；缺失时 `best-effort` 明确降级。 |
| Windows | 支持 | 使用 Job Object 管理进程树；当前不宣称完整文件系统/网络沙箱。 |
| macOS | 开发与本机测试 | 支持构建、CLI smoke 和进程组降级；正式发布门禁以 Linux/Windows 为主。 |

使用 `cyber-code doctor` 查看当前机器的 Provider 凭据、状态目录、沙箱、通知、语音、MCP 和 LSP 状态及修复建议。`cyber-code terminal-setup` 只显示平台指导，不会修改 shell profile、注册表或终端配置。

## Security Runtime：cyber-agent

要运行网络安全任务，显式选择 `cyber-agent`，不会根据提示词自动切换：

```bash
./cyber-code --runtime cyber-agent --runtime-location local \
  --input openapi.yaml \
  "评估输入描述中的授权目标并生成可追踪报告"
```

Web、Desktop、VS Code 使用同一 Session API 和 InputManifest。安全任务的 Scope proposal、审批、
子 Agent、工具回执、Evidence、Finding 和 Report 均由 cyber-agent 提供权威状态；客户端只投影。
四个可复跑场景和 PASS/SKIP 边界见 [cyber-agent 运维指南](docs/operations/cyber-agent.md)、
[能力矩阵](docs/capability-matrix.md) 和 [验收矩阵](docs/operations/acceptance-matrix.md)。

## MCP、插件与 VS Code

```bash
# MCP
cyber-code mcp add remote --url https://example.com/mcp
cyber-code mcp test remote

# Claude-compatible 插件市场
cyber-code plugins marketplace add community https://github.com/example/marketplace.git#v1.0.0
cyber-code plugins marketplace search review
cyber-code plugins marketplace install review-kit@community
```

VS Code 扩展位于 `editors/vscode`。将设置 `cyber-code.executable` 指向本机二进制后，扩展会启动 `cyber-code serve`；凭据仍由 CLI 配置和环境变量管理，不会写入编辑器设置或协议消息。协议与开发说明见 [本机 SDK 文档](docs/sdk.md)。

## 可选本地工具

- `/bug DESCRIPTION` 在状态目录生成限长、脱敏、私有权限的 Markdown 报告，默认不上传，也不自动打开浏览器。
- `version-check` 默认不联网；只有显式提供 `--enable` 才读取签名 JSON envelope。发行构建可注入受信任的 HTTPS metadata URL 与 Ed25519 公钥，flags 可显式覆盖；命令只报告 URL、SHA-256 和大小，不会下载或安装二进制。
- `terminal-setup` 只检测平台并输出建议，不会静默修改系统设置。

## 开发与验证

```bash
go test ./...
go test -race ./...
go vet ./...

scripts/release-smoke.sh
scripts/platform-matrix.sh

sh scripts/check-placeholders.sh
sh scripts/check-todos.sh
sh scripts/check-brand.sh
sh scripts/check-entrypoint-reachability.sh
sh scripts/check-coverage.sh

npm ci --prefix editors/vscode
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
```

交叉构建：

```bash
GOOS=linux GOARCH=amd64 go build -o cyber-code-linux-amd64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o cyber-code-windows-amd64.exe ./cmd/cli
```

真实云服务验收始终是显式 opt-in；测试和日志不得保存凭据或完整敏感响应。

`platform-matrix.sh` 只把当前主机实际运行过的项目记为 `PASS`；缺少 Linux/Windows 真机、pnpm、bubblewrap 或云账号时会输出带原因的 `SKIP`。交叉编译和本地 Provider contract test 不会冒充真机或真实云验收。

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [能力矩阵](docs/capability-matrix.md) | 已实现、部分支持和明确不在范围内的能力。 |
| [配置参考](docs/configuration.md) | 配置优先级、权限、沙箱、Context、Skills、Hooks、MCP 和插件市场。 |
| [Provider 指南](docs/providers.md) | Anthropic、OpenAI-compatible、DeepSeek 与受管云 Provider。 |
| [安全模型](docs/security.md) | Permission Broker、路径边界、脱敏、审计和进程隔离。 |
| [Runtime 威胁模型](docs/security/phase4-runtime-threat-model.md) | Local/Remote Runtime 的认证、重放、Scope、Evidence、cursor 与 capability gate。 |
| [本机 SDK](docs/sdk.md) | JSONL 协议、IDE context、diff、VS Code 扩展和 Bridge。 |
| [Tactical Ops](docs/tactical-ops.md) | 默认 Demo、快捷键、恢复语义和经典 TUI 迁移。 |
| [验收矩阵](docs/operations/acceptance-matrix.md) | 发布签名、原生平台、云证据以及 OIDC/SCIM/策略/插件运维边界。 |
| [PostgreSQL 运维](docs/operations/postgres.md) | 多实例授权存储、迁移、备份恢复与显式 SKIP 规则。 |
| [Telemetry 与 SLO](docs/operations/slo.md) | 脱敏指标、告警阈值、队列 drain 和关闭顺序。 |

---

<div align="center">

**cyber-code — 独立、可审计、可扩展的本地编码 Agent。**

</div>

<div align="center">

# cyber-code

**面向真实开发工作流的本地 Go 编码 Agent**

统一支持 Anthropic 原生 API 与 OpenAI-compatible API，内置流式 TUI、安全工具链、可恢复会话和本机集成协议。

[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows-4C8BF5?style=flat-square)
![Provider](https://img.shields.io/badge/API-Anthropic%20%7C%20OpenAI--compatible-7B61FF?style=flat-square)
![Interface](https://img.shields.io/badge/Interface-TUI%20%7C%20Print%20%7C%20SDK-18A957?style=flat-square)

[快速开始](#快速开始) · [配置模型](#配置模型) · [常用命令](#常用命令) · [安全边界](#安全边界) · [能力矩阵](docs/capability-matrix.md)

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
./cyber-code
```

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
| 启动交互 TUI | `cyber-code` |
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

交互 TUI 中还可以使用：

```text
/help           /status          /config
/init           /cost            /stats
/clear          /compact         /vim
/checkpoint     /rewind          /branch
/diff           /review          /commit
/mcp            /memory          /bug
```

运行 `/help` 可查看当前版本实际注册的命令和参数。

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
- `version-check` 默认不联网；只有显式提供 `--enable`、受信任的 HTTPS metadata URL 和 Ed25519 公钥时才检查版本，且不会下载或安装二进制。
- `terminal-setup` 只检测平台并输出建议，不会静默修改系统设置。

## 开发与验证

```bash
go test ./...
go test -race ./...
go vet ./...

sh scripts/check-placeholders.sh
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

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [能力矩阵](docs/capability-matrix.md) | 已实现、部分支持和明确不在范围内的能力。 |
| [配置参考](docs/configuration.md) | 配置优先级、权限、沙箱、Context、Skills、Hooks、MCP 和插件市场。 |
| [Provider 指南](docs/providers.md) | Anthropic、OpenAI-compatible、DeepSeek 与受管云 Provider。 |
| [安全模型](docs/security.md) | Permission Broker、路径边界、脱敏、审计和进程隔离。 |
| [本机 SDK](docs/sdk.md) | JSONL 协议、IDE context、diff、VS Code 扩展和 Bridge。 |

---

<div align="center">

**cyber-code — 独立、可审计、可扩展的本地编码 Agent。**

</div>

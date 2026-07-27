# Claude Code Go 产品化重构设计

**日期：** 2026-07-27  
**状态：** 已批准  
**目标平台：** Windows、Linux

## 1. 背景

当前仓库由 Claude Code v2.1.88 的 Source Map 还原结果翻译而来。代码能够构建，但主调用链只连接了少量组件，认证、交互 UI、权限和工具执行之间存在断点；README 宣称完成的多个模块仍是占位实现或未接入代码。项目还缺少有效许可证、持续集成和足够测试。

本次工作采用“核心重构、模块择优复用”：重写不可靠的装配层和核心运行时，保留经过契约测试验证的解析器、协议与服务代码。不追求兼容官方 Claude Code v2.1.88 的内部接口，优先保证功能完整、行为明确、安全和稳定。

## 2. 目标与完成定义

项目最终应成为可在 Windows 和 Linux 上使用的本地编码 Agent，并完整接入以下能力：

- Anthropic 原生协议与 OpenAI-compatible 协议，可按 Profile 切换；
- Anthropic API Key/OAuth，以及 Bedrock、Vertex、Azure 认证适配；
- 交互 TUI 与非交互 Print 模式；
- 内置文件、搜索、Shell、Web、Notebook、Todo 和用户交互工具；
- 权限、安全执行、Hooks 与审计日志；
- MCP、Plugins、Skills、LSP；
- Tasks、后台任务与子 Agent；
- 会话恢复、导出、Compact、Token 与成本统计；
- Vim 输入、通知和语音能力；
- 可执行的 config、doctor、mcp、plugins、sessions 管理命令。

模块只有同时满足以下条件才算完成：可从 CLI/TUI 触发、进入统一主调用链、产生真实效果、经过权限层，并具备成功路径及关键失败路径测试。文件存在、空实现、静默成功和 placeholder 均不算完成。

## 3. 方案选择

### 方案 A：原地补丁

逐个修复当前装配问题。初期改动小，但会保留重复 API Client、两套 UI、分散状态和权限旁路，长期回归风险高。

### 方案 B：核心重构、模块择优复用（采用）

重写配置、Provider、Agent Runtime、权限和 UI 主链路；现有 Bash 解析、MCP、LSP、Tasks 等模块通过测试后迁移到统一接口。每个阶段都能交付可运行的垂直切片。

### 方案 C：全量重写

架构最干净，但会丢弃大部分现有实现，验证成本最高，也偏离修复现有项目的目标。

## 4. 总体架构

系统只有一条主调用链：

```text
CLI / TUI / Print
        |
Application Runtime
        |
Agent Event Loop
   |-- Provider: Anthropic / OpenAI-compatible / Cloud adapters
   |-- Permission Broker
   |-- Tool Registry: built-in / MCP / LSP / Plugins
   |-- Tasks and Sub-agents
   |-- Hooks
   `-- Session / Compact / Usage
```

设计原则：

- `cmd` 只负责参数解析和依赖装配，不包含业务逻辑。
- Runtime 统一管理生命周期、状态、配置、Provider、工具和事件。
- Agent Loop 使用内部统一消息与事件模型，不感知供应商协议。
- TUI 与 Print 消费相同事件流，不维护各自的 Agent 状态。
- 所有工具经 Tool Registry 注册并经 Permission Broker 执行。
- 平台相关的 Shell、进程、通知和语音通过 Windows/Linux 适配器提供。
- 删除或合并重复 API Client、未使用 UI 和无法证明正确的旁路实现。

## 5. Provider 与配置

Provider Profile 使用如下逻辑结构：

```yaml
profiles:
  anthropic:
    provider: anthropic
    model: claude-model
    api_key_env: ANTHROPIC_API_KEY

  local:
    provider: openai
    base_url: http://localhost:8000/v1
    model: model-name
    api_key_env: OPENAI_API_KEY
```

配置优先级为：命令行参数、环境变量、项目配置、用户配置、默认值。密钥只引用环境变量或系统凭据存储，不写入项目配置、会话或日志。

OpenAI-compatible 基线为 `/v1/chat/completions`、SSE 流式响应、标准 `tool_calls` 和 JSON Schema 工具定义。Provider 暴露能力描述；`doctor` 实际探测流式响应、工具调用和结构化参数能力。能力不足必须产生明确错误，不能静默降级为不可执行的纯聊天。

Anthropic、OpenAI-compatible、Bedrock、Vertex 和 Azure 响应统一转换为以下事件：文本增量、思考增量、工具调用、工具参数增量、使用量、正常结束和错误。

## 6. Agent 数据流

一次完整回合：

```text
用户输入
 -> 写入会话事件日志
 -> 组装系统提示、历史和工具定义
 -> Provider 流式请求
 -> 归一化 Runtime 事件并渲染
 -> ToolCall 进入 Permission Broker
 -> PreToolUse Hook
 -> 执行内置工具、MCP、LSP 或插件
 -> PostToolUse Hook
 -> 工具结果写回会话
 -> 继续模型循环，直至结束或达到限制
```

会话使用 JSONL 事件日志与原子快照，支持恢复、继续、导出和 Compact。Compact 生成派生摘要，不销毁原始事件。只读且声明并发安全的工具可以并行；Shell、写文件和编辑等变更操作串行执行。每轮执行受次数、Token、费用、超时和取消信号约束。

## 7. 权限与安全

权限模型默认拒绝，并保证能力不可升级：

- `default`：工作区只读工具可自动允许；写文件、Shell、网络、MCP 和外部进程需要确认。
- `plan`：只允许只读操作。
- `accept-edits`：允许工作区内文件修改，Shell 和外部访问仍需确认。
- `bypass`：仅可由显式 CLI 操作启用，并显示高风险警告；项目配置不能开启。
- 非交互模式默认拒绝需要询问的操作，仅显式 CLI 授权规则可放行。

安全要求：

- 文件路径必须绝对化、解析符号链接并验证工作区边界；越界操作单独授权。
- Shell 权限基于解析后的命令、参数、工作目录和重定向目标，不使用简单字符串前缀作为安全边界。
- Linux 提供可选 bubblewrap 沙箱；Windows 使用 Job Object、受限进程和进程树终止。只有策略隔离时必须明确提示。
- 子 Agent、Tasks、插件和 MCP 只能继承权限子集，不能提升权限。
- 插件使用 manifest 声明工具、网络、文件和进程能力，默认独立子进程运行。
- Hooks 可以拒绝或转换操作，但不能绕过 Permission Broker。
- 网络内容、MCP 输出、插件输出和模型文本均为不可信数据，不能改变授权状态。
- API Key、Authorization、Cookie 等字段统一脱敏，包括调试日志和错误上下文。
- 权限决策、工具调用、文件变更和进程执行写入审计事件。
- 文件写入使用临时文件与原子替换；批量编辑失败时回滚。

## 8. 模块职责

- **Provider/Auth：** 协议转换、流式事件、认证、重试、Token 和错误映射。
- **Tools：** 统一 Schema、只读性、并发性、权限需求、结果限制和取消语义。
- **MCP：** stdio 与 HTTP/SSE 传输、连接、重连、工具/资源发现、OAuth 和权限映射。
- **Tasks/Agents：** 前台、后台和子 Agent，具有独立上下文、预算、取消和状态查询。
- **Hooks：** Session、Prompt、PreToolUse、PostToolUse、Stop 等事件，支持超时、拒绝与结构化输出。
- **Plugins/Skills：** 发现、验证、启停与生命周期管理，不直接修改全局 Registry。
- **LSP：** 按工作区管理服务器，提供诊断、定义、引用、补全与 Hover，支持健康检查和重启。
- **Session/Compact：** 恢复、继续、导出、压缩、Token 估算和成本统计。
- **UI：** 流式输出、权限弹窗、工具进度、任务列表、模型切换、Vim 输入和非交互结构化输出。
- **Platform：** Windows PowerShell/Job Object/通知与 Linux Shell/进程组/通知；语音依赖缺失时降级并给出诊断。
- **Operations：** config、doctor、mcp、plugins、sessions 命令执行真实操作，并支持 JSON 输出。

## 9. 错误处理

错误分为配置、认证、Provider、限流、权限、工具、MCP/LSP、平台依赖、取消和内部错误。只有明确可重试的限流、过载和临时网络错误自动重试；认证失败、权限拒绝和无效工具参数立即返回。内部错误保留因果链，用户输出脱敏并提供可执行建议。

取消必须向 Provider 请求、工具进程、MCP/LSP 请求、Tasks 和插件子进程传播。后台进程必须可回收，不能留下孤儿进程。

## 10. 测试策略

- 单元测试覆盖 Provider 编解码、权限规则、路径边界、Shell 解析、Compact 和配置合并。
- 本地模拟服务器执行 Anthropic 与 OpenAI-compatible 契约测试，包括流式响应、工具调用、限流和错误。
- 集成测试覆盖 Agent 工具循环、Hooks、MCP、LSP、插件、Tasks 和会话恢复。
- 安全测试覆盖路径穿越、符号链接逃逸、命令注入、权限升级、恶意 MCP/插件和敏感信息泄漏。
- TUI 测试覆盖按键、流式渲染、权限弹窗、取消与 Vim 状态。
- CI 在 Windows 与 Linux 执行构建、单元/集成测试和 CLI smoke test。
- 真实服务测试必须显式启用并由用户提供凭据，不进入默认 CI。

## 11. 发布门槛

- `go test ./...`、`go test -race ./...`、`go vet ./...`、静态检查和漏洞扫描全部通过。
- 核心包覆盖率至少 80%，安全边界包至少 90%。
- 所有目标模块都有端到端验收，`doctor` 能检测配置、Provider 能力和平台依赖。
- README 只声明已通过验收的功能。
- Windows 与 Linux 构建产物通过 smoke test，版本升级和配置迁移可重复执行。
- 在重新分发或商业使用前，必须完成源码权属与许可证审查；当前仓库的“仅供研究”声明不能替代正式许可证。

## 12. 实施策略

实施按可运行垂直切片推进：先建立统一类型、配置和 Provider 契约，再打通安全 Agent Loop 与基础工具；随后依次接入会话/TUI、MCP、Hooks/Plugins、Tasks/Agents、LSP/Compact、平台能力和运维命令。每个切片必须先写失败测试，再实现、集成并通过发布门槛，不允许一次性重写后集中调试。

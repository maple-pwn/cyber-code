# cyber-code 能力对齐设计

**日期：** 2026-07-29  
**状态：** 已批准  
**采用方案：** 架构优先的本地完整能力  
**目标平台：** Windows、Linux（macOS 作为开发与兼容平台）

## 1. 背景与范围

Task 25 已完成 cyber-code 的产品化基线：统一 Runtime 与事件流、Anthropic 和 OpenAI-compatible Provider、权限 Broker、基础工具、Hooks、会话、MCP、LSP、Skills、Plugins、后台任务、子 Agent、Print/TUI，以及 Windows/Linux 构建与发布门禁。

本阶段以 `liuup/claude-code-analysis` 仓库提交 `7b7b915d7da804088a8152ed24c68e3da2d1110e` 的公开研究结果作为能力清单，而不复制其实现。目标是补齐一个本地编码 Agent 可以合理实现的 Claude Code 类能力。Anthropic 私有服务、内部协议、未公开基础设施、隐藏命令和品牌专属行为不属于兼容目标。

完成度必须以可从正式入口触发、进入统一 Runtime、经过权限与审计、产生真实效果并有失败路径测试为准。仅存在包、接口或占位命令不算完成。

## 2. 方案选择

### 方案 A：按可见功能逐项补齐

优先增加 Web、Todo、AskUser 和斜杠命令，短期可见功能增长快，但会在提示词、会话和 UI 中形成重复状态。

### 方案 B：架构优先的本地完整能力（采用）

先统一 instruction/context pipeline、命令控制面和会话事务，再增加 Memory、多 Agent、沙箱和扩展入口。新增能力共享稳定契约，适合跨 Windows/Linux 维护。

### 方案 C：同时建设完整远程平台

并行实现 CLI、IDE、远程桥接和团队服务，覆盖最广，但过早引入服务端身份、同步和部署复杂度，也依赖不属于公开兼容范围的能力。

## 3. 能力差距与优先级

| 能力域 | 当前状态 | 本阶段目标 | 优先级 |
|---|---|---|---|
| Prompt / Context | 固定身份提示、基础 Compact | 分层指令、预算、来源追踪、压缩状态重注入 | P0 |
| 控制面 | 管理子命令、基础 TUI | CLI/TUI 共用斜杠命令、模型/上下文/权限/任务状态 | P0 |
| 会话 | 事件日志、快照、恢复、导出 | checkpoint、rewind、branch、搜索、修复 | P0 |
| 工具 | 文件、搜索、Shell、MCP/LSP | AskUser、Todo、Web、Notebook、增强编辑 | P0 |
| Memory | 无持久分层记忆 | 用户/项目/会话/Agent 记忆及受控检索 | P1 |
| 多 Agent | 基础子 Agent 与后台任务 | Agent 定义、协调器、任务板、邮箱、权限子集 | P1 |
| 隔离 | Broker、路径校验、进程控制 | OS 强制文件/网络沙箱和 fail-closed 模式 | P1 |
| MCP | HTTP/stdio、资源与工具 | 重连、认证缓存、超时、健康状态 | P1 |
| 产品入口 | CLI、Print、TUI | 结构化 SDK 协议、MCP Server、IDE Bridge | P2 |

## 4. 目标架构

```text
CLI / TUI / Print / SDK / IDE Bridge / MCP Server
                       |
                  ControlPlane
                       |
                 Runtime Facade
       +---------------+----------------+
       |               |                |
 ContextBuilder    SessionGraph    CapabilityRegistry
       |               |                |
 Instructions       Event Log      Tools / MCP / LSP
 Memory Retrieval   Checkpoints    Agents / Plugins
 Token Budget       Branches       Hooks / Tasks
       +---------------+----------------+
                       |
          Provider + Permission Broker
                       |
          Platform Sandbox / Process Runner
```

现有 `runtime.Runtime` 仍是生命周期与回合执行的唯一入口。新增组件通过小接口注入，不允许 CLI、TUI 或 SDK 绕过 Runtime 直接调用 Provider、工具或状态存储。

## 5. Instruction 与 Context Pipeline

新增 `internal/contextbuilder`，负责构造每次 Provider 请求。输入为运行时身份、工作区、会话状态、模型能力、工具目录和用户消息；输出为不可变的 Context Plan，包括最终系统指令、消息、工具、Token 预算、来源清单和诊断。

指令层按低到高优先级组合：

1. cyber-code 编译期身份与安全边界；
2. 用户级指令；
3. 项目根到当前目录的项目指令；
4. 当前 Agent 定义；
5. 已显式加载的 Skill；
6. 会话 Compact 摘要与相关 Memory；
7. 当前回合的运行时约束。

安全边界不能被更高层覆盖。项目文件和外部内容均标记为不可信指令来源，不能修改权限、密钥处理或沙箱策略。加载器限制单文件和总字节数，解析符号链接后验证作用域，并记录每段来源和摘要，供 `/context` 与调试输出使用。

Token 预算先为固定安全指令、最近消息和未完成工具回合保留空间，再分配项目指令、Memory 与历史。超限时返回结构化诊断或触发 Compact，不能静默截断工具调用配对。

## 6. Control Plane

新增统一命令注册表。命令由名称、别名、参数 Schema、适用入口、权限需求和执行器组成。CLI 交互输入与 TUI 在模型请求前解析斜杠命令；Print/SDK 可直接发送结构化命令，避免字符串二次解析。

第一组命令包括 `/help`、`/model`、`/context`、`/compact`、`/permissions`、`/hooks`、`/skills`、`/tasks`、`/checkpoint`、`/rewind`、`/branch`、`/memory` 和 `/status`。命令结果统一为 Runtime 事件，因此所有前端看到一致状态。

命令不能借助交互入口提升权限。影响文件、网络、进程、会话历史或持久 Memory 的命令仍需经过 Broker 或明确的状态事务。

## 7. SessionGraph 与恢复

现有追加式事件日志扩展为会话图。原始事件不可原地改写；checkpoint 保存事件序列、Context 元数据和工作区变更描述；rewind 创建新的活动分支指针；branch 创建共享祖先的新会话标识。

```text
session root -> checkpoint A -> checkpoint B -> current
                       |             |
                       |             `-> rewind branch
                       `-> named branch
```

文件恢复只处理由 cyber-code 记录且验证过内容摘要的变更。默认 rewind 仅回退对话状态；工作区恢复需要单独确认，并在冲突时停止而不是覆盖用户修改。所有图和索引更新使用跨进程锁、临时文件、fsync 与原子替换。恢复时检测尾部损坏、孤立快照和索引漂移，并提供只追加的修复记录。

## 8. Memory 与多 Agent

Memory 分为用户、项目、会话、Agent 和团队作用域。每条记录包含来源、创建者、时间、可信等级、标签、内容摘要和可选失效时间。默认只自动提取低风险的项目事实与用户偏好；密钥、凭据、原始工具输出和推测性结论不得自动持久化。

检索先使用确定性的作用域、标签和文本评分，向量检索作为可选适配器，不成为离线运行的必要条件。注入 Context 前执行字节、条数和 Token 限制，并保留引用信息。

自定义 Agent 定义声明提示、工具、模型、预算、Memory 作用域和权限上限。协调器通过持久任务板和邮箱分派工作。子 Agent 只能继承权限子集；写操作仍由拥有工作区租约的执行者串行提交。消息、任务转换和权限桥接全部进入审计日志。

## 9. 工具与外部内容

工具契约增加副作用分类、并发策略、网络目标、结果上限和可恢复性描述。新增工具均注册到现有 Registry 并经过 Runner、Hooks 与 Broker：

- AskUser：交互入口显示结构化问题；非交互入口返回明确的需要输入状态。
- Todo：持久任务列表、状态转换和会话关联。
- WebFetch/WebSearch：限制协议、重定向、地址解析、响应大小和内容类型，防止访问本机与私网地址；网络内容标记为不可信。
- Notebook：使用结构化 notebook 解析与原子写入，不进行字符串拼接。
- 增强编辑：支持唯一匹配、批量事务、内容摘要和冲突检测。

## 10. OS 沙箱与扩展生命周期

Broker 负责授权，Sandbox 负责操作系统强制执行，两者缺一不可。沙箱支持 `off`、`best-effort` 和 `required`：`required` 无法建立隔离时必须拒绝启动。

- Linux：优先使用 bubblewrap 或可验证的等价后端，隔离挂载、工作目录、进程、临时目录和网络。
- Windows：使用受限 Token、Job Object、进程树约束和明确的文件 ACL；无法满足网络隔离策略时 fail closed。
- macOS：提供开发兼容后端，不作为本阶段正式发布门槛。

MCP 与插件复用统一受管连接状态机：disconnected、connecting、ready、degraded、reconnecting、closed。连接具有并发上限、调用超时、指数退避、健康信息和显式关闭；OAuth/令牌只存入凭据存储，绝不进入配置、事件和导出。

## 11. SDK、MCP Server 与 IDE Bridge

二级入口基于版本化 JSON 消息协议，覆盖启动会话、发送输入、响应权限、取消、订阅事件和查询状态。协议只映射 ControlPlane 与 Runtime 事件，不暴露内部 Go 类型。

MCP Server 模式将经过授权的 cyber-code 会话能力暴露为有限工具；IDE Bridge 使用同一协议提供选择范围、诊断、文件焦点和 diff。远程账户、云同步和 Anthropic 私有 bridge 不在本阶段范围内。

## 12. 错误与安全模型

新增错误类别：instruction、context-budget、command、checkpoint、memory、collaboration、sandbox 和 bridge。错误保留因果链与稳定代码，用户输出必须脱敏。自动重试仅用于幂等连接和临时网络失败；写工具、Memory 更新和会话图事务不得盲目重试。

所有来自项目文件、网页、MCP、插件、LSP、子 Agent 和模型的内容都是不可信数据。只有编译期策略、用户的显式本地配置和 Broker 决策可以改变安全状态。任何入口都不得通过提示词、工具结果或命令文本启用 bypass。

## 13. 测试策略

每项能力先写失败测试，再做最小实现和集成：

- 单元测试：指令优先级、预算、命令解析、会话图、Memory 检索和状态机。
- 属性/模糊测试：命令参数、事件日志恢复、路径边界、Web URL 与 notebook 结构。
- 集成测试：CLI/TUI/Print 共用命令、Compact 重注入、checkpoint/branch、Agent 协作和 MCP 重连。
- 安全测试：提示注入越权、SSRF、符号链接逃逸、权限升级、恶意 Memory/MCP/插件和凭据泄漏。
- 并发测试：会话租约、分支写入、任务板、连接重试和取消传播。
- 平台测试：Windows/Linux 原生测试与交叉构建；沙箱 `required` 的成功和拒绝路径。
- 真实 Provider 测试：继续使用用户显式提供的 DeepSeek `deepseek-v4-pro` 等 Profile，只记录脱敏状态。

每个 Task 通过定向测试后运行受影响包测试；阶段完成时运行 `go test ./...`、`go test -race ./...`、`go vet ./...`、品牌/占位/覆盖率门禁、漏洞扫描和 Windows/Linux 构建。

## 14. 实施阶段

1. Task 26：统一 instruction/context pipeline。
2. Task 27：统一斜杠命令与交互控制面。
3. Task 28：会话 checkpoint、rewind、branch 与一致性恢复。
4. Task 29：AskUser、Todo、Web、Notebook 与增强工具。
5. Task 30：分层 Memory、自动提取与相关检索。
6. Task 31：Agent 定义、协调器、任务板和邮箱。
7. Task 32：Windows/Linux 强沙箱与 fail-closed 模式。
8. Task 33：MCP 重连、认证与生命周期强化。
9. Task 34：结构化 SDK、MCP Server 和 IDE Bridge。
10. Task 35：完整能力对照、跨平台验收、安全复查和发布文档。

每个 Task 必须经过需求核对、TDD 实现、定向验证、全量回归和独立代码审查。Critical/Important 问题修复后才能进入下一项。

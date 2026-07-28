# cyber-code 能力对齐实施计划

> **执行要求：** 按 Task 顺序使用 TDD。每个 Task 完成定向测试、全量回归和代码审查后才进入下一项。

**目标：** 在 Task 25 的稳定产品基线上，实现公开可复现的 Claude Code 类本地编码 Agent 能力，同时保持 Anthropic/OpenAI-compatible Provider、Windows/Linux 和统一权限安全边界。

**架构：** 保留现有 Runtime、Agent、Provider、Broker、Registry 和 Session Store。新增 Context Builder、Control Plane、Session Graph、Memory、Collaboration、Sandbox 和 Bridge 组件；所有入口只通过 Runtime 访问能力。

**技术栈：** Go、Cobra、Bubble Tea、JSONL/原子快照、现有 Provider/Tool/Permission 接口、Windows/Linux 平台适配。

**设计依据：** `docs/plans/2026-07-29-cyber-code-capability-parity-design.md`

---

## 通用完成规则

每个 Task 都执行以下循环：

1. 写出会失败的契约、集成和安全测试。
2. 运行定向测试，确认失败原因与目标行为一致。
3. 实现满足测试的最小完整能力，删除被替代旁路。
4. 运行受影响包测试和 `go test -race`。
5. 运行 `git diff --check`、`go vet ./...` 和相关门禁。
6. 审查 Critical/Important 问题并修复。
7. 使用单一职责提交，记录验证结果。

不得使用 skip、空成功或仅 mock 装配来宣称功能完成。真实服务测试只在用户显式提供的 Profile 下运行，日志不得包含凭据或完整敏感响应。

## Task 26：统一 Instruction / Context Pipeline

**Files:**

- Create: `internal/contextbuilder/types.go`
- Create: `internal/contextbuilder/builder.go`
- Create: `internal/contextbuilder/builder_test.go`
- Create: `internal/contextbuilder/loader.go`
- Create: `internal/contextbuilder/loader_test.go`
- Create: `internal/contextbuilder/budget.go`
- Create: `internal/contextbuilder/budget_test.go`
- Modify: `internal/agent/options.go`
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/engine_test.go`
- Modify: `internal/cli/runtime_builder.go`
- Modify: `internal/cli/runtime_builder_test.go` or nearest composition test
- Modify: `internal/cli/task_runtime.go`
- Modify: `internal/services/token_estimation.go`
- Create: `tests/integration/context_pipeline_test.go`
- Modify: `docs/configuration.md`

### Step 1：定义不可变 Context Plan 契约

先写测试，覆盖：

- Source 具有稳定 ID、Kind、Path、Priority、Trusted 和 Content。
- Build 输入不会被修改，输出切片不与输入共享可变存储。
- 编译期 identity/safety 段始终位于第一层且不能覆盖。
- 相同输入产生字节稳定的系统段和来源顺序。
- 空白段被忽略，重复 Source ID 返回明确错误。

Run: `go test ./internal/contextbuilder -run 'TestBuilder|TestPlan' -count=1`

Expected: FAIL，包或目标类型尚不存在。

实现 `SourceKind`、`Source`、`BuildInput`、`Plan`、`Diagnostic` 和 `Builder`。Plan 输出 `[]core.ContentBlock`，并保留只含路径、摘要、字节数和 Token 估算的来源元数据，不复制密钥或完整调试内容。

### Step 2：实现跨平台指令发现

先写表驱动测试，使用临时目录覆盖：

- 用户指令：`<state>/instructions.md`。
- 项目指令：工作区根 `CYBER.md` 与从根到当前目录逐层的 `.cyber-code/instructions.md`。
- 同层稳定顺序和低到高优先级。
- 不存在文件正常忽略。
- 非普通文件、单文件过大、总量过大返回诊断或错误。
- 指令路径经 `EvalSymlinks` 后逃离允许根时拒绝。
- Windows 风格大小写/分隔符行为由平台辅助函数测试。

Run: `go test ./internal/contextbuilder -run 'TestLoader' -count=1`

Expected: FAIL，Loader 尚未实现。

实现有界 Loader。默认单文件上限 256 KiB、总量上限 1 MiB；读取前后检查文件元数据，避免把目录、设备或符号链接逃逸作为普通指令读取。用户与项目内容标记为不可信策略输入。

### Step 3：实现 Context Token 预算

先写测试，覆盖：

- identity/safety、最近用户消息和未闭合 tool_call/tool_result 永不被静默截断。
- 指令按优先级和配置上限分配预算。
- 超限段产生 `excluded`/`truncated` 诊断和确定性摘要。
- 无法容纳保留内容时返回 `ErrBudgetExceeded`。
- 中英文、JSON Schema 和工具参数使用统一保守估算。

Run: `go test ./internal/contextbuilder -run 'TestBudget' -count=1`

Expected: FAIL，Budget 尚未实现。

复用并收敛 `services.EstimateCoreRequestTokens` 的估算逻辑。不要把 Provider 的精确 CountTokens 放进 Builder，以避免每个工具回合额外发网络请求；精确计数仍由 Compact/usage 路径负责。

### Step 4：让 Agent 每个 Provider 回合使用 Builder

先扩展 Agent 测试，断言：

- 首轮和工具结果后的下一轮都重新 Build。
- Build 错误产生稳定的 configuration/context 错误事件，Provider 不被调用。
- Builder nil 时仍使用仅包含 `product.DefaultSystemPrompt` 的安全默认实现。
- Compact 后的消息与摘要进入下一次 Build。
- Provider 收到多个有序 system blocks，而不是 CLI 拼出的不透明字符串。

Run: `go test ./internal/agent -run 'TestEngine.*Context|TestEngine.*Compact' -count=1`

Expected: FAIL，Engine 仍直接读取 `Options.SystemPrompt`。

在 `agent.Options` 中引入最小 `ContextBuilder` 接口和基础 Build 输入。逐步废弃 `SystemPrompt` 字符串，但在本 Task 内只保留测试/内部兼容适配，正式 CLI 不再使用字符串拼接。

### Step 5：在 CLI 组合根接入用户、项目、Skill 与子 Agent

先写组合测试，断言：

- 正式 Runtime 自动加载用户和项目指令。
- Skills 只作为目录/工具能力呈现，未显式加载的完整 Skill 内容不进入所有请求。
- 主 Agent 与子 Agent 使用同一 Builder 规则，但具有独立运行时 Source。
- 工作目录与项目根解析失败时启动明确失败。
- 恢复会话不会将旧系统提示作为普通历史重复注入。

Run: `go test ./internal/cli -run 'TestCompose.*Context|TestTask.*Context' -count=1`

Expected: FAIL，CLI 仍调用 `systemPromptWithSkills`。

在 `composeRuntime` 中创建 Loader 与 Builder，并注入主 Agent/子 Agent。删除 `systemPromptWithSkills` 及其未使用测试。Skill 工具返回的显式内容作为当前回合工具结果存在，不复制到全局系统提示。

### Step 6：增加 Context Pipeline 集成验收

用本地假 Provider 运行正式 CLI 组合，验证用户指令、根项目指令、嵌套指令、品牌安全段、工具定义和用户消息的最终顺序。增加恶意项目指令尝试更名、启用 bypass 和索取凭据的用例，确认它只能作为不可信文本出现且无法修改 Broker/配置。

Run: `go test ./tests/integration -run 'TestContextPipeline' -count=1 -v`

Expected: PASS。

### Step 7：文档与 Task 26 验证

更新配置文档，说明文件位置、优先级、大小限制、信任边界和调试元数据。不要宣称尚未实现的 `/context` UI；该命令属于 Task 27。

Run:

```bash
gofmt -w internal/contextbuilder internal/agent internal/cli internal/services tests/integration
go test ./internal/contextbuilder ./internal/agent ./internal/cli ./tests/integration -count=1
go test -race ./internal/contextbuilder ./internal/agent ./internal/cli -count=1
go test ./... -count=1
go vet ./...
git diff --check
scripts/check-placeholders.sh
scripts/check-brand.sh
```

Expected: 全部 PASS。

### Step 8：审查与提交

检查 Builder 是否存在可变切片泄漏、非确定性 map 顺序、路径逃逸、指令无限增长、工具调用配对破坏或安全段被覆盖。修复所有 Critical/Important 后提交：

```bash
git add internal/contextbuilder internal/agent internal/cli internal/services tests/integration docs/configuration.md
git commit -m "feat: add layered context pipeline"
```

## Task 27：统一斜杠命令与交互控制面

**Files:**

- Create: `internal/controlplane/command.go`
- Create: `internal/controlplane/registry.go`
- Create: `internal/controlplane/parser.go`
- Create: `internal/controlplane/*_test.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/core/event.go`
- Modify: `internal/frontend/print.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/cli/root.go`
- Create: `tests/integration/controlplane_test.go`

步骤：先测试 Unicode/引号/转义和未知命令解析；实现结构化 Command/Result；让 Runtime 在调用 Agent 前分派命令；实现 `/help`、`/model`、`/context`、`/compact`、`/permissions`、`/hooks`、`/skills`、`/tasks` 和 `/status`；让 TUI/Print 消费同一事件；验证命令不能启用 bypass 或绕过 Broker。

定向验证：

```bash
go test ./internal/controlplane ./internal/runtime ./internal/ui ./internal/frontend -count=1
go test ./tests/integration -run TestControlPlane -count=1 -v
go test -race ./internal/controlplane ./internal/runtime ./internal/ui -count=1
```

提交：`feat: add unified runtime control plane`

## Task 28：会话 Checkpoint、Rewind、Branch 与修复

**Files:**

- Create: `internal/session/graph.go`
- Create: `internal/session/checkpoint.go`
- Create: `internal/session/repair.go`
- Create: `internal/session/*_test.go`
- Modify: `internal/session/snapshot.go`
- Modify: `internal/session/store.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/cli/sessions_cmd.go`
- Modify: `internal/controlplane/registry.go`
- Create: `tests/integration/session_graph_test.go`

步骤：先定义只追加图与稳定 ID；测试 checkpoint 原子提交和并发租约；实现仅对话 rewind；为工作区恢复记录路径、前后摘要和可恢复内容并要求确认；实现 branch 共享祖先但使用独立租约；检测截断 JSONL、孤立快照和索引漂移；加入 `/checkpoint`、`/rewind`、`/branch` 及 sessions inspect/repair；用故障注入验证中断后仍可恢复。

安全规则：默认不覆盖 checkpoint 后被用户修改的文件；冲突必须停止并保留双方内容；所有恢复写入经过 Broker 与审计。

提交：`feat: add transactional session checkpoints`

## Task 29：补齐交互、任务、Web、Notebook 与编辑工具

**Files:**

- Create: `internal/tool/builtin/ask_user.go`
- Create: `internal/tool/builtin/todo.go`
- Create: `internal/tool/builtin/web.go`
- Create: `internal/tool/builtin/notebook.go`
- Create: matching `*_test.go`
- Modify: `internal/tool/builtin/edit.go`
- Modify: `internal/core/event.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/frontend/print.go`
- Modify: `internal/cli/runtime_builder.go`
- Create: `tests/integration/extended_tools_test.go`

步骤：先扩展 Tool Spec 的副作用和恢复属性；实现 AskUser 的暂停/响应协议与 headless 错误；实现持久 Todo 状态机；使用受限 HTTP Client 实现 WebFetch/WebSearch Provider 接口并覆盖 DNS rebinding、重定向和私网 SSRF；使用结构化 JSON Notebook 编辑与原子替换；增强 Edit 的唯一匹配、批量事务和冲突摘要；完成 UI/Print 集成。

提交：`feat: complete interactive and content tools`

## Task 30：分层 Memory 与相关检索

**Files:**

- Create: `internal/memory/types.go`
- Create: `internal/memory/store.go`
- Create: `internal/memory/retriever.go`
- Create: `internal/memory/extractor.go`
- Create: `internal/memory/*_test.go`
- Modify: `internal/contextbuilder/builder.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/controlplane/registry.go`
- Create: `tests/integration/memory_test.go`

步骤：定义 user/project/session/agent/team 作用域；实现原子有界 Store、来源与过期；先采用确定性文本/标签检索；把引用和预算后的 Memory 注入 Context；自动提取只接受低风险事实并拒绝 secret 模式；加入 `/memory list/add/remove/why`；测试恶意 Memory 不能改变权限策略。

提交：`feat: add scoped agent memory`

## Task 31：Agent 定义、协调器、任务板与邮箱

**Files:**

- Create: `internal/collaboration/definition.go`
- Create: `internal/collaboration/board.go`
- Create: `internal/collaboration/mailbox.go`
- Create: `internal/collaboration/coordinator.go`
- Create: `internal/collaboration/*_test.go`
- Modify: `internal/tasks/subagent.go`
- Modify: `internal/tasks/executor.go`
- Modify: `internal/cli/task_runtime.go`
- Modify: `internal/contextbuilder/builder.go`
- Create: `tests/integration/collaboration_test.go`

步骤：发现用户/项目 Agent 定义并验证 Schema；执行权限交集和预算上限；实现持久任务状态机与 mailbox 游标；协调器只通过显式消息/任务接口调度；工作区写租约串行化；加入取消、超时、崩溃恢复和 leader 权限桥接；验证子 Agent 不能扩大模型、工具、路径、网络或权限范围。

提交：`feat: add coordinated agent collaboration`

## Task 32：Windows/Linux 强沙箱

**Files:**

- Create: `internal/platform/sandbox.go`
- Modify/Create: `internal/platform/sandbox_linux.go`
- Create: `internal/platform/sandbox_windows.go`
- Create: platform-specific tests
- Modify: `internal/platform/exec.go`
- Modify: `internal/permissions/broker.go`
- Modify: `internal/config/types.go`
- Modify: `internal/doctor/doctor.go`
- Modify: `docs/security.md`
- Create: `tests/integration/sandbox_test.go`

步骤：定义 off/best-effort/required 和可验证 Capability；Linux 接入 bubblewrap 并测试 mount/network/process 边界；Windows 接入受限 Token、Job Object、ACL 和网络能力检测；让外部进程统一通过 Sandbox Runner；required 缺能力时启动失败；doctor 输出脱敏能力矩阵；在目标 OS CI 运行真实越界拒绝测试。

提交：`feat: enforce cross-platform process sandboxing`

## Task 33：MCP 认证、重连与生命周期

**Files:**

- Modify: `internal/mcp/manager.go`
- Modify: `internal/mcp/http.go`
- Modify: `internal/mcp/stdio.go`
- Create: `internal/mcp/auth.go`
- Create: `internal/mcp/state.go`
- Modify/Create: matching tests
- Modify: `internal/cli/mcp_cmd.go`
- Modify: `internal/doctor/doctor.go`
- Create: `tests/integration/mcp_lifecycle_test.go`

步骤：显式状态机和连接单飞；有界指数退避与 jitter；区分幂等查询和不可重试调用；实现 OAuth discovery/回调抽象及凭据存储；处理 session expiry 和 transport EOF；暴露 status/reconnect/disable；压力测试并发连接、关闭和取消，不泄漏 goroutine/process/token。

提交：`feat: harden MCP connection lifecycle`

## Task 34：结构化 SDK、MCP Server 与 IDE Bridge

**Files:**

- Create: `internal/protocol/types.go`
- Create: `internal/protocol/codec.go`
- Create: `internal/protocol/server.go`
- Create: `internal/protocol/*_test.go`
- Create: `internal/bridge/ide.go`
- Create: `internal/bridge/mcpserver.go`
- Create: `cmd/cyber-code-sdk/main.go` or a `serve` subcommand after interface review
- Modify: `internal/cli/root.go`
- Create: `tests/integration/protocol_test.go`
- Create: `docs/sdk.md`

步骤：定义版本协商和 JSON 消息上限；实现 start/input/permission/cancel/events/status；所有请求映射 Runtime/ControlPlane；实现本机 stdio SDK；将有限会话能力映射为 MCP Server 工具；实现 IDE 文件焦点、选择范围、诊断和 diff 消息；测试畸形帧、慢消费者、断线、重连和权限响应伪造。

提交：`feat: add structured local integration protocol`

## Task 35：最终能力验收与发布

**Files:**

- Create/Modify: `tests/integration/*`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.MD`
- Modify: `docs/configuration.md`
- Modify: `docs/security.md`
- Modify: `docs/providers.md`
- Create: `docs/capability-matrix.md`

步骤：逐项把参考能力标记为 implemented/partial/out-of-scope 并链接测试；运行 Anthropic/OpenAI-compatible 本地契约；使用用户 Profile 对 DeepSeek `deepseek-v4-pro` 做脱敏真实 smoke；Windows/Linux 原生执行 CLI/TUI/沙箱/进程树/会话恢复；清理过期兼容路径和未接线代码；独立完成安全与发布审查。

最终门禁：

```bash
gofmt -w cmd internal tests
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
git diff --check
scripts/check-placeholders.sh
scripts/check-placeholders.sh --self-test
scripts/check-brand.sh
scripts/check-coverage.sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
go build -o dist/cyber-code ./cmd/cli
GOOS=linux GOARCH=amd64 go build -o dist/cyber-code-linux-amd64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o dist/cyber-code-windows-amd64.exe ./cmd/cli
```

Expected: 全部 PASS，无可达已知漏洞，能力矩阵与真实入口一致。

提交：`release: complete cyber-code capability parity`

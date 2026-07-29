# Claude Code Go Productization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将当前可构建但未接通的 Go 原型重构为在 Windows 和 Linux 上稳定运行、支持 Anthropic 与 OpenAI-compatible、并完整接入安全工具链和扩展模块的编码 Agent。

**Architecture:** 新建统一的 core、provider、agent、tool、permissions、session 与 platform 边界，用单一事件流连接 CLI、TUI 和 Print 模式。现有模块在契约测试保护下逐个迁移，旧 API Client、旧 UI 和权限旁路只在替代路径完成后删除。

**Tech Stack:** Go 1.26、Cobra、Bubble Tea、Lip Gloss、标准库 HTTP/SSE/JSON-RPC、Windows Job Object、Linux process group/bubblewrap、GitHub Actions。

---

## 实施约定

- 每个任务使用 `@superpowers:test-driven-development`：先写失败测试，确认失败，再实现。
- 每个任务结束前使用 `@superpowers:verification-before-completion` 运行列出的完整验证命令。
- 默认测试不得访问真实云服务；Provider、MCP、LSP、插件和 OAuth 使用本地测试服务器或伪进程。
- 所有新接口先服务于一条可运行主链路，不并行保留第二套业务状态。
- 提交只包含当前任务相关文件；不要顺手格式化或删除无关旧模块。

### Task 1: 建立干净基线与跨平台 CI

**Files:**
- Modify: `internal/state/manager.go`
- Create: `internal/state/manager_test.go`
- Create: `.github/workflows/ci.yml`
- Create: `scripts/check-placeholders.sh`

**Step 1: 写失败测试证明状态快照不会复制锁**

在 `internal/state/manager_test.go` 创建测试，要求 `Manager.Snapshot()` 返回独立数据快照，并验证修改快照不会改变 Manager 内部状态：

```go
func TestSnapshotReturnsIndependentState(t *testing.T) {
	m := NewManager()
	m.Update(func(s *AppState) { s.SessionID = "original" })

	snapshot := m.Snapshot()
	snapshot.SessionID = "changed"

	if got := m.Snapshot().SessionID; got != "original" {
		t.Fatalf("internal state changed through snapshot: %q", got)
	}
}
```

**Step 2: 运行测试和 vet，确认基线失败**

Run: `go test ./internal/state -run TestSnapshotReturnsIndependentState -v`

Expected: FAIL，因为 `Snapshot` 尚未提供安全语义或仍复制含锁对象。

Run: `go vet ./...`

Expected: FAIL，报告 `assignment copies lock value`。

**Step 3: 分离锁与纯状态数据**

将互斥锁只保留在 `Manager` 中；`AppState` 变为不含锁的纯数据。`Snapshot()` 在读锁内逐字段复制 map/slice，`Update` 在写锁内修改。

**Step 4: 添加 CI 和 placeholder 门禁**

`.github/workflows/ci.yml` 使用 `ubuntu-latest` 与 `windows-latest` 矩阵运行：

```yaml
- run: go test ./...
- run: go vet ./...
- run: go build ./cmd/cli
```

`scripts/check-placeholders.sh` 仅检查已迁移目录，拒绝 `not implemented`、`placeholder response` 和空成功实现。

**Step 5: 验证**

Run: `gofmt -w internal/state/manager.go internal/state/manager_test.go`

Run: `go test ./internal/state -v && go test ./... && go vet ./...`

Expected: PASS，vet 无输出。

**Step 6: 提交**

```bash
git add internal/state/manager.go internal/state/manager_test.go .github/workflows/ci.yml scripts/check-placeholders.sh
git commit -m "test: establish clean cross-platform baseline"
```

### Task 2: 定义统一消息、事件与错误模型

**Files:**
- Create: `internal/core/message.go`
- Create: `internal/core/event.go`
- Create: `internal/core/errors.go`
- Create: `internal/core/core_test.go`

**Step 1: 写核心模型序列化失败测试**

覆盖文本、思考、工具调用和工具结果的 JSON 往返，并要求错误分类保留原因：

```go
func TestEventJSONRoundTripToolCall(t *testing.T) {
	want := Event{Type: EventToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"README.MD"}`)}}
	b, err := json.Marshal(want)
	if err != nil { t.Fatal(err) }
	var got Event
	if err := json.Unmarshal(b, &got); err != nil { t.Fatal(err) }
	if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
}
```

不要引入 `go-cmp`；使用字段级断言，保持依赖最小。

**Step 2: 运行测试确认失败**

Run: `go test ./internal/core -v`

Expected: FAIL，package 或类型不存在。

**Step 3: 实现规范类型**

`internal/core` 至少定义：

```go
type Message struct { Role Role; Content []ContentBlock }
type ToolCall struct { ID string; Name string; Arguments json.RawMessage }
type ToolResult struct { ToolCallID string; Content []ContentBlock; IsError bool }
type Event struct { Type EventType; Text string; ToolCall *ToolCall; Usage *Usage; Err *Error }
type Error struct { Kind ErrorKind; Op string; Message string; Retryable bool; Cause error }
```

为 Error 实现 `error`、`Unwrap()` 和脱敏后的用户消息。禁止 Provider 类型泄漏到 core。

**Step 4: 验证并提交**

Run: `gofmt -w internal/core && go test ./internal/core -v && go vet ./internal/core`

Expected: PASS。

```bash
git add internal/core
git commit -m "feat: add canonical agent message model"
```

### Task 3: 实现 Profile 配置与凭据解析

**Files:**
- Create: `internal/config/types.go`
- Create: `internal/config/loader.go`
- Create: `internal/config/validate.go`
- Create: `internal/config/loader_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: 写配置优先级测试**

使用 `t.TempDir()` 创建用户和项目 YAML，验证优先级：CLI > env > project > user > default；验证配置输出不包含解析后的 API Key。

```go
func TestLoadPrecedence(t *testing.T) {
	t.Setenv("CLAUDE_GO_PROFILE", "env-profile")
	got, err := Load(LoadOptions{UserFile: userFile, ProjectFile: projectFile, CLI: Overrides{Profile: "cli-profile"}})
	if err != nil { t.Fatal(err) }
	if got.ActiveProfile != "cli-profile" { t.Fatalf("got %q", got.ActiveProfile) }
}
```

**Step 2: 确认失败**

Run: `go test ./internal/config -v`

Expected: FAIL，loader 不存在。

**Step 3: 实现配置模型**

```go
type Profile struct {
	Provider string `yaml:"provider"`
	BaseURL string `yaml:"base_url,omitempty"`
	Model string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}
type Config struct {
	ActiveProfile string `yaml:"active_profile"`
	Profiles map[string]Profile `yaml:"profiles"`
	PermissionMode string `yaml:"permission_mode,omitempty"`
}
```

使用 `gopkg.in/yaml.v3` 严格解析未知字段。提供 `ResolveCredential(profile)`，仅在构造 Provider 时读取环境变量。验证 Base URL scheme、profile 引用和允许的权限模式。

**Step 4: 验证并提交**

Run: `gofmt -w internal/config && go test ./internal/config -v && go vet ./internal/config`

Expected: PASS。

```bash
git add go.mod go.sum internal/config
git commit -m "feat: add layered provider configuration"
```

### Task 4: 定义 Provider 契约与本地契约测试工具

**Files:**
- Create: `internal/provider/provider.go`
- Create: `internal/provider/registry.go`
- Create: `internal/provider/testkit/server.go`
- Create: `internal/provider/provider_test.go`

**Step 1: 写 Registry 和取消测试**

伪 Provider 必须支持注册、按名称创建、流式事件和 context 取消：

```go
func TestRegistryRejectsDuplicateProvider(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("fake", fakeFactory); err != nil { t.Fatal(err) }
	if err := r.Register("fake", fakeFactory); err == nil { t.Fatal("expected duplicate error") }
}
```

**Step 2: 确认失败**

Run: `go test ./internal/provider -v`

Expected: FAIL。

**Step 3: 实现契约**

```go
type Capabilities struct { Streaming, ToolCalls, Thinking, TokenCounting bool }
type Provider interface {
	Name() string
	Capabilities(context.Context) (Capabilities, error)
	Stream(context.Context, core.Request) (<-chan core.Event, error)
	CountTokens(context.Context, core.Request) (int, error)
}
```

Registry 只保存 factory；testkit 提供可脚本化的 `httptest.Server`、SSE 写入器和请求捕获器。

**Step 4: 验证并提交**

Run: `gofmt -w internal/provider && go test ./internal/provider/... -v`

Expected: PASS。

```bash
git add internal/provider
git commit -m "feat: define provider contract and testkit"
```

### Task 5: 实现 Anthropic Provider

**Files:**
- Create: `internal/provider/anthropic/client.go`
- Create: `internal/provider/anthropic/codec.go`
- Create: `internal/provider/anthropic/stream.go`
- Create: `internal/provider/anthropic/client_test.go`

**Step 1: 写契约测试**

本地服务器断言 `/v1/messages`、`x-api-key`、`anthropic-version`、system、tools 和 messages；返回文本与工具调用 SSE，断言得到规范 `core.Event`。另测 401 不重试、429 可重试、取消立即关闭流。

**Step 2: 确认失败**

Run: `go test ./internal/provider/anthropic -v`

Expected: FAIL。

**Step 3: 实现请求与 SSE 解析**

使用逐行 SSE parser，只处理 `event:` 与 `data:` 帧；禁止用 `json.Decoder` 直接解析 SSE。所有响应体设大小上限，错误通过 `core.Error` 分类。重试服从 Retry-After、指数退避和 context。

**Step 4: 验证并提交**

Run: `go test ./internal/provider/anthropic -v -race`

Expected: PASS，无 goroutine 泄漏。

```bash
git add internal/provider/anthropic
git commit -m "feat: implement Anthropic provider"
```

### Task 6: 实现 OpenAI-compatible Provider

**Files:**
- Create: `internal/provider/openai/client.go`
- Create: `internal/provider/openai/codec.go`
- Create: `internal/provider/openai/stream.go`
- Create: `internal/provider/openai/client_test.go`

**Step 1: 写 Chat Completions 契约测试**

验证自定义 Base URL、Bearer Token、SSE `[DONE]`、并行 `tool_calls` 参数增量、usage 和 finish_reason。加入“后端不支持工具调用”能力探测测试。

**Step 2: 确认失败**

Run: `go test ./internal/provider/openai -v`

Expected: FAIL。

**Step 3: 实现适配器**

将 core messages 映射为 `/v1/chat/completions` 格式；按 tool call index 聚合 ID、name 和 JSON arguments；结束时验证 arguments 为合法 JSON。Base URL 使用 `url.JoinPath`，不丢失反向代理前缀。

**Step 4: 验证并提交**

Run: `go test ./internal/provider/openai -v -race`

Expected: PASS。

```bash
git add internal/provider/openai
git commit -m "feat: implement OpenAI compatible provider"
```

### Task 7: 接入 OAuth 与云 Provider 认证

**Files:**
- Create: `internal/credential/store.go`
- Create: `internal/credential/file_store.go`
- Create: `internal/credential/file_store_test.go`
- Create: `internal/provider/bedrock/provider.go`
- Create: `internal/provider/vertex/provider.go`
- Create: `internal/provider/azure/provider.go`
- Create: `internal/provider/cloud_contract_test.go`
- Modify: `internal/services/oauth/client.go`
- Reuse/Modify: `internal/services/api/auth_aws.go`
- Reuse/Modify: `internal/services/api/auth_vertex.go`
- Reuse/Modify: `internal/services/api/auth_azure.go`

**Step 1: 写凭据存储和云认证测试**

测试 token 文件仅用户可读、写入原子化、刷新并发只发生一次；用本地 HTTP server 验证 AWS 签名稳定向量、Vertex token refresh 和 Azure service principal/managed identity 请求。

**Step 2: 确认失败**

Run: `go test ./internal/credential ./internal/provider/bedrock ./internal/provider/vertex ./internal/provider/azure -v`

Expected: FAIL。

**Step 3: 实现统一 CredentialStore 与云适配**

CredentialStore 不向日志返回 secret。Linux 文件权限要求 `0600`；Windows 使用 ACL 限定当前用户。将现有认证函数改成显式依赖注入，不读取全局状态。Provider 复用 core 事件契约。

**Step 4: 验证并提交**

Run: `go test ./internal/credential ./internal/provider/... ./internal/services/oauth ./internal/services/api -race`

Expected: PASS。

```bash
git add internal/credential internal/provider/bedrock internal/provider/vertex internal/provider/azure internal/services/oauth internal/services/api
git commit -m "feat: add OAuth and cloud provider authentication"
```

### Task 8: 实现 Runtime 生命周期与无工具 Agent 回合

**Files:**
- Create: `internal/agent/engine.go`
- Create: `internal/agent/options.go`
- Create: `internal/agent/engine_test.go`
- Create: `internal/runtime/runtime.go`
- Create: `internal/runtime/runtime_test.go`

**Step 1: 写 Fake Provider 回合测试**

Fake Provider 依次发送 text、usage、done；断言 Engine 输出顺序、历史写入、最大轮次和取消行为。

```go
func TestEngineStreamsTextAndCompletion(t *testing.T) {
	engine := NewEngine(fakeProvider(text("hello"), done()), Dependencies{})
	events := collect(engine.Run(context.Background(), "hi"))
	assertEventTypes(t, events, core.EventUserMessage, core.EventTextDelta, core.EventCompleted)
}
```

**Step 2: 确认失败**

Run: `go test ./internal/agent ./internal/runtime -v`

Expected: FAIL。

**Step 3: 实现单一事件流**

Engine 拥有消息历史但不拥有 UI；Runtime 负责 Provider、Engine 和服务生命周期。所有 goroutine 由传入 context 管理，并在输出 channel 关闭前退出。

**Step 4: 验证并提交**

Run: `go test ./internal/agent ./internal/runtime -race -v`

Expected: PASS。

```bash
git add internal/agent internal/runtime
git commit -m "feat: add canonical agent runtime"
```

### Task 9: 实现默认拒绝的 Permission Broker

**Files:**
- Create: `internal/permissions/broker.go`
- Create: `internal/permissions/policy.go`
- Create: `internal/permissions/path.go`
- Create: `internal/permissions/audit.go`
- Create: `internal/permissions/broker_test.go`
- Modify: `internal/permissions/manager.go`

**Step 1: 写权限矩阵与逃逸测试**

覆盖 default、plan、accept-edits、bypass；测试 `..`、符号链接逃逸、项目配置尝试开启 bypass、headless ask 转 deny、子 Agent 尝试升级权限。

**Step 2: 确认失败**

Run: `go test ./internal/permissions -v`

Expected: FAIL，当前 BaseTool 永远 Allow。

**Step 3: 实现 Broker**

```go
type Request struct { Tool, Action, Workspace, Command string; Paths []string; Network []string }
type Decision struct { Behavior Behavior; Reason string; RuleID string }
type Confirmer func(context.Context, Request) (Decision, error)
```

规则评估顺序固定为 deny、显式 allow、模式默认、ask。项目级规则只能缩小权限。审计记录不得包含完整 secret 或文件内容。

**Step 4: 验证并提交**

Run: `go test ./internal/permissions -race -v && go vet ./internal/permissions`

Expected: PASS。

```bash
git add internal/permissions
git commit -m "feat: enforce permission broker"
```

### Task 10: 建立 Tool 契约、Registry 与安全文件工具

**Files:**
- Create: `internal/tool/tool.go`
- Create: `internal/tool/registry.go`
- Create: `internal/tool/runner.go`
- Create: `internal/tool/registry_test.go`
- Create: `internal/tool/builtin/file.go`
- Create: `internal/tool/builtin/search.go`
- Create: `internal/tool/builtin/file_test.go`

**Step 1: 写 Registry、权限和原子写测试**

测试重复工具拒绝、Schema 验证、每次执行均调用 Broker、越界路径拒绝、符号链接拒绝、原子替换、编辑匹配 0/多次时不写文件。

**Step 2: 确认失败**

Run: `go test ./internal/tool/... -v`

Expected: FAIL。

**Step 3: 实现 Tool 接口**

```go
type Spec struct { Name, Description string; Schema json.RawMessage; ReadOnly, ConcurrencySafe bool }
type Tool interface {
	Spec() Spec
	Authorize(context.Context, json.RawMessage) (permissions.Request, error)
	Run(context.Context, json.RawMessage) (core.ToolResult, error)
}
```

Runner 依次执行参数校验、Authorize、Broker、Hooks（后续注入）、Run 和结果截断。文件写使用同目录临时文件、fsync 与 rename。

**Step 4: 验证并提交**

Run: `go test ./internal/tool/... -race -v`

Expected: PASS。

```bash
git add internal/tool
git commit -m "feat: add secure tool registry and file tools"
```

### Task 11: 实现 Windows/Linux Shell 平台层

**Files:**
- Create: `internal/platform/exec.go`
- Create: `internal/platform/exec_linux.go`
- Create: `internal/platform/exec_windows.go`
- Create: `internal/platform/sandbox_linux.go`
- Create: `internal/platform/exec_test.go`
- Create: `internal/tool/builtin/shell.go`
- Create: `internal/tool/builtin/shell_test.go`

**Step 1: 写 Shell 安全与取消测试**

测试命令解析结果进入权限请求；超时/取消终止整个进程树；环境变量按 allowlist/denylist 脱敏；工作目录固定；缺少 bubblewrap 时报告 policy-only 隔离。

**Step 2: 确认失败**

Run: `go test ./internal/platform ./internal/tool/builtin -run 'Shell|Process' -v`

Expected: FAIL。

**Step 3: 实现平台 Runner**

Linux 使用独立 process group 并在取消时终止进程组，可选通过 bubblewrap 限定工作区。Windows 使用 Job Object 绑定子进程树。ShellTool 不直接调用 `exec.Command`，只调用 platform Runner。

**Step 4: 交叉编译验证**

Run: `GOOS=linux GOARCH=amd64 go test ./internal/platform ./internal/tool/builtin -c`

Run: `GOOS=windows GOARCH=amd64 go test ./internal/platform ./internal/tool/builtin -c`

Expected: 两个命令成功生成测试二进制。

**Step 5: 提交**

```bash
git add internal/platform internal/tool/builtin/shell.go internal/tool/builtin/shell_test.go
git commit -m "feat: add cross-platform shell execution"
```

### Task 12: 打通安全工具调用 Agent Loop

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/options.go`
- Create: `internal/agent/tool_loop_test.go`
- Modify: `internal/runtime/runtime.go`

**Step 1: 写完整工具循环测试**

Fake Provider 第一轮发出 read_file 与 shell tool calls，第二轮返回总结。断言 read_file 可自动允许、shell 触发 confirmer、工具结果按原 ID 回传、被拒绝的调用返回 `is_error` 且不会执行。

**Step 2: 确认失败**

Run: `go test ./internal/agent -run ToolLoop -v`

Expected: FAIL。

**Step 3: 实现循环**

Agent 对工具调用按 Spec 分组：只读且并发安全的调用可并行，其他串行。每一批结果稳定按原调用顺序写回历史。达到轮次、预算或取消时产生唯一终止事件。

**Step 4: 验证并提交**

Run: `go test ./internal/agent ./internal/runtime ./internal/tool/... -race -v`

Expected: PASS。

```bash
git add internal/agent internal/runtime
git commit -m "feat: connect permissioned agent tool loop"
```

### Task 13: 实现会话事件日志、恢复与导出

**Files:**
- Create: `internal/session/store.go`
- Create: `internal/session/eventlog.go`
- Create: `internal/session/snapshot.go`
- Create: `internal/session/export.go`
- Create: `internal/session/store_test.go`
- Modify: `internal/runtime/runtime.go`

**Step 1: 写崩溃恢复测试**

测试追加事件、半写 JSONL 尾部恢复、原子快照、并发会话隔离、resume 恢复历史、导出时凭据脱敏。

**Step 2: 确认失败**

Run: `go test ./internal/session -v`

Expected: FAIL。

**Step 3: 实现 Store**

事件包含递增序号、会话 ID、时间和 core Event。写入先编码完整行再单次 append；快照写临时文件并原子 rename。恢复忽略最后一条不完整记录，但拒绝中间损坏。

**Step 4: 验证并提交**

Run: `go test ./internal/session ./internal/runtime -race -v`

Expected: PASS。

```bash
git add internal/session internal/runtime/runtime.go
git commit -m "feat: persist and resume agent sessions"
```

### Task 14: 实现 Token、成本与 Compact

**Files:**
- Create: `internal/session/compact.go`
- Create: `internal/session/usage.go`
- Create: `internal/session/compact_test.go`
- Reuse/Modify: `internal/services/compact.go`
- Reuse/Modify: `internal/services/token_estimation.go`
- Modify: `internal/agent/engine.go`

**Step 1: 写压缩不丢失原始记录测试**

测试超过阈值触发 compact、保留最近工具上下文、摘要作为派生事件、原始 JSONL 未删除、Provider 精确 token count 优先于估算、价格按模型配置。

**Step 2: 确认失败**

Run: `go test ./internal/session -run 'Compact|Usage' -v`

Expected: FAIL。

**Step 3: 实现 Compact 服务**

Compact 输入为不可变消息快照，输出摘要消息和覆盖到的事件序号。失败时继续使用原历史并发出可见警告，不覆盖快照。

**Step 4: 验证并提交**

Run: `go test ./internal/session ./internal/agent -race -v`

Expected: PASS。

```bash
git add internal/session internal/services/compact.go internal/services/token_estimation.go internal/agent/engine.go
git commit -m "feat: add session compaction and usage tracking"
```

### Task 15: 接入 Hooks

**Files:**
- Modify: `internal/hooks/hooks.go`
- Create: `internal/hooks/runner.go`
- Create: `internal/hooks/runner_test.go`
- Modify: `internal/tool/runner.go`
- Modify: `internal/runtime/runtime.go`

**Step 1: 写 Hook 顺序、超时与拒绝测试**

验证 SessionStart、UserPromptSubmit、PreToolUse、PostToolUse、Stop 顺序；PreToolUse 可拒绝但不能允许 Broker 已拒绝的操作；超时取消 hook 子进程。

**Step 2: 确认失败**

Run: `go test ./internal/hooks ./internal/tool -run Hook -v`

Expected: FAIL。

**Step 3: 实现 Hook Runner**

Hook 结果只允许：继续、拒绝、对输入做受约束转换、附加上下文。权限结果采用交集，任何 hook 不得提升权限。输出大小和执行时间有上限。

**Step 4: 验证并提交**

Run: `go test ./internal/hooks ./internal/tool ./internal/runtime -race -v`

Expected: PASS。

```bash
git add internal/hooks internal/tool/runner.go internal/runtime/runtime.go
git commit -m "feat: connect lifecycle and tool hooks"
```

### Task 16: 接入 MCP 工具与资源

**Files:**
- Create: `internal/mcp/types.go`
- Create: `internal/mcp/transport.go`
- Create: `internal/mcp/stdio.go`
- Create: `internal/mcp/http.go`
- Create: `internal/mcp/manager.go`
- Create: `internal/mcp/manager_test.go`
- Reuse/Modify: `internal/services/mcp/*`
- Modify: `internal/runtime/runtime.go`

**Step 1: 写伪 MCP Server 集成测试**

伪进程/HTTP server 实现 initialize、tools/list、tools/call、resources/list、resources/read。测试连接、重连、超时、非法 JSON-RPC、工具名冲突、OAuth header 脱敏和权限映射。

**Step 2: 确认失败**

Run: `go test ./internal/mcp -v`

Expected: FAIL。

**Step 3: 实现 Manager 与 Registry 桥接**

每个 MCP 连接拥有 context 和健康状态；发现的工具使用 `mcp__server__tool` 稳定命名并转换成 Tool 接口。MCP 子进程和网络访问先经过 Permission Broker。

**Step 4: 验证并提交**

Run: `go test ./internal/mcp ./internal/runtime ./internal/tool/... -race -v`

Expected: PASS。

```bash
git add internal/mcp internal/services/mcp internal/runtime/runtime.go
git commit -m "feat: integrate MCP tools and resources"
```

### Task 17: 实现插件与 Skill 生命周期

**Files:**
- Create: `internal/plugin/manifest.go`
- Create: `internal/plugin/manager.go`
- Create: `internal/plugin/process.go`
- Create: `internal/plugin/manager_test.go`
- Create: `internal/skill/loader.go`
- Create: `internal/skill/loader_test.go`
- Reuse/Modify: `internal/services/plugin_loader.go`
- Reuse/Modify: `internal/services/plugins.go`

**Step 1: 写恶意 manifest 与生命周期测试**

测试未知字段、越界入口、重复工具、未声明能力、启动失败、停止超时、热重载回滚和 Skill 指令发现顺序。

**Step 2: 确认失败**

Run: `go test ./internal/plugin ./internal/skill -v`

Expected: FAIL。

**Step 3: 实现隔离式 Manager**

插件 manifest 明确声明文件、网络、进程与工具能力；Manager 将声明转换为 Permission 请求。插件默认子进程运行，通过有大小限制的 JSON-RPC 通道调用，不获得 Registry 指针。

**Step 4: 验证并提交**

Run: `go test ./internal/plugin ./internal/skill ./internal/runtime -race -v`

Expected: PASS。

```bash
git add internal/plugin internal/skill internal/services/plugin_loader.go internal/services/plugins.go
git commit -m "feat: add isolated plugins and skills"
```

### Task 18: 实现 Tasks、后台任务与子 Agent

**Files:**
- Modify: `internal/tasks/types.go`
- Modify: `internal/tasks/framework.go`
- Modify: `internal/tasks/executor.go`
- Create: `internal/tasks/registry.go`
- Create: `internal/tasks/executor_test.go`
- Modify: `internal/runtime/runtime.go`

**Step 1: 写状态机与权限继承测试**

测试 pending/running/completed/failed/cancelled 合法转换；后台 Shell 取消；子 Agent 预算上限；父级 plan 权限不能生成 accept-edits 子任务；Runtime Shutdown 回收所有任务。

**Step 2: 确认失败**

Run: `go test ./internal/tasks -v`

Expected: FAIL。

**Step 3: 实现 Registry/Executor**

Registry 不暴露可变 Task 指针；状态更新通过方法和事件。子 Agent 使用新的 Engine 实例、历史副本、Provider 引用和权限子集。后台输出有上限并可分页读取。

**Step 4: 验证并提交**

Run: `go test ./internal/tasks ./internal/runtime ./internal/agent -race -v`

Expected: PASS。

```bash
git add internal/tasks internal/runtime/runtime.go
git commit -m "feat: add managed tasks and sub-agents"
```

### Task 19: 接入 LSP 生命周期与工具

**Files:**
- Create: `internal/lsp/protocol.go`
- Create: `internal/lsp/client.go`
- Create: `internal/lsp/manager.go`
- Create: `internal/lsp/tools.go`
- Create: `internal/lsp/manager_test.go`
- Reuse/Modify: `internal/services/lsp_client.go`
- Reuse/Modify: `internal/services/lsp_server_manager.go`

**Step 1: 写伪 LSP Server 测试**

伪进程支持 initialize、diagnostics、definition、references、completion、hover。测试 framing、请求关联、超时、崩溃重启、工作区隔离和取消。

**Step 2: 确认失败**

Run: `go test ./internal/lsp -v`

Expected: FAIL。

**Step 3: 实现 Manager 与 Tool 适配**

使用 Content-Length framing；Manager 按 workspace/language 复用 server，指数退避重启并限制频率。LSP 进程启动经过 Permission Broker，LSP tools 作为只读工具注册。

**Step 4: 验证并提交**

Run: `go test ./internal/lsp ./internal/tool/... -race -v`

Expected: PASS。

```bash
git add internal/lsp internal/services/lsp_client.go internal/services/lsp_server_manager.go
git commit -m "feat: integrate managed LSP tools"
```

### Task 20: 统一 TUI 与 Print 前端

**Files:**
- Replace: `internal/ui/app.go`
- Replace: `internal/ui/ui.go`
- Modify: `internal/ui/components/chat.go`
- Modify: `internal/ui/components/dialog.go`
- Create: `internal/ui/app_test.go`
- Create: `internal/frontend/print.go`
- Create: `internal/frontend/print_test.go`
- Modify: `internal/cli/app.go`

**Step 1: 写 UI 事件测试**

模拟按 Enter 发送 prompt，断言 Runtime 收到请求；模拟 text/tool/permission/completed 事件，断言状态与渲染；Print 模式分别测试 text、JSON 和退出码。

**Step 2: 确认失败**

Run: `go test ./internal/ui ./internal/frontend ./internal/cli -v`

Expected: FAIL；当前简单 Model 不提交查询。

**Step 3: 只保留一个 Bubble Tea Model**

Model 通过命令发送 Runtime 请求和读取事件，不从 goroutine 直接修改 UI 状态。权限询问产生可测试的 dialog message。Print frontend 不输出 TUI 控制字符，并将认证/配置错误映射为非零退出码。

**Step 4: 验证并提交**

Run: `go test ./internal/ui ./internal/frontend ./internal/cli -race -v`

Expected: PASS。

```bash
git add internal/ui internal/frontend internal/cli/app.go
git commit -m "feat: unify interactive and print frontends"
```

### Task 21: 接入 Vim 输入模式

**Files:**
- Modify: `internal/vim/types.go`
- Modify: `internal/vim/transitions.go`
- Create: `internal/vim/transitions_test.go`
- Modify: `internal/ui/components/input.go`
- Modify: `internal/ui/app_test.go`

**Step 1: 写状态转换表测试**

表驱动覆盖 normal/insert/visual、h/j/k/l、word motion、delete/change/yank、undo、repeat、查找和 Esc；验证 Unicode rune 索引而非 byte 索引。

**Step 2: 确认失败**

Run: `go test ./internal/vim ./internal/ui -run Vim -v`

Expected: FAIL。

**Step 3: 将 Vim 状态接入唯一 Input Model**

Vim transition 返回纯 action，不直接修改全局 UI；Input Model 应用 action 并维护 undo/redo。配置切换立即生效且不丢失输入。

**Step 4: 验证并提交**

Run: `go test ./internal/vim ./internal/ui -race -v`

Expected: PASS。

```bash
git add internal/vim internal/ui/components/input.go internal/ui/app_test.go
git commit -m "feat: integrate Vim input mode"
```

### Task 22: 实现通知与语音平台适配

**Files:**
- Create: `internal/platform/notify.go`
- Create: `internal/platform/notify_linux.go`
- Create: `internal/platform/notify_windows.go`
- Create: `internal/platform/voice.go`
- Create: `internal/platform/voice_linux.go`
- Create: `internal/platform/voice_windows.go`
- Create: `internal/platform/media_test.go`
- Reuse/Modify: `internal/services/notifier.go`
- Reuse/Modify: `internal/voice/voice.go`

**Step 1: 写能力检测和降级测试**

注入伪 command finder/runner，测试 Linux `notify-send`/录音后端、Windows toast/ffmpeg 后端；缺失依赖返回 unavailable 诊断而非 panic；取消录音清理临时文件。

**Step 2: 确认失败**

Run: `go test ./internal/platform ./internal/voice ./internal/services -run 'Notify|Voice' -v`

Expected: FAIL。

**Step 3: 实现平台接口**

通知和语音都通过 Runtime capability 注册。录音文件位于安全临时目录，大小/时长受限，转录 Provider 可替换。所有外部进程通过 platform Runner 和 Permission Broker。

**Step 4: 交叉编译与提交**

Run: `GOOS=linux GOARCH=amd64 go test ./internal/platform ./internal/voice -c`

Run: `GOOS=windows GOARCH=amd64 go test ./internal/platform ./internal/voice -c`

Expected: PASS。

```bash
git add internal/platform internal/services/notifier.go internal/voice
git commit -m "feat: add Windows and Linux notification and voice adapters"
```

### Task 23: 实现真实 CLI 管理命令与 doctor

**Files:**
- Replace: `cmd/cli/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/config_cmd.go`
- Create: `internal/cli/doctor_cmd.go`
- Create: `internal/cli/mcp_cmd.go`
- Create: `internal/cli/plugins_cmd.go`
- Create: `internal/cli/sessions_cmd.go`
- Create: `internal/doctor/doctor.go`
- Create: `internal/doctor/doctor_test.go`
- Create: `internal/cli/commands_test.go`

**Step 1: 写命令级测试**

使用注入的 Runtime/FS 检查 `config list/get/set/validate`、`doctor --json`、`mcp list/add/remove/test`、`plugins list/enable/disable`、`sessions list/resume/export/delete`。断言待办文本不再出现。

**Step 2: 确认失败**

Run: `go test ./internal/cli ./internal/doctor ./cmd/cli -v`

Expected: FAIL，当前命令仅打印 placeholder。

**Step 3: 实现 composition root**

`Execute(ctx, stdin, stdout, stderr, args)` 返回退出码，便于测试。doctor 检查配置语法、凭据存在性、Provider 能力、MCP/LSP 健康、沙箱、通知/语音依赖和目录权限，JSON 输出使用稳定 schema。

**Step 4: 验证并提交**

Run: `go test ./internal/cli ./internal/doctor ./cmd/cli -race -v`

Run: `go run ./cmd/cli doctor --json`

Expected: 命令输出结构化检查结果；缺少可选依赖不导致崩溃。

```bash
git add cmd/cli/main.go internal/cli internal/doctor
git commit -m "feat: implement operational CLI commands"
```

### Task 24: 安全回归与秘密脱敏

**Files:**
- Create: `internal/security/redact.go`
- Create: `internal/security/redact_test.go`
- Create: `tests/security/path_escape_test.go`
- Create: `tests/security/permission_escalation_test.go`
- Create: `tests/security/secret_leak_test.go`
- Modify: `internal/core/errors.go`
- Modify: `internal/permissions/audit.go`
- Modify: `internal/provider/anthropic/client.go`
- Modify: `internal/provider/openai/client.go`

**Step 1: 写攻击用例**

覆盖目录穿越、符号链接交换、Shell 重定向越界、子 Agent 权限升级、恶意 MCP/插件返回指令、Authorization/API Key/Cookie 在日志与错误中泄漏。

**Step 2: 运行并确认至少一个用例失败**

Run: `go test ./tests/security ./internal/security -v`

Expected: FAIL，直到所有边界统一接入 redactor/policy。

**Step 3: 实现集中脱敏与缺口修复**

Redactor 同时处理结构化 header 和文本中的已知 secret 值；Provider、audit、doctor、CLI 只接收已脱敏错误。不要用宽泛正则修改普通源代码输出。

**Step 4: 验证并提交**

Run: `go test ./tests/security ./internal/security ./internal/permissions ./internal/provider/... -race -v`

Expected: PASS。

```bash
git add internal/security tests/security internal/core/errors.go internal/permissions/audit.go internal/provider
git commit -m "security: harden trust boundaries and redaction"
```

### Task 25: 端到端验收、清理旧路径与发布文档

**Files:**
- Create: `tests/integration/agent_anthropic_test.go`
- Create: `tests/integration/agent_openai_test.go`
- Create: `tests/integration/mcp_lsp_plugin_test.go`
- Create: `tests/integration/session_task_test.go`
- Create: `tests/integration/cli_smoke_test.go`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.MD`
- Create: `docs/configuration.md`
- Create: `docs/security.md`
- Create: `docs/providers.md`
- Remove after replacement: `pkg/api/client.go`
- Remove after replacement: obsolete code in `internal/query/engine.go`
- Remove after replacement: duplicate UI path in `internal/ui/ui.go` or `internal/ui/app.go`
- Remove after replacement: placeholder tools in `internal/tools/missing_tools.go`
- Remove after replacement: root `main.go`

**Step 1: 写端到端验收测试**

本地伪服务必须验证：两个 Provider 的文本与工具回合、权限确认/拒绝、MCP 资源、LSP 查询、插件工具、Hooks、子 Agent、Compact、会话恢复、Print JSON、TUI smoke、通知/语音降级。

**Step 2: 运行测试，记录尚未接线的失败**

Run: `go test ./tests/integration -v`

Expected: 初次 FAIL，并准确列出未接线能力；不得通过 skip 隐藏必需能力。

**Step 3: 删除已替代旧路径**

先用 `rg` 证明无新代码引用，再逐个删除重复 API Client、旧 QueryEngine、旧 UI 和 placeholder 工具。更新 README，仅保留测试已覆盖功能。保留法律声明，并明确重新分发前需要许可证审查。

**Step 4: 完整发布门禁**

Run: `git ls-files -z '*.go' | xargs -0 gofmt -w`

Run: `go test ./...`

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `go build ./cmd/cli`

Run: `mkdir -p dist`

Run: `GOOS=linux GOARCH=amd64 go build -o dist/claude-go-linux-amd64 ./cmd/cli`

Run: `GOOS=windows GOARCH=amd64 go build -o dist/claude-go-windows-amd64.exe ./cmd/cli`

Run: `scripts/check-placeholders.sh`

Expected: 全部 PASS；目标目录中无 placeholder/待办成功路径。

**Step 5: 覆盖率与漏洞检查**

Run: `go test -coverprofile=coverage.out ./...`

Run: `go tool cover -func=coverage.out`

Expected: core/agent/provider/tool/session 核心包至少 80%，permissions/security 至少 90%。

Run: `govulncheck ./...`

Expected: 无可达已知漏洞。

**Step 6: 提交**

```bash
git add .github README.MD docs cmd internal tests go.mod go.sum
git commit -m "release: complete Windows and Linux agent productization"
```

## 最终人工验收清单

在默认 CI 通过后，使用用户控制的测试凭据执行以下 opt-in 验收，结果只记录状态，不记录密钥或完整响应：

1. Anthropic 原生 API：流式文本、工具调用、限流恢复、取消。
2. 一个 OpenAI-compatible 后端：自定义 Base URL、模型选择、工具调用和流式输出。
3. Bedrock、Vertex、Azure：分别验证至少一种受支持认证路径。
4. Windows：TUI、PowerShell、进程树取消、文件权限、通知和语音能力检测。
5. Linux：TUI、Shell、进程组取消、bubblewrap 可用/不可用两种路径、通知和语音能力检测。
6. MCP stdio 与 HTTP/SSE、LSP、插件、Hooks、子 Agent 和恢复会话。
7. 检查审计日志、错误、doctor JSON 和导出会话中没有 secret。

真实服务验收失败时不得修改测试去适应错误行为；应保存脱敏后的最小复现并回到对应任务修复。

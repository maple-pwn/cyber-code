# Subagent Observability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add complete real-time subagent observability with a full-screen switchable TUI view, task list, lifecycle/status data, and an accurate `/tasks` command.

**Architecture:** Child engines continue producing provider-independent `core.Event` values. `tasks.ToolService` wraps those events with task identity, updates bounded task snapshots, and publishes them through a non-blocking observer hub; `runtime.Runtime` exposes the owned source through a separate observation stream so background task events outlive a parent turn without keeping that turn open. The TUI routes observed events into isolated per-task views and switches the whole conversation viewport between the parent and selected child.

**Tech Stack:** Go 1.26, Bubble Tea, existing `core`, `agent`, `tasks`, `runtime`, `controlplane`, and `ui` packages.

---

### Task 1: Define canonical subagent observation events

**Files:**
- Modify: `internal/core/event.go`
- Modify: `internal/core/core_test.go`

**Step 1: Write the failing test**

Add a serialization and deep-copy test for a lifecycle event carrying task ID, agent, description, status, usage, recent tool, truncation state, and one nested canonical event. Assert that no Provider-specific payload is required.

```go
event := core.Event{
    Type: core.EventSubagentEvent,
    Subagent: &core.SubagentEvent{
        TaskID: "task-1", Agent: "reviewer", Status: "running",
        Event: &core.Event{Type: core.EventTextDelta, Text: "checking"},
    },
}
```

**Step 2: Run test to verify it fails**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/core -run TestSubagentEvent -count=1`

Expected: FAIL because the event constants and payload do not exist.

**Step 3: Write minimal implementation**

Add `EventSubagentStarted`, `EventSubagentEvent`, `EventSubagentStatus`, and a `SubagentEvent` payload to `core.Event`. Keep nested events pointer-based and prohibit recursive subagent wrapping in the publishing layer.

**Step 4: Run test to verify it passes**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/core -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/core/event.go internal/core/core_test.go
git commit -m "feat: define subagent observation events"
```

### Task 2: Add bounded task snapshots and observation hub

**Files:**
- Create: `internal/tasks/observability.go`
- Create: `internal/tasks/observability_test.go`
- Modify: `internal/tasks/types.go`
- Modify: `internal/tasks/tool.go`
- Modify: `internal/tasks/framework.go`
- Modify: `internal/tasks/executor.go`

**Step 1: Write the failing tests**

Cover:

- subscribers receive ordered events for separate task IDs;
- one full subscriber does not block publishers or another subscriber;
- terminal status remains represented in the snapshot after dropped display events;
- snapshots are immutable copies and sorted deterministically;
- usage and recent tool activity update through `Registry.Update`, not mutations of cloned values;
- cancel closes subscribers without a goroutine leak.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/tasks -run 'TestObservation|TestTaskSnapshot' -count=1`

Expected: FAIL because the hub and snapshot API do not exist.

**Step 3: Write minimal implementation**

Create an observer hub owned by `ToolService` with:

```go
type Snapshot struct {
    ID, Agent, Description string
    Status TaskStatus
    Usage core.Usage
    RecentTool string
    Truncated bool
}

func (s *ToolService) Observe(context.Context) <-chan core.Event
func (s *ToolService) Snapshots() []Snapshot
```

Use a mutex-protected subscriber map, fixed channel capacity, non-blocking delivery, and cancellation-aware subscriber removal. Update progress through `Registry.Update` so registry clone boundaries remain intact.

**Step 4: Run task tests**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/tasks -count=1`

Expected: PASS.

**Step 5: Run race test**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test -race ./internal/tasks -count=1`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/tasks
git commit -m "feat: publish bounded task observations"
```

### Task 3: Forward child Engine events into Task Service

**Files:**
- Modify: `internal/tasks/tool.go`
- Modify: `internal/tasks/tool_test.go`
- Modify: `internal/cli/task_runtime.go`
- Create: `internal/cli/task_runtime_observability_test.go`

**Step 1: Write the failing tests**

Build a child provider sequence containing thinking, text, tool call, tool result, usage, warning, completed, and error cases. Assert:

- started/running appears before nested events;
- nested order is preserved;
- tool and usage fields update the snapshot;
- terminal status is completed, failed, or cancelled;
- a nested subagent event is rejected to prevent recursive wrapping;
- foreground and background tasks both publish observations.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/tasks ./internal/cli -run 'Test.*Subagent.*Observ' -count=1`

Expected: FAIL because `AgentRequest` cannot emit events and the child loop only accumulates final text.

**Step 3: Write minimal implementation**

Add an event emitter to `tasks.AgentRequest`. Have `ToolService.run` bind it to the task ID and lifecycle metadata. In `configureTaskService`, call the emitter for every safe child event before existing final-text handling.

Do not place child observations in the parent Agent history and do not alter the `task_run` final tool result contract.

**Step 4: Run focused tests**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/tasks ./internal/cli -run 'Test.*Subagent.*Observ' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/tasks/tool.go internal/tasks/tool_test.go internal/cli/task_runtime.go internal/cli/task_runtime_observability_test.go
git commit -m "feat: forward subagent runtime events"
```

### Task 4: Expose owned observation sources through Runtime

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/runtime_test.go`
- Modify: `internal/cli/runtime_builder.go`

**Step 1: Write the failing tests**

Create fake owned services implementing both `io.Closer` and an observation source. Assert `Runtime.Observe(ctx)`:

- forwards task events independently of `Run`;
- remains active after a parent turn completes;
- supports multiple UI subscribers without stealing events;
- closes on subscriber cancellation and Runtime shutdown;
- never persists child display events into the parent session event log.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/runtime -run TestRuntimeObserve -count=1`

Expected: FAIL because Runtime has no observation API.

**Step 3: Write minimal implementation**

Define a small Runtime-side interface:

```go
type ObservationSource interface {
    Observe(context.Context) <-chan core.Event
}
```

Discover owned sources during Runtime construction and merge them into a cancellation-aware `Observe` stream. Keep the normal `Run` stream terminal and unchanged.

**Step 4: Run Runtime tests and race**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test -race ./internal/runtime -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/runtime/runtime.go internal/runtime/runtime_test.go internal/cli/runtime_builder.go
git commit -m "feat: expose runtime observation stream"
```

### Task 5: Route subagent observations into isolated TUI views

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Create: `internal/ui/subagents.go`
- Create: `internal/ui/subagents_test.go`

**Step 1: Write the failing state tests**

Use a runner implementing `Run` and `Observe`. Assert:

- observation starts from `Model.Init` and continually reschedules;
- events are routed by task ID;
- parent messages/tools/usage remain unchanged;
- child text, thinking, tool state, result, usage, warning and terminal errors are represented;
- concurrent task streams never mix;
- each view retains an independent scroll offset;
- bounded display storage sets a visible truncation flag.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/ui -run 'TestModelObserves|TestSubagentView' -count=1`

Expected: FAIL because Model has no observation stream or child view state.

**Step 3: Write minimal implementation**

Add an optional observer interface next to `Runner`, observation messages/commands, and a `SubagentView` model containing task metadata, messages, tools, usage, error, status and scroll offset. Reuse existing Markdown, diff and tool rendering helpers.

**Step 4: Run focused UI tests**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/ui -run 'TestModelObserves|TestSubagentView' -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go internal/ui/subagents.go internal/ui/subagents_test.go
git commit -m "feat: track subagent TUI views"
```

### Task 6: Add full-screen task navigation

**Files:**
- Create: `internal/ui/components/task_list.go`
- Create: `internal/ui/components/task_list_test.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Modify: `README.MD`

**Step 1: Write the failing interaction tests**

Assert:

- `Ctrl+T` opens the task list from the parent;
- direction keys select tasks and `Enter` switches the entire viewport;
- `Esc` and `Ctrl+T` return to the parent;
- `[` and `]` switch task views in deterministic order;
- input history and permission/question modals keep priority;
- active child header includes description, ID, status, usage and shortcut hint;
- completed and failed agents remain viewable;
- switching restores each view's scroll position.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/ui ./internal/ui/components -run 'Test.*TaskList|Test.*SubagentNavigation' -count=1`

Expected: FAIL because task navigation components do not exist.

**Step 3: Write minimal implementation**

Implement a bounded task list component and full-screen child rendering. Do not mix child messages into the parent transcript. Keep footer input visible but disable prompt submission while a child view or task list is active; navigation keys remain local.

**Step 4: Run UI tests and race**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test -race ./internal/ui ./internal/ui/components -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/ui internal/ui/components README.MD
git commit -m "feat: add full-screen subagent navigation"
```

### Task 7: Make `/tasks` report real task snapshots

**Files:**
- Modify: `internal/cli/controlplane.go`
- Modify: `internal/cli/controlplane_test.go`
- Modify: `internal/cli/runtime_builder.go`
- Modify: `docs/configuration.md`

**Step 1: Write the failing tests**

Pass deterministic task snapshots through `ControlActions` and assert `/tasks` displays ID, status, description, token totals, recent tool and truncation warning. Assert no-task output is `tasks: none` and arguments are rejected.

**Step 2: Run tests to verify they fail**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/cli -run TestControlPlaneTasks -count=1`

Expected: FAIL because `/tasks` returns the fixed text `tasks: enabled`.

**Step 3: Write minimal implementation**

Add `TaskSnapshots func() []tasks.Snapshot` to `ControlActions`, wire it from `taskService.Snapshots`, and format bounded safe summaries in the command handler.

**Step 4: Run CLI tests**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./internal/cli -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli/controlplane.go internal/cli/controlplane_test.go internal/cli/runtime_builder.go docs/configuration.md
git commit -m "feat: report live subagent tasks"
```

### Task 8: Verify real Provider tool loops and release gates

**Files:**
- Create: `tests/integration/subagent_observability_test.go`
- Modify: `docs/capability-matrix.md`

**Step 1: Write end-to-end tests**

Run a parent task call against OpenAI-compatible and Anthropic test servers. Make the child emit text, parallel tool activity, usage and completion. Assert the parent final result remains correct while Runtime observations contain the complete ordered child stream.

**Step 2: Run integration tests**

Run: `GOCACHE=/private/tmp/cyber-code-test-cache go test ./tests/integration -run TestSubagentObservability -count=1`

Expected: PASS.

**Step 3: Run full verification**

Run:

```text
GOCACHE=/private/tmp/cyber-code-test-cache go test ./... -count=1
GOCACHE=/private/tmp/cyber-code-test-cache go test -race ./internal/core ./internal/tasks ./internal/runtime ./internal/cli ./internal/ui ./tests/integration -count=1
GOCACHE=/private/tmp/cyber-code-test-cache go vet ./...
GOCACHE=/private/tmp/cyber-code-test-cache sh scripts/check-coverage.sh
sh scripts/check-brand.sh
sh scripts/check-entrypoint-reachability.sh
sh scripts/check-placeholders.sh
sh scripts/check-todos.sh
```

Expected: all commands exit 0.

**Step 4: Build all target platforms**

Build `./cmd/cli` for current macOS, `linux/amd64`, and `windows/amd64` into `/private/tmp`; do not overwrite workspace binaries.

**Step 5: Commit**

```bash
git add tests/integration/subagent_observability_test.go docs/capability-matrix.md
git commit -m "test: verify subagent observability end to end"
```

**Step 6: Push and verify remote SHA**

Push `main` to `cyber-code-private`, then compare `git rev-parse HEAD` with `git ls-remote cyber-code-private refs/heads/main`.

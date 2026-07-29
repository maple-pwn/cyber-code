# Local Product Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete cyber-code's remaining public local-agent workflows across the active TUI, control plane, workspace tools, context governance, diagnostics, sessions, and optional product utilities.

**Architecture:** Keep `runtime.Runtime` as the sole owner of conversation and usage state. Connect existing components to the active Bubble Tea model, expose state changes through narrow control-plane callbacks, and route every workspace operation through the canonical Tool Runner and Permission Broker.

**Tech Stack:** Go, Bubble Tea/Lip Gloss/Glamour, Cobra, canonical Runtime events, existing session/config/tool packages, TypeScript VS Code client, GitHub Actions.

---

### Task 36: Compose The Active TUI

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Modify: `internal/ui/components/status.go`
- Modify: `internal/ui/components/tool_result.go`
- Test: `internal/ui/render_test.go`

1. Add failing model tests proving processing ticks advance a visible spinner without moving the input area.
2. Run `go test ./internal/ui -run 'Spinner|Processing' -count=1` and confirm the missing integration fails.
3. Add failing tests for structured running/completed/error tool panels, bounded output, and file/diff previews.
4. Run `go test ./internal/ui -run 'ToolPanel|FilePreview' -count=1` and confirm RED.
5. Add active `ProcessingModel` and structured tool rendering state to `ui.Model`; schedule bounded Bubble Tea ticks only while processing.
6. Render existing Markdown/message, tool, usage, permission, and processing components through one stable viewport.
7. Run `go test ./internal/ui ./internal/ui/components -count=1` and confirm GREEN.
8. Commit: `feat: compose rich active TUI`.

### Task 37: Complete The Slash-Command Control Plane

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/runtime_test.go`
- Modify: `internal/cli/controlplane.go`
- Modify: `internal/cli/controlplane_test.go`
- Modify: `internal/cli/runtime_builder.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Modify: `internal/config/types.go`
- Modify: `docs/configuration.md`

1. Add failing Runtime tests for cumulative usage snapshots and transactional history clearing.
2. Implement a mutex-protected usage ledger updated from canonical usage events plus `ClearHistory` with session persistence semantics.
3. Add failing control-plane tests for `/init`, `/cost`, `/stats`, `/clear`, `/vim`, and `/config`, including argument validation, existing-file refusal, no-secret output, and UI state callbacks.
4. Introduce a narrow `ControlActions` dependency containing instruction initialization, effective configuration, Vim switching, and Runtime state operations.
5. Register the commands without coupling the control-plane package to Bubble Tea or Provider implementations.
6. Add `/init` atomic creation of `CYBER.md`; never overwrite an existing file or follow an escaping symlink.
7. Run `go test ./internal/runtime ./internal/cli ./internal/ui ./tests/integration -run 'Usage|Clear|Init|Cost|Stats|Vim|Config' -count=1`.
8. Commit: `feat: complete interactive control plane`.

### Task 38: Add Dedicated Grep, Glob, And Multi-Edit Tools

**Files:**
- Create: `internal/tool/builtin/grep.go`
- Create: `internal/tool/builtin/grep_test.go`
- Create: `internal/tool/builtin/glob.go`
- Create: `internal/tool/builtin/glob_test.go`
- Modify: `internal/tool/builtin/file.go`
- Modify: `internal/tool/builtin/file_test.go`
- Modify: `internal/cli/runtime_builder.go`
- Modify: `internal/tool/builtin/contracts_test.go`

1. Add failing contract and security tests for recursive glob semantics, content search with line numbers, ignored binary/oversized files, cancellation, result limits, workspace traversal, and symlink escape.
2. Select a maintained doublestar matcher if the standard library cannot satisfy the tested recursive semantics; use structured APIs rather than ad hoc pattern rewriting.
3. Implement read-only/concurrency-safe `glob_files` and `grep_files` tools and retain `search_files` compatibility.
4. Add failing multi-edit tests for ordered non-overlapping edits, duplicate/zero matches, stale SHA256, overlapping edits, all-or-nothing writes, and canonical diff output.
5. Extend `edit_file` with an additive `edits` array; keep the existing single-edit request valid.
6. Run `go test ./internal/tool/builtin ./internal/tool ./tests/security -run 'Grep|Glob|MultiEdit|EditFile' -count=1`.
7. Commit: `feat: add precise workspace search and editing`.

### Task 39: Wire Automatic Context Governance

**Files:**
- Modify: `internal/contextbuilder/types.go`
- Modify: `internal/contextbuilder/builder.go`
- Modify: `internal/contextbuilder/budget_test.go`
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/compact_test.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/runtime_test.go`
- Modify: `internal/config/types.go`
- Modify: `docs/configuration.md`

1. Add failing budget tests for utilization ratio and warning/compact thresholds.
2. Add failing agent/runtime tests showing warning events before exhaustion and one cancelable automatic compact per threshold crossing.
3. Extend the immutable Context Plan with budget utilization metadata.
4. Gate automatic compaction in Runtime so it cannot recurse or compact concurrently with another turn.
5. Preserve recent tool context and emit canonical warning/compacted events to all frontends.
6. Run `go test ./internal/contextbuilder ./internal/agent ./internal/runtime ./internal/session -run 'Budget|Warning|AutoCompact' -count=1`.
7. Commit: `feat: add automatic context governance`.

### Task 40: Improve Doctor And Session Discovery

**Files:**
- Modify: `internal/doctor/doctor.go`
- Modify: `internal/doctor/doctor_test.go`
- Modify: `internal/cli/doctor_cmd.go`
- Modify: `internal/cli/sessions_cmd.go`
- Modify: `internal/cli/commands_test.go`
- Modify: `internal/session/snapshot.go`

1. Add failing Doctor tests for structured remediation text on configuration, credential, sandbox, notification, voice, MCP, and LSP warnings.
2. Add optional `remediation` fields while preserving the existing JSON schema fields.
3. Add failing session tests for newest-first ordering, stable columns, terminal-width truncation, bounded first-user-message summaries, and unchanged JSONL compatibility.
4. Persist or derive a redacted bounded summary without loading unbounded transcripts.
5. Run `go test ./internal/doctor ./internal/cli ./internal/session -run 'Doctor|SessionList|Summary' -count=1`.
6. Commit: `feat: improve diagnostics and session discovery`.

### Task 41: Add Optional Utilities And Release

**Files:**
- Create: `internal/update/checker.go`
- Create: `internal/update/checker_test.go`
- Create: `internal/bugreport/report.go`
- Create: `internal/bugreport/report_test.go`
- Create: `internal/platform/terminal_setup.go`
- Create: `internal/platform/terminal_setup_test.go`
- Modify: `internal/cli/controlplane.go`
- Modify: `internal/cli/root.go`
- Modify: `README.MD`
- Modify: `docs/capability-matrix.md`
- Modify: `.github/workflows/ci.yml`

1. Add failing updater tests for explicit opt-in, bounded timeouts, signed/HTTPS metadata policy, semantic version comparison, and no automatic installation.
2. Add failing bug-report tests for redaction, bounded diagnostics, local-only output by default, and optional issue URL generation.
3. Add failing platform tests for terminal setup detection/guidance without silently mutating shell profiles.
4. Register `/bug`, a version-check command, and terminal guidance with clear unsupported-platform errors.
5. Update documentation strictly from passing entry points and tests.
6. Run full release gates: `go test ./... -count=1`, focused race tests, `go vet ./...`, repository scripts, coverage, VS Code tests/compile, macOS CLI smoke, and Linux/Windows cross-builds.
7. Request final code review; fix every Critical/Important issue and rerun gates.
8. Commit, fast-forward private `main`, remove the temporary worktree/branch, and verify the root worktree is clean.

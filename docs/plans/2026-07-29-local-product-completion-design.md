# cyber-code Local Product Completion Design

## Scope

Complete the remaining public, local coding-agent workflows without copying private Claude Code services or weakening cyber-code's permission boundaries. The work covers the active TUI, slash-command control plane, workspace tools, context governance, diagnostics, session presentation, and optional local product utilities.

Remote accounts, cloud synchronization, transcript sharing, telemetry, mobile clients, private prompts, internal feature flags, and mandatory GitHub App integration remain out of scope.

## Architecture

The canonical Runtime remains the only owner of conversation history, usage, compaction, tools, and lifecycle. TUI, Print, SDK, and IDE clients consume Runtime events and do not call Providers or mutate session state independently.

Existing UI components are connected to the active `ui.Model`; no parallel UI state tree is introduced. Slash commands are registered through the existing control-plane registry and use narrow Runtime/configuration interfaces. New Grep and Glob tools use the existing Registry, Runner, and Permission Broker. File mutations continue to resolve workspace paths immediately before I/O and use optimistic content hashes plus atomic replacement.

## Delivery Stages

### Task 36: Active TUI composition

Connect `ProcessingModel`, spinner ticks, structured tool summaries/results, file previews, and the scrolling message model to the active Bubble Tea application. Preserve streaming Markdown and diff rendering, stable viewport dimensions, permission dialogs, multiline editing, history, completion, and Vim input behavior.

### Task 37: Control-plane completion

Add `/init`, `/cost`, `/stats`, `/clear`, `/vim`, and `/config`. `/init` creates a bounded `CYBER.md` template without overwriting existing content. Usage and cost come from one Runtime-owned ledger. `/clear` changes Runtime history transactionally. `/vim` changes the active input model through a UI command event. `/config` reports effective non-secret configuration and directs persistent changes through the existing config command surface.

### Task 38: Workspace tool completion

Split `search_files` into dedicated Grep and Glob contracts while retaining compatibility. Prefer established matching/search libraries when they materially improve recursive glob and ignore behavior. Add bounded multi-edit support based on exact content plus optional SHA256 preconditions; line numbers are presentation hints, not mutation authority.

### Task 39: Context governance

Expose context utilization from the Context Builder, emit warnings before exhaustion, and trigger compaction through the Runtime at configured thresholds. Automatic compaction must preserve recent tool context, remain cancelable, avoid recursive compaction, and emit canonical warning/compacted events to every frontend.

### Task 40: Diagnostics and sessions

Give Doctor checks structured remediation text with platform-specific commands where safe. Improve session listing with updated-time ordering, bounded summaries, stable table formatting, and unchanged JSON output compatibility.

### Task 41: Optional local utilities and release

Add an opt-in version check that never silently installs binaries, a `/bug` command that creates a redacted local report or opens a documented issue URL, and platform-gated terminal setup guidance. Finish with native macOS smoke tests, Linux/Windows cross-builds, VS Code tests, full race/vet/coverage/security gates, capability documentation, private-main push, and removal of the temporary worktree.

## Safety And Compatibility

- Headless default mode continues to allow reads and deny unconfirmed writes, execution, deletion, and network access.
- `--permission-mode bypass` remains explicit CLI-only authority; no "smart" shell auto-approval is added.
- Provider credentials never enter UI state, session summaries, bug reports, updater requests, or protocol messages.
- Existing JSONL Print and SDK formats remain compatible; new fields and events are additive.
- Existing `search_files` and configuration commands remain available during migration.
- Updater, bug-report, and terminal helpers fail closed when platform or network prerequisites are unavailable.

## Verification

Every behavior is developed test-first. Package tests cover rendering, control-plane state changes, usage accounting, path boundaries, matching semantics, compaction thresholds, remediation output, and redaction. Integration tests exercise the actual CLI and Runtime with temporary state. Release gates include all Go tests, focused race tests, vet, repository checks, coverage thresholds, VS Code tests/compile, macOS CLI smoke tests, and Linux/Windows cross-builds.

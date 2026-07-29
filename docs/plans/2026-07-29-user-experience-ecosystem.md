# cyber-code User Experience and Ecosystem Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the user-visible CLI/TUI, Git, multimodal, plugin marketplace, and VS Code workflows while preserving cyber-code's existing runtime and security boundaries.

**Architecture:** Extend the canonical core event/content model and keep `cmd/cli` as the only product entry point. Frontends consume runtime events without owning execution; all filesystem, process, network, plugin, and IDE permission decisions remain in the existing Broker and protocol layers.

**Tech Stack:** Go 1.26, Bubble Tea/Lip Gloss, Cobra, provider SSE codecs, JSONL local protocol, TypeScript/VS Code extension API.

---

### Task 1: Print Observability And Headless Guidance

**Files:**
- Modify: `internal/frontend/print.go`
- Modify: `internal/frontend/print_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/permissions/broker.go`
- Modify: `internal/permissions/broker_test.go`

**Step 1: Write the failing tests**

Add tests proving that `PrintOptions{Verbose:true}` writes safe tool start/result and cumulative token usage to stderr while stdout remains assistant text only. Add a CLI test proving `--verbose` reaches print options. Add a Broker test requiring headless confirmation denial to explain that a reviewed permission rule or explicit `--permission-mode bypass` is required.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/frontend ./internal/cli ./internal/permissions -run 'Verbose|Headless' -count=1`

Expected: FAIL because verbose formatting and actionable denial are absent.

**Step 3: Implement the minimal behavior**

Add `Verbose bool` to `PrintOptions`, a `--verbose` root flag, stderr-only event formatting, cumulative usage counters, and a safe headless denial reason. Do not print tool arguments or results in verbose mode; JSON mode remains the complete machine-readable stream.

**Step 4: Verify green**

Run: `go test ./internal/frontend ./internal/cli ./internal/permissions -count=1`

**Step 5: Commit**

Run: `git commit -am "feat: expose safe print progress and permission guidance"`

### Task 2: TUI Runtime Status And Stable Viewport

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Create: `internal/ui/render.go`
- Create: `internal/ui/render_test.go`

**Step 1: Write the failing tests**

Test correlated tool states (`running`, `succeeded`, `failed`), accumulated input/output/cache tokens, a visible processing indicator, and viewport clipping that always leaves the input visible at 80x24 and 40x12.

**Step 2: Verify red**

Run: `go test ./internal/ui -run 'ToolState|Usage|Viewport' -count=1`

Expected: FAIL because the active model stores tool output as undifferentiated messages and renders every line.

**Step 3: Implement the minimal behavior**

Add frontend-only tool presentation state keyed by call ID, usage totals, and a bounded renderer. Preserve the canonical runtime event and never execute work from the renderer.

**Step 4: Verify green and commit**

Run: `go test ./internal/ui -count=1`

Run: `git commit -am "feat: add stable TUI runtime status"`

### Task 3: Multiline Input, History Search, And Completion

**Files:**
- Modify: `internal/ui/components/input.go`
- Modify: `internal/ui/components/input_test.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`
- Create: `internal/ui/completion.go`
- Create: `internal/ui/completion_test.go`

**Step 1: Write failing interaction tests**

Test Shift+Enter newline insertion, Enter submission, logical-line cursor motion, Ctrl+R history search, slash-command completion, workspace-path completion, and Unicode cursor correctness.

**Step 2: Verify red**

Run: `go test ./internal/ui/... -run 'Multiline|HistorySearch|Completion' -count=1`

**Step 3: Implement**

Keep editing state in `InputModel`. Completion receives an injected command/path catalog and performs no shell execution. Clamp suggestions and history by count and bytes.

**Step 4: Verify and commit**

Run: `go test ./internal/ui/... -count=1`

Run: `git commit -am "feat: complete interactive TUI input"`

### Task 4: Markdown, Code, And Diff Rendering

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/ui/markdown.go`
- Create: `internal/ui/markdown_test.go`
- Modify: `internal/ui/render.go`
- Modify: `internal/ui/render_test.go`

**Step 1: Write failing rendering tests**

Use golden-free structural assertions for headings, lists, fenced Go code, tables, long Unicode lines, ANSI stripping, and unified diff additions/deletions. Assert rendered width never exceeds the viewport after removing ANSI escapes.

**Step 2: Verify red**

Run: `go test ./internal/ui -run 'Markdown|Diff' -count=1`

**Step 3: Implement**

Use a maintained terminal Markdown renderer already compatible with Lip Gloss, configured with a fixed width. Add a dedicated unified-diff renderer; treat content as data and never interpret terminal control sequences from model/tool output.

**Step 4: Verify and commit**

Run: `go test ./internal/ui -count=1`

Run: `git commit -am "feat: render markdown and diffs in the TUI"`

### Task 5: Git Workflows And Rich Session Management

**Files:**
- Create: `internal/gitworkflow/service.go`
- Create: `internal/gitworkflow/service_test.go`
- Modify: `internal/cli/controlplane.go`
- Modify: `internal/cli/controlplane_test.go`
- Modify: `internal/cli/sessions_cmd.go`
- Modify: `internal/cli/commands_test.go`

**Step 1: Write failing tests**

Test `/diff`, `/review`, and `/commit` against temporary repositories. Require bounded output, permission checks, explicit commit confirmation, and no automatic staging of unrelated files. Test session list text and JSON output with ID, model, profile, updated time, sequence, and message count.

**Step 2: Verify red**

Run: `go test ./internal/gitworkflow ./internal/cli -run 'Git|SessionMetadata' -count=1`

**Step 3: Implement**

Build workflows on the authorized platform executor. Add `sessions list --json`; enrich the index atomically when a turn completes without making old indexes unreadable.

**Step 4: Verify and commit**

Run: `go test ./internal/gitworkflow ./internal/cli ./internal/session -count=1`

Run: `git commit -am "feat: add Git workflows and rich sessions"`

### Task 6: Canonical Image Inputs And Provider Codecs

**Files:**
- Modify: `internal/core/message.go`
- Modify: `internal/core/core_test.go`
- Modify: `internal/provider/anthropic/codec.go`
- Modify: `internal/provider/anthropic/client_test.go`
- Modify: `internal/provider/openai/codec.go`
- Modify: `internal/provider/openai/client_test.go`
- Create: `internal/attachment/image.go`
- Create: `internal/attachment/image_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Step 1: Write failing tests**

Test PNG/JPEG/WebP/GIF validation, size limits, workspace-boundary and symlink checks, canonical image blocks, Anthropic image sources, OpenAI image URLs, repeated `--image` flags, and clear unsupported-provider errors.

**Step 2: Verify red**

Run: `go test ./internal/core ./internal/attachment ./internal/provider/anthropic ./internal/provider/openai ./internal/cli -run 'Image' -count=1`

**Step 3: Implement**

Add `ContentImage` with validated MIME type and base64 data. Resolve CLI images before the turn and attach them to the user message through a typed runtime input API; never place raw image bytes in logs or errors.

**Step 4: Verify and commit**

Run: `go test ./internal/core ./internal/attachment ./internal/provider/... ./internal/cli -count=1`

Run: `git commit -am "feat: add bounded multimodal image input"`

### Task 7: Claude-Compatible Marketplace Adapter

**Files:**
- Create: `internal/marketplace/types.go`
- Create: `internal/marketplace/catalog.go`
- Create: `internal/marketplace/import.go`
- Create: `internal/marketplace/install.go`
- Create: `internal/marketplace/marketplace_test.go`
- Modify: `internal/cli/plugins_cmd.go`
- Modify: `internal/cli/commands_test.go`
- Modify: `docs/configuration.md`

**Step 1: Write failing security and compatibility tests**

Fixture-test the public Claude-compatible marketplace and plugin manifests. Reject unknown security-sensitive fields, traversal, symlink escapes, oversized archives, missing digests, mutable unpinned installs, duplicate names, and undeclared capabilities. Test `marketplace add/list/search/install/update/remove` with a local HTTP fixture.

**Step 2: Verify red**

Run: `go test ./internal/marketplace ./internal/cli -run 'Marketplace' -count=1`

**Step 3: Implement**

Normalize compatible manifests into `plugin.Manifest`, persist configured catalogs atomically, require an installation permission preview, pin the resolved version and SHA-256, and install into a staging directory before atomic activation. Do not implement accounts, private endpoints, telemetry, or silent updates.

**Step 4: Verify and commit**

Run: `go test ./internal/marketplace ./internal/plugin ./internal/cli -count=1`

Run: `git commit -am "feat: add secure plugin marketplace compatibility"`

### Task 8: VS Code Extension On The Local Protocol

**Files:**
- Create: `editors/vscode/package.json`
- Create: `editors/vscode/tsconfig.json`
- Create: `editors/vscode/src/extension.ts`
- Create: `editors/vscode/src/client.ts`
- Create: `editors/vscode/src/client.test.ts`
- Create: `editors/vscode/src/protocol.ts`
- Modify: `internal/protocol/types.go`
- Modify: `internal/protocol/protocol_test.go`
- Modify: `docs/sdk.md`

**Step 1: Write failing protocol and client tests**

Test workspace/focus/selection/diagnostic context, streamed events, cancellation, permission challenge IDs, diff application, process restart, malformed JSON isolation, and disconnect cleanup.

**Step 2: Verify red**

Run: `go test ./internal/protocol -run IDE -count=1`

Run: `npm test --prefix editors/vscode`

**Step 3: Implement**

Extend protocol v1 compatibly with optional IDE context messages. Implement a VS Code output/chat surface, permission prompts, diff preview, and a managed `cyber-code serve` child process. Store no provider credentials in extension settings.

**Step 4: Verify and commit**

Run: `go test ./internal/protocol ./internal/bridge -count=1`

Run: `npm test --prefix editors/vscode && npm run compile --prefix editors/vscode`

Run: `git commit -am "feat: add VS Code integration"`

### Task 9: Remove Unreachable Legacy Product Paths

**Files:**
- Modify: `scripts/check-placeholders.sh`
- Create: `scripts/check-entrypoint-reachability.sh`
- Modify/Delete: only files proven unreachable from `cmd/cli` and not imported by supported packages
- Modify: `docs/capability-matrix.md`
- Modify: `README.MD`

**Step 1: Add a failing release check**

The check must reject user-facing placeholder strings and duplicate legacy CLI/query/UI product entry paths while allowing test fixtures and historical plans.

**Step 2: Verify red**

Run: `scripts/check-entrypoint-reachability.sh`

Expected: FAIL and list the reachable or confusing legacy paths without deleting anything.

**Step 3: Prove and remove**

Use `go list -deps`, import searches, and package tests to prove each target is unreachable. Remove one package group at a time; never delete configuration migration or exported compatibility code without a replacement test.

**Step 4: Verify and commit**

Run: `go test ./... -count=1 && scripts/check-placeholders.sh && scripts/check-entrypoint-reachability.sh`

Run: `git commit -am "refactor: remove unreachable legacy product paths"`

### Task 10: Release Verification And Truthful Capability Matrix

**Files:**
- Modify: `docs/capability-matrix.md`
- Modify: `README.MD`
- Modify: `docs/configuration.md`
- Modify: `docs/sdk.md`

**Step 1: Run deterministic release gates**

Run:

```bash
go test ./... -count=1
go test -race ./internal/protocol ./internal/bridge ./internal/cli ./internal/ui ./tests/integration -count=1
go vet ./...
git diff --check
scripts/check-placeholders.sh
scripts/check-brand.sh
scripts/check-coverage.sh
GOOS=linux GOARCH=amd64 go build -o /tmp/cyber-code-linux-amd64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o /tmp/cyber-code-windows-amd64.exe ./cmd/cli
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
```

**Step 2: Run real entry-point smoke tests**

Use temporary config/state directories to exercise help, config, doctor, sessions, marketplace, print text/JSON/verbose, resume, and the local protocol. Run real provider tests only when explicitly configured and redact all credentials and response bodies.

**Step 3: Update documentation from evidence**

Mark a capability `implemented` only when its public entry point and automated test both pass. Record native Windows, macOS, Linux, and real-provider checks separately.

**Step 4: Final commit**

Run: `git commit -am "release: verify user experience and ecosystem completion"`

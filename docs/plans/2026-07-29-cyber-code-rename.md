# cyber-code Full Rename Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename the complete product identity to `cyber-code` and inject a provider-independent system identity into every canonical agent request.

**Architecture:** Define compile-time identity once in a dependency-free `internal/product` package and consume it at the canonical agent boundary and all runtime user-facing boundaries. Apply the remaining rename across the Go module, CLI surface, configuration paths, environment variables, runtime client identifiers, UI, build artifacts, scripts, and current documentation as a deliberate breaking change with no legacy fallback; use a residual scan for non-Go release metadata.

**Tech Stack:** Go 1.26.5, Cobra, Bubble Tea, YAML configuration, Go tests and cross-compilation.

---

### Task 1: Canonical system identity

**Files:**
- Modify: `internal/agent/options.go`
- Modify: `internal/agent/engine.go`
- Create: `internal/product/product.go`
- Create: `internal/product/product_test.go`
- Test: `internal/agent/engine_test.go`
- Test: `tests/integration/agent_openai_test.go`

**Step 1: Write the failing test**

Add an agent test whose capture Provider asserts that `core.Request.System` contains `cyber-code`, identifies it as an independent coding agent, and tells it not to claim another product identity.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent -run SystemIdentity -count=1`

Expected: FAIL because the canonical request has no system identity.

**Step 3: Write minimal implementation**

Create the canonical product manifest and add an optional `SystemPrompt` field to `agent.Options`. Build `core.Request.System` from the configured prompt, falling back to the identity exported by `internal/product`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent ./tests/integration -run 'SystemIdentity|OpenAI' -count=1`

Expected: PASS and the fake OpenAI server observes the system message.

**Step 5: Commit**

```bash
git add internal/agent tests/integration/agent_openai_test.go
git commit -m "feat: establish cyber-code agent identity"
```

### Task 2: Go module and executable identity

**Files:**
- Modify: `go.mod`
- Modify: all tracked `*.go` imports
- Modify: `internal/cli/root.go`
- Modify: `internal/ui/app.go`
- Modify: `cmd/cli/main.go`
- Test: `internal/cli/commands_test.go`
- Test: `internal/ui/app_test.go`

**Step 1: Write failing visible-brand tests**

Assert root usage and TUI output contain `cyber-code` and do not contain the old product name.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli ./internal/ui -run Brand -count=1`

Expected: FAIL with the old CLI/TUI label.

**Step 3: Apply the module and visible rename**

Change the Go module to the canonical module identifier, rewrite internal import paths, and derive root command usage and TUI heading from `internal/product`.

**Step 4: Format and test**

Run: `gofmt -w $(git ls-files '*.go')`

Run: `go test ./internal/cli ./internal/ui ./cmd/cli -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add go.mod cmd internal tests
git commit -m "refactor: rename module and CLI to cyber-code"
```

### Task 3: Configuration and state hard cut

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/config/config.go`
- Modify: `internal/doctor/doctor.go`
- Modify: configuration tests under `internal/cli`, `internal/config`, and `internal/doctor`

**Step 1: Write failing path and environment tests**

Assert only `CYBER_CODE_CONFIG`, `CYBER_CODE_STATE_DIR`, and other `CYBER_CODE_*` overrides are read; assert default directories and project files use `cyber-code` and `.cyber-code.yaml`.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli ./internal/config ./internal/doctor -run 'CyberCode|Environment|DefaultPath' -count=1`

Expected: FAIL on old names.

**Step 3: Implement the hard cut**

Replace old environment variables, default paths and temporary-file prefixes. Do not add aliases or migration logic.

**Step 4: Run package tests**

Run: `go test ./internal/cli ./internal/config ./internal/doctor -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli internal/config internal/doctor
git commit -m "feat: switch configuration namespace to cyber-code"
```

### Task 4: Runtime client and service branding

**Files:**
- Modify: `internal/constants/system.go`
- Modify: `internal/utils/http.go`
- Modify: `internal/mcp/manager.go`
- Modify: `internal/services/mcp/client.go`
- Modify: `internal/services/notifier.go`
- Modify: other current product-facing code found by residual scan
- Test: corresponding package tests

**Step 1: Add failing identifier tests**

Assert User-Agent, MCP implementation info and notification title use `cyber-code`.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/utils ./internal/mcp ./internal/services -run 'Brand|UserAgent|ClientInfo|Notification' -count=1`

Expected: FAIL on old identifiers.

**Step 3: Replace product-facing identifiers**

Rename client info, User-Agent, notification titles, generated instruction names and safe temporary prefixes. Preserve Anthropic wire fields, Claude model identifiers and factual upstream references.

**Step 4: Run package tests**

Run: `go test ./internal/utils ./internal/mcp ./internal/services/... -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal
git commit -m "refactor: brand runtime services as cyber-code"
```

### Task 5: Documentation, scripts and release artifacts

**Files:**
- Modify: `README.MD`
- Modify: `docs/configuration.md`
- Modify: `docs/providers.md`
- Modify: `docs/security.md`
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/check-placeholders.sh`
- Modify: `.gitignore`

**Step 1: Add a residual-brand gate**

Extend the placeholder/release scan to reject old current-product identifiers outside an explicit allowlist for historical plans and third-party protocol semantics.

**Step 2: Run it to verify it fails**

Run: `scripts/check-placeholders.sh`

Expected: FAIL and list current old-brand references.

**Step 3: Rename current documentation and build output**

Update command examples, binary names, configuration paths, environment variables, CI build names and release artifact names to `cyber-code`.

**Step 4: Run documentation and build gates**

Run: `scripts/check-placeholders.sh && git diff --check && go build -o dist/cyber-code ./cmd/cli`

Expected: PASS.

**Step 5: Commit**

```bash
git add README.MD docs .github scripts .gitignore
git commit -m "docs: complete cyber-code product rename"
```

### Task 6: Resume Productization Task 25

**Files:**
- Continue: `docs/plans/2026-07-27-claude-code-go-productization.md`
- Modify: CLI/runtime integration files required by Task 25 review

**Step 1: Return to the open Task 25 checklist**

Finish CLI reachability for Hooks, Compact, Tasks/sub-agents, LSP, Skills and MCP resources; then address cloud Provider reachability and the remaining security review findings.

**Step 2: Run final release gates**

Run fresh full tests, race tests, vet, coverage, vulnerability scan, Linux/Windows builds and residual scans using `cyber-code` artifact names.

**Step 3: Independent review and final commit**

Review all Task 25 requirements against the canonical CLI, fix Critical/High findings, and commit the completed release work.

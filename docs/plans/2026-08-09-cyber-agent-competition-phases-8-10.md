# Cyber Agent Competition Integration: Phases 8-10 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `cyber-agent` a real, observable security runtime in every `cyber-code` client, accept the competition's required input formats, and prove four real security workflows end to end.

**Architecture:** Extend the existing `cyber-agent` REST/SSE Session API with capability negotiation, idempotent mutations, structured authority events, and referenced input Artifacts. Add a `CyberAgentRemoteSource` and local supervisor to `cyber-code`, then project the same authoritative event stream through Go, Web, Desktop, and VS Code clients without moving authorization decisions into a UI.

**Tech Stack:** Python 3.12, Pydantic 2, FastAPI/SSE, pytest; Go 1.26, Bubble Tea, existing product protocol; TypeScript 5.9, React 19, Vite, Vitest, Playwright, Tauri 2; MCP and native security tools.

---

## Execution Rules

- Work in the existing dedicated `cyber-code` worktree and create a separate
  `cyber-agent` worktree from its current committed HEAD. Do not move or delete
  the user's untracked `.qoder/`, `run_ctf_round3.py`, `run_ctf_unified.py`, or
  `workspace/` entries in the original checkout.
- Use TDD for every behavior: failing focused test, minimal implementation,
  passing focused test, relevant regression suite, commit.
- Keep commits repository-local. A commit in `cyber-agent` must not include
  generated fixtures or user files; a commit in `cyber-code` must not include
  built `dist`, `target`, coverage, or Playwright result output.
- Real external-tool and native-host tests must report explicit `SKIP` when the
  dependency is unavailable. Never represent a mock, cross-build, or fixture as
  native acceptance.
- Complete tasks in order. Later UI tasks depend on the contracts established by
  the earlier runtime tasks.

## Phase 8: Complete Runtime Integration

### Task 1: Publish cyber-agent capability and idempotency contracts

**Repository:** `cyber-agent`

**Files:**
- Create: `common/contracts/api.py`
- Modify: `common/contracts/session.py`
- Modify: `infra/api.py`
- Test: `tests/test_api_protocol.py`

**Step 1: Write failing contract tests**

Add tests that assert:

```python
response = client.get("/v1/capabilities")
assert response.json()["protocol_version"] == 1
assert "session.events.v1" in response.json()["capabilities"]

first = client.post("/v1/sessions", headers={"Idempotency-Key": "create-1"}, json=payload)
repeat = client.post("/v1/sessions", headers={"Idempotency-Key": "create-1"}, json=payload)
assert repeat.json() == first.json()
assert client.post("/v1/sessions", headers={"Idempotency-Key": "create-1"}, json=changed).status_code == 409
```

Cover every mutating Session endpoint, reject missing/blank/oversized keys, and
prove duplicate approval responses never execute twice.

**Step 2: Run the focused tests and verify failure**

Run: `pytest -q tests/test_api_protocol.py -k 'capabilities or idempotency'`

Expected: FAIL because the capability endpoint and idempotency store do not exist.

**Step 3: Implement the minimal public contract**

Add frozen Pydantic models for capability response and mutation receipt. Add a
bounded in-memory idempotency ledger to the Session API composition that binds
`(principal, route, key)` to the canonical request digest and response. Return
the stored response for an exact replay and HTTP 409 for a conflicting replay.

**Step 4: Verify focused and protocol suites**

Run: `pytest -q tests/test_api_protocol.py tests/test_session_service.py`

Expected: PASS.

**Step 5: Commit**

```bash
git add common/contracts/api.py common/contracts/session.py infra/api.py tests/test_api_protocol.py
git commit -m "feat: version and deduplicate session api mutations"
```

### Task 2: Add structured Scope, Finding, Artifact, and Report events

**Repository:** `cyber-agent`

**Files:**
- Modify: `common/contracts/events.py`
- Modify: `common/contracts/session.py`
- Modify: `orchestrator/session_event_store.py`
- Modify: `orchestrator/session_service.py`
- Test: `tests/test_session_events.py`
- Test: `tests/test_session_service.py`

**Step 1: Write failing event tests**

Define test payloads for:

```python
"scope.proposed"
"scope.confirmed"
"finding.created"
"finding.verifying"
"finding.confirmed"
"finding.rejected"
"artifact.available"
"report.drafted"
"report.frozen"
```

Assert stable IDs, session/task binding, Evidence references, legal Finding
transitions, and immutable Artifact identity. Add a `scope_confirmation`
interaction whose response cannot widen or modify the proposed Scope.

**Step 2: Run tests and verify failure**

Run: `pytest -q tests/test_session_events.py tests/test_session_service.py -k 'scope or finding or artifact or report'`

Expected: FAIL on unknown topics or missing models.

**Step 3: Implement typed payloads and service methods**

Add strict payload models and validation to `EventEnvelope`. Emit structured
Scope before execution and pause the session on its interaction. Add narrow
service methods for Artifact, Finding, and Report transitions; do not let API
clients supply arbitrary trusted event payloads.

**Step 4: Verify session recovery**

Run: `pytest -q tests/test_session_events.py tests/test_session_service.py tests/test_native_session_runtime.py`

Expected: PASS, including checkpoint restore with an unresolved Scope interaction.

**Step 5: Commit**

```bash
git add common/contracts/events.py common/contracts/session.py orchestrator/session_event_store.py orchestrator/session_service.py tests/test_session_events.py tests/test_session_service.py
git commit -m "feat: publish authoritative security product events"
```

### Task 3: Make the Session API usable by supervised and remote clients

**Repository:** `cyber-agent`

**Files:**
- Create: `infra/session_auth.py`
- Modify: `infra/api.py`
- Modify: `infra/health.py`
- Modify: `infra/cli.py`
- Test: `tests/test_api_protocol.py`
- Test: `tests/test_health.py`

**Step 1: Write failing transport-boundary tests**

Test loopback bearer authentication, constant response shape for unauthorized
requests, `/healthz`, `/readyz`, explicit bind address and port `0`, and an
injected OIDC verifier for non-loopback deployments. Assert the bearer is never
returned in JSON or error detail.

**Step 2: Run tests and verify failure**

Run: `pytest -q tests/test_api_protocol.py tests/test_health.py -k 'bearer or ready or oidc'`

Expected: FAIL because the current router only checks loopback origin.

**Step 3: Implement the minimal host interface**

Add `cyber-agent serve` options for bind address, port, bearer source, and
optional injected OIDC verification. Keep loopback as the default. Print one
bounded JSON readiness record to stdout containing only endpoint, PID, version,
and protocol version so a parent supervisor can connect.

**Step 4: Verify API and CLI tests**

Run: `pytest -q tests/test_api_protocol.py tests/test_health.py tests/test_cli.py`

Expected: PASS.

**Step 5: Commit**

```bash
git add infra/session_auth.py infra/api.py infra/health.py infra/cli.py tests/test_api_protocol.py tests/test_health.py tests/test_cli.py
git commit -m "feat: expose authenticated session runtime service"
```

### Task 4: Implement the Go cyber-agent Session API client

**Repository:** `cyber-code`

**Files:**
- Create: `internal/cyberagent/contracts.go`
- Create: `internal/cyberagent/client.go`
- Create: `internal/cyberagent/sse.go`
- Create: `internal/cyberagent/client_test.go`
- Create: `internal/cyberagent/sse_test.go`

**Step 1: Write failing HTTP/SSE tests**

Use `httptest.Server` fixtures to cover capability negotiation, create/get/turn,
interaction, resume/cancel/compact, idempotency headers, bounded response sizes,
SSE multiline data, comments, reconnect cursor parameters, and malformed events.

**Step 2: Run tests and verify failure**

Run: `go test ./internal/cyberagent -run 'Client|SSE' -count=1`

Expected: FAIL because the package does not exist.

**Step 3: Implement the client**

Use `net/http`, strict JSON decoding, context cancellation, response size limits,
and an injected token provider. Expose typed operations and an event iterator;
do not import UI packages or product state.

**Step 4: Run package tests**

Run: `go test ./internal/cyberagent -race -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cyberagent
git commit -m "feat: add cyber-agent session api client"
```

### Task 5: Implement local cyber-agent discovery and supervision

**Repository:** `cyber-code`

**Files:**
- Create: `internal/cyberagent/discovery.go`
- Create: `internal/cyberagent/supervisor.go`
- Create: `internal/cyberagent/supervisor_unix.go`
- Create: `internal/cyberagent/supervisor_windows.go`
- Create: `internal/cyberagent/supervisor_test.go`
- Modify: `internal/cli/doctor_cmd.go`
- Test: `internal/cli/commands_test.go`

**Step 1: Write failing supervisor tests**

Build a small test child that emits the readiness JSON record. Test explicit
path precedence, `PATH` discovery, incompatible version, readiness timeout,
graceful stop, crash/restart budget, and secret absence from argv/log output.

**Step 2: Run tests and verify failure**

Run: `go test ./internal/cyberagent ./internal/cli -run 'Supervisor|CyberAgentDoctor' -count=1`

Expected: FAIL on missing supervisor and doctor checks.

**Step 3: Implement discovery and supervisor**

Start `cyber-agent serve` with a random loopback port and pass the process bearer
through an inherited pipe or platform-restricted handle. Parse exactly one
bounded readiness record, negotiate capabilities, and own shutdown/restart.

**Step 4: Verify race and platform builds**

Run:

```bash
go test ./internal/cyberagent ./internal/cli -race -count=1
GOOS=linux GOARCH=amd64 go test ./internal/cyberagent -c -o /tmp/cyberagent-linux.test
GOOS=windows GOARCH=amd64 go test ./internal/cyberagent -c -o /tmp/cyberagent-windows.test.exe
```

Expected: tests PASS locally and cross-builds succeed; cross-builds are not
recorded as native acceptance.

**Step 5: Commit**

```bash
git add internal/cyberagent internal/cli/doctor_cmd.go internal/cli/commands_test.go
git commit -m "feat: supervise local cyber-agent runtime"
```

### Task 6: Project cyber-agent events into product state

**Repository:** `cyber-code`

**Files:**
- Create: `internal/cyberagent/source.go`
- Create: `internal/cyberagent/project.go`
- Create: `internal/cyberagent/source_test.go`
- Create: `internal/cyberagent/project_test.go`
- Modify: `internal/productprotocol/validate.go`
- Modify: `internal/productstate/project.go`
- Test: `internal/productstate/project_test.go`

**Step 1: Write failing canonical mapping tests**

Use checked-in JSON event fixtures from `cyber-agent`. Assert lifecycle, plan,
parent/child Agent, tool receipt, Scope, approval, Evidence, Artifact, Finding,
and Report mappings. Assert unknown source events remain unknown and cursor gaps
request resynchronization.

**Step 2: Run tests and verify failure**

Run: `go test ./internal/cyberagent ./internal/productprotocol ./internal/productstate -run 'CyberAgent|Artifact|Plan' -count=1`

Expected: FAIL because the source and new product events are absent.

**Step 3: Implement mapping and durable binding**

Implement a source adapter that stores task/session ID and the acknowledged
source cursor in the existing runtime state store. Fetch snapshot and replay on
cursor conflict. Keep source event IDs in projection metadata for report
traceability.

**Step 4: Verify replay and recovery**

Run: `go test ./internal/cyberagent ./internal/productprotocol ./internal/productstate ./internal/runtimeapi -race -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cyberagent internal/productprotocol internal/productstate
git commit -m "feat: project cyber-agent security sessions"
```

### Task 7: Expose Security Runtime in CLI and both TUIs

**Repository:** `cyber-code`

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/tactical.go`
- Modify: `internal/cli/runtime_builder.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/adapter/factory.go`
- Modify: `internal/ui/mission/model.go`
- Modify: `internal/ui/mission/view.go`
- Test: `internal/cli/root_test.go`
- Test: `internal/ui/app_test.go`
- Test: `internal/ui/mission/model_test.go`

**Step 1: Write failing entry-point tests**

Test `--runtime coding|cyber-agent`, `--runtime-location local|remote`, explicit
switching in Classic TUI, Security Runtime identity in Tactical Ops, and
reattaching the same session after switching back. Assert the default remains
explicit and never prompt-routes a task.

**Step 2: Run tests and verify failure**

Run: `go test ./internal/cli ./internal/ui/... -run 'SecurityRuntime|CyberAgent' -count=1`

Expected: FAIL on unknown flags and absent runtime state.

**Step 3: Wire the common source**

Compose the supervisor/client/source in CLI runtime building. Render runtime
name, version, location, connection, session, and authority. Route approvals and
interrupt/resume through the source rather than the coding Agent permission path.

**Step 4: Verify focused and full UI suites**

Run: `go test ./internal/cli ./internal/ui/... -race -count=1`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cli internal/ui
git commit -m "feat: expose cyber-agent in cli and tui"
```

### Task 8: Add a TypeScript cyber-agent EventSource

**Repository:** `cyber-code`

**Files:**
- Create: `packages/runtime-client/src/cyber-agent-event-source.ts`
- Create: `packages/runtime-client/src/cyber-agent-event-source.test.ts`
- Modify: `packages/runtime-client/src/index.ts`
- Modify: `packages/runtime-client/src/source-factory.ts`
- Test: `packages/runtime-client/src/conformance.test.ts`

**Step 1: Write failing EventSource conformance tests**

Cover capability handshake, task/session binding, SSE replay, interaction
responses, snapshot resync, abort, unauthorized, incompatible, and unknown
events using an injected fetch/stream transport.

**Step 2: Run tests and verify failure**

Run: `pnpm exec vitest run packages/runtime-client/src/cyber-agent-event-source.test.ts`

Expected: FAIL because the EventSource is absent.

**Step 3: Implement the source**

Adapt the Session API to the existing `RuntimeClient` interface. Keep token
provision and local process control in trusted hosts; the browser source receives
only an injected transport.

**Step 4: Verify TypeScript runtime suites**

Run: `pnpm exec vitest run packages/runtime-client/src && pnpm --filter @cyber/runtime-client typecheck`

Expected: PASS.

**Step 5: Commit**

```bash
git add packages/runtime-client
git commit -m "feat: add cyber-agent web runtime source"
```

### Task 9: Connect Web, Desktop, and VS Code surfaces

**Repository:** `cyber-code`

**Files:**
- Modify: `apps/web/src/source-factory.ts`
- Modify: `apps/web/src/source-factory.test.ts`
- Modify: `apps/desktop/src/source-factory.ts`
- Modify: `apps/desktop/src/source-factory.test.ts`
- Modify: `apps/desktop/src-tauri/src/runtime_process.rs`
- Modify: `packages/product-app/src/ProductApp.tsx`
- Modify: `packages/product-app/src/pages/MissionControlPage.tsx`
- Modify: `editors/vscode/src/client.ts`
- Modify: `editors/vscode/src/extension.ts`
- Test: `apps/web/tests/golden-path.spec.ts`
- Test: `apps/desktop/tests/window.spec.ts`
- Test: `editors/vscode/src/client.test.ts`

**Step 1: Write failing surface tests**

Test the visible `Security Runtime - cyber-agent` identity, unavailable setup
guidance, local Desktop supervision, remote Web transport, VS Code session tree,
approval action, child Agent state, and no fallback to Demo after a failure.

**Step 2: Run tests and verify failure**

Run:

```bash
pnpm exec vitest run apps/web/src apps/desktop/src packages/product-app/src
npm test --prefix editors/vscode
```

Expected: FAIL because the product surfaces do not expose the new source.

**Step 3: Implement shared presentation and trusted host bridges**

Use the common TypeScript source. Desktop owns process start/restart/stop; Web
requires an injected HTTPS or trusted loopback transport; VS Code owns its child
process and renders the session through existing tree/output interfaces.

**Step 4: Verify all client suites**

Run:

```bash
pnpm exec vitest run packages/runtime-client packages/product-app apps/web/src apps/desktop/src
pnpm typecheck
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml
```

Expected: PASS.

**Step 5: Commit**

```bash
git add apps packages/product-app editors/vscode
git commit -m "feat: connect cyber-agent across product clients"
```

### Task 10: Surface third-party Skill lifecycle and observability

**Repositories:** `cyber-agent`, then `cyber-code`

**Files (`cyber-agent`):**
- Modify: `infra/api.py`
- Modify: `infra/skill_commands.py`
- Test: `tests/test_skill_registry.py`
- Test: `tests/test_api_protocol.py`

**Files (`cyber-code`):**
- Modify: `internal/cyberagent/contracts.go`
- Modify: `internal/cyberagent/client.go`
- Modify: `internal/cli/controlplane.go`
- Modify: `packages/product-app/src/ProductApp.tsx`
- Test: `internal/cli/controlplane_test.go`
- Test: `packages/product-app/src/ProductApp.test.tsx`

**Step 1: Write failing Skill API tests**

Test list/search/install/remove, installed version/digest/trust, missing tool
dependencies, discovered/activated event projection, and an inactive Skill that
requires an unavailable MCP tool.

**Step 2: Run tests and verify failure in both repositories**

Run:

```bash
pytest -q tests/test_skill_registry.py tests/test_api_protocol.py -k 'command or dependency or api'
go test ./internal/cyberagent ./internal/cli -run Skill -count=1
pnpm exec vitest run packages/product-app/src/ProductApp.test.tsx
```

Expected: FAIL on missing HTTP/client/UI lifecycle methods.

**Step 3: Implement the narrow lifecycle API and clients**

Wrap the existing `SkillRegistry` and `SkillCommands`; do not create a second
package format. Expose status and dependency information and reuse runtime
discovery/activation events for observability.

**Step 4: Verify both repositories**

Run the same commands and expect PASS.

**Step 5: Commit separately**

```bash
# cyber-agent
git add infra/api.py infra/skill_commands.py tests/test_skill_registry.py tests/test_api_protocol.py
git commit -m "feat: expose third-party skill lifecycle api"

# cyber-code
git add internal/cyberagent internal/cli packages/product-app
git commit -m "feat: manage cyber-agent skills from product clients"
```

## Phase 9: Function-First Multi-Format Intake

### Task 11: Define input manifests and upload references

**Repository:** `cyber-agent`

**Files:**
- Create: `common/contracts/intake.py`
- Create: `intake/input_store.py`
- Create: `infra/intake_api.py`
- Modify: `common/contracts/runtime.py`
- Modify: `infra/api.py`
- Test: `tests/test_input_store.py`
- Test: `tests/test_api_protocol.py`

**Step 1: Write failing input contract tests**

Test upload ID, filename, media type, SHA-256, size, original/derived relation,
source location, parser status, and an `InputManifestSubmission`. Test duplicate
content reuse and missing upload rejection.

**Step 2: Run tests and verify failure**

Run: `pytest -q tests/test_input_store.py tests/test_api_protocol.py -k input`

Expected: FAIL because the contracts and store are absent.

**Step 3: Implement content-addressed local input storage**

Store original bytes under the configured session artifact root, return bounded
metadata, and add upload/get endpoints. Keep file bytes out of SSE and reference
the manifest from `TaskSubmission`.

**Step 4: Verify tests**

Run: `pytest -q tests/test_input_store.py tests/test_api_protocol.py tests/test_contracts.py`

Expected: PASS.

**Step 5: Commit**

```bash
git add common/contracts/intake.py common/contracts/runtime.py intake/input_store.py infra/intake_api.py infra/api.py tests/test_input_store.py tests/test_api_protocol.py
git commit -m "feat: add referenced task input manifests"
```

### Task 12: Parse archives and documents

**Repository:** `cyber-agent`

**Files:**
- Create: `intake/archive_parser.py`
- Create: `intake/document_parser.py`
- Modify: `intake/task_intent_parser.py`
- Modify: `pyproject.toml`
- Test: `tests/test_multiformat_intake.py`
- Test fixtures: `tests/fixtures/intake/`

**Step 1: Write failing format tests**

Generate small ZIP/TAR/TGZ, TXT/Markdown/JSON/YAML/XML, PDF, and DOCX fixtures in
tests. Assert extracted text, child Artifact lineage, target discovery, and
explicit unsupported/invalid errors. Add path traversal, maximum file count, and
maximum expanded bytes failures.

**Step 2: Run tests and verify failure**

Run: `pytest -q tests/test_multiformat_intake.py -k 'archive or document'`

Expected: FAIL because `parse_archive` and `parse_document` reject all input.

**Step 3: Implement direct parsers**

Use standard `zipfile`, `tarfile`, XML, JSON, and YAML support; add a pinned PDF
text dependency and parse DOCX text from its OOXML package. Feed normalized text
or structured mappings into existing TaskIntent extraction.

**Step 4: Verify parser and intent regressions**

Run: `pytest -q tests/test_multiformat_intake.py tests/test_task_intent_parser.py`

Expected: PASS.

**Step 5: Commit**

```bash
git add intake pyproject.toml tests/test_multiformat_intake.py tests/fixtures/intake
git commit -m "feat: parse archive and document task inputs"
```

### Task 13: Parse API descriptions and expose input UI

**Repositories:** `cyber-agent`, then `cyber-code`

**Files (`cyber-agent`):**
- Create: `intake/api_document_parser.py`
- Modify: `intake/task_intent_parser.py`
- Test: `tests/test_multiformat_intake.py`

**Files (`cyber-code`):**
- Modify: `internal/cyberagent/client.go`
- Modify: `internal/cli/root.go`
- Modify: `packages/runtime-client/src/cyber-agent-event-source.ts`
- Modify: `packages/product-app/src/pages/NewTaskPage.tsx`
- Modify: `packages/product-app/src/pages/ScopeReviewPage.tsx`
- Modify: `apps/desktop/src/native.ts`
- Modify: `editors/vscode/src/extension.ts`
- Test: `packages/product-app/src/pages/new-task.test.tsx`
- Test: `packages/product-app/src/pages/scope-review.test.tsx`

**Step 1: Write failing API/parser and UI tests**

Cover OpenAPI 2, OpenAPI 3 JSON/YAML, and Postman v2. Assert servers, paths,
methods, parameters, and auth descriptions become derived inputs and Scope
proposals. In UI tests, attach files, display parse progress/errors, inspect
derived targets, and require explicit Scope confirmation.

**Step 2: Run tests and verify failure**

Run:

```bash
pytest -q tests/test_multiformat_intake.py -k api
go test ./internal/cyberagent ./internal/cli -run Input -count=1
pnpm exec vitest run packages/product-app/src/pages/new-task.test.tsx packages/product-app/src/pages/scope-review.test.tsx
```

Expected: FAIL on unimplemented API parsing and upload commands.

**Step 3: Implement parsers and shared upload workflow**

Parse only local/inline references in this phase. Add CLI repeated `--input`
flags and shared client upload methods. Web receives browser `File` objects;
Desktop and VS Code use trusted host file reads. All clients display the same
manifest and Scope proposal.

**Step 4: Verify both repositories**

Run the same focused suites plus `pnpm typecheck`; expect PASS.

**Step 5: Commit separately**

```bash
# cyber-agent
git add intake/api_document_parser.py intake/task_intent_parser.py tests/test_multiformat_intake.py tests/fixtures/intake
git commit -m "feat: parse api description task inputs"

# cyber-code
git add internal/cyberagent internal/cli packages/runtime-client packages/product-app apps/desktop editors/vscode
git commit -m "feat: upload and review security task inputs"
```

## Phase 10: Four Real Security Workflows

### Task 14: Add common external-tool capability discovery

**Repository:** `cyber-agent`

**Files:**
- Create: `executor/external_tools.py`
- Modify: `executor/tool_capabilities.py`
- Modify: `executor/tool_gateway.py`
- Modify: `infra/health.py`
- Modify: `infra/cli.py`
- Test: `tests/test_external_tools.py`
- Test: `tests/test_health.py`

**Step 1: Write failing discovery tests**

Test executable discovery, version probing, supported/missing/incompatible state,
timeout, structured invocation receipt, and explicit skip reason. Cover `nmap`,
`nuclei`, `yara`, `tshark`, `semgrep`, `binwalk`, `radare2`, and Ghidra Headless.

**Step 2: Run tests and verify failure**

Run: `pytest -q tests/test_external_tools.py tests/test_health.py -k tool`

Expected: FAIL because there is no common external-tool registry.

**Step 3: Implement the registry and doctor projection**

Centralize executable lookup, version command, timeout, normalized result, and
installation hint. Existing Nmap/Nuclei servers consume the registry without
changing their MCP refs.

**Step 4: Verify tests**

Run: `pytest -q tests/test_external_tools.py tests/test_health.py tests/test_recon_server.py`

Expected: PASS.

**Step 5: Commit**

```bash
git add executor infra/health.py infra/cli.py tests/test_external_tools.py tests/test_health.py
git commit -m "feat: discover native security tool capabilities"
```

### Task 15: Complete the penetration-testing acceptance flow

**Repository:** `cyber-agent`

**Files:**
- Modify: `executor/mcp_servers/recon/server.py`
- Modify: `executor/mcp_servers/nuclei/server.py`
- Create: `executor/mcp_servers/web/server.py`
- Modify: `executor/mcp.json`
- Modify: `skills/builtin/network/recon/skill.toml`
- Modify: `skills/builtin/web/assessment/skill.toml`
- Create: `tests/fixtures/scenarios/pentest/`
- Create: `tests/test_pentest_scenario_e2e.py`

**Step 1: Write a failing real-lab E2E test**

Start a bounded loopback HTTP fixture, submit an authorized task, require Nmap
service discovery, Nuclei or HTTP validation, one failed/replanned action, a
confirmed Finding, Evidence, Tool Receipts, and a Report. Mark only the native
tool portion `SKIP` when executables are unavailable.

**Step 2: Run and verify failure**

Run: `pytest -q tests/test_pentest_scenario_e2e.py -rs`

Expected: deterministic orchestration FAIL until the missing adapter/report
events are implemented; native lanes may SKIP with reasons.

**Step 3: Implement the minimum flow**

Add bounded HTTP inspection and normalize real scanner output into existing
trusted findings. Update Skill requirements and ensure the planner can recover
from the injected failed action.

**Step 4: Verify pentest suites**

Run: `pytest -q tests/test_pentest_scenario_e2e.py tests/test_real_nmap_integration.py tests/test_real_nuclei_application_integration.py -rs`

Expected: deterministic E2E PASS; available native lanes PASS, unavailable lanes SKIP.

**Step 5: Commit**

```bash
git add executor skills/builtin/network skills/builtin/web tests/fixtures/scenarios/pentest tests/test_pentest_scenario_e2e.py
git commit -m "feat: complete penetration testing scenario"
```

### Task 16: Complete the incident-response acceptance flow

**Repository:** `cyber-agent`

**Files:**
- Create: `executor/mcp_servers/forensics/server.py`
- Modify: `executor/mcp.json`
- Modify: `skills/builtin/incident/response/skill.toml`
- Modify: `skills/builtin/incident/response/SKILL.md`
- Create: `tests/fixtures/scenarios/incident/`
- Create: `tests/test_incident_scenario_e2e.py`

**Step 1: Write a failing incident E2E test**

Use synthetic logs, filesystem metadata, IOC files, and a small PCAP fixture.
Require hash/metadata, timeline, IOC correlation, optional YARA/tshark receipt,
an Evidence-backed impact Finding, and response recommendations.

**Step 2: Run and verify failure**

Run: `pytest -q tests/test_incident_scenario_e2e.py -rs`

Expected: FAIL because the forensics MCP server is absent.

**Step 3: Implement deterministic core and optional native adapters**

Implement hash, metadata, log timeline, and IOC extraction in Python. Invoke
YARA/tshark through the external-tool registry when available and preserve their
receipts without making them mandatory for deterministic CI.

**Step 4: Verify scenario and Skill tests**

Run: `pytest -q tests/test_incident_scenario_e2e.py tests/test_skill_contracts.py -rs`

Expected: deterministic E2E PASS with explicit native PASS/SKIP records.

**Step 5: Commit**

```bash
git add executor/mcp_servers/forensics executor/mcp.json skills/builtin/incident tests/fixtures/scenarios/incident tests/test_incident_scenario_e2e.py
git commit -m "feat: complete incident response scenario"
```

### Task 17: Complete the vulnerability-research acceptance flow

**Repository:** `cyber-agent`

**Files:**
- Create: `executor/mcp_servers/code_security/server.py`
- Modify: `executor/mcp.json`
- Modify: `skills/builtin/vulnerability/hunt/skill.toml`
- Modify: `skills/builtin/vulnerability/hunt/SKILL.md`
- Create: `tests/fixtures/scenarios/vulnerability/`
- Create: `tests/test_vulnerability_scenario_e2e.py`

**Step 1: Write a failing vulnerable-repository E2E test**

Use a deliberately vulnerable local fixture. Require source discovery, candidate
Finding, reachability evidence, a bounded validation test, verified status, and
repair guidance. Exercise Semgrep/dependency-audit adapters when available.

**Step 2: Run and verify failure**

Run: `pytest -q tests/test_vulnerability_scenario_e2e.py -rs`

Expected: FAIL because normalized code-security tools are absent.

**Step 3: Implement the minimum code-security server**

Wrap workspace search, Git context, test execution, Semgrep JSON, and available
ecosystem audit commands behind stable MCP schemas. Map structured results to
candidate/reachable/verified Finding transitions.

**Step 4: Verify scenario and verifier regressions**

Run: `pytest -q tests/test_vulnerability_scenario_e2e.py tests/test_pentest_verifier_outcomes.py tests/test_skill_contracts.py -rs`

Expected: PASS or explicit native SKIP only.

**Step 5: Commit**

```bash
git add executor/mcp_servers/code_security executor/mcp.json skills/builtin/vulnerability tests/fixtures/scenarios/vulnerability tests/test_vulnerability_scenario_e2e.py
git commit -m "feat: complete vulnerability research scenario"
```

### Task 18: Complete the reverse-engineering acceptance flow

**Repository:** `cyber-agent`

**Files:**
- Create: `executor/mcp_servers/reverse/server.py`
- Modify: `executor/mcp.json`
- Modify: `skills/builtin/reverse/analysis/skill.toml`
- Modify: `skills/builtin/reverse/analysis/SKILL.md`
- Create: `tests/fixtures/scenarios/reverse/`
- Create: `tests/test_reverse_scenario_e2e.py`

**Step 1: Write a failing harmless-binary E2E test**

Compile or use a reproducibly generated benign fixture that contains recoverable
configuration. Require file metadata, hashes, strings/import information,
optional Binwalk/radare2/Ghidra receipts, recovered behavior/configuration,
Evidence, Finding, and Report.

**Step 2: Run and verify failure**

Run: `pytest -q tests/test_reverse_scenario_e2e.py -rs`

Expected: FAIL because the reverse MCP server is absent.

**Step 3: Implement deterministic inspection and native adapters**

Provide portable digest/string/format inspection and wrap available Binwalk,
radare2, and Ghidra Headless commands through the common registry. Normalize
outputs into behavior and recovered-configuration facts.

**Step 4: Verify scenario and Skill suites**

Run: `pytest -q tests/test_reverse_scenario_e2e.py tests/test_skill_contracts.py -rs`

Expected: deterministic E2E PASS; native tools PASS or explicit SKIP.

**Step 5: Commit**

```bash
git add executor/mcp_servers/reverse executor/mcp.json skills/builtin/reverse tests/fixtures/scenarios/reverse tests/test_reverse_scenario_e2e.py
git commit -m "feat: complete reverse engineering scenario"
```

### Task 19: Run cross-repository acceptance and publish operator docs

**Repositories:** `cyber-agent`, then `cyber-code`

**Files (`cyber-agent`):**
- Create: `docs/competition-scenarios.md`
- Create: `scripts/run_phase10_acceptance.sh`
- Modify: `README.MD`

**Files (`cyber-code`):**
- Create: `docs/operations/cyber-agent.md`
- Modify: `docs/operations/acceptance-matrix.md`
- Modify: `docs/capability-matrix.md`
- Modify: `README.MD`
- Create: `scripts/cyber-agent-integration-smoke.sh`

**Step 1: Add failing smoke assertions**

The smoke scripts must fail when capability negotiation, one-to-one session
binding, Scope confirmation, child Agent observation, tool receipt, Evidence,
Finding, Report export, or any supported input format is absent. Native external
tools produce explicit PASS/SKIP records.

**Step 2: Run the complete deterministic verification matrix**

Run in `cyber-agent`:

```bash
ruff check .
ruff format --check .
pytest -q
```

Run in `cyber-code`:

```bash
gofmt -l .
go vet ./...
go test ./... -race -count=1
pnpm typecheck
pnpm lint
pnpm test
pnpm build
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml
git diff --check
```

Expected: deterministic checks PASS. Missing native hosts, cloud accounts, or
external tools are recorded as SKIP with a reason.

**Step 3: Run a real local integration smoke**

Start `cyber-agent` through the `cyber-code` supervisor, submit one checked-in
scenario, confirm Scope, resolve one approval, observe a child Agent and tool
receipt, resume after a forced stream disconnect, and export the Report.

**Step 4: Document exact operation and evidence**

Document install/discovery, local and remote launch, all client entry points,
input formats, four scenario commands, third-party Skill installation, tool
prerequisites, recovery, and explicit platform/tool acceptance status.

**Step 5: Commit separately**

```bash
# cyber-agent
git add README.MD docs/competition-scenarios.md scripts/run_phase10_acceptance.sh
git commit -m "docs: publish phase 10 scenario acceptance"

# cyber-code
git add README.MD docs scripts/cyber-agent-integration-smoke.sh
git commit -m "docs: publish cyber-agent integration acceptance"
```

## Final Completion Gate

Do not mark Phase 8 complete until all six product clients can observe and
control the same authoritative session without duplicating Scope or approval
decisions.

Do not mark Phase 9 complete until every listed format produces a referenced
InputManifest, derived Artifact provenance, and a user-confirmed Scope proposal.

Do not mark Phase 10 complete until every scenario passes deterministic E2E and
every available external-tool lane records real execution evidence. Preserve
explicit `SKIP` for unavailable native tools or hosts.

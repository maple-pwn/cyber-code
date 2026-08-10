# CYBER Phase 4 Real Runtime EventSources Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace demo-only execution with authenticated local and remote runtime EventSources that satisfy the existing protocol, authority, replay, and recovery contracts.

**Architecture:** Introduce a versioned Go runtime service and transport-neutral conformance suite. Tauri uses an authenticated local IPC bridge, Web uses an explicit loopback or remote connection, and all clients continue through `RuntimeClient` without source-specific UI logic.

**Tech Stack:** Go 1.26, TypeScript 5.9, NDJSON IPC, HTTP/WebSocket or SSE, Tauri invoke, TLS, OS credential storage, Vitest, Go integration tests.

---

## Public transport contract

```text
Handshake  -> protocol versions, runtime ID, principal, role, capabilities
Subscribe  -> ordered ProductEvents strictly above afterCursor
Snapshot   -> ProductState with matching committed cursor
Command    -> accepted/rejected receipt with idempotency key
Health     -> readiness only; never credentials or task content
Close      -> release client transport, not runtime task ownership
```

Every transport must implement the same conformance suite. A source cannot report healthy until handshake and cursor continuity have both succeeded.

### Task 1: Freeze runtime conformance contracts

**Files:** Create `packages/runtime-client/src/conformance.ts`, `internal/runtimeapi/contract.go`, and `tests/fixtures/runtime-conformance/`.

- [ ] Add tests for handshake negotiation, replay after cursor, exact duplicate handling, gap recovery, snapshot mismatch, unknown events, command rejection, expired approval, stale lease, cancellation, and reconnect during active execution.
- [ ] Add source metadata `{ mode: 'demo' | 'local' | 'remote', runtimeId, principal, capabilities }` and require UI labels to use it.
- [ ] Add idempotency keys to mutating runtime commands without allowing clients to forge event IDs or cursors.
- [ ] Run TypeScript and Go contract suites and commit as `feat: define runtime source conformance`.

### Task 2: Adapt cyber-code core events to ProductEvents

**Files:** Create `internal/runtimeapi/{service.go,adapter.go,store.go}` and tests; modify core runner integration only at the adapter boundary.

- [ ] Map task, agent, tool, permission, Evidence, Finding, report, and terminal lifecycle events to schema-versioned ProductEvents.
- [ ] Persist event IDs, cursors, immutable Evidence payloads, approval challenges, control leases, and snapshots atomically per task.
- [ ] Derive approval parameter digests server-side and reject edited, expired, replayed, or out-of-Scope responses.
- [ ] Test crash recovery between event persistence and delivery; no acknowledged event may disappear.
- [ ] Run `go test -race ./internal/runtimeapi/...` and commit as `feat: project real runtime events`.

### Task 3: Implement the local runtime service

**Files:** Add `cyber-code runtime serve` under `cmd/cli`, local IPC handlers, lifecycle tests, and Tauri bridge commands.

- [ ] Bind to inherited stdio or a user-private local endpoint; never expose an unauthenticated wildcard listener.
- [ ] Generate a per-launch bearer secret through the desktop process manager and keep it out of argv, logs, events, and browser storage.
- [ ] Enforce one runtime owner with explicit additional control leases; closing a UI does not silently cancel tasks.
- [ ] Test executable discovery, startup timeout, crash, restart, stale socket cleanup, version mismatch, and no remote fallback.
- [ ] Commit as `feat: add authenticated local runtime source`.

### Task 4: Implement TypeScript local EventSource adapters

**Files:** Create `packages/runtime-client/src/local-event-source.ts`, desktop bridge adapter, and Web loopback adapter.

- [ ] Run the shared EventSource conformance suite against an in-process test service and a built `cyber-code` binary.
- [ ] Validate every incoming envelope before projection and map transport failures to existing connection states.
- [ ] Keep the last trusted ProductState visible while disabling writes during recovery.
- [ ] Label a successful source `Local`; retain an explicit source switch back to `Demo` for testing.
- [ ] Commit as `feat: connect local cyber runtime`.

### Task 5: Implement the remote EventSource

**Files:** Create `packages/runtime-client/src/remote-event-source.ts`, remote service handlers, and integration fixtures.

- [ ] Require TLS, authenticated handshake, principal/role/capability claims, origin checks, and bounded reconnect backoff.
- [ ] Store desktop tokens in the OS credential service; Web keeps access tokens in memory and uses secure refresh cookies where deployed.
- [ ] Test revoked credentials, capability loss, lease transfer, network partition, resume after cursor, snapshot recovery, and server version skew.
- [ ] Never migrate a failed local task to remote; switching sources creates an explicit new task context.
- [ ] Commit as `feat: add authenticated remote runtime source`.

### Task 6: Add source selection and honest execution states

- [ ] Replace hard-coded ScenarioPlayer construction with a source factory in Web, desktop, and TUI bootstraps.
- [ ] Disable unavailable source choices and show actionable setup status before task creation.
- [ ] Display Demo, Local, or Remote persistently in New Task, Scope Review, Mission Control, reports, and exported audit metadata.
- [ ] Add end-to-end tests proving a real local fixture process executes while Demo remains deterministic.
- [ ] Commit as `feat: expose real runtime selection`.

### Task 7: Security, load, and release gates

- [ ] Threat-model local IPC, remote auth, replay, confused deputy, Scope widening, approval substitution, Evidence mutation, cursor rollback, and credential leakage.
- [ ] Run concurrent task, reconnect storm, large Evidence, slow consumer, and bounded-memory tests.
- [ ] Run all Web, desktop, TUI, VS Code, Go race/security/integration, protocol conformance, and cross-platform build jobs.
- [ ] Roll out real sources behind an explicit capability flag; keep Demo available but never selected implicitly after a real-source failure.
- [ ] Commit as `ci: gate real cyber runtime sources`.

## Phase 4 acceptance

- A real local task can complete the authorized golden path through the built cyber-code runtime.
- Local and remote sources pass identical conformance suites.
- Recovery never advances unproven state or enables writes early.
- Source mode is always honest and auditable.
- No credential, approval secret, or mutable Evidence crosses an unauthorized boundary.

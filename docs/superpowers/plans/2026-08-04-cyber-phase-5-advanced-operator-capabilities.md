# CYBER Phase 5 Advanced Operator Capabilities Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add terminal, editor, asset graph, and team authorization capabilities as four independently releasable workstreams on top of proven real runtime sources.

**Architecture:** Extend the product protocol additively and keep each capability behind runtime-advertised permissions. Terminal and editor operate on bounded local/runtime resources, the graph is a projection of immutable Evidence relationships, and team authorization is enforced by the remote service rather than UI state.

**Tech Stack:** Tauri 2, portable PTY, xterm.js, Monaco, React, Go services, PostgreSQL-compatible persistence for team deployments, protocol fixture tests.

---

## Entry gate

Phase 5 work cannot start until Phase 4 local and remote EventSources pass conformance and real-execution acceptance. Each workstream has a separate feature flag, threat model, test matrix, and rollback path.

### Workstream 5A: Bounded terminal

**Files:** Create terminal protocol events/commands, `apps/desktop/src/terminal/`, Tauri PTY commands, and TUI native terminal adapters.

- [x] Specify terminal session ID, process identity, working directory, Scope, byte-stream sequencing, resize, exit, and audit events.
- [x] Require explicit Scope and approval for command classes that exceed the current task policy; never pass arbitrary UI strings directly to a shell command builder.
- [x] Implement PTY lifecycle, backpressure, output caps, resize, cancellation, crash recovery, and terminal injection sanitization.
- [x] Keep Web terminal observation read-only unless connected to a remote runtime capability that explicitly permits input.
- [x] Test shell quoting, hostile output, bidi/control characters, large output, disconnect, ownership transfer, and orphan cleanup.
- [x] Run desktop, Go race/security, and cross-platform PTY suites; commit as `feat: add bounded operator terminal`.

**Exit gate:** Terminal input is Scope-bound, auditable, revocable, and cannot outlive its runtime lease unnoticed.

### Workstream 5B: Evidence-aware editor

**Files:** Create `packages/editor-model/`, React Monaco surfaces, desktop file bridge commands, and protocol annotations.

- [x] Define immutable source Evidence references separately from editable human notes and proposed patches.
- [x] Open only runtime-advertised or user-selected workspace files; canonicalize paths and reject traversal and symlink escapes.
- [x] Model edits as drafts with explicit save/apply commands and approval where policy requires it.
- [x] Preserve provenance from Finding to Evidence range, editor selection, patch, reviewer, and resulting verification event.
- [x] Test large files, binary detection, encoding, concurrent modification, stale drafts, offline mode, and malicious language-service output.
- [x] Run editor unit, accessibility, desktop integration, and report-provenance tests; commit as `feat: add evidence aware editor`.

**Exit gate:** Editing cannot mutate raw Evidence and every applied change has a causal, auditable runtime event.

### Workstream 5C: Asset relationship graph

**Files:** Extend protocol graph types, create `packages/asset-graph/`, graph projector tests, and React/TUI graph summaries.

- [x] Define stable node identities for targets, services, routes, credentials, Findings, Evidence, Agents, and reports plus typed causal edges.
- [x] Build graph state only from committed events; unknown nodes and edges remain inspectable without fabricating topology.
- [x] Add filtering, keyboard navigation, textual alternative, bounded layout, Evidence drill-down, and snapshot export.
- [x] Keep phone limited to searchable lists and neighbor summaries rather than a dense canvas.
- [x] Test deterministic layout seeds, duplicate edges, deleted/revoked assets, huge graphs, accessibility, and cross-client state digests.
- [x] Commit as `feat: add evidence relationship graph`.

**Exit gate:** Every rendered relationship traces to committed Evidence or an explicit human annotation.

### Workstream 5D: Teams, organizations, and RBAC

**Files:** Create remote authorization service modules, organization/team protocol claims, audit storage, admin APIs, and UI management surfaces.

- [x] Define roles and capabilities for task creation, Scope confirmation, approval, control takeover, Evidence access, report freeze/export, and administration.
- [x] Enforce authorization server-side on every command and subscription; UI capability hiding is convenience only.
- [x] Add invitations, membership lifecycle, least-privilege defaults, token/session revocation, tenant isolation, and immutable audit records.
- [x] Require step-up authentication for high-risk approvals, credential access, exports, and organization administration.
- [ ] Test horizontal/vertical privilege escalation, IDOR, stale claims, revoked membership, cross-tenant cursors, audit tampering, and emergency access.
- [ ] Run security, migration, concurrency, remote conformance, and end-to-end multi-user tests; commit as `feat: add team authorization controls`.

**Exit gate:** Cross-tenant access is denied by construction, authorization decisions are auditable, and revocation takes effect without trusting client refresh.

## Cross-workstream integration

- [ ] Add new protocol events additively with schema fixtures and unknown-event compatibility tests before enabling any UI.
- [ ] Ensure control leases cover Terminal and Editor mutations while Graph observation remains separately authorized.
- [ ] Include terminal commands, patches, graph annotations, and team decisions in report provenance without embedding secrets.
- [ ] Run the complete Web, desktop, TUI, VS Code, Go, conformance, accessibility, security, and packaging matrix for every enabled combination.
- [ ] Publish capability-specific migration and rollback notes; no workstream may force-enable another.

## Phase 5 acceptance

Implementation status as of 2026-08-08: terminal, editor, graph, and core authorization libraries are implemented and covered by Go/TypeScript/Rust tests. The remaining unchecked items are deliberately reserved for real platform acceptance, multi-instance persistence, and end-to-end multi-user validation; they are not claims of completion.

- Each capability can ship, disable, and roll back independently.
- Runtime capabilities and server authorization, not client presence, control access.
- Evidence remains immutable and every mutation has causal provenance.
- Local-only users are not forced into team infrastructure.
- Advanced interfaces remain usable by keyboard and have non-visual equivalents.

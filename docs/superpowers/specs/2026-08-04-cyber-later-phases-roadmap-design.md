# CYBER Later Phases Roadmap Design

**Date:** 2026-08-04
**Status:** Approved for planning
**Parent specification:** `docs/superpowers/specs/2026-08-03-cyber-unified-ui-design.md`

## 1. Purpose

This roadmap turns the unified UI specification's Phase 2-5 outline into staged delivery gates. It does not change the product model established in Phase 1: Scope, approval, Evidence, cursor continuity, and control leases remain runtime-owned and all clients remain projections of validated product events.

The roadmap uses capability gates rather than calendar promises. A later phase starts only after the prior phase's contract and recovery tests pass on every supported platform.

## 2. Delivery sequence

```text
Phase 1  Web product prototype and deterministic scenario source (complete)
   |
   +--> Phase 2  Tauri desktop shell and native capabilities
   |       |
   |       +--> Phase 3  Tactical Ops TUI mapping
   |                 |
   +-----------------+--> Phase 4  Real local and remote EventSources
                              |
                              +--> Phase 5A Terminal
                              +--> Phase 5B Editor
                              +--> Phase 5C Asset graph
                              +--> Phase 5D Teams and RBAC
```

Phase 2 and Phase 3 initially consume the deterministic source. This proves client behavior without coupling UI completion to runtime transport work. Phase 4 replaces the source behind the existing `EventSource` interface and is the first phase that may claim real execution.

Detailed plans:

- `docs/superpowers/plans/2026-08-04-cyber-phase-2-desktop-shell.md`
- `docs/superpowers/plans/2026-08-04-cyber-phase-3-tactical-ops-tui.md`
- `docs/superpowers/plans/2026-08-04-cyber-phase-4-real-runtime-sources.md`
- `docs/superpowers/plans/2026-08-04-cyber-phase-5-advanced-operator-capabilities.md`

## 3. Phase boundaries

| Phase | Outcome | Runtime truth | Primary exit gate |
| --- | --- | --- | --- |
| 2 | Installable Tauri 2 desktop client | Scenario source | Signed desktop builds preserve Web semantics and native security boundaries |
| 3 | Tactical Ops Bubble Tea client | Scenario and fixture sources | TUI reaches semantic parity at supported terminal sizes |
| 4 | Local and remote runtime transports | Real cyber-code runtime | Conformance, recovery, authority, and no-fallback tests pass |
| 5 | Advanced operator workspaces | Real runtime only | Each independent workstream passes its own threat model and acceptance gate |

## 4. Cross-phase invariants

1. Clients never mint approvals, widen Scope, mutate Evidence, or advance cursors locally.
2. A local runtime failure never silently becomes a remote task.
3. Commands are disabled while offline, resyncing, unauthorized, or displaced by a control lease.
4. Raw credentials never enter ordinary browser storage, logs, product events, or reports.
5. Unknown event types remain retained and inspectable without changing committed known state.
6. Scenario mode is visibly labelled `Demo`; real sources are visibly labelled `Local` or `Remote`.
7. Desktop, Web, TUI, and VS Code consume the same versioned protocol fixtures.
8. Accessibility, keyboard operation, reduced motion, and terminal injection defenses remain release gates.

## 5. Architecture decisions

### 5.1 Shared React product shell

Phase 2 extracts the route/store composition from `apps/web` into a shared `packages/product-app` package. Web and desktop keep separate bootstraps and source factories. Shared presentation must not import Tauri APIs.

### 5.2 Native boundary

Tauri commands expose narrow typed operations for capability discovery, notifications, secure token storage, report export, and later local runtime transport. Shell execution and unrestricted filesystem plugins are excluded.

### 5.3 TUI parity

The Go TUI reuses Bubble Tea and Lip Gloss but projects the same product-event fixtures through a Go projector. It shares semantics and fixtures with React, not rendering code.

### 5.4 Real EventSources

Phase 4 introduces a versioned runtime service with handshake, event replay, snapshot, command, and health operations. Local and remote transports implement identical conformance tests. ScenarioPlayer remains available for tests and an explicitly marked demo mode.

### 5.5 Advanced capabilities

Phase 5 is four projects, not one release train. Terminal, editor, graph, and team authorization may ship independently after their prerequisites pass. Team/RBAC cannot be used as a prerequisite for local-only Terminal or Editor work.

## 6. Repository targets

```text
apps/desktop/                  Tauri 2 shell and desktop bootstrap
packages/product-app/         Shared React application composition
packages/runtime-client/      Scenario, local, and remote EventSources
packages/protocol/            Versioned product contracts and fixtures
internal/productprotocol/     Go product-event types and validation
internal/productstate/        Go deterministic projector
internal/runtimeapi/          Real runtime service boundary
internal/ui/                  Tactical Ops Bubble Tea presentation
tests/fixtures/product-events Cross-language canonical fixtures
```

## 7. Release and migration policy

- Every phase lands behind an explicit capability or source selector until its exit gate passes.
- Protocol additions are additive within schema version 1; breaking changes require a new schema version and dual-read migration tests.
- CI keeps the existing Go, Web, and VS Code jobs. Desktop and cross-language conformance jobs are added, never substituted.
- Phase 1 screenshot baselines remain the Web visual contract while desktop-specific window behavior receives separate smoke tests.

## 8. Success criteria

The roadmap is complete when every phase plan has named owners in code, deterministic tests, CI commands, security boundaries, rollback behavior, and an unambiguous statement of whether it executes real work. No phase may claim completion solely because its primary screen renders.

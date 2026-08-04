# CYBER Phase 3 Tactical Ops TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Map the unified CYBER product semantics into a complete Tactical Ops Bubble Tea client while retaining terminal-native interaction and injection defenses.

**Architecture:** Add Go protocol validation and deterministic projection backed by the same canonical fixtures as TypeScript. Evolve the existing `internal/ui` model behind a feature flag, then make Tactical Ops the default only after parity tests pass.

**Tech Stack:** Go 1.26, Bubble Tea, Lip Gloss, golden terminal fixtures, shared JSON product-event fixtures.

---

## File structure

| Path | Responsibility |
| --- | --- |
| `tests/fixtures/product-events/` | Canonical cross-language event, snapshot, and conflict fixtures |
| `internal/productprotocol/` | Go schema types and strict validation |
| `internal/productstate/` | Pure deterministic Go projector |
| `internal/ui/mission/` | Tactical Ops model, update, view, and components |
| `internal/ui/adapter/` | Scenario/fixture source to Bubble Tea messages |
| `cmd/cli/` | Explicit UI mode and capability selection |

### Task 1: Establish cross-language protocol fixtures

- [ ] Export canonical Phase 1 event streams for Allow, Deny, gap recovery, unknown event, duplicate replay, and conflict into `tests/fixtures/product-events/`.
- [ ] Add TypeScript fixture tests proving every file validates and projects to its recorded cursor and state digest.
- [ ] Add a fixture manifest containing schema version, expected terminal state, and SHA-256 digest.
- [ ] Run `pnpm exec vitest run packages/protocol` and commit as `test: publish product event fixtures`.

### Task 2: Implement Go validation and projection

**Files:** Create `internal/productprotocol/{types.go,validate.go,validate_test.go}` and `internal/productstate/{state.go,project.go,project_test.go}`.

- [ ] Write table tests for all known events, unknown retention, immutable Evidence, legal Finding transitions, one-shot approvals, lease monotonicity, duplicate replay, stale cursors, gaps, and event-ID conflicts.
- [ ] Mirror schema version 1 field names exactly; JSON fixture round trips must preserve canonical bytes.
- [ ] Keep `Project(previous, event)` pure and return `Applied`, `Duplicate`, or `ResyncRequired` without mutating previous state.
- [ ] Run `go test ./internal/productprotocol ./internal/productstate -count=1` and fixture parity tests.
- [ ] Commit as `feat: add go product state projector`.

### Task 3: Build the Tactical Ops mission model

**Files:** Create `internal/ui/mission/{model.go,update.go,view.go,styles.go,model_test.go}`.

- [ ] Test stable regions for task header, chronological stream, Agent summary, Scope, Evidence, Finding, report status, connection banner, approval panel, and instruction composer.
- [ ] Support `Ctrl+T` task panel, `Enter` agent detail, `[`/`]` agent switching, `Esc` parent return, explicit Review then Confirm approval, and immediate Deny.
- [ ] Render color-independent severity, confidence, verification, and connection text; sanitize control and bidi characters before every view.
- [ ] Preserve current conversation features until equivalent mission behavior is covered.
- [ ] Run `go test ./internal/ui/... -count=1` and commit as `feat: add tactical ops mission model`.

### Task 4: Add responsive terminal layouts

- [ ] Add golden views for 80x24, 100x30, 120x40, 160x50, no-color, and CJK-width terminals.
- [ ] At narrow widths keep observation, approval, pause, cancel, and connection status; move Inspector detail to a full-screen subview.
- [ ] Verify no panic or horizontal corruption from long paths, long English labels, combining marks, emoji, or malicious ANSI input.
- [ ] Run golden tests on Linux, Windows, and macOS and commit as `test: cover tactical ops terminal layouts`.

### Task 5: Connect scenario commands and recovery

**Files:** Create `internal/ui/adapter/source.go` and add `--ui=classic|tactical` plus `--source=scenario` CLI flags.

- [ ] Test exact command mapping for task lifecycle, Scope confirmation, approval, control takeover, and instructions.
- [ ] Test offline/read-only, gap resync, incompatible schema, displaced lease, blocked agent, and local failure with no remote fallback.
- [ ] Keep scenario mode explicitly labelled Demo in the header.
- [ ] Commit as `feat: connect tactical ops scenario workflow`.

### Task 6: Promote Tactical Ops after parity

- [ ] Run Go unit, race, security, integration, vet, coverage, fixture parity, and terminal golden suites.
- [ ] Compare the Allow and Deny state digests against Web for identical fixture streams.
- [ ] Make Tactical Ops the default only after all gates pass; retain `--ui=classic` for one deprecation cycle.
- [ ] Document keys and migration, then commit as `feat: promote tactical ops tui`.

## Phase 3 acceptance

- Web and TUI reach identical committed state for canonical fixtures.
- Terminal rendering remains safe under hostile text and small dimensions.
- Approval and control authority remain runtime-owned.
- TUI remains visibly Demo until Phase 4 real sources are enabled.

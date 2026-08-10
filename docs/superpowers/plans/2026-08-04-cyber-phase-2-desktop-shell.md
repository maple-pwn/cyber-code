# CYBER Phase 2 Desktop Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver an installable Tauri 2 desktop client that reuses the Liquid Command product shell and adds tightly scoped native capabilities without claiming real runtime execution.

**Architecture:** Extract React application composition into `packages/product-app`, retain separate Web and desktop bootstraps, and expose native operations through a typed Tauri command boundary. Desktop runs the deterministic source until Phase 4 supplies a real local source.

**Tech Stack:** React 19, TypeScript 5.9, Tauri 2, Rust stable, pnpm 10, Vitest, Playwright, cargo test.

---

## File structure

| Path | Responsibility |
| --- | --- |
| `packages/product-app/src/` | Shared routes, store composition, pages, and shell |
| `apps/web/src/main.tsx` | Browser bootstrap and scenario source selection |
| `apps/desktop/src/main.tsx` | Desktop bootstrap and native capability adapter |
| `apps/desktop/src/native.ts` | Typed invoke wrapper with no generic shell access |
| `apps/desktop/src-tauri/` | Tauri configuration, commands, state, and tests |
| `.github/workflows/ci.yml` | Rust checks and platform build matrix |

### Task 1: Extract the shared product application

**Files:** Move `apps/web/src/{App.tsx,app-store.ts,pages}` to `packages/product-app/src/`; modify Web imports and workspace references.

- [ ] Add failing package tests proving Web routes and exact runtime commands remain unchanged after extraction.
- [ ] Create `@cyber/product-app` with peer dependencies on React and workspace dependencies on protocol, runtime-client, UI, and i18n.
- [ ] Export `ProductApp`, `AppStore`, and `createAppStore`; keep source construction outside the package.
- [ ] Run `pnpm exec vitest run packages/product-app apps/web/src` and `pnpm typecheck`; expect all existing tests to pass.
- [ ] Commit as `refactor: share cyber product application`.

### Task 2: Bootstrap a minimal Tauri 2 application

**Files:** Create `apps/desktop/package.json`, `apps/desktop/index.html`, `apps/desktop/src/main.tsx`, and `apps/desktop/src-tauri/{Cargo.toml,tauri.conf.json,src/lib.rs,src/main.rs}`.

- [ ] Add a smoke test asserting the desktop bootstrap labels the source `Demo` and never labels it `Local`.
- [ ] Configure the Tauri dev URL and frontend distribution without enabling shell or unrestricted filesystem plugins.
- [ ] Render `ProductApp` with `ScenarioPlayer({ runtimeId: 'scenario-local' })` and desktop capability metadata.
- [ ] Run `pnpm --filter @cyber/desktop typecheck`, `cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml`, and `cargo tauri build --debug`.
- [ ] Commit as `build: bootstrap cyber desktop shell`.

### Task 3: Add a typed native capability boundary

**Files:** Create `apps/desktop/src/native.ts`, `apps/desktop/src/native.test.ts`, and `apps/desktop/src-tauri/src/commands.rs`.

- [ ] Define only these initial operations: `capabilities`, `notify`, `store_secret`, `delete_secret`, and `export_report`.
- [ ] Test request/response validation, path traversal rejection, notification allowlisting, secret redaction, and unsupported-command errors.
- [ ] Store secrets through the OS credential service; never return secret values to React after storage.
- [ ] Restrict report export to user-selected destinations and immutable bytes supplied by the report exporter.
- [ ] Run focused Vitest, cargo tests, and `cargo clippy --all-targets -- -D warnings`.
- [ ] Commit as `feat: add bounded desktop capabilities`.

### Task 4: Implement window, focus, and notification behavior

**Files:** Modify desktop Rust state and shared product shell; create `apps/desktop/tests/window.spec.ts`.

- [ ] Test window restoration within the current monitor, single-instance focus, deep-link rejection, and notification routing for approvals and terminal task states only.
- [ ] Restore Inspector focus after overlays and preserve the active route across safe window restarts.
- [ ] Verify reduced motion, keyboard-only golden path, 1024x768 minimum layout, and 200% OS scaling.
- [ ] Commit as `feat: complete desktop interaction shell`.

### Task 5: Add desktop security and packaging gates

**Files:** Modify Tauri capabilities/CSP, CI, release manifest, and security tests.

- [ ] Deny arbitrary command execution, arbitrary URL opening, wildcard filesystem paths, remote code loading, and untrusted navigation.
- [ ] Add Linux, Windows, and macOS debug-build jobs plus Rust format, clippy, audit, unit, and frontend checks.
- [ ] Produce unsigned CI artifacts; keep signing and notarization in protected release workflows only.
- [ ] Run `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings`, `cargo test`, `pnpm test:coverage`, and `pnpm test:e2e`.
- [ ] Commit as `ci: verify cyber desktop application`.

## Phase 2 acceptance

- Desktop and Web display identical ProductState for the same fixture stream.
- Desktop is explicitly marked Demo and cannot execute a real task.
- Native APIs are allowlisted, typed, tested, and deny by default.
- Secrets do not appear in events, browser storage, logs, snapshots, or reports.
- CI builds the shell on Linux, Windows, and macOS without weakening existing jobs.

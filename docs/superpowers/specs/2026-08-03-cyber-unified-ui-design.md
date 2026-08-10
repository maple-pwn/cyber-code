# CYBER Unified UI Product Design

**Date:** 2026-08-03  
**Status:** Approved  
**Product:** `cyber-code` (`CYBER` in the interface)  
**Core runtime:** `cyber-agent runtime`

## 1. Purpose

Build one security-agent product with three native entry points:

- a Web console;
- a Tauri desktop application;
- a terminal UI.

The product is conversation-driven and primarily designed for security researchers and penetration testers. `cyber-code` owns the product experience. `cyber-agent` remains an independently deployable, UI-free security runtime.

Phase 1 delivers a high-fidelity, product-grade Web prototype driven by deterministic mock events. It validates the complete authorized Web-assessment workflow before either runtime is integrated.

## 2. Product decisions

### 2.1 One product, three entry points

All clients share:

- domain concepts and event semantics;
- task and session identity;
- authorization and approval semantics;
- navigation terminology;
- keyboard conventions;
- severity, confidence, and status semantics;
- bilingual terminology.

They do not share rendering code where the medium differs. Web and desktop share React components. The Go TUI implements an equivalent terminal-native presentation.

### 2.2 Primary workflow

The default experience is a conversation-driven security workspace:

```text
Describe objective
→ review authorization scope
→ run and observe agents
→ approve bounded risky actions
→ inspect Findings and Evidence
→ review and freeze report
```

Task monitoring, causal graphs, and multi-agent topology are supporting views rather than the initial landing experience.

### 2.3 Primary user

Phase 1 is optimized for security researchers and penetration testers. The interface exposes professional details through progressive disclosure rather than hiding them:

1. current target, progress, key Findings, and next action;
2. tools, agents, approvals, Evidence, and causal relationships;
3. raw output, parameters, budgets, receipts, and audit metadata.

### 2.4 Authorization model

Every new active security task passes through a Scope Review Sheet. It displays:

- selected runtime and principal;
- targets and allowed scope;
- workspace;
- allowed actions;
- explicitly denied actions;
- risk ceiling;
- task-only validity.

The confirmed scope becomes an immutable task-level snapshot. Widening the target set or risk level requires a new confirmation.

Low-risk operations admitted by the confirmed scope proceed without repeated prompts. High-risk operations require a runtime-generated approval challenge. `destructive` risk is unavailable in Phase 1.

## 3. Architecture and ownership

```text
Web Console         Desktop Application       Terminal UI
React               React + Tauri 2           Go / Bubble Tea
Forensic Noir       Forensic Noir             Tactical Ops
       \                  |                     /
        \                 |                    /
              Shared Product Contract
 Task / Runtime / Agent / Scope / Event / Finding /
        Evidence / Approval / Report / Control Lease
                         |
              Local or Remote Runtime
                         |
               cyber-agent Runtime
```

### 3.1 `cyber-code` owns

- product navigation and conversation;
- task control and cross-client recovery experience;
- normalized approval presentation;
- runtime connection selection and health presentation;
- notifications;
- keyboard interaction and accessibility;
- report review and export UX;
- system-keychain integration in the desktop application.

### 3.2 `cyber-agent` owns

- scope and capability admission;
- skills, agent scheduling, and ToolGateway execution;
- task persistence;
- trusted Evidence and Finding state;
- approval challenge validation and final execution admission;
- audit records and terminal outcomes.

### 3.3 Security invariants

1. A client may present and request actions but cannot expand runtime authority.
2. Client- or model-authored prose cannot become trusted Evidence.
3. Local and remote runtimes expose the same product contract.
4. The runtime owns task lifetime; clients restore state through cursored events.
5. One task has at most one controlling client at a time.
6. A disconnected client never implies approval.
7. The TUI and React clients share state semantics, not rendering code.
8. Mock and real event sources implement the same interface.

## 4. Repository structure

Phase 1 extends `cyber-code` into a Go, TypeScript, and later Rust product repository:

```text
cyber-code/
├── apps/
│   ├── web/                    # React Web console
│   └── desktop/                # Tauri 2 shell (Phase 2)
├── packages/
│   ├── protocol/               # Domain types and product events
│   ├── runtime-client/         # Scenario/local/remote event sources
│   ├── scenario-player/        # Deterministic mock event driver
│   ├── ui/                     # Forensic Noir components
│   └── i18n/                   # Translations and terminology
├── internal/
│   └── ui/                     # Existing Go TUI
└── editors/vscode/
```

The JavaScript workspace uses pnpm workspaces, Vite, TypeScript project references, and root scripts for lint, type checking, tests, and builds. Phase 1 does not introduce Turborepo or Nx. The VS Code extension should later join the workspace to avoid two JavaScript installation systems.

## 5. Visual language

### 5.1 Web and desktop: Forensic Noir

- graphite-black and cool-gray surfaces;
- restrained low-saturation mint accent;
- amber for pending actions, verification, and medium risk;
- red only for failure, blocking state, and severe Findings;
- minimal ornamentation;
- no neon glow, decorative glassmorphism, or movie-hacker styling;
- comfortable long-duration reading.

### 5.2 TUI: Tactical Ops

- monospaced typography;
- compact grids and terminal status symbols;
- restrained green for live execution paths;
- amber for approval and unverified state;
- semantic line frames and text hierarchy;
- no full-screen fluorescent green.

### 5.3 Shared semantics

- color expresses Finding severity only;
- confidence uses dots/shapes and text;
- verification status uses an independent text badge;
- all essential meaning has a non-color representation.

Severity:

```text
Critical  red
High      orange-red
Medium    amber
Low       blue-gray
Info      cool gray
```

Confidence:

```text
●●● High
●●○ Medium
●○○ Low
```

Finding status:

```text
candidate · verifying · confirmed · rejected · mitigated
```

## 6. Information architecture

### 6.1 Primary navigation

```text
Conversation · Tasks · Evidence · Reports · Settings
```

Assets, Findings, causal graphs, audit, and agent topology live inside task context rather than expanding the primary navigation.

### 6.2 Mission Control layout

At wide desktop widths:

```text
┌──────────┬──────────────────────────────┬──────────────────┐
│ Primary  │ Task header                  │ Inspector        │
│ nav      ├──────────────────────────────┤                  │
│          │ Narrative Stream             │ Agents           │
│          │                              │ Scope            │
│          │ Messages                     │ Evidence         │
│          │ Agent events                 │                  │
│          │ Tools / Findings / approvals │                  │
│          ├──────────────────────────────┤                  │
│ Runtime  │ Composer                     │                  │
└──────────┴──────────────────────────────┴──────────────────┘
```

The right Inspector has three tabs:

```text
AGENTS · SCOPE · EVIDENCE
```

`AGENTS` is the default. Updates add badges but do not forcibly switch a user away from the current tab.

### 6.3 Narrative Stream

User messages, agent responses, meaningful tool milestones, Evidence commits, Findings, questions, and approvals appear in one chronological stream.

Default cards show:

- agent and current action;
- tool name, target, duration, and outcome;
- Finding severity and verification state;
- Evidence count and key summary;
- scope and risk ceiling.

Expanded views reveal:

- complete normalized parameters;
- bounded raw stdout/stderr or artifact references;
- budget usage;
- model, skill, receipt, and audit identifiers.

Routine scheduler noise does not enter the main narrative.

### 6.4 Agent Inspector

Web and desktop keep the Agent Inspector available in the right sidebar. Each agent row shows name, state, progress, and current action. Selecting an agent reveals:

- objective;
- runtime state;
- budget use;
- admitted tools;
- child tasks;
- recent events.

A full agent topology remains available as a professional task detail view.

The TUI shows an agent summary in the header and opens the task panel with `Ctrl+T`. `Enter` opens the selected agent, `[` and `]` switch agents, and `Esc` returns to the parent thread.

## 7. Golden-path pages

### 7.1 New Task

- conversation-style objective entry;
- local/remote runtime selection;
- recent workspaces;
- connection and capability summary.

Local runtime is the default in desktop and TUI clients. Web requires an initial connection choice. A local failure never silently sends work to a remote runtime.

### 7.2 Scope Review Sheet

A single-page professional review replaces a multi-step wizard. The page shows all authority-relevant fields simultaneously and offers edit or confirm actions.

### 7.3 Mission Control

- Narrative Stream;
- Inspector;
- approval cards;
- pause, resume, cancel;
- explicit cross-client takeover;
- connection and cursor state.

### 7.4 Finding and Evidence

A Finding is a security conclusion. Evidence is immutable trusted support for that conclusion.

```text
Finding
├── title, severity, status
├── affected scope
├── confidence
├── supporting Evidence
│   ├── tool receipts
│   ├── HTTP Evidence
│   ├── service fingerprints
│   └── verification results
└── remediation guidance
```

Unassociated trusted facts appear under “Other Evidence.” Model prose is explanatory only and cannot raise verification state.

### 7.5 Report Review

```text
Task completed
→ runtime drafts from confirmed Findings and Evidence
→ user performs structured review
→ citation integrity validation
→ freeze report version
→ export Markdown / HTML / PDF / JSON
```

Users may edit narrative and recommendations and add human notes. They may not edit raw Evidence. Excluding a Finding requires an audited reason. Generated content and human additions remain distinguishable.

## 8. Responsive and client-specific behavior

### 8.1 Breakpoints

- `>=1280px`: full three-column Mission Control;
- `768–1279px`: collapsed primary navigation and overlay Inspector;
- tablet: single-column workspace with Inspector drawer;
- phone: observation and emergency decisions only.

Phone clients may:

- observe task state;
- read key Findings;
- receive notifications;
- inspect normalized approval parameters;
- approve or deny;
- pause or cancel.

Phone clients may not create complex scopes, edit reports, analyze graphs, compare large Evidence, or operate a local workspace.

### 8.2 TUI scope

The TUI is a complete task control client supporting:

- task creation and Scope Review;
- Narrative Stream;
- Agent panel;
- Finding and Evidence viewing;
- approval;
- pause, cancel, takeover, and recovery;
- report preview and export.

Complex report editing, full graphs, large Evidence comparison, and advanced audit queries open in Web or desktop.

## 9. Hybrid runtime and identity

### 9.1 Runtime selection

Phase 1 models local and remote runtimes as equal contract implementations, while keeping local-first behavior:

- desktop and TUI default to local;
- Web asks for an initial connection;
- remote selection is explicit;
- runtime location remains visible in the task header and Scope Review Sheet;
- an existing task never changes runtime.

### 9.2 Remote identity

Phase 1 models a remote connection as:

```text
name + endpoint + personal token
```

Desktop tokens belong in the OS keychain. TUI reads from the keychain or environment. Browser clients must not place tokens in ordinary local storage. The runtime reports `principal`, `role`, and `capabilities`.

Team, organization, invitation, and complete RBAC functionality are deferred.

### 9.3 Task ownership and control lease

The runtime owns the task. Execution continues after ordinary client disconnects. A task awaiting approval remains safely blocked.

One client holds a single-writer control lease. Other clients observe. A new client may explicitly take control:

1. runtime atomically increments the lease revision and transfers control;
2. the old controller becomes read-only immediately;
3. the old client receives a notification if connected;
4. pending approvals remain valid but only the new controller may answer;
5. the transfer is audited.

Old-client confirmation is not required, avoiding deadlock after device loss.

## 10. Approval and safety interaction

A runtime creates an immutable approval challenge. The client presents normalized fields and returns only the user decision. The runtime remains the final admission authority.

Approval cards show:

- requesting agent;
- normalized action;
- target and scope binding;
- parameter summary and full-detail entry;
- risk and expected impact;
- replay semantics;
- expiry;
- `Allow once` and `Deny`.

Rules:

1. clients cannot modify approved parameters;
2. responses bind to the challenge ID;
3. runtime revalidates parameter digest, scope, task state, and expiry;
4. a challenge resolves at most once;
5. expired challenges cannot be approved;
6. client disconnect defaults to no approval;
7. approval has no easy-to-mistype single-key shortcut.

## 11. Event model and state projection

### 11.1 Data flow

```text
Scenario / Local / Remote EventSource
→ Event Normalizer
→ State Projector
→ ProductState
→ React UI / Tauri shell / Go TUI adapter
```

The UI does not maintain separate authoritative business state inside pages. Recoverable business state comes from ordered immutable runtime events.

### 11.2 Event envelope

```ts
type ProductEvent<TType extends EventType, TPayload> = {
  schemaVersion: 1
  eventId: string
  taskId: string
  cursor: number
  occurredAt: string
  type: TType
  source: {
    runtimeId: string
    agentId?: string
    toolCallId?: string
  }
  payload: TPayload
}
```

Constraints:

- event IDs are unique within a runtime;
- cursors strictly increase within a task;
- clients deduplicate by event ID;
- unknown events remain in raw logs but do not receive speculative projection;
- unknown schema versions fail explicitly;
- large outputs use artifact references;
- client timestamps never replace runtime occurrence time.

### 11.3 Event families

Lifecycle:

```text
task.created
task.started
task.paused
task.resumed
task.cancel.requested
task.cancelled
task.completed
task.failed
task.blocked
```

Authority:

```text
scope.proposed
scope.confirmed
runtime.capabilities.updated
control.acquired
control.transferred
control.released
approval.requested
approval.resolved
question.requested
question.resolved
```

Execution and evidence:

```text
agent.started
agent.progressed
agent.completed
agent.failed
tool.started
tool.completed
tool.failed
evidence.committed
finding.created
finding.verifying
finding.confirmed
finding.rejected
finding.mitigated
```

Reporting:

```text
report.drafted
report.edited
report.validation.failed
report.validated
report.frozen
report.exported
```

### 11.4 Product state

```ts
type ProductState = {
  connection: ConnectionState
  activeRuntime: RuntimeSummary | null
  task: TaskState | null
  scope: ScopeSnapshot | null
  controlLease: ControlLease | null
  agents: Record<string, AgentState>
  timeline: TimelineItem[]
  approvals: Record<string, ApprovalState>
  findings: Record<string, FindingState>
  evidence: Record<string, EvidenceState>
  report: ReportState | null
}
```

The projector is a pure deterministic function:

```ts
project(previousState, event): ProductState
```

### 11.5 Recovery

The client records the last committed cursor, reconnects, and requests `events(afterCursor)`. If a gap appears:

1. enter `resyncing`;
2. disable writes and approval responses;
3. request a snapshot and subsequent events;
4. validate the snapshot cursor;
5. resume only after continuity is restored;
6. show an explicit unrecoverable state if continuity cannot be proven.

## 12. Scenario player

Phase 1 uses an event-driven scenario player implementing the real EventSource shape. The authorized local Web lab scenario branches on approval:

```text
Scope confirmed
→ reconnaissance
→ Web assessment
→ approval requested
   ├── Allow
   │   → bounded verification
   │   → confirmed High Finding
   │   → verified-impact report
   └── Deny
       → no active verification
       → candidate or rejected Finding
       → report records Evidence limitation
```

The player supports speed control, pause, resume, cancel, disconnect/reconnect, cursor replay, takeover, tool failure, and blocked-agent states.

The mock target is an explicitly authorized local laboratory such as `juice-shop.lab`, never a real public target.

## 13. Error and notification model

Errors use four presentation levels:

- **Inline:** tool, verification, or approval failures tied to a timeline event;
- **Banner:** connection degradation, read-only state, resync, or capability change;
- **Blocking surface:** invalid scope, incompatible protocol, expired credentials, or unrecoverable cursor gap;
- **System notification:** actionable and important terminal events only.

Every error communicates:

1. what happened;
2. whether the task remains safe;
3. which state remains trusted;
4. what the user can do next.

Connection states:

```text
connecting · healthy · degraded · reconnecting · resyncing
· offline · incompatible · unauthorized
```

Task terminal/blocking states remain distinct:

```text
blocked_retryable · blocked_capability · failed_terminal
· cancelled · completed
```

System notifications are limited to:

- approvals;
- agent questions;
- blocked or failed tasks;
- confirmed High/Critical Findings;
- completion;
- control takeover;
- connection problems that affect execution.

Routine tools, scheduling, low-level Evidence, candidate Findings, budget updates, and normal stage changes remain in the application.

## 14. Accessibility, keyboard, and localization

### 14.1 Accessibility

Web and desktop target keyboard-first operation and WCAG AA:

- every core workflow works without a mouse;
- visible focus rings and skip-to-content;
- correct screen-reader names and semantic roles;
- focus trapping and restoration for dialogs;
- non-color state encoding;
- AA contrast;
- `prefers-reduced-motion` support.

### 14.2 Keyboard conventions

- `Ctrl/Cmd+K`: command palette;
- `g` then `c/t/e/r/s`: Conversation, Tasks, Evidence, Reports, Settings;
- `Ctrl/Cmd+Enter`: submit;
- `Ctrl+T`: Agent Inspector/task panel across GUI and TUI;
- approvals require deliberate multi-key navigation plus explicit activation.

### 14.3 Localization

The default interface language is Simplified Chinese, with complete English support. UI strings use i18n keys from the beginning.

Standard terms such as Finding, Evidence, Scope, Runtime, Agent, and CVSS remain English or bilingual according to the shared terminology table. Raw tool output is not translated automatically. Report language is independently selectable.

## 15. Report integrity

Reports are generated as drafts from confirmed Findings and trusted Evidence. Human edits and generated text remain distinguishable. Raw Evidence is immutable.

Before freezing, validation checks:

- every included Finding has valid Evidence references;
- referenced Evidence belongs to the task;
- excluded Findings include an audit reason;
- no mutable draft reference is presented as frozen;
- the export format records report version and task identity.

## 16. Phase 1 testing

### 16.1 Unit tests

- event schema valid and invalid cases;
- event ID idempotency;
- cursor monotonicity and gaps;
- unknown event behavior;
- unsupported schema rejection;
- deterministic replay;
- Allow/Deny branch determinism;
- immutable Evidence;
- legal Finding state transitions;
- approval expiry and one-shot behavior;
- lease revision monotonicity.

### 16.2 Component and accessibility tests

Priority components:

- `ScopeReviewSheet`;
- `NarrativeStream`;
- `ApprovalCard`;
- `AgentInspector`;
- `FindingCard`;
- `EvidenceDrawer`;
- `ReportEditor`;
- `CommandPalette`;
- `ConnectionBanner`;
- `ControlLeaseBanner`.

Tests cover keyboard order, screen-reader naming, dialog focus behavior, tab semantics, non-color states, AA contrast, reduced motion, and bilingual text expansion.

### 16.3 End-to-end paths

**Allow path:** create task, confirm scope, observe agents, approve bounded verification, inspect confirmed Finding and Evidence, edit report notes, validate, freeze, and export.

**Deny path:** deny verification, preserve the limitation, keep or reject the Finding according to Evidence, and generate an honest report.

**Recovery path:** disconnect during execution, enter read-only cached mode, reconnect from cursor, explicitly take control, and continue.

## 17. Phase 1 acceptance criteria

All criteria are mandatory:

1. create an authorized Web-lab task from empty state;
2. complete Scope Review;
3. display a multi-agent Narrative Stream;
4. progressively display tools and Evidence;
5. produce valid distinct Allow and Deny branches;
6. pause, resume, and cancel;
7. simulate disconnection and cursor recovery;
8. display Finding/Evidence relationships;
9. draft, review, freeze, and export a report;
10. switch Chinese/English without layout failure;
11. complete the golden path using only the keyboard;
12. meet WCAG AA for the primary flow;
13. operate at common desktop and laptop widths;
14. provide phone observation, approval, pause, and cancel;
15. produce no browser console errors;
16. cover core projection and branches with automated tests.

## 18. Phase boundaries

### Phase 1: Web prototype

Includes:

- pnpm workspace;
- shared protocol, runtime client, scenario player, UI, and i18n packages;
- Forensic Noir design system;
- complete Web golden path;
- mock local/remote connection states;
- responsive behavior, accessibility, keyboard support, and tests.

Excludes:

- real cyber-code or cyber-agent runtime integration;
- native Tauri process management;
- Go TUI redesign;
- embedded terminal;
- Monaco editor;
- asset relationship graph;
- teams, organizations, invitations, and complete RBAC;
- production remote authentication;
- real public-target security testing.

### Later phases

```text
Phase 2  Tauri shell and desktop-native capabilities
Phase 3  Tactical Ops TUI mapping
Phase 4  Real local and remote runtime EventSources
Phase 5  Terminal, editor, asset graph, and team capabilities
```

## 19. Delivery approach

Use shared-foundation-first delivery:

```text
protocol and event model
→ design tokens and primitives
→ scenario player and projector
→ Web shell
→ Scope and Mission Control
→ Finding/Evidence and report
→ responsive, i18n, accessibility, and tests
→ desktop shell
→ TUI mapping
```

This order protects the shared semantics without trying to implement all three clients in parallel.

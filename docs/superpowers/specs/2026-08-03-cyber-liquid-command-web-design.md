# CYBER Liquid Command Web Design

**Date:** 2026-08-03  
**Status:** Approved  
**Product:** `cyber-code` Web console  
**Parent specification:** `2026-08-03-cyber-unified-ui-design.md`

## 1. Purpose

Replace the Phase 1 Web prototype's inexpensive, default-looking presentation with a coherent professional security workspace. The redesign is named **Liquid Command**: Apple-like frosted glass carries a dense Mission Control interface without weakening security semantics, runtime authority, accessibility, or the existing golden path.

This specification refines the Web visual and interaction layer. The parent unified UI specification remains authoritative for product concepts, protocol semantics, scope, Evidence, control leases, and cross-client behavior.

## 2. Goals and non-goals

### 2.1 Goals

- Make the first viewport feel like a finished professional security product.
- Use glass to establish hierarchy over real Evidence context, not as decoration.
- Preserve high information density and fast scanning for security practitioners.
- Turn inline approval into a complete decision surface rather than a generic card.
- Keep Narrative Stream, Agent Inspector, Scope, Evidence, Findings, and reports visually coherent.
- Preserve responsive, keyboard, reduced-motion, and runtime recovery behavior.
- Keep the bottom-right active-agent indicator as a useful persistent signal.

### 2.2 Non-goals

- No protocol, projector, runtime authority, or Evidence integrity changes.
- No Phase 2 runtime integration, desktop implementation, or TUI redesign.
- No new destructive capability or expanded approval authority.
- No complex Scope editing, report editing, graph analysis, or large Evidence comparison on phones.
- No decorative landing page, marketing hero, ambient blobs, or card-within-card composition.

## 3. Approved direction

The approved direction combines these decisions:

| Area | Decision |
| --- | --- |
| Overall material | Liquid Glass |
| Approval placement | Inline Glass Panel in the Narrative Stream |
| Information density | High density |
| Primary navigation | Floating icon rail |
| Background | Real Evidence context |
| Glass opacity | Strongly frosted; context remains perceptible but denoised |
| Active-agent indicator | Persistent bottom-right floating ribbon |
| Accent system | Semantic adaptive |
| Typography | Humanist system sans; monospace only for technical tokens |
| Motion | Tactical Snap on state changes; static at rest |
| Main surface shape | Soft capsule with large, controlled radii |
| Desktop composition | Stable Command Deck with fixed Inspector |
| Phone composition | Continuity Stack with inline approval |
| Approval protection | Review, then confirm |
| System states | Preserve the last meaningful context |

## 4. Visual system

### 4.1 Evidence-backed canvas

Mission Control uses the active request, response, terminal output, Evidence metadata, or report content as the spatial background. This context is real task material and must correspond to the current selection. It must not be stock imagery, a decorative gradient illustration, or fabricated telemetry.

The background is deliberately subdued beneath working surfaces:

- low-contrast ink and text;
- strong blur through foreground glass;
- no sensitive value made more prominent merely because it is behind a panel;
- enough recognizable structure to communicate where the operator is working.

When no active Evidence exists, use the recent target and report context. The empty state must not invent request or response data.

### 4.2 Surface hierarchy

Glass is limited to structural layers:

1. floating navigation rail;
2. Narrative Stream workspace;
3. fixed or overlay Inspector;
4. inline approval decision surface;
5. active-agent ribbon;
6. drawers, dialogs, and recovery overlays already required by the product.

Timeline events, fact rows, status lines, and ordinary page sections remain unframed. They use dividers, spacing, and typography rather than becoming nested cards.

Recommended base tokens:

```css
--canvas: #070b0e;
--surface-glass: rgb(18 24 29 / 61%);
--surface-glass-strong: rgb(18 24 29 / 72%);
--glass-border: rgb(255 255 255 / 21%);
--glass-highlight: rgb(255 255 255 / 30%);
--text-primary: #edf3f4;
--text-secondary: #8b9ca5;
--accent-running: #75d9c3;
--accent-pending: #edc76e;
--accent-failure: #ec6969;
```

Final tokens may be adjusted only to meet contrast and cross-browser rendering requirements. Their semantic roles must not change.

Primary structural surfaces use approximately `24px` radii. Nested decision surfaces and repeated agent rows use `14-18px`; compact controls use `8-12px`. Text labels must not be placed in pills when an icon, plain label, or status row is clearer.

### 4.3 Glass treatment

Foreground surfaces use a strongly frosted treatment equivalent to approximately `blur(24px)` with restrained saturation. Each surface has one thin translucent border, a subtle top or inset highlight, and one deep ambient shadow. Double rims, multiple glow outlines, bright gradients, and continuous shimmer are excluded.

When `backdrop-filter` is unavailable, the fallback surface becomes more opaque. Text contrast and hierarchy must remain equivalent without blur.

### 4.4 Typography and icons

The primary typeface is the platform's humanist system sans stack, with Apple system fonts preferred where available. Headings use restrained sizes and medium-to-semibold weight. No viewport-scaled typography or negative letter spacing is allowed.

Monospace is reserved for values whose shape is operationally meaningful:

- digests and identifiers;
- timestamps and cursors;
- methods, paths, and normalized parameters;
- raw Evidence and protocol output.

Navigation and tool actions use familiar icons from the existing icon library. Icons must have accessible names or tooltips where their meaning is not universal. Rounded, text-filled navigation controls are not used when an icon is sufficient.

### 4.5 Semantic color

- Mint indicates healthy connection, active progress, verified continuity, and the primary permitted action.
- Amber indicates pending approval, waiting, verification, expiry pressure, or another state requiring attention.
- Red is reserved for failure, protocol incompatibility, severe risk, or a blocked recovery state.
- Neutral text and borders carry ordinary hierarchy.

Every status includes a text label or equivalent accessible name; color never carries meaning alone.

### 4.6 Motion

Tactical Snap is a short, decisive transition used when an event enters, an approval changes state, a panel opens, or continuity is restored. A typical transition lasts `120-180ms`, using opacity and a small translation. Surfaces remain static after settling.

Continuous floating, breathing panels, decorative parallax, and looping glow are excluded. `prefers-reduced-motion: reduce` removes translation and nonessential animation while preserving immediate state feedback.

## 5. Mission Control composition

### 5.1 Desktop Command Deck

At `>=1280px`, Mission Control uses three stable regions:

```text
┌──────────┬──────────────────────────────────┬────────────────────┐
│ Floating │ Task header + Narrative Stream   │ Fixed Inspector    │
│ icon rail│ Inline approvals and Findings    │ Agents/Scope/      │
│          │ Composer and task controls       │ Evidence tabs      │
└──────────┴──────────────────────────────────┴────────────────────┘
                                          Active Agent ribbon ↗
```

The icon rail is narrow and vertically centered within its glass surface. The Narrative Stream receives the majority of width. The Inspector remains visible and does not cover the stream. Task state, target, risk, and cursor stay near the top without becoming a separate dashboard of metric cards.

### 5.2 Laptop and tablet

At `768-1279px`, primary navigation collapses to a compact floating rail. The Inspector becomes a glass overlay or drawer opened by an explicit control. Opening it preserves the Narrative Stream underneath, traps focus where required, and restores focus on close.

At narrower tablet widths, the workspace becomes a single readable column. Inspector content remains accessible through the same tabs in a drawer.

### 5.3 Phone Continuity Stack

Phones use a single chronological column. The phone retains:

- task observation and current state;
- key Findings and notifications;
- normalized approval review, Deny, and Confirm allow once;
- pause and cancel;
- compact navigation and the active-agent indicator.

Approval stays at its causal position in the Narrative Stream. It does not become a bottom sheet or take over the entire screen. The active-agent ribbon compresses into a small floating capsule above bottom navigation and must not cover decision controls or timeline content.

## 6. Core components

### 6.1 Floating navigation rail

The rail exposes existing primary destinations with icons, selected state, tooltips, keyboard focus, and notification badges. It maximizes working width and must not become a second source of task state.

### 6.2 Narrative Stream

The stream remains the chronological source of user messages, agent responses, meaningful tool milestones, Evidence commits, Findings, questions, approvals, and recovery events. Events use dividers and a compact causal marker rather than individual cards.

New events use Tactical Snap. Inactive tabs may update counts but never steal selection or scroll position.

### 6.3 Inline approval panel

The approval panel is a complete decision surface embedded at the causal point in the stream. Its waiting state displays:

- risk and explicit `Approval required` label;
- title and plain-language impact;
- target and normalized action;
- agent identity;
- digest or immutable challenge fingerprint;
- expiry countdown;
- attempt limit and persistence behavior where applicable;
- `Deny` and `Review parameters` actions.

The panel is amber because it is pending, not because approval is recommended.

### 6.4 Agent Inspector

The Inspector retains the `Agents | Scope | Evidence` tabs. Agent rows show name, state, progress, and current action. Selecting an agent reveals its current tool, budgets, causal parent, Evidence, and recent milestones using unframed detail rows inside the Inspector surface.

### 6.5 Active-agent ribbon

The bottom-right ribbon remains globally visible in Mission Control. It shows:

- active agent;
- current action;
- progress;
- wait reason when blocked.

It is mint while actively progressing, amber while awaiting approval or another operator decision, and red only for a genuine failure. It is not a toast and does not disappear on a timer. Selecting it focuses the corresponding agent in the Inspector.

## 7. Approval interaction and authority

Approval is a three-state process:

1. **Waiting:** show the immutable challenge summary; `Deny` resolves immediately, while `Review parameters` opens the complete normalized view in place.
2. **Reviewing:** show all normalized fields and current control-lease identity. The final `Confirm allow once` action appears only here. A changed or expired challenge, changed digest, lost control lease, disconnect, or resync exits this state and disables confirmation.
3. **Resolved:** project the runtime's immutable resolution event and provide navigation to its causal event or resulting Evidence.

The browser never authors approved parameters. On confirmation it sends only:

```ts
{
  type: 'approval.respond',
  challengeId,
  decision: 'allow'
}
```

The runtime remains the final admission authority. Approval has no easy-to-mistype single-key shortcut. Keyboard activation requires deliberate navigation plus explicit confirmation. Touch targets are at least `44px` where the pointer is coarse. Deny and allow actions remain spatially distinct.

## 8. Empty, loading, and failure states

System states preserve meaningful context instead of replacing the workspace with a generic full-page message.

- **Empty:** show recent legitimate target or report context and a clear New Task action. Do not fabricate Evidence.
- **Loading:** keep stable layout dimensions and show progress without causing panel movement.
- **Resyncing:** preserve cached Narrative Stream and Inspector state, display the expected continuity check, and lock every write.
- **Offline:** preserve the last trusted cursor and view, label it read-only, disable controls and approvals, and expose Reconnect.
- **Incompatible or unauthorized:** explain the blocking state, keep untrusted data out of the projection, and disable all commands.
- **Inline failures:** attach tool, verification, or approval errors to their causal event.

No disconnected state implies approval. No recovery animation may suggest that task authority moved to the browser.

## 9. State and data flow

The redesign preserves the existing unidirectional boundary:

```text
Trusted runtime
  owns Scope, ApprovalChallenge, Evidence, cursor, control lease
        ↓ validated events and snapshots
Projector
  validates ordering and produces ProductState
        ↓ ProductState only
Liquid Command UI
  renders state and sends minimal RuntimeCommand values
```

Visual review state, open tabs, drawer state, and focus restoration are local UI concerns. They cannot modify `ProductState`, expand Scope, mutate Evidence, change a challenge digest, or claim control ownership.

## 10. Accessibility and performance

- Meet WCAG AA contrast after compositing glass over the brightest supported Evidence background.
- Preserve semantic landmarks, headings, tabs, buttons, dialogs, drawers, and live regions.
- Provide visible focus independent of color and blur.
- Maintain text labels for severity, connection, waiting, failure, and control state.
- Keep controls stable under long translated labels and at phone widths.
- Avoid layout shifts when counts, progress, countdowns, or Agent state changes.
- Disable nonessential motion under reduced-motion preference.
- Use opaque fallbacks for unsupported or performance-constrained backdrop filtering.
- Limit blur to structural surfaces and avoid stacking multiple blurred ancestors.

## 11. Phase scope

This redesign applies to the existing Phase 1 Web golden path:

1. New Task;
2. Scope Review;
3. Mission Control and recovery;
4. Findings and Evidence inspection;
5. report review and freeze.

Mission Control establishes the design language. The other Phase 1 pages reuse the same canvas, surface hierarchy, typography, semantic color, controls, states, and responsive rules without imitating Mission Control's three-column layout where it is unnecessary.

## 12. Verification and acceptance

The implementation plan must add or update tests for:

- component behavior and approval review-state transitions;
- unchanged runtime command shapes and authority invariants;
- desktop, laptop, tablet, and phone landmarks;
- no overlap among the rail, Inspector, Narrative Stream, approval panel, and active-agent ribbon;
- long labels, countdown changes, progress changes, and translated content without layout shift or overflow;
- empty, loading, resyncing, offline, incompatible, and unauthorized states;
- keyboard focus, focus restoration, tab semantics, deliberate approval activation, and `axe` scans;
- reduced-motion computed styles;
- fallback rendering without `backdrop-filter`;
- Playwright screenshots for the five golden-path pages at representative desktop and phone widths;
- canvas or screenshot pixel checks that detect blank, clipped, or incorrectly composited primary surfaces;
- no console errors throughout the golden path.

Existing protocol, projector, runtime client, workspace build, Go, race, security, integration, vet, VS Code, coverage, and `git diff --check` verification remain required. Visual changes do not reduce the existing test matrix.

## 13. Acceptance summary

The redesign is accepted when:

1. the first Mission Control viewport visibly matches Liquid Command rather than default component-library styling;
2. the desktop Command Deck, laptop overlay Inspector, and phone Continuity Stack preserve the same task semantics;
3. approval remains inline and requires review before allow confirmation;
4. the active-agent ribbon remains useful without obscuring content or controls;
5. failures preserve the last trustworthy context and correctly lock writes;
6. glass remains readable, responsive, accessible, and performant with or without blur support;
7. all existing Phase 1 behavior and authority invariants continue to pass.

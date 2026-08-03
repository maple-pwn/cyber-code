# CYBER Liquid Command Web Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Phase 1 Web prototype's default presentation with the approved Liquid Command glass interface while preserving runtime authority, the complete golden path, responsive behavior, and accessibility.

**Architecture:** Keep protocol and runtime packages unchanged. Extend the shared UI package with visual tokens and focused presentational components, keep approval review state local to `ApprovalCard`, and compose Mission Control from `ProductState` in the Web package. CSS owns breakpoint-specific presentation; React owns semantic open/closed and review states.

**Tech Stack:** React 19, TypeScript 5.9, CSS custom properties and backdrop-filter fallbacks, Lucide React 1.28.0, Testing Library/Vitest, Playwright, axe-core.

---

## File structure

| Path | Responsibility |
| --- | --- |
| `packages/ui/src/tokens.css` | Liquid Command color, glass, radius, spacing, typography, focus, motion, and fallback tokens. |
| `packages/ui/src/components.css` | Shared component structure and glass presentation without Web page layout. |
| `packages/ui/src/components/ApprovalCard.tsx` | Local waiting/reviewing approval interaction; emits only final decision. |
| `packages/ui/src/components/AgentInspector.tsx` | Dense agent progress/action rows and existing Scope/Evidence tabs. |
| `packages/ui/src/components/NarrativeStream.tsx` | Compact causal timeline with human labels and technical metadata. |
| `packages/ui/src/components/ActiveAgentRibbon.tsx` | Persistent active-agent status, progress, action, and wait reason. |
| `packages/ui/src/components/EvidenceBackdrop.tsx` | Read-only contextual Evidence canvas with an honest empty fallback. |
| `packages/i18n/src/index.ts` | New review, Inspector, agent, event, and shell labels in Chinese and English. |
| `apps/web/src/App.tsx` | Icon navigation rail, compact product bar, page context classes, and tooltips. |
| `apps/web/src/pages/MissionControlPage.tsx` | Command Deck composition, Inspector open state, active-agent derivation, and unchanged commands. |
| `apps/web/src/styles.css` | Web shell, page composition, breakpoints, phone continuity stack, and no-overlap rules. |
| `apps/web/tests/visual.spec.ts` | Screenshot, geometry, blur fallback, and primary-surface pixel assertions. |

### Task 1: Establish Liquid Command tokens and glass primitives

**Files:**
- Modify: `packages/ui/src/tokens.test.ts`
- Modify: `packages/ui/src/tokens.css`
- Modify: `packages/ui/src/components.css`

- [ ] **Step 1: Write the failing token tests**

Add assertions for the approved roles and fallbacks:

```ts
test('defines Liquid Command glass, radius, motion, and semantic tokens', () => {
  for (const token of [
    '--cyber-canvas', '--cyber-glass', '--cyber-glass-strong',
    '--cyber-glass-border', '--cyber-glass-highlight',
    '--cyber-running', '--cyber-pending', '--cyber-failure',
    '--cyber-radius-shell', '--cyber-radius-panel', '--cyber-motion-snap',
  ]) expect(css).toContain(token);
  expect(css).toContain('@supports not ((backdrop-filter: blur(1px))');
  expect(css).toContain('color-scheme: dark');
});
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/tokens.test.ts`  
Expected: FAIL because `--cyber-glass` and the backdrop-filter fallback do not exist.

- [ ] **Step 3: Implement the approved token contract**

Replace the base visual values while retaining existing compatibility aliases:

```css
:root {
  color-scheme: dark;
  --cyber-canvas: #070b0e;
  --cyber-glass: rgb(18 24 29 / 61%);
  --cyber-glass-strong: rgb(18 24 29 / 72%);
  --cyber-glass-border: rgb(255 255 255 / 21%);
  --cyber-glass-highlight: rgb(255 255 255 / 30%);
  --cyber-text: #edf3f4;
  --cyber-muted: #9cabb3;
  --cyber-running: #75d9c3;
  --cyber-pending: #edc76e;
  --cyber-failure: #ec6969;
  --cyber-bg: var(--cyber-canvas);
  --cyber-surface: var(--cyber-glass);
  --cyber-surface-raised: var(--cyber-glass-strong);
  --cyber-border: var(--cyber-glass-border);
  --cyber-accent: var(--cyber-running);
  --cyber-focus: #a8f2df;
  --cyber-radius-shell: 24px;
  --cyber-radius-panel: 16px;
  --cyber-radius-control: 10px;
  --cyber-radius: var(--cyber-radius-control);
  --cyber-motion-snap: 160ms cubic-bezier(.2, .8, .2, 1);
  --cyber-font-sans: ui-rounded, -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", sans-serif;
  --cyber-font-mono: "SFMono-Regular", Consolas, monospace;
}

.cyber-glass {
  border: 1px solid var(--cyber-glass-border);
  background: var(--cyber-glass);
  box-shadow: inset 0 1px var(--cyber-glass-highlight), 0 20px 44px rgb(0 0 0 / 40%);
  backdrop-filter: blur(24px) saturate(1.35);
}

@supports not ((backdrop-filter: blur(1px)) or (-webkit-backdrop-filter: blur(1px))) {
  .cyber-glass { background: rgb(18 24 29 / 96%); }
}
```

Apply `.cyber-glass` equivalent properties to structural shared surfaces only. Leave timeline rows and fact rows unframed.

- [ ] **Step 4: Run shared UI tests**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/tokens.test.ts packages/ui/src/components/components.test.tsx`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add packages/ui/src/tokens.css packages/ui/src/tokens.test.ts packages/ui/src/components.css
git commit -m "style: add liquid command visual foundation"
```

### Task 2: Build the icon-first application shell

**Files:**
- Modify: `apps/web/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `apps/web/src/App.test.tsx`
- Modify: `apps/web/src/App.tsx`
- Modify: `apps/web/src/styles.css`

- [ ] **Step 1: Install the icon library**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/web add lucide-react@1.28.0`  
Expected: `apps/web/package.json` and `pnpm-lock.yaml` include `lucide-react`.

- [ ] **Step 2: Write a failing shell semantics test**

Add:

```ts
test('renders a compact icon rail with accessible route names', async () => {
  await renderApp();
  const navigation = screen.getByRole('navigation', { name: 'Primary' });
  expect(within(navigation).getByRole('button', { name: '新建任务' })).toHaveAttribute('aria-current', 'page');
  expect(within(navigation).getAllByTestId('nav-icon')).toHaveLength(4);
  expect(screen.getByTestId('app-canvas')).toHaveClass('app-canvas');
});
```

- [ ] **Step 3: Run the test and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run apps/web/src/App.test.tsx`  
Expected: FAIL because `nav-icon` and `app-canvas` are absent.

- [ ] **Step 4: Implement the shell**

Define routes with Lucide components and keep their translated names as `aria-label` values:

```tsx
import { Activity, FileText, Radar, ShieldPlus, type LucideIcon } from 'lucide-react';

const routes: { route: AppRoute; key: RouteKey; chord: string; icon: LucideIcon }[] = [
  { route: 'new-task', key: 'nav.newTask', chord: 'n', icon: ShieldPlus },
  { route: 'mission-control', key: 'nav.missionControl', chord: 'm', icon: Radar },
  { route: 'findings', key: 'nav.findings', chord: 'f', icon: Activity },
  { route: 'reports', key: 'nav.reports', chord: 'r', icon: FileText },
];
```

Render `.app-canvas`, `.product-bar`, and `.navigation-rail cyber-glass`. Each route button contains `<Icon data-testid="nav-icon" aria-hidden="true" />` and a visually hidden translated label. Preserve command-palette routing and language controls.

- [ ] **Step 5: Implement the responsive shell CSS**

Use a fixed-width floating rail on desktop, a compact horizontal floating rail on phone, and full-bleed Evidence-colored canvas. Ensure `.workspace` has `min-width: 0`, `min-height: 0`, and padding that leaves room for the floating rail.

- [ ] **Step 6: Run the shell and workspace checks**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run apps/web/src/App.test.tsx && npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/web typecheck`  
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/web/package.json pnpm-lock.yaml apps/web/src/App.tsx apps/web/src/App.test.tsx apps/web/src/styles.css
git commit -m "feat: add liquid command application shell"
```

### Task 3: Add the two-stage approval interaction

**Files:**
- Modify: `packages/i18n/src/index.ts`
- Modify: `packages/i18n/src/index.test.ts`
- Modify: `packages/ui/src/components/components.test.tsx`
- Modify: `packages/ui/src/components/ApprovalCard.tsx`
- Modify: `packages/ui/src/components.css`

- [ ] **Step 1: Write failing approval tests**

Replace direct-allow expectations with:

```ts
test('requires parameter review before allow and resets when the challenge changes', async () => {
  const decision = vi.fn();
  const { rerender } = render(<ApprovalCard approval={approval} t={t} onApprovalDecision={decision} />);
  expect(screen.queryByRole('button', { name: 'Allow once' })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: 'Review parameters' }));
  expect(screen.getByRole('button', { name: 'Confirm allow once' })).toBeVisible();
  await userEvent.click(screen.getByRole('button', { name: 'Confirm allow once' }));
  expect(decision).toHaveBeenCalledWith('approval-1', 'allow_once');
  rerender(<ApprovalCard approval={{ ...approval, parameterDigest: 'sha256:changed' }} t={t} onApprovalDecision={decision} />);
  expect(screen.queryByRole('button', { name: 'Confirm allow once' })).not.toBeInTheDocument();
});
```

Also assert that Deny remains one click, disabled state blocks review/confirm, and no Ctrl/Meta+Enter shortcut allows directly.

- [ ] **Step 2: Run the focused test and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/components/components.test.tsx`  
Expected: FAIL because direct Allow exists and Review does not.

- [ ] **Step 3: Add exact bilingual labels**

Add dictionary keys for `approval.review`, `approval.confirmAllowOnce`, `approval.back`, `approval.reviewing`, `approval.oneAttempt`, and `approval.noPersistence`, with matching English and Chinese strings. Extend the i18n test to assert both locales.

- [ ] **Step 4: Implement local review state**

Use state keyed by immutable challenge identity:

```tsx
const reviewKey = `${approval.id}:${approval.parameterDigest}:${approval.expiresAt}`;
const [reviewedKey, setReviewedKey] = useState<string | null>(null);
const reviewing = reviewedKey === reviewKey;
```

Waiting renders `Deny` and `Review parameters`. Reviewing renders the full definition list, `Back`, and `Confirm allow once`. Only confirmation calls `onApprovalDecision(approval.id, 'allow_once')`. The component never exposes editable approval fields.

- [ ] **Step 5: Run UI and i18n tests**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/i18n/src packages/ui/src/components/components.test.tsx`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add packages/i18n/src packages/ui/src/components/ApprovalCard.tsx packages/ui/src/components/components.test.tsx packages/ui/src/components.css
git commit -m "feat: add reviewed approval decisions"
```

### Task 4: Add contextual Evidence and active-agent components

**Files:**
- Create: `packages/ui/src/components/EvidenceBackdrop.tsx`
- Create: `packages/ui/src/components/ActiveAgentRibbon.tsx`
- Modify: `packages/ui/src/components/index.ts`
- Modify: `packages/ui/src/components/components.test.tsx`
- Modify: `packages/ui/src/components/AgentInspector.tsx`
- Modify: `packages/ui/src/components/NarrativeStream.tsx`
- Modify: `packages/ui/src/components.css`

- [ ] **Step 1: Write failing component tests**

Add assertions equivalent to:

```tsx
render(<EvidenceBackdrop evidence={{ [evidence.id]: evidence }} />);
expect(screen.getByTestId('evidence-backdrop')).toHaveTextContent('POST');
expect(screen.getByTestId('evidence-backdrop')).toHaveTextContent('/rest/user/login');

render(<ActiveAgentRibbon agent={{ id: 'a', name: 'Verification', status: 'waiting', progress: 68, currentAction: 'Approval required' }} onSelect={onSelect} />);
expect(screen.getByRole('button', { name: /Verification/ })).toHaveAttribute('data-tone', 'pending');
expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '68');
```

Extend Inspector tests to assert progress and current action. Extend Narrative tests to assert event type, time, source agent, and cursor are represented without one article per event.

- [ ] **Step 2: Run the component tests and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/components/components.test.tsx`  
Expected: FAIL because the new exports do not exist.

- [ ] **Step 3: Implement `EvidenceBackdrop`**

Accept `Readonly<Record<string, ImmutableEvidence>>`; choose the most recent object insertion order entry; render string/number/boolean data as read-only `<code>` lines behind `aria-hidden="true"`. With no Evidence render only a neutral grid and no fabricated request text.

- [ ] **Step 4: Implement `ActiveAgentRibbon`**

Accept `{ agent: AgentState | null; onSelect?: (agentId: string) => void }`. Derive `running`, `pending`, or `failure` tone from status/action. Render a button only when an agent exists, include current action, a bounded `0-100` progressbar when progress exists, and invoke `onSelect(agent.id)`.

- [ ] **Step 5: Upgrade the Inspector and Narrative Stream**

Agent rows render name, explicit status, current action, and progress. Timeline rows render a causal marker, human-readable event type, source identity, timestamp, and cursor. Preserve tab roles, live-region behavior, and stable keys.

- [ ] **Step 6: Run shared component checks**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/components/components.test.tsx && npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/ui typecheck`  
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add packages/ui/src/components packages/ui/src/components.css
git commit -m "feat: add contextual mission signals"
```

### Task 5: Compose the Liquid Command Mission Control

**Files:**
- Modify: `packages/i18n/src/index.ts`
- Modify: `apps/web/src/pages/mission-control.test.tsx`
- Modify: `apps/web/src/pages/MissionControlPage.tsx`
- Modify: `apps/web/src/styles.css`

- [ ] **Step 1: Write failing Mission Control behavior tests**

Assert:

```ts
expect(screen.getByTestId('mission-command-deck')).toBeInTheDocument();
expect(screen.getByTestId('evidence-backdrop')).toBeInTheDocument();
expect(screen.getByRole('button', { name: 'Open inspector' })).toHaveAttribute('aria-expanded', 'false');
await userEvent.click(screen.getByRole('button', { name: 'Open inspector' }));
expect(screen.getByRole('button', { name: 'Open inspector' })).toHaveAttribute('aria-expanded', 'true');
expect(screen.getByRole('button', { name: /Recon Agent/ })).toBeInTheDocument();
```

Update approval command assertions to click Review then Confirm and still expect exactly `{ type: 'approval.respond', challengeId: 'approval-1', decision: 'allow_once' }`.

- [ ] **Step 2: Run the focused test and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run apps/web/src/pages/mission-control.test.tsx`  
Expected: FAIL because the Command Deck and Inspector toggle do not exist.

- [ ] **Step 3: Implement Command Deck state and derivation**

Add local `inspectorOpen`; select the active agent by preferring waiting/blocked, then running, then the first agent. `ActiveAgentRibbon.onSelect` sets the Inspector tab to agents and opens it. Keep `writesDisabled = transportBlocked || displaced` unchanged.

Render in this order:

```tsx
<div className="mission-canvas">
  <EvidenceBackdrop evidence={view.product.evidence} />
  <div className="mission-command-deck" data-testid="mission-command-deck">
    <header className="mission-header cyber-glass">...</header>
    <section className="mission-stream-surface cyber-glass">...</section>
    <aside className="mission-inspector-surface cyber-glass" data-open={inspectorOpen}>...</aside>
  </div>
  <ActiveAgentRibbon agent={activeAgent} onSelect={focusAgent} />
</div>
```

Task controls use Lucide icon buttons with accessible names and tooltips. Composer remains a stable row. Approval remains inside the Narrative Stream's causal flow.

- [ ] **Step 4: Implement desktop composition CSS**

At `>=1280px`, render the stream and fixed Inspector as non-overlapping grid columns. Use the Evidence backdrop beneath both. Keep the active ribbon outside the grid at bottom right with reserved safe area.

- [ ] **Step 5: Run behavior and type checks**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run apps/web/src/pages/mission-control.test.tsx apps/web/src/App.test.tsx && npm exec --yes --package=pnpm@10.15.1 -- pnpm --filter @cyber/web typecheck`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add packages/i18n/src/index.ts apps/web/src/pages/MissionControlPage.tsx apps/web/src/pages/mission-control.test.tsx apps/web/src/styles.css
git commit -m "feat: compose liquid command mission control"
```

### Task 6: Complete responsive, recovery, and no-overlap behavior

**Files:**
- Modify: `apps/web/tests/golden-path.spec.ts`
- Modify: `apps/web/tests/accessibility.spec.ts`
- Modify: `apps/web/src/styles.css`

- [ ] **Step 1: Write failing responsive E2E assertions**

For laptop, require Inspector to be hidden initially, open from its button, have fixed positioning, close with Escape, and restore focus. For phone, require inline Review/Confirm approval, visible pause/cancel, hidden complex Inspector until opened, and no horizontal overflow.

Add a geometry helper:

```ts
const boxesOverlap = (a: DOMRect, b: DOMRect) =>
  a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
```

Assert the active-agent ribbon does not overlap the visible approval action row or phone navigation.

- [ ] **Step 2: Run laptop and phone tests and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec playwright test apps/web/tests/golden-path.spec.ts --project=laptop --project=phone`  
Expected: FAIL on Inspector open/close and direct approval behavior.

- [ ] **Step 3: Implement laptop/tablet/phone rules**

At `768-1279px`, hide the Inspector unless `[data-open="true"]`; then use a fixed glass overlay with bounded height. Below `768px`, use one-column continuity flow, bottom navigation, an Inspector drawer, `44px` pointer targets, and reserved bottom padding for the agent ribbon plus navigation. Keep approval in document flow.

- [ ] **Step 4: Implement focus and reduced-motion behavior**

Focus the Inspector's selected tab after opening and restore focus to its trigger after Escape/close. Ensure all Tactical Snap selectors are disabled by the existing reduced-motion block.

- [ ] **Step 5: Run responsive and accessibility E2E**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec playwright test apps/web/tests/golden-path.spec.ts apps/web/tests/accessibility.spec.ts --project=laptop --project=phone`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add apps/web/src/styles.css apps/web/tests/golden-path.spec.ts apps/web/tests/accessibility.spec.ts
git commit -m "feat: add responsive liquid command workflow"
```

### Task 7: Apply the language to all golden-path pages and states

**Files:**
- Modify: `apps/web/src/pages/NewTaskPage.tsx`
- Modify: `apps/web/src/pages/ScopeReviewPage.tsx`
- Modify: `apps/web/src/pages/FindingsPage.tsx`
- Modify: `apps/web/src/pages/ReportsPage.tsx`
- Modify: `packages/ui/src/components/ScopeReviewSheet.tsx`
- Modify: `packages/ui/src/components/FindingCard.tsx`
- Modify: `packages/ui/src/components/ReportEditor.tsx`
- Modify: `packages/ui/src/components/ConnectionBanner.tsx`
- Modify: `packages/ui/src/components/ControlLeaseBanner.tsx`
- Modify: `packages/ui/src/components.css`
- Modify: `apps/web/src/styles.css`

- [ ] **Step 1: Extend component and page tests with stable visual hooks**

Assert that each priority page exposes one structural glass surface, ordinary content is not nested in extra `.cyber-panel` wrappers, status banners include explicit text, and technical values receive `.cyber-mono`.

- [ ] **Step 2: Run affected Vitest suites and verify failure**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src/components/components.test.tsx apps/web/src/pages`  
Expected: FAIL on the new class and state assertions.

- [ ] **Step 3: Apply Liquid Command hierarchy**

Use one glass structural surface per form, Scope review, Finding item, and report editor. Keep sections unframed; use dividers for fact groups. Apply semantic status tones and monospace only to IDs, paths, methods, digests, timestamps, and raw Evidence. Add stable dimensions for runtime selectors, report controls, Evidence blocks, and buttons.

- [ ] **Step 4: Style preserved-context recovery states**

Connection and control banners become inline glass status strips over the current page. Offline, resyncing, incompatible, and unauthorized retain the last rendered product view and visually lock commands. No generic full-page replacement is introduced.

- [ ] **Step 5: Run component, page, and axe tests**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run packages/ui/src apps/web/src`  
Expected: PASS with no axe violations.

- [ ] **Step 6: Commit**

```bash
git add apps/web/src packages/ui/src
git commit -m "style: unify liquid command golden path"
```

### Task 8: Add visual regression and complete verification

**Files:**
- Create: `apps/web/tests/visual.spec.ts`
- Create: `apps/web/tests/visual.spec.ts-snapshots/*.png`
- Modify: `apps/web/tests/console.spec.ts`
- Modify: `playwright.config.ts`

- [ ] **Step 1: Write visual and geometry tests**

Create an English deterministic task and capture `new-task`, `scope-review`, `mission-control-waiting`, `mission-control-reviewing`, `findings`, `reports`, and offline Mission Control at desktop and phone widths. Use masks only for countdown text. Assert primary screenshot pixel variance is nonzero and foreground surfaces are not clipped.

```ts
await expect(page).toHaveScreenshot('mission-control-waiting.png', {
  animations: 'disabled',
  mask: [page.locator('[data-testid="approval-expiry"]')],
  maxDiffPixelRatio: 0.01,
});
```

Add a context with `backdrop-filter: none !important` and assert the computed glass background alpha is at least `0.9` and text remains visible.

- [ ] **Step 2: Generate baselines and inspect every screenshot**

Run: `npm exec --yes --package=pnpm@10.15.1 -- pnpm exec playwright test apps/web/tests/visual.spec.ts --update-snapshots`  
Expected: PASS and create desktop/phone PNG baselines. Inspect each PNG for blank canvases, clipping, overlap, illegible Evidence, or cheap nested-card styling; fix implementation and regenerate if any issue exists.

- [ ] **Step 3: Run Web unit and E2E verification**

Run:

```bash
npm exec --yes --package=pnpm@10.15.1 -- pnpm exec vitest run
npm exec --yes --package=pnpm@10.15.1 -- pnpm exec playwright test
npm exec --yes --package=pnpm@10.15.1 -- pnpm run test:coverage
npm exec --yes --package=pnpm@10.15.1 -- pnpm run typecheck
npm exec --yes --package=pnpm@10.15.1 -- pnpm run lint
npm exec --yes --package=pnpm@10.15.1 -- pnpm run build
```

Expected: all tests pass, no unexpected skips, and coverage remains above the repository thresholds.

- [ ] **Step 4: Run repository-wide verification**

Run:

```bash
go test ./...
go test -race ./...
go test -tags=security ./...
go vet ./...
git diff --check
```

Also run the existing VS Code extension test and compile commands documented in the Phase 1 plan. Expected: every command exits zero.

- [ ] **Step 5: Commit**

```bash
git add apps/web/tests playwright.config.ts
git commit -m "test: verify liquid command visual experience"
```

## Final acceptance

- [ ] Mission Control visibly matches the approved Liquid Command mockups at desktop and phone widths.
- [ ] Approval requires Review then Confirm while Deny remains immediate.
- [ ] Runtime command shapes, Scope, Evidence, cursor, and control-lease authority remain unchanged.
- [ ] The floating rail, fixed/overlay Inspector, inline approval, and active-agent ribbon never overlap incoherently.
- [ ] Empty and recovery states preserve honest, last-trusted context.
- [ ] All Phase 1 unit, E2E, accessibility, coverage, build, Go, VS Code, and repository checks pass.

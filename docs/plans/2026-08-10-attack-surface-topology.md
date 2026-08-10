# Attack Surface Topology Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a real layered assessment topology from cyber-agent endpoint observations and the Desktop product state.

**Architecture:** cyber-agent persists target, service, and endpoint asset events. `@cyber/asset-graph` composes those durable assets with derived Finding, Evidence, and Report nodes, then lays them out in deterministic horizontal layers. The Desktop page renders the composed graph with filters, collapse controls, a legend, a node inspector, and report navigation.

**Tech Stack:** Python 3.12, pytest, TypeScript 5.9, React 19, Vitest, Testing Library, SVG, pnpm.

---

### Task 1: Project Trusted HTTP Evidence Into Endpoint Assets

**Files:**
- Modify: `/Users/maple/.config/superpowers/worktrees/cyber-agent/cyber-code-competition-integration/orchestrator/session_service.py`
- Test: `/Users/maple/.config/superpowers/worktrees/cyber-agent/cyber-code-competition-integration/tests/test_session_service.py`

**Step 1: Write the failing endpoint-normalization test**

Add tests for a helper that accepts trusted evidence data containing a URL, method, status code, and content type and returns a normalized endpoint identity. Cover default ports, path preservation, query removal, and malformed payloads.

**Step 2: Run the focused test and verify RED**

Run:

```bash
cd /Users/maple/.config/superpowers/worktrees/cyber-agent/cyber-code-competition-integration
.venv/bin/pytest -q tests/test_session_service.py -k endpoint_asset
```

Expected: FAIL because endpoint projection does not exist.

**Step 3: Implement minimal endpoint extraction and projection**

Add a private normalized endpoint record and helpers that:

- search stable evidence fields and bounded preview JSON for HTTP URLs;
- accept only `http` and `https`;
- normalize scheme, hostname, port, path, and method;
- create stable `asset-endpoint-*` and `asset-edge-*` identifiers;
- emit endpoint `asset.node.committed` events with evidence provenance;
- emit service-to-endpoint `hosts` edges;
- skip malformed observations without aborting the turn projection.

Do not infer endpoints that are not present in trusted evidence.

**Step 4: Add idempotent replay tests**

Run the projection twice and assert that endpoint nodes and edges are not duplicated or rewritten.

**Step 5: Run backend regression tests**

```bash
.venv/bin/pytest -q tests/test_session_service.py tests/test_native_session_runtime.py tests/test_native_acceptance.py
```

Expected: all pass.

**Step 6: Commit**

```bash
git add orchestrator/session_service.py tests/test_session_service.py
git commit -m "feat: project observed endpoints into asset topology"
```

### Task 2: Compose Assessment Product Nodes And Edges

**Files:**
- Modify: `packages/asset-graph/src/index.ts`
- Modify: `packages/asset-graph/src/index.test.ts`

**Step 1: Write failing composition tests**

Create a realistic ProductState fixture with target, service, endpoint, evidence, four findings, and a frozen report. Assert that `createAssessmentGraph(product, options)` produces:

- the durable asset nodes and edges;
- one derived node per selected Finding and Evidence;
- one report node;
- `supports` edges from evidence to finding;
- `included_in` edges from finding to report;
- no fabricated endpoint association when evidence lacks an endpoint identity.

**Step 2: Verify RED**

```bash
pnpm vitest run packages/asset-graph/src/index.test.ts
```

Expected: FAIL because `createAssessmentGraph` is missing.

**Step 3: Implement minimal composition**

Add:

```ts
export type AssessmentGraphOptions = {
  includeEvidence?: boolean;
  includeReceipts?: boolean;
};

export function createAssessmentGraph(
  product: ProductState,
  options: AssessmentGraphOptions = {},
): AssetGraph;
```

Use stable IDs such as `finding-result:<id>`, `evidence-result:<id>`, and `report-result:<id>`. Derive provenance from the referenced evidence. Keep receipt composition disabled until a durable receipt model is available in `ProductState`.

**Step 4: Test collapse behavior and unresolved references**

Assert `includeEvidence: false` preserves Finding and Report nodes while omitting detail evidence nodes.

**Step 5: Run package tests**

```bash
pnpm vitest run packages/asset-graph/src/index.test.ts packages/protocol/src/asset-graph.test.ts
```

Expected: all pass.

**Step 6: Commit**

```bash
git add packages/asset-graph/src/index.ts packages/asset-graph/src/index.test.ts
git commit -m "feat: compose assessment result topology"
```

### Task 3: Replace Scatter Layout With Deterministic Layers

**Files:**
- Modify: `packages/asset-graph/src/index.ts`
- Modify: `packages/asset-graph/src/index.test.ts`

**Step 1: Write failing layered-layout tests**

Assert that target, service, endpoint, finding, evidence, and report nodes receive increasing X coordinates in that order. Assert deterministic vertical ordering and stable output across replays.

**Step 2: Verify RED**

```bash
pnpm vitest run packages/asset-graph/src/index.test.ts -t layered
```

Expected: FAIL because the current layout is hash-scattered.

**Step 3: Implement layered layout**

Replace hash-based placement with a kind-to-layer map. Preserve a fallback layer for unknown asset kinds. Sort Finding nodes by severity, then label and ID; sort other layers by label and ID. Return layout dimensions and node layer metadata needed by the SVG renderer.

**Step 4: Test graph caps**

Ensure core nodes remain visible when the graph exceeds `maxNodes`; trim Evidence detail before target, service, endpoint, finding, or report nodes.

**Step 5: Run tests and commit**

```bash
pnpm vitest run packages/asset-graph/src/index.test.ts
git add packages/asset-graph/src/index.ts packages/asset-graph/src/index.test.ts
git commit -m "feat: lay out assessment graph by semantic layer"
```

### Task 4: Upgrade The Desktop Topology Page

**Files:**
- Modify: `packages/product-app/src/pages/AssetGraphPage.tsx`
- Modify: `packages/product-app/src/pages/AssetGraphPage.test.tsx`
- Modify: `packages/product-app/src/styles.css`
- Modify: `packages/product-app/src/styles.test.ts`
- Modify: `packages/i18n/src/index.ts`

**Step 1: Write failing page behavior tests**

Test that the page:

- renders summary counts for targets, services, endpoints, confirmed findings, and evidence;
- offers an Evidence visibility toggle;
- renders a node-type legend;
- opens a selected node inspector;
- calls `onOpenReport` from the report node;
- retains a usable internal scroll area on narrow screens.

**Step 2: Verify RED**

```bash
pnpm vitest run packages/product-app/src/pages/AssetGraphPage.test.tsx packages/product-app/src/styles.test.ts
```

Expected: FAIL for missing controls and semantic node rendering.

**Step 3: Implement graph controls and inspector**

Use `createAssessmentGraph` instead of manually appending a report node. Add type/status/search filters, an Evidence toggle, summary metrics, legend, edge labels, and a right-side inspector. Keep controls compact and use Lucide icons for commands.

**Step 4: Implement semantic SVG nodes**

Render node kinds with reusable SVG components:

- target hexagon;
- service rounded rectangle;
- endpoint compact rounded rectangle;
- finding diamond with severity class;
- evidence circle;
- report document rectangle.

Use accessible `role="button"`, keyboard activation, tooltips, and full labels in `<title>` elements.

**Step 5: Implement responsive CSS**

Use a grid with an internally scrollable graph canvas and a bounded inspector. Ensure the SVG cannot extend beyond the page window and long labels wrap or truncate without overlap.

**Step 6: Run frontend regression**

```bash
pnpm vitest run packages/product-app/src/pages/AssetGraphPage.test.tsx packages/product-app/src/styles.test.ts packages/asset-graph/src/index.test.ts
pnpm typecheck
```

Expected: all pass.

**Step 7: Commit**

```bash
git add packages/product-app/src/pages/AssetGraphPage.tsx packages/product-app/src/pages/AssetGraphPage.test.tsx packages/product-app/src/styles.css packages/product-app/src/styles.test.ts packages/i18n/src/index.ts
git commit -m "feat: visualize layered attack surface topology"
```

### Task 5: Replay Juice Shop And Verify Desktop End To End

**Files:**
- Modify if needed: `packages/runtime-client/src/cyber-agent-event-source.ts`
- Test if needed: `packages/runtime-client/src/cyber-agent-event-source.test.ts`
- Update: `/tmp/cyber-juice-eval/attack-chain.md`
- Update: `/tmp/cyber-juice-eval/experiments.md`

**Step 1: Run all automated tests**

```bash
cd /Users/maple/workspace/cyber-claude
pnpm test
pnpm typecheck
pnpm build

cd /Users/maple/.config/superpowers/worktrees/cyber-agent/cyber-code-competition-integration
.venv/bin/pytest -q
```

Expected: all tests pass, with only documented skips and warnings.

**Step 2: Restart the real runtime**

Start cyber-agent with the existing loopback bearer configuration and the shared `CYBER_AGENT_DATA_DIR`. Do not print credentials or API keys.

**Step 3: Submit a fresh authorized Juice Shop assessment**

Use the established read-only task prompt and confirm scope. Verify that the event stream includes endpoint asset nodes and service-to-endpoint edges in addition to Finding, Evidence, and frozen Report products.

**Step 4: Verify Desktop visually**

Run Desktop with `VITE_CYBER_REAL_SOURCES_ENABLED=true`. Use Playwright screenshots at desktop and narrow-window sizes. Confirm:

- at least 1 target, 1 service, and 4 endpoint nodes;
- 4 confirmed Finding nodes;
- Evidence nodes appear when expanded;
- the frozen Report node opens the actual report;
- no graph, inspector, or report content extends beyond the window.

**Step 5: Update experiment records**

Record the new session ID, final cursor, node/edge counts, screenshot paths, and verification outcome in the two `/tmp/cyber-juice-eval` files.

**Step 6: Final verification commit**

Commit only repository files relevant to the topology implementation. Preserve unrelated user changes in both worktrees.

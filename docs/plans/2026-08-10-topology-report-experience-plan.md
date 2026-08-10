# Topology and Report Experience Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the asset topology visibly useful and turn long reports into navigable, structured result views without changing the event protocol or export source.

**Architecture:** Keep the existing deterministic SVG asset graph and derive a presentation-only report result node from `ProductState.report`. The report UI will extract Markdown headings into a local table of contents and derive compact charts from structured findings/evidence. A report drawer/panel will reuse the existing report route rather than duplicating report state or export logic.

**Tech Stack:** React, TypeScript, Vitest, existing SVG/CSS components, `react-markdown`/`remark-gfm`.

---

### Task 1: Define report navigation and chart data

**Files:** `packages/ui/src/components/ReportEditor.tsx`, `packages/ui/src/components/components.test.tsx`

1. Add failing tests for extracting stable heading anchors, rendering a table of contents, and rendering severity/evidence summary bars.
2. Run focused UI tests and verify they fail because the navigation and charts do not exist.
3. Implement pure heading extraction and presentation-only navigation/chart markup; keep narrative text unchanged.
4. Run focused UI tests.

### Task 2: Add responsive report layout

**Files:** `packages/ui/src/components.css`, `packages/product-app/src/styles.css`, `packages/product-app/src/styles.test.ts`

1. Add failing CSS assertions for the report two-column layout, sticky/scrollable navigation, and narrow-screen collapse.
2. Run CSS tests and verify RED.
3. Add the layout styles with `min-width: 0`, bounded overflow, and a narrow-screen horizontal section list.
4. Run CSS tests and the UI component tests.

### Task 3: Add a report result node to the asset topology

**Files:** `packages/product-app/src/pages/AssetGraphPage.tsx`, `packages/product-app/src/pages/AssetGraphPage.test.tsx`, `packages/product-app/src/styles.css`

1. Add failing tests for an empty-state message and a report result node when `product.report` exists.
2. Run focused page tests and verify RED.
3. Add a derived report node/edge in the page presentation model, make it keyboard accessible, and route activation to the reports page or report drawer.
4. Add visual styling for result nodes and empty graph state.
5. Run focused page and full product-app tests.

### Task 4: Integrate and verify

**Files:** `packages/product-app/src/ProductApp.tsx`, relevant tests

1. Add a failing integration test proving report-node activation opens the report route.
2. Implement the smallest route callback needed; do not change protocol events.
3. Run all tests, type checks, and a WebKit viewport check at 390px and 1280px.
4. Commit the feature branch changes, merge into `main`, and rerun the verification suite on the merge result.

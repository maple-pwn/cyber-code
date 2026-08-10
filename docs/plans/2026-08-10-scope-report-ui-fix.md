# Scope Confirmation and Report Rendering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make scope confirmation responsive and render cyber-agent Markdown reports as readable, window-bounded structured content with trustworthy metadata.

**Architecture:** The product shell navigates to Mission Control as soon as the trusted command is dispatched, while the existing event subscription projects background progress. Report narrative remains immutable source data; a presentation-only renderer displays sanitized GFM Markdown. Report metadata is derived from structured findings, evidence, and the matching report event rather than generated prose, while a dedicated table wrapper contains wide Markdown content.

**Tech Stack:** React, TypeScript, Vitest, `react-markdown`, `remark-gfm`, Tauri Desktop.

---

### Task 1: Scope confirmation navigation

**Files:** `packages/product-app/src/ProductApp.tsx`, `packages/product-app/src/ProductApp.test.tsx`

1. Add a regression test with a deferred `scope.confirm` dispatch and assert Mission Control appears before the promise resolves.
2. Run the focused test and verify it fails because navigation currently awaits completion.
3. Navigate immediately after dispatch is queued, preserve rejected-command reporting through the existing store connection, and avoid duplicate navigation.
4. Run focused product-app tests.

### Task 2: Markdown report presentation

**Files:** `packages/ui/src/components/ReportEditor.tsx`, `packages/ui/src/components/components.test.tsx`, `package.json`, `pnpm-lock.yaml`

1. Add a failing component test asserting headings, GFM tables, and fenced code render as semantic elements.
2. Add `react-markdown` and `remark-gfm`, and render narrative through a constrained Markdown component.
3. Strip only known runtime preambles before presentation; preserve the original narrative for exports.
4. Run focused UI tests and the full TypeScript/test suite.

### Task 3: Contain report width

**Files:** `packages/ui/src/components/ReportEditor.tsx`, `packages/ui/src/components/components.test.tsx`, `packages/ui/src/components.css`, `packages/product-app/src/styles.css`, `packages/product-app/src/styles.test.ts`

1. Add failing tests asserting a dedicated table scroll wrapper and shrink-safe CSS across the report sizing chain.
2. Run focused tests and verify the wrapper/CSS assertions fail for the missing containment behavior.
3. Wrap Markdown tables in a presentation-only scroll region and constrain the page, desktop wrapper, editor, narrative, and Markdown root.
4. Run focused tests and confirm wide tables are contained without changing report source data.

### Task 4: Add report time and deterministic summary

**Files:** `packages/product-app/src/pages/ReportsPage.tsx`, `packages/product-app/src/ProductApp.test.tsx`, `packages/ui/src/components/ReportEditor.tsx`, `packages/ui/src/components/components.test.tsx`

1. Add failing tests for the matching `report.drafted` event time, missing-time fallback, and confirmed/total/evidence summary counts.
2. Run focused tests and verify they fail because the metadata is not displayed.
3. Resolve the matching event timestamp in `ReportsPage`, pass it to `ReportEditor`, and render localized, deterministic metadata.
4. Run focused tests and confirm the report heading and summary are correct.

### Task 5: Desktop verification

1. Run typecheck, focused tests, and the Tauri debug build.
2. Relaunch the debug Desktop app with the existing trusted cyber-agent executable and report the executable path and verification results.

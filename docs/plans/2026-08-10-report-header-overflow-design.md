# Report Header and Overflow Design

## Goal

Keep the report page within the application window, display the report generation time in its heading, and provide a concise deterministic summary.

## Layout

The report page, desktop wrapper, editor, narrative, and Markdown root all participate in the same grid sizing chain. Every item in that chain must allow shrinking with `min-width: 0` and remain bounded by `max-width: 100%`. Wide Markdown tables render inside a dedicated scroll container so only the table scrolls horizontally; the page and editor never extend beyond the window.

## Report Metadata

`ReportsPage` locates the latest `report.drafted` event matching the visible report and passes its `occurredAt` timestamp to `ReportEditor`. This keeps event time as the source of truth without changing the persisted `ReportState` protocol. If no matching event exists, the heading explicitly reports that the time was not recorded.

The header summary is derived from structured state and contains confirmed findings, total included findings, and immutable evidence counts. It does not summarize model prose or make new security claims.

## Testing

Component tests verify the timestamp, deterministic counts, missing-time fallback, and a dedicated table scroll wrapper. Product tests verify event-time selection. A CSS regression test verifies that every parent in the report sizing chain can shrink and that horizontal overflow is contained. Focused tests, TypeScript checks, and a narrow-viewport browser check complete verification.

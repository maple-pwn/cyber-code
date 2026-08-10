# Topology and Report Experience Design

## User outcome

The topology view should explain what was assessed and where the final result lives. Long reports should be scannable before the user reads every line.

## Topology

The existing SVG graph remains the renderer. A report result node is derived only for presentation from `ProductState.report`; it is not persisted as an asset event. When assets are absent, the page shows an explicit empty state with counts and a link to the available report/task result. Node activation remains keyboard accessible.

## Report

The report editor becomes a bounded two-column reading surface. The left column contains a generated table of contents from Markdown headings. The right column contains the existing narrative, human notes, recommendations, evidence, and export controls. On narrow screens the table of contents becomes a horizontally scrollable section strip. Two compact deterministic visualizations summarize finding severity and evidence coverage; neither creates new security claims.

## Verification

Tests cover heading extraction, anchor navigation, chart counts, graph empty state, report node rendering, and route activation. CSS tests cover width containment and responsive collapse. WebKit verifies that both report and topology pages remain inside 390px and 1280px viewports.

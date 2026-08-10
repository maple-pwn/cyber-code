# Attack Surface Topology Design

## Goal

Replace the current two-node target-to-service graph with a real assessment topology that explains how targets, services, endpoints, findings, evidence, receipts, and the final report relate to one another.

The graph must be built from durable runtime products. It must not depend on demo-only nodes or duplicate Finding, Evidence, and Report state into a second persistence model.

## Architecture

Use a hybrid topology model:

1. cyber-agent emits durable asset nodes and edges for targets, services, and observed endpoints.
2. The runtime client continues translating those product events into protocol asset state.
3. The asset-graph package composes a presentation graph by adding derived Finding, Evidence, Receipt, and Report nodes from the normalized product state.
4. Desktop renders the composed graph with a deterministic left-to-right layered layout.

This keeps asset discovery authoritative while using existing product references for assessment results.

## Node Model

The visible graph contains these node classes:

| Layer | Kind | Source |
|---|---|---|
| 1 | target | `asset.node.committed` |
| 2 | service | `asset.node.committed` |
| 3 | endpoint | `asset.node.committed` |
| 4 | finding | derived from Finding state |
| 5 | evidence | derived from Evidence state |
| 5 | receipt | derived from receipt references when available |
| 6 | report | derived from the latest drafted or frozen Report |

Derived node identifiers use stable domain identifiers and do not invent random IDs.

## Edge Model

Durable asset edges:

- `exposes`: target to service
- `hosts`: service to endpoint

Derived product edges:

- `observed_at`: evidence to endpoint when the evidence URL or endpoint identity matches
- `supports`: evidence to finding through `evidence_refs`
- `produced`: receipt to evidence when receipt provenance is available
- `included_in`: finding to report through `finding_ids`

When an endpoint association cannot be resolved, the evidence remains connected to its finding. The graph must not fabricate an endpoint relationship.

## Backend Projection

During completed-turn product projection, cyber-agent extracts HTTP endpoints from trusted evidence payloads. It normalizes each endpoint using scheme, host, port, path, and method, then emits one immutable endpoint asset node and a service-to-endpoint `hosts` edge.

Endpoint attributes include:

- URL
- path
- HTTP method when known
- status code when known
- content type when known
- evidence references

Repeated observations of the same endpoint must be idempotent. Conflicting immutable identities remain errors.

## Presentation Graph

The asset-graph package exposes a composition function that accepts the full product state and returns one display graph. The existing pure asset graph remains available for export and protocol-level validation.

The layout groups nodes into stable horizontal layers rather than scattering them by hash. Within a layer, nodes are ordered deterministically by severity, label, and identifier. Long layers receive additional vertical space and remain scrollable.

Evidence and receipt nodes are collapsed by default. Expanding a finding reveals its supporting evidence and receipts. This keeps the first view readable while preserving provenance.

## Desktop Interaction

The topology page provides:

- node-type and status filters
- search across labels, URLs, paths, and IDs
- expand/collapse controls for evidence and receipts
- a legend for node shapes and edge meanings
- a right-side inspector for the selected node
- direct navigation from a report node to the report page
- a compact summary showing targets, services, endpoints, confirmed findings, and evidence counts

Node shapes:

- target: hexagon
- service: rounded rectangle
- endpoint: pill-like compact rectangle
- finding: diamond
- evidence: circle
- receipt: small square
- report: document-style rectangle

Severity colors apply only to Finding nodes. Other nodes use type colors and status outlines, avoiding a misleading all-red topology.

## Error And Degraded States

- Missing referenced nodes remain visible as unresolved references in the text view.
- Unsupported or malformed endpoint evidence is skipped without aborting product projection.
- Large graphs cap rendered detail nodes while preserving target, service, endpoint, finding, and report nodes.
- A graph with only target and service events continues to render using the existing fallback behavior.
- Report navigation is disabled when no report product exists.

## Testing

Backend tests verify endpoint normalization, idempotent endpoint projection, and malformed evidence handling.

Asset-graph tests verify product-state composition, reference-derived edges, deterministic layering, collapse behavior, and unresolved references.

Product-app tests verify accessible node controls, filters, inspector content, report navigation, and responsive graph bounds.

Runtime-client tests verify that expanded asset events remain compatible with existing protocol validation.

An end-to-end Juice Shop replay must produce at least:

- 1 target
- 1 service
- 4 endpoints
- 4 confirmed findings
- supporting evidence nodes
- 1 frozen report node

The report node must open the actual frozen report generated by the same session.

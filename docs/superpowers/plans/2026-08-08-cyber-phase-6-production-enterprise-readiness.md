# CYBER Phase 6 Production and Enterprise Readiness Plan

> This phase follows Phase 5. It does not silently force-enable remote teams or cloud dependencies for local-only users.

**Goal:** Move the implemented operator capabilities from single-process/library readiness to repeatable production deployments on Linux and Windows, with explicit release, identity, persistence, and observability contracts.

**Architecture:** Keep runtime and admin authorization server-side. Introduce interfaces at persistence and authentication boundaries, then provide a portable local implementation and production adapters. Every new capability is feature-gated, auditable, and independently rollbackable.

## 6A: Remote service and persistence

- [x] Add `cyber-code runtime remote-serve` with configurable listen address, TLS certificate/key, allowed origins, and graceful shutdown.
- [x] Mount `/runtime` and `/admin` through the shared authenticated team handler; add request IDs, bounded bodies, and health/readiness endpoints.
- [x] Define a concurrency-safe `SnapshotStore` interface and PostgreSQL implementation with migrations and transaction boundaries.
- [x] Add crash recovery, multi-instance locking, optimistic revision checks, and backup/restore verification.
- [ ] Run multi-process integration tests for tenant isolation, stale claims, revocation, and concurrent admin writes.

## 6B: Identity and enterprise policy

- [ ] Add OIDC/OAuth browser login with PKCE callback, issuer/audience validation, and secure session registration.
- [x] Add managed rules, trust levels, `allowed_tools`, and `deny_tools` without trusting client-provided role/capability fields.
- [x] Define emergency access with bounded TTL, explicit reason, dual audit events, and mandatory revocation.
- [ ] Add SCIM-compatible membership provisioning behind an explicit deployment flag.

## 6C: Operations and observability

- [ ] Add structured logs, metrics, traces, request correlation, audit export, and redaction invariants.
- [ ] Publish SLOs for command latency, event lag, terminal availability, and authorization decision latency.
- [ ] Add queue/worker limits, backpressure, retry policy, and graceful drain for long-running tasks and terminals.
- [ ] Add admin UI for memberships, invitations, sessions, audit decisions, and emergency access.

## 6D: Release and platform acceptance

- [ ] Productize signed update metadata hosting and public-key rotation procedure.
- [ ] Verify Linux and Windows native TUI, ConPTY, Job Object sandbox, orphan cleanup, and hostile-output suites on real machines.
- [ ] Verify Bedrock, Vertex, and Azure providers with real accounts; record redacted smoke evidence.
- [ ] Run Web/Desktop/TUI/VS Code/Go/Rust packaging matrix and document migration/rollback per capability.

## Exit gate

Implementation status as of 2026-08-08: the portable and PostgreSQL-compatible persistence code, authenticated HTTPS composition, health endpoints, and recovery primitives are implemented. Real PostgreSQL failover, OIDC/SCIM, cloud-account smoke tests, and Windows/Linux native acceptance remain unchecked until external test environments provide evidence.

- Local-only mode remains functional without PostgreSQL, OIDC, or cloud credentials.
- Remote mode has authenticated `/runtime` and `/admin` routes, durable multi-instance state, and revocation that takes effect without client refresh.
- Every production mutation has immutable provenance and redacted audit evidence.
- Linux and Windows acceptance evidence is recorded; unverified environments are clearly marked partial.

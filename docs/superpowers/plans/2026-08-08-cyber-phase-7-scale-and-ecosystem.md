# CYBER Phase 7 Scale and Ecosystem Plan

**Goal:** Complete the deployment and ecosystem capabilities that require external infrastructure while preserving a fully functional local-only product.

## Identity and provisioning

- [x] Complete browser-based OIDC PKCE login, callback binding, nonce/state validation, and secure logout.
- [x] Add JWK discovery/cache with key rotation and bounded stale-key behavior.
- [x] Add SCIM 2.0 Users provisioning behind an explicit deployment flag, with tenant-bound bearer credentials and audit events.
- [x] Add managed policy distribution and signed policy revisions for trust/allow/deny rules.

## Scale and reliability

- [x] Add a PostgreSQL multi-instance harness for serialization conflicts, revocation visibility, backup restore, and migration rollback.
- [x] Run the PostgreSQL harness against a disposable `postgres:17-alpine` service with two independent connection pools.
- [x] Add queue/worker persistence, bounded retries, graceful drain, and terminal orphan cleanup across process restarts.
- [x] Add OpenTelemetry spans and metrics exporters with redacted attributes and configurable sampling.

## Product ecosystem

- [x] Complete plugin marketplace signature verification, sandboxed install, dependency pinning, and rollback.
- [x] Add stable extension protocol version negotiation for Web, Desktop, TUI, VS Code, and MCP bridges.
- [x] Add release smoke automation for signed metadata, artifact hashes, install/upgrade/rollback fixtures, and public-key rotation.

## Acceptance

- [x] macOS arm64 CLI/PTY/terminal and VS Code protocol acceptance recorded on 2026-08-09.
- [ ] Linux and Windows real-machine matrix recorded (`SKIP` locally; the manual release workflow now has native runners).
- [ ] Bedrock, Vertex, Azure, and OpenAI-compatible cloud smoke evidence recorded (`SKIP`: no controlled cloud credentials/evidence files supplied).
- [ ] Multi-user Web/Desktop/TUI/VS Code end-to-end evidence recorded (Go protocol/TUI, VS Code 11/11, and Web/Desktop Vitest 32/32 passed; real multi-user and GUI Extension Host checks remain pending).

## Recorded local evidence

- `go test ./...`: PASS.
- `go test -race ./...`: PASS.
- `go vet ./...`: PASS.
- `scripts/test-postgres-integration.sh`: PASS against a disposable PostgreSQL 17 service; the container was removed by the script trap.
- `scripts/release-smoke.sh`: PASS for signed install, rotated-key upgrade, rollback, artifact integrity, and embedded metadata URL.
- `scripts/platform-matrix.sh`: PASS for protocol/TUI Runtime tests, VS Code Node tests, macOS arm64 CLI/terminal tests, and deterministic provider contracts; all unavailable hosts and cloud accounts emitted explicit `SKIP` records.
- `vitest run apps/web/src apps/desktop/src`: PASS, 8 files and 32 tests; this is not represented as a GUI host or multi-user end-to-end pass.
- Implementation-marker, brand, entry-point reachability, and configured coverage gates: PASS; every package governed by `check-coverage.sh` met its required threshold.
- Compact semantics were reconciled with Runtime behavior: automatic compact triggers when either the configured context ratio or absolute token threshold crosses; it rearms only after both fall below threshold. Manual `/compact` uses only the absolute threshold.

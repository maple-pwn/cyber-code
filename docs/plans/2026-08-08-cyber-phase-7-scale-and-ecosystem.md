# CYBER Phase 7 Scale and Ecosystem Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the externally-dependent identity, scale, observability, plugin, protocol, release, and platform acceptance capabilities required for a production-ready cyber-code deployment while preserving local-only operation.

**Architecture:** Keep security decisions and tenant boundaries server-side. Extend existing injected interfaces (`internal/runtimeapi`, `internal/authorization`, `internal/marketplace`, and UI adapters) with narrow production adapters and deterministic test doubles. Every remote or enterprise capability is opt-in, auditable, bounded by time/size/retry limits, and falls back cleanly when its dependency is unavailable.

**Tech Stack:** Go standard library and existing `database/sql` abstractions; PostgreSQL integration tests via a disposable service; OAuth 2.0/OIDC PKCE; SCIM 2.0 JSON; Ed25519 signatures; OpenTelemetry-compatible interfaces; GitHub Actions; Linux/Windows native acceptance harnesses; existing Web/Desktop/TUI/VS Code/MCP protocol adapters.

---

## Execution Rules

- Use TDD for every task: add a focused failing test, run it, implement the smallest change, rerun the focused test, then run the package suite.
- Do not enable cloud identity, SCIM, queues, or telemetry by default for local-only users.
- Do not log access tokens, authorization headers, policy signatures, plugin contents, or raw user-entered emergency reasons.
- Keep commits small and independently revertible. Use the commit messages specified below.
- Go commands must use a writable cache when needed: `GOCACHE=/private/tmp/cyber-code-build-cache`.

### Task 1: OIDC browser PKCE flow and callback binding

**Files:**
- Create: `internal/services/oauth/pkce_browser.go`
- Create: `internal/services/oauth/pkce_browser_test.go`
- Modify: `internal/services/oauth/client.go`
- Modify: `internal/mcp/oauth_callback.go`
- Modify: `internal/constants/oauth.go`
- Test: `internal/services/oauth/client_test.go`, `internal/mcp/oauth_callback_test.go`

**Steps:**
1. Write tests for authorization URL generation, S256 verifier/challenge, state and nonce single-use binding, loopback callback rejection, redirect URI mismatch, and secure logout/token revocation.
2. Run `go test ./internal/services/oauth ./internal/mcp`; expect failures for the missing browser flow and callback binding.
3. Implement a loopback callback server with an ephemeral port, strict redirect URI matching, bounded callback body, state/nonce store with expiry and replay rejection, code exchange, and token revocation.
4. Add an explicit `BrowserLoginOptions` deployment boundary; preserve existing environment/file credential paths.
5. Run `go test ./internal/services/oauth ./internal/mcp -v`; expect PASS.
6. Commit: `feat: complete oidc browser pkce flow`.

### Task 2: JWK discovery, caching, and rotation

**Files:**
- Create: `internal/runtimeapi/jwk_resolver.go`
- Create: `internal/runtimeapi/jwk_resolver_test.go`
- Modify: `internal/runtimeapi/oidc_auth.go`
- Modify: `internal/runtimeapi/oidc_auth_test.go`

**Steps:**
1. Write tests for discovery metadata validation, cache hits, refresh on unknown `kid`, key rotation, expiry, bounded stale-key grace, and issuer/audience mismatch.
2. Run `go test ./internal/runtimeapi`; expect failures because the verifier only accepts an injected resolver.
3. Implement an HTTP resolver with HTTPS/loopback rules, response-size limits, cache TTL, singleflight refresh, and no stale use beyond a fixed grace period; inject it into `OIDCRemoteAuthenticator`.
4. Run `go test ./internal/runtimeapi -race`; expect PASS.
5. Commit: `feat: add rotating oidc jwk resolver`.

### Task 3: SCIM 2.0 user provisioning

**Files:**
- Create: `internal/scim/types.go`
- Create: `internal/scim/service.go`
- Create: `internal/scim/http.go`
- Create: `internal/scim/service_test.go`
- Create: `internal/scim/http_test.go`
- Modify: `internal/authorization/store.go`
- Modify: `internal/authorization/http.go`
- Modify: `internal/runtimeapi/team_handler.go`

**Steps:**
1. Write tests for tenant-bound bearer authentication, Users POST/GET/PATCH/DELETE, pagination, filter by externalId/userName, idempotent creates, deactivation instead of destructive deletion, and audit emission.
2. Run `go test ./internal/scim ./internal/authorization`; expect package-not-found or failing contract tests.
3. Implement SCIM schemas, bounded JSON decoding, ETags/revision checks, tenant isolation, explicit feature flag, and authorization-store adapter methods.
4. Run `go test ./internal/scim ./internal/authorization -race`; expect PASS.
5. Commit: `feat: add tenant-scoped scim provisioning`.

### Task 4: Signed managed policy distribution

**Files:**
- Create: `internal/authorization/policy_bundle.go`
- Create: `internal/authorization/policy_bundle_test.go`
- Modify: `internal/authorization/policy.go`
- Modify: `internal/config/loader.go`
- Modify: `internal/config/types.go`
- Modify: `internal/runtimeapi/team_handler.go`

**Steps:**
1. Write tests for Ed25519 signature verification, issuer/tenant/version/expiry validation, monotonic revision enforcement, rollback rejection, key rotation, and intersection/union enforcement of allow/deny rules.
2. Run `go test ./internal/authorization ./internal/config`; expect failures for unsigned/remote policy handling.
3. Implement canonical JSON signing, pinned verification keys, bounded fetch/cache, atomic revision install, and audit records containing digest and revision only.
4. Run `go test ./internal/authorization ./internal/config -race`; expect PASS.
5. Commit: `feat: verify signed managed policy bundles`.

### Task 5: PostgreSQL multi-instance integration harness

**Files:**
- Create: `internal/authorization/postgres_integration_test.go`
- Create: `scripts/test-postgres-integration.sh`
- Modify: `internal/authorization/postgres_store.go`
- Create: `docs/operations/postgres.md`

**Steps:**
1. Write integration tests requiring `CYBER_CODE_TEST_POSTGRES_DSN` for concurrent claims, serialization conflicts, revocation visibility, backup restore, migration rollback, and two store instances.
2. Run `go test ./internal/authorization -run PostgreSQLIntegration -v`; without the DSN expect an explicit SKIP, never a false PASS.
3. Add transaction/index constraints and a script that starts the disposable PostgreSQL service when available, applies migrations, runs the suite, and tears it down.
4. Run the script and then `go test ./internal/authorization`; expect PASS or a clearly reported environment skip.
5. Commit: `test: add postgres multi-instance acceptance harness`.

### Task 6: Durable queue, worker drain, and orphan cleanup

**Files:**
- Create: `internal/tasks/queue.go`
- Create: `internal/tasks/queue_test.go`
- Modify: `internal/tasks/tool.go`
- Modify: `internal/tasks/observability.go`
- Modify: `internal/runtimeapi/service.go`

**Steps:**
1. Write tests for bounded enqueue, backpressure, exponential retry with jitter, idempotency keys, graceful drain, restart recovery, and terminal orphan cleanup.
2. Run `go test ./internal/tasks ./internal/runtimeapi`; expect failures for persistence/drain contracts.
3. Implement a local durable queue abstraction, worker lifecycle, retry/dead-letter limits, shutdown context, and reconciliation of abandoned leases.
4. Run `go test ./internal/tasks ./internal/runtimeapi -race`; expect PASS.
5. Commit: `feat: add durable task queue and graceful drain`.

### Task 7: OpenTelemetry-compatible tracing and SLO metrics

**Files:**
- Create: `internal/observability/otel.go`
- Create: `internal/observability/otel_test.go`
- Modify: `internal/runtimeapi/team_observer.go`
- Modify: `internal/runtimeapi/team_handler.go`
- Modify: `internal/authorization/admin.go`
- Create: `docs/operations/slo.md`

**Steps:**
1. Write tests proving span/metric attributes are redacted, request IDs propagate, sampling is configurable, and exporter failures do not fail user requests.
2. Run `go test ./internal/observability ./internal/runtimeapi ./internal/authorization`; expect failures for the missing telemetry boundary.
3. Implement a small provider-neutral observer interface with optional OTLP exporter wiring, latency/error counters, event lag, terminal availability, authorization latency, and bounded cardinality.
4. Document SLO targets and alert thresholds; run focused tests with the exporter disabled and enabled using an in-memory exporter.
5. Commit: `feat: add redacted telemetry and slo metrics`.

### Task 8: Signed plugin marketplace install and rollback

**Files:**
- Create: `internal/marketplace/signature.go`
- Create: `internal/marketplace/signature_test.go`
- Modify: `internal/marketplace/manager.go`
- Modify: `internal/marketplace/marketplace_test.go`
- Modify: `internal/plugin/manager.go`

**Steps:**
1. Write tests for signed catalog and plugin digest verification, dependency/version pinning, path traversal/symlink rejection, atomic activation, failed install rollback, and uninstall rollback.
2. Run `go test ./internal/marketplace ./internal/plugin`; expect failures for signature and rollback cases.
3. Implement canonical manifest signatures, trusted-key configuration, dependency graph validation, staged sandboxed install, active revision pointer, and recovery of the previous revision.
4. Run `go test ./internal/marketplace ./internal/plugin -race`; expect PASS.
5. Commit: `feat: secure marketplace installs with signatures and rollback`.

### Task 9: Extension protocol version negotiation

**Files:**
- Create: `internal/protocol/version.go`
- Create: `internal/protocol/version_test.go`
- Modify: `internal/protocol/server.go`
- Modify: `internal/protocol/types.go`
- Modify: `internal/ui/adapter/source.go`
- Modify: `internal/ui/adapter/factory.go`
- Modify: `internal/mcp/manager.go`
- Modify: `editors/vscode/src/protocol.ts`
- Modify: `editors/vscode/src/client.ts`
- Modify: `editors/vscode/src/client.test.ts`

**Steps:**
1. Write compatibility tests for major-version rejection, minor-version feature negotiation, capability downgrade, and clear diagnostics across Web/Desktop/TUI/VS Code/MCP.
2. Run `go test ./internal/protocol ./internal/ui/adapter ./internal/mcp`; expect failures for absent negotiation.
3. Implement a shared protocol envelope and handshake; require capability intersection before subscribing or accepting commands. Mirror the constants and downgrade behavior in the VS Code TypeScript client.
4. Run `go test ./...` and `npm test --prefix editors/vscode`; expect PASS.
5. Commit: `feat: negotiate extension protocol capabilities`.

### Task 10: Release smoke and platform/cloud acceptance matrix

**Files:**
- Create: `scripts/release-smoke.sh`
- Create: `scripts/platform-matrix.sh`
- Create: `docs/operations/acceptance-matrix.md`
- Modify: `.github/workflows/release-bundle.yml`
- Modify: `scripts/release-manifest/main.go`
- Modify: `scripts/release-manifest/main_test.go`

**Steps:**
1. Write script-level checks for signed `latest.json`, artifact hashes, metadata URL, public-key rotation, and install/upgrade/rollback behavior.
2. Run the smoke script against a local temporary HTTPS fixture; expect failure before the fixture and signing checks are wired.
3. Add Linux native TUI, Windows ConPTY/Job Object, macOS, Web/Desktop/TUI/VS Code, and Bedrock/Vertex/Azure/OpenAI-compatible smoke jobs. External credentials and hosts must produce explicit SKIP records, not fabricated passes.
4. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `git diff --check`, and the release smoke script; expect all local checks PASS and external checks either PASS with evidence or SKIP with a reason.
5. Commit: `ci: add release and platform acceptance matrix`.

### Task 11: Final verification and documentation

**Files:**
- Modify: `docs/superpowers/plans/2026-08-08-cyber-phase-7-scale-and-ecosystem.md`
- Modify: `README.MD`
- Modify: `docs/operations/postgres.md`
- Modify: `docs/operations/slo.md`
- Modify: `docs/operations/acceptance-matrix.md`

**Steps:**
1. Reconcile the checklist with actual test evidence, including compact threshold semantics and all external skips.
2. Run the complete verification commands from Tasks 5, 7, 9, and 10.
3. Update operator runbooks for OIDC, SCIM, policy keys, PostgreSQL, queue drain, telemetry, plugin rollback, and release key rotation.
4. Commit: `docs: record phase 7 acceptance evidence`.

## Completion Gate

Phase 7 is complete only when local-only mode still passes the full Go/race/vet suites, every enabled remote mutation is audited and tenant-bound, release artifacts verify from clean checkout, and every unavailable external environment is explicitly recorded as pending rather than represented as tested.

# Phase 4 Runtime Source Threat Model

Status: release gate

## Security objective

Local and remote runtime sources may execute only within the authority proven by their authenticated handshake. A client may project committed events, but it may not mint authority, widen Scope, substitute approvals, mutate Evidence, or advance a cursor without runtime proof.

## Assets and trust boundaries

Protected assets are runtime credentials, task and controller identities, Scope, approval challenges, immutable Evidence, event cursors, control leases, and exported audit metadata.

The relevant boundaries are:

1. Web or desktop UI to the TypeScript `EventSource`.
2. Desktop host to the local runtime process over authenticated NDJSON IPC.
3. Web or desktop host to the remote HTTPS endpoint.
4. Runtime request authentication to task-context and controller authorization.
5. Durable event log to projected snapshots and client replay.

Demo is outside the real-execution boundary. It remains visibly labelled and selectable, but a failed Local or Remote operation never migrates or falls back to Demo.

## Threats and controls

| Threat | Attack | Required control | Verification |
| --- | --- | --- | --- |
| Local IPC impersonation | Another local process sends runtime commands | Per-launch bearer is generated outside argv, compared in constant time, excluded from responses/logs, and local transport is not wildcard network listening | `TestLocalServerRequiresBearerAndNegotiatesLocalHandshake`; built-binary conformance |
| Remote authentication theft or revocation | A stolen or revoked token retains access | TLS-only endpoint, exact Origin allowlist, bounded bearer parser, per-request authentication, in-memory Web token provider, desktop OS credential bridge | `TestRemoteHandlerRequiresTLSBearerAndExactOrigin`; remote credential tests |
| Replay | A command or approval is repeated | Mutating commands require idempotency keys; receipts persist; approvals are one-shot and expire | `TestLocalServerPreservesIdempotencyAcrossRestart`; approval replay/expiry conformance |
| Confused deputy | One controller reuses another controller's idempotency key or authority | Receipt digest binds command, resolved task context, controller ID, runtime ID, principal, and role | `TestLocalServerBindsIdempotencyToControllerAndAuthority` |
| Scope widening | A client changes target, action, or risk after confirmation | Scope is runtime-owned; widening creates a new proposal and invalidates prior approval context | service Scope tests and shared conformance suite |
| Approval substitution | Approved parameters are replaced before execution | Runtime derives the canonical parameter digest and verifies challenge identity, Scope activity, expiry, and single use | service approval tests and `approval substitution` conformance case |
| Evidence mutation | Existing Evidence ID is committed with different data | Store compares canonical immutable Evidence and rejects conflicts before commit | store Evidence conflict tests and report integrity tests |
| Cursor rollback | A server or client presents older or discontinuous state | Handshake carries `afterCursor`; replay is ordered; snapshots cannot move behind the trusted cursor; writes remain disabled during recovery | local/remote conformance and RuntimeClient recovery tests |
| Credential leakage | Secrets appear in URL, body, argv, browser storage, event, report, or diagnostics | Redirects are blocked; tokens stay in authorization headers or OS credential bridge; bearer is absent from protocol output; reports contain source identity but no credentials | secret-leak tests, transport tests, built-binary assertion |
| Resource exhaustion | Large requests, responses, reconnects, or slow consumers grow memory without bound | Inbound and outbound transport sizes are capped, reconnect attempts/backoff are bounded, subscriptions are cancellable, delivery is sequential, and event storage is durable rather than an unbounded delivery queue | runtime load tests and response-limit tests |

## Rollout and failure policy

- Real sources are disabled unless the trusted host or operator explicitly enables the real-runtime capability.
- Web reads the flag only from `__CYBER_RUNTIME_CONFIG__`; browser storage cannot enable it.
- Desktop reads `VITE_CYBER_REAL_SOURCES_ENABLED=true` at build/host configuration time.
- TUI requires `--enable-real-sources` together with `--source local`.
- Demo remains the only default source. Selecting a real source creates a new task context.
- A real-source handshake, command, or reconnect failure preserves the last trusted state read-only and never selects Demo or another real source automatically.

## Residual risk

The local runtime still inherits the operating-system account's filesystem authority. Phase 4 authenticates and isolates the protocol boundary; it does not replace the command sandbox. Remote deployment is responsible for TLS termination, token issuance and revocation, request-rate limits, and per-tenant storage isolation. Release approval requires the CI gates plus native Linux, Windows, and macOS build results.

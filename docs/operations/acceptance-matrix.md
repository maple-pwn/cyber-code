# Release, Platform, and Cloud Acceptance

This matrix separates deterministic repository checks from evidence that requires a native host or a real cloud account. A cross-compile is never recorded as native acceptance. Missing infrastructure is reported as `SKIP` with a reason rather than as a pass.

## Commands

Run the signed release fixture:

```bash
scripts/release-smoke.sh
```

Run the host and product-surface matrix:

```bash
scripts/platform-matrix.sh
```

Each output record has one of these forms:

```text
PASS check=<name> evidence=<local-evidence>
SKIP check=<name> reason=<missing-environment>
```

The release fixture is local and performs no network requests. Its artifact URLs use an HTTPS-only fixture origin so the same URL validation used by production manifests remains active. It verifies:

- canonical Ed25519-signed `latest.json` metadata;
- artifact size and SHA-256;
- the embedded release metadata URL;
- current and previous public keys during rotation;
- a signed install, upgrade, failed health check, and rollback to the last healthy artifact;
- rejection of an artifact modified after signing.

## Native Matrix

| Surface | Local deterministic coverage | Native acceptance rule |
| --- | --- | --- |
| TUI and protocol | Go protocol, UI adapter, and Runtime tests | `PASS` only on the current host OS |
| macOS | CLI build, PTY/process and terminal-manager tests | macOS runner |
| Linux | CLI build, PTY/process and terminal-manager tests | Linux runner |
| Linux strong sandbox | required-sandbox tests | Linux runner with `bwrap`; otherwise `SKIP` |
| Windows ConPTY/Job Object | Windows platform and terminal-manager tests | Windows runner; cross-compilation is insufficient |
| Web | Vitest suite | `pnpm` and installed workspace dependencies |
| Desktop | Vitest suite | `pnpm` and installed workspace dependencies; Tauri GUI host remains a separate manual check |
| VS Code | TypeScript compile and Node protocol tests | `npm` and installed extension dependencies; Extension Host UI remains a separate manual check |

The manual release workflow runs the native matrix on `ubuntu-latest`, `macos-latest`, and `windows-latest`. It has read-only repository permissions and uploads a bundle; it does not publish a release.

The 2026-08-09 local verification also ran `scripts/test-postgres-integration.sh` against a disposable `postgres:17-alpine` container. The multi-instance authorization suite passed and the script removed the container on exit.

The same local verification ran the Web and Desktop Vitest suites directly from the locked workspace dependencies: 8 files and 32 tests passed. This confirms the TypeScript product surfaces and transports, but it is not a Tauri GUI, VS Code Extension Host, or real multi-user end-to-end acceptance result.

## Cloud Evidence

Local Bedrock, Vertex, Azure, and OpenAI-compatible contract tests are recorded separately from real-account smoke evidence. To attach redacted evidence from a controlled environment, set the matching variable to a non-empty evidence file before running `scripts/platform-matrix.sh`:

| Provider | Evidence variable |
| --- | --- |
| Bedrock | `CYBER_CODE_ACCEPTANCE_BEDROCK_EVIDENCE` |
| Vertex | `CYBER_CODE_ACCEPTANCE_VERTEX_EVIDENCE` |
| Azure | `CYBER_CODE_ACCEPTANCE_AZURE_EVIDENCE` |
| OpenAI-compatible | `CYBER_CODE_ACCEPTANCE_OPENAI_COMPATIBLE_EVIDENCE` |

Evidence files must be redacted and should contain the UTC time, region/endpoint class, model identifier, credential mechanism, request ID, and final result. They must not contain access tokens, API keys, authorization headers, raw prompts, or model output. When no evidence file is supplied, the script emits `SKIP`; local provider contract tests remain `PASS` but are not represented as cloud acceptance.

## Release Key Rotation

Release binaries embed the current metadata URL and public key. During a signing-key rotation, acceptance tooling may trust both the current and immediately previous public key while verifying an existing bundle. New release binaries must embed only the intended current key. Remove the previous key from operational verification after all supported release channels have moved beyond the rotation window.

## Managed Deployment Runbook

### OIDC browser login and JWK rotation

- Keep browser login disabled unless the deployment explicitly supplies `BrowserLoginOptions` with HTTPS authorization/token endpoints, a client ID, scopes, and a loopback ephemeral callback address.
- Bind the callback listener to loopback with port `0`; do not expose it on a LAN address or configure a fixed callback port.
- Keep browser opening explicit. Headless hosts should display the authorization URL through their trusted UI rather than silently launching a browser.
- Construct `OIDCJWKResolver` with the exact issuer and a bounded cache/stale grace. Discovery and JWK URLs must use HTTPS except controlled loopback tests.
- During IdP key rotation, publish the new JWK before signing tokens with it. Unknown `kid` causes a bounded refresh; stale keys are not accepted beyond the configured grace.
- On logout, revoke the token through the configured HTTPS revocation endpoint and clear the deployment-owned credential store.

### SCIM provisioning

- SCIM is opt-in. Mount `scim.NewHandler` under `/scim/v2/` only through `NewObservedTeamHandlerWithSCIM` with `Enabled: true`.
- Provision a separate bearer for each tenant and bind it to a tenant ID and service principal. Store only a hash or secret-manager reference outside test fixtures.
- Require TLS at the external listener, cap reverse-proxy request bodies at or below the application's 1 MiB limit, and preserve `If-Match`/ETag headers.
- Treat DELETE as deactivation, monitor audit events for every mutation, and verify that cross-tenant reads and writes remain denied.
- Disable the route, rotate the tenant bearer, and review audit records immediately if a provisioning credential is exposed.

### Managed policy keys

- Pin Ed25519 public keys by key ID and bind every verifier to one HTTPS issuer and one tenant.
- Accept only monotonic revisions with `trust_level: managed`; deny rules may only reduce local authority.
- Publish a new verification key before signing a higher revision with it. Keep the previous public key only for the bounded transition window.
- Expired, future-issued, wrong-tenant, unknown-key, or rollback revisions fail closed. Audit only the policy digest and revision, never the payload signature or emergency reason.

### Signed marketplace and plugin rollback

- Managed deployments should construct the marketplace manager with `RequireSignatures: true` and a pinned key map.
- Catalog signatures cover exact plugin versions, dependency pins, and plugin tree digests. Reject cycles, unpinned dependencies, symlinks, traversal, or a changed installed tree.
- Installation and upgrade occur in staging before the active pointer changes. A state-write or activation failure restores the previous pointer, skills, and installed record.
- Before removing a plugin, retain the last active revision until state persistence succeeds. Runtime loading must call `LoadVerified` with the recorded digest.

PostgreSQL migration/restore procedures are in [postgres.md](postgres.md), telemetry and drain procedures are in [slo.md](slo.md), and release construction is in [../releases.md](../releases.md).

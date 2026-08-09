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

# Cyber Agent Competition Integration: Phases 8-10 Design

**Date:** 2026-08-09

**Status:** Approved

## 1. Objective

Integrate `cyber-agent` as the explicit security runtime for every `cyber-code`
client, add the multi-format task intake required by XH-202609, and deliver real
end-to-end workflows for penetration testing, incident response, vulnerability
research, and reverse engineering.

This work retains the Phase 1-7 product architecture. It adds a competition
delivery track rather than replacing the existing coding agent, product event
model, clients, or authorization UI.

Phases 11-13 are out of scope. Domestic-model gateway deployment, benchmark and
A/B evidence, presentation materials, videos, declarations, and live-event
rehearsal are owned separately by the user.

## 2. Product Boundary

`cyber-code` owns the product experience. `cyber-agent` owns security execution.

- Users explicitly select `Security Runtime`; prompt classification never
  silently moves a task from the coding agent to `cyber-agent`.
- One `cyber-code` security task binds to exactly one `cyber-agent` session.
- `cyber-agent` is the final authority for Scope, Capability Leases, admission,
  approvals, tool execution, Findings, Evidence, Artifacts, and Reports.
- `cyber-code` displays and forwards authoritative state. It does not reconstruct
  or independently approve security actions.
- Switching back to the coding agent does not terminate the security session.
  The user may later reattach and continue it.

The integration uses a `CyberAgentRemoteSource` adapter rather than making
`cyber-agent` implement the internal Go runtime protocol:

```text
CLI / Classic TUI / Tactical Ops / Web / Desktop / VS Code
                              |
                  SecurityRuntimeClient
                              |
                  CyberAgentRemoteSource
                    /                   \
          Local Process Supervisor    HTTPS/OIDC
                    \                   /
                 cyber-agent Session API
                              |
          Scope / Approval / Agent Runtime / Evidence
```

## 3. Phase 8: Complete Runtime Integration

### 3.1 Session protocol

The adapter consumes the existing Session API:

- `POST /v1/sessions`
- `GET /v1/sessions/{id}`
- `POST /v1/sessions/{id}/turns`
- `POST /v1/sessions/{id}/interactions`
- `POST /v1/sessions/{id}/resume`
- `POST /v1/sessions/{id}/cancel`
- `POST /v1/sessions/{id}/compact`
- `GET /v1/sessions/{id}/events`

The public contract gains:

- `GET /v1/capabilities` for protocol and runtime capability negotiation;
- a structured Scope proposal and confirmation exchange;
- idempotency keys for every mutating operation;
- stable, reference-based Finding, Artifact, and Report events.

Large outputs remain Artifacts. REST and SSE carry bounded metadata and stable
references instead of embedding complete binaries, reports, or tool output.

### 3.2 Event projection

The adapter preserves `cyber-agent` event identity and projects it into the
existing `cyber-code` product schema:

| cyber-agent event | cyber-code projection |
|---|---|
| `session.created`, `session.status`, `session.terminal` | task lifecycle |
| `plan.created`, `plan.revised` | plan timeline and task tree |
| `agent.dispatched`, `agent.result`, `subagent.lifecycle` | agent lifecycle |
| `action.*`, `tool.progress`, `tool.receipt` | tool execution and receipt |
| `interaction.*` | question and approval state |
| `evidence.committed` | immutable Evidence |
| `artifact.available` | Artifact metadata and retrieval |
| structured Finding and Report events | finding and report lifecycle |

Unknown forward-compatible events are retained as unknown product events. They
are never discarded solely because an older client cannot render them.

### 3.3 Recovery and correctness

- Persist the last acknowledged `(sequence, event_id)` with the task/session
  binding and replay strictly after that cursor.
- Reconnect SSE with bounded exponential backoff.
- On a cursor conflict, fetch the authoritative snapshot and resume from its
  committed cursor.
- Retry mutation requests only with the same idempotency key.
- Never display success before `cyber-agent` confirms the transition.
- Enter explicit incompatible, unauthorized, reconnecting, or read-only states;
  never fall back silently to Demo or another runtime.

### 3.4 Local supervisor

`cyber-code` discovers an independently installed `cyber-agent` from explicit
configuration, `PATH`, and platform installation locations. It validates the
version and capabilities, starts it on a random loopback port, waits for health
and readiness, and records no bearer in argv or logs.

The supervisor performs bounded restart and session reattachment after a child
crash. Normal client shutdown attempts graceful child termination. `doctor`
reports missing executables, incompatible versions, unavailable dependencies,
and actionable installation guidance.

### 3.5 Remote runtime

Remote mode uses HTTPS and OIDC. The client validates issuer, audience, tenant,
and negotiated capabilities. Credentials remain outside product events and
reports. Expired or revoked credentials move the client to read-only or
reauthentication state; they do not trigger runtime fallback.

### 3.6 Product surfaces

All entry points use the same `SecurityRuntimeClient` and display runtime
identity, version, location, connection state, session ID, and authority source:

```text
Security Runtime - cyber-agent 0.x
Local - Connected
Session: <id>
Authority: cyber-agent
```

- CLI exposes explicit runtime and location flags.
- Classic TUI can switch between coding and security sessions and switch back.
- Tactical Ops renders the full plan, agent tree, tools, approvals, evidence,
  Findings, Artifacts, and Reports.
- Web exposes configured local/remote sources instead of presenting Demo as a
  real runtime.
- Desktop owns the trusted local process bridge and remote credential bridge.
- VS Code exposes commands, task tree, approvals, Findings, and Evidence.

## 4. Phase 9: Function-First Multi-Format Intake

`cyber-code` selects and uploads input. `cyber-agent` parses it, creates derived
Artifacts, proposes TaskIntent and Scope, and waits for user confirmation before
active execution.

Supported input:

- ZIP, TAR, TAR.GZ, and TGZ archives;
- TXT, Markdown, JSON, YAML, XML, PDF, and DOCX documents;
- OpenAPI 2/3, Swagger, and Postman Collection v2 descriptions.

The implementation is deliberately function-first. It uses direct parsing in
`cyber-agent` rather than building a separate document sandbox, malware scanner,
content-disarm service, resumable chunk store, or complete secret-reference
system in this phase.

Three minimum correctness protections remain mandatory: archive paths may not
escape the staging directory, total extracted size is bounded, and extracted
file count is bounded. These prevent filesystem corruption and resource
exhaustion without expanding the phase into a security platform.

Each original input receives a stable SHA-256 Artifact. Derived entries record
their parent, relative path, digest, size, parser result, and source location.
The parser extracts objectives, targets, endpoints, parameters, authentication
descriptions, and relevant constraints. Extracted targets form a Scope proposal,
not implicit authorization.

`TaskSubmission` references an `InputManifest`; large file bytes and complete
document text do not travel through SSE. CLI, TUI, Web, Desktop, and VS Code all
use the same upload and intake status contract.

Deferred items include parser isolation, malware detection, complex MIME
spoofing checks, encrypted-document handling, recursive archive policy, remote
OpenAPI reference policy, and resumable remote upload.

## 5. Phase 10: Four Real Security Workflows

Every scenario must complete this chain:

```text
input -> TaskIntent -> Scope -> dynamic plan -> tool execution
      -> Tool Receipt -> Evidence -> Finding -> verification -> Report
```

A Skill manifest alone is not acceptance evidence.

### 5.1 Penetration testing

Use and complete the existing Nmap and Nuclei integrations, HTTP inspection,
and Phase 9 API enumeration. The runtime must discover services, validate a
bounded vulnerability or exposure, adapt after a failed action, and produce a
report against an explicitly authorized local lab.

### 5.2 Incident response

Add file hash and metadata, log parsing and search, timeline construction, IOC
extraction and correlation, YARA, PCAP summary, and bounded host-observation
adapters. A synthetic incident fixture must produce a sourced timeline, impact
assessment, IOC set, response recommendations, and referenced Evidence.

### 5.3 Vulnerability research

Extend workspace, process, and Git tools with static-analysis and dependency
audit adapters, data-flow search, bounded validation-case generation, and test
result ingestion. Acceptance uses an intentionally vulnerable local repository
and distinguishes candidate, reachable, and verified Findings.

### 5.4 Reverse engineering

Add file type, digest, strings, sections, imports/exports, Binwalk, radare2, and
Ghidra Headless adapters plus bounded local execution output where supported.
Acceptance uses a harmless, reproducibly built challenge binary and must recover
behavior or configuration into Evidence and a Report.

### 5.5 Tool availability and acceptance

External tools use capability discovery. `doctor` reports installed, missing,
incompatible, and unsupported tools with installation guidance. A missing native
tool produces an explicit `SKIP`; mocks and cross-builds cannot represent real
tool or native-platform acceptance.

Each scenario must have:

- at least one real external tool invocation;
- at least one Evidence-backed Finding;
- a recoverable failure or an explicit blocked outcome;
- observable parent and child Agent state in all product clients;
- a Report traceable to Tool Receipts and original Artifacts;
- reproducible critical conclusions for the checked-in fixture.

The phase ships four fixtures, four end-to-end acceptance flows, tool installation
documentation, and exportable scenario reports.

## 6. Third-Party Skills and Tools

The four built-in workflows are defaults, not a closed scenario list.

`cyber-agent` continues to accept third-party Skill packages containing
`skill.toml`, `SKILL.md`, references, and assets. Manifests declare version,
publisher, applicability, inputs, outputs, dependencies, conflicts, execution
profiles, and required MCP tool references. Existing digest, lock, dependency,
and optional signature behavior remains compatible.

Skills define planning and evidence guidance; tools perform actions. New tools
join through MCP or plugins. A third-party Skill may depend on such tools and
remains inactive with an explicit missing-dependency state until they are
available.

Phase 8 exposes Skill search, add, remove, list, discovery, activation, trust,
and dependency state through the common client model. Plans and executions
originating from third-party Skills remain observable like built-in Skills.

## 7. Testing Strategy

Implementation follows TDD in both repositories.

- Contract tests cover capabilities, idempotency, Scope exchange, event mapping,
  unknown events, cursor replay, and snapshot recovery.
- Supervisor tests use controlled child fixtures for discovery, readiness,
  restart, shutdown, and incompatible versions.
- Client tests exercise the same session through CLI, both TUIs, Web, Desktop,
  and VS Code adapters.
- Intake tests use small checked-in archive, document, OpenAPI, and Postman
  fixtures plus traversal, file-count, and size-limit failures.
- Scenario tests separate deterministic orchestration coverage from opt-in real
  external-tool acceptance.
- External prerequisites always report `SKIP` with a reason when unavailable.

## 8. Completion Criteria

Phase 8 is complete when every client can create, observe, approve, interrupt,
resume, and export one real `cyber-agent` session without duplicating authority.

Phase 9 is complete when every supported format can enter that session, produce
traceable derived inputs and a Scope proposal, and report unsupported or invalid
input explicitly.

Phase 10 is complete when all four checked-in scenarios pass deterministic E2E
coverage and each available external tool lane records real execution evidence.
Unavailable native tools or hosts remain explicit `SKIP`, never implied `PASS`.

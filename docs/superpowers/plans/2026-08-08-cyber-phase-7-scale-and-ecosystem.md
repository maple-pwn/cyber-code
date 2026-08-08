# CYBER Phase 7 Scale and Ecosystem Plan

**Goal:** Complete the deployment and ecosystem capabilities that require external infrastructure while preserving a fully functional local-only product.

## Identity and provisioning

- [ ] Complete browser-based OIDC PKCE login, callback binding, nonce/state validation, and secure logout.
- [ ] Add JWK discovery/cache with key rotation and bounded stale-key behavior.
- [ ] Add SCIM 2.0 Users provisioning behind an explicit deployment flag, with tenant-bound bearer credentials and audit events.
- [ ] Add managed policy distribution and signed policy revisions for trust/allow/deny rules.

## Scale and reliability

- [ ] Run real PostgreSQL multi-instance tests with serialization conflicts, failover, backup restore, and migration rollback.
- [ ] Add queue/worker persistence, bounded retries, graceful drain, and terminal orphan cleanup across process restarts.
- [ ] Add OpenTelemetry spans and metrics exporters with redacted attributes and configurable sampling.

## Product ecosystem

- [ ] Complete plugin marketplace signature verification, sandboxed install, dependency pinning, and rollback.
- [ ] Add stable extension protocol version negotiation for Web, Desktop, TUI, VS Code, and MCP bridges.
- [ ] Add release smoke automation for signed metadata, artifact hashes, platform installers, and public-key rotation.

## Acceptance

- [ ] Linux and Windows real-machine matrix recorded.
- [ ] Bedrock, Vertex, Azure, and OpenAI-compatible cloud smoke evidence recorded.
- [ ] Multi-user Web/Desktop/TUI/VS Code end-to-end evidence recorded.

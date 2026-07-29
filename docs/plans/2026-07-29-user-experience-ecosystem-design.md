# cyber-code User Experience and Ecosystem Design

## Goal

Bring cyber-code's user-visible workflows up to the level implied by its runtime capabilities without replacing the existing provider, permission, session, tool, or protocol foundations.

## Approach

Extend the current event-driven runtime incrementally. TUI, print mode, the local SDK, and IDE clients must consume the same stable event model rather than growing separate execution paths. Existing script-compatible stdout behavior remains unchanged; optional progress is emitted on stderr or enabled explicitly.

## Delivery Stages

1. Improve print-mode observability and permission guidance, then expose tool state and usage in the TUI.
2. Add multiline editing, scrolling, history search, completion, Markdown rendering, code highlighting, and diff presentation.
3. Add user-facing Git workflows and richer session metadata and management.
4. Add image inputs for Anthropic and capable OpenAI-compatible providers, with explicit capability errors for unsupported models.
5. Import the public Claude-compatible plugin and marketplace format through a cyber-code compatibility adapter. Installation, hooks, MCP servers, local processes, updates, and network access remain subject to cyber-code permission checks.
6. Build a VS Code extension on the existing `cyber-code serve` protocol. Keep the protocol IDE-neutral so a later JetBrains adapter does not require runtime changes.
7. Remove unreachable legacy implementations, regenerate the capability matrix from verified entry points, and run platform-specific acceptance checks.

## Runtime And Event Model

The runtime remains the single owner of agent execution. User-facing event metadata will be extended only when a frontend cannot derive the required state from existing events. Tool calls are correlated by ID; usage is accumulated per turn and per session; permission failures carry a safe, actionable remediation without exposing secrets or command contents beyond the existing safe summary.

Plain print mode continues to write only assistant text to stdout. A verbose flag writes tool lifecycle and usage information to stderr. JSON mode continues to emit the complete structured event stream.

## TUI

The active `internal/ui.Model` will be upgraded rather than replaced by the currently disconnected legacy chat model. Rendering is separated from runtime state so Markdown, code blocks, diffs, tool calls, and errors can be tested without a terminal. Stable viewport dimensions prevent streaming content from moving the input surface unexpectedly.

Enter submits the prompt. Shift+Enter inserts a newline. History navigation applies only when the cursor is at the first or last logical line, and reverse search is explicit. Completion uses slash-command names and workspace paths without executing shell commands.

## Git Workflows

Git commands are control-plane workflows built from the existing authorized process runner. `/diff` is read-only. `/review` gathers bounded repository evidence for the model. `/commit` shows the proposed message and staged diff summary and requires the same process permission boundary as other mutating commands. No workflow silently stages unrelated files.

## Multimodal Input

Images are represented as typed content blocks with bounded size and supported MIME types. Frontends pass file references through the workspace boundary, the runtime reads and validates them, and provider codecs translate the canonical block. Unsupported providers fail before sending a request.

## Plugin Marketplace Compatibility

Marketplace sources are user-configured. Metadata can be browsed without installation. Installation requires a source URL, pinned revision or version, content digest, license metadata when available, and an explicit permission preview. Imported manifests are normalized into cyber-code's internal plugin manifest; unknown security-sensitive fields fail closed. Updates never run automatically.

## IDE Integration

The first client is a VS Code extension. It launches or connects to `cyber-code serve`, sends workspace/focus/selection/diagnostic context, renders streaming messages and diffs, and responds to permission requests. The Go protocol remains the authority for lifecycle, cancellation, and permission IDs.

## Error Handling And Security

All new external effects use the existing Broker and audit log. Headless denial messages name the rejected capability and show safe alternatives such as a reviewed permission rule or an explicit bypass mode. Rendering treats model, tool, Web, and plugin text as data. Marketplace archives and image files are size-limited and validated before parsing.

## Testing And Acceptance

Each behavior is developed with a failing test first. Package tests cover rendering, event formatting, provider codecs, manifest normalization, and protocol messages. Integration tests exercise real CLI entry points with temporary state and fake providers. Release checks include the full Go suite, race tests for shared protocol/runtime paths, vet, formatting, coverage gates, cross-builds, and smoke tests for the VS Code extension. Real provider and native platform tests remain explicit opt-in checks and are reported separately from deterministic CI.

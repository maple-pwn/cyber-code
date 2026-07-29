# cyber-code README Refresh Design

## Goal

Turn the Chinese README into a polished open-source project landing page that helps a new user understand cyber-code, configure a provider, and run the CLI within a few minutes.

## Audience

- Developers evaluating a local coding agent
- DeepSeek, OpenAI-compatible, and Anthropic API users
- Contributors verifying Linux, Windows, macOS, or VS Code workflows

## Information Architecture

1. Hero section with concise positioning, truthful badges, navigation, and the existing legal disclaimer.
2. A short feature overview grouped by agent experience, provider compatibility, extensibility, and safety.
3. A one-minute quick start before detailed provider configuration.
4. Platform-specific DeepSeek setup for Unix shells and PowerShell.
5. Scannable command and capability tables instead of one long command block.
6. Focused sections for permissions, sessions, MCP/plugins/VS Code, optional utilities, platform boundaries, and contributor verification.
7. A documentation index linking to authoritative detailed guides.

## Visual Direction

Use a conventional open-source style: centered hero, restrained shields.io badges, compact tables, blockquotes for important boundaries, and collapsible detail where platform duplication would otherwise dominate. Avoid decorative assets, unsupported claims, benchmark numbers, or badges that depend on unpublished services.

## Accuracy And Safety

- Preserve the research/non-official disclaimer near the top.
- Keep API credentials environment-only.
- State that headless writes, external processes, and network operations require explicit authority.
- Distinguish native macOS smoke testing from Linux/Windows supported release targets.
- Link detailed capability and security claims to the maintained documentation.

## Verification

Run brand, placeholder, and entry-point checks; inspect every relative link; render-check Markdown structure; and confirm all documented commands exist in CLI help or automated tests.

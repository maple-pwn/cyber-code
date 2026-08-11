# Changelog

本文件记录 Cyber Platform 的公开版本变化。

## [0.1.0] - 2026-08-11

首个公开版本，将 `cyber-code` 与 `cyber-agent` 作为一个统一平台发布。

### Added

- 流式 Agent 对话、工具调用、会话恢复与上下文压缩。
- Anthropic、OpenAI-compatible、DeepSeek、Bedrock、Vertex 和 Azure Provider 接口。
- 文件、Shell、Git、Grep、Glob、LSP、MCP、插件和子 Agent 工具链。
- cyber-agent 安全 Runtime：Scope、权限审批、任务事件、Tool Receipt、Evidence、Finding 和 Report。
- Web、Tauri Desktop、Bubble Tea TUI、Print/JSONL 和 VS Code 使用入口。
- 统一产品架构图与 cyber-code/cyber-agent 双子星视觉素材。
- 开源贡献、行为准则、安全报告和 Apache-2.0 许可证文档。

### Changed

- 安全评估报告 Markdown 内容在窄窗口和长代码行下自动换行。
- 统一 README、运行指南和平台边界说明。

### Verification

- Go、Runtime、Web、Desktop 和 VS Code 的发布门禁通过。
- `packages/ui/src/components-css.test.ts` 报告布局回归测试通过。

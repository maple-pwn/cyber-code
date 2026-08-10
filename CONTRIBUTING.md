# 贡献指南

感谢你愿意改进 cyber-code。这里的贡献包括代码、测试、文档、可复现的 bug 报告和可审计的安全改进。

## 开始之前

1. 阅读 [README](README.MD)、[能力矩阵](docs/capability-matrix.md) 和相关运维/安全文档。
2. 对行为变更先开 Issue，说明目标、约束和验收方式；小型文档或测试修复可以直接提交 PR。
3. 不要提交 API key、token、真实目标数据、完整敏感响应、状态目录或构建产物。

## 本地环境

- Go `1.26.5+`
- Node.js `22.12+`
- pnpm `10.15.1`（使用 Corepack）
- 需要时安装平台对应的 Tauri、bubblewrap 或云 Provider 工具

```bash
corepack enable
pnpm install --frozen-lockfile
go mod download
```

## 开发和验证

提交前根据改动范围运行：

```bash
pnpm test
pnpm typecheck
pnpm build
go test ./...
sh scripts/check-brand.sh
sh scripts/check-placeholders.sh
```

涉及 Go 并发、权限、Runtime、Provider 或工具调用时，再运行 `go test -race ./...` 以及对应的安全/集成测试。涉及 Web/Desktop 时，运行相关 Vitest、Playwright 或 Tauri 检查。真实云服务、真实网络目标和破坏性工具必须显式授权，不能在 CI 中使用真实凭据。

## 提交和 PR

- 从最新 `main` 创建短期分支，使用清晰的 Conventional Commit 消息，例如 `fix: preserve shell stderr in tool results`。
- 一个 PR 聚焦一个主题，说明用户影响、实现边界、测试命令和已知 `SKIP` 项。
- UI 变更应附带截图或录屏；协议/状态变更应补充 fixture、契约测试或迁移说明。
- 不要把生成的 `dist/`、`coverage/`、`playwright-report/`、本地状态和编辑器凭据提交进来。
- 维护者可能要求拆分提交、补充回归测试或更新能力矩阵。

行为和沟通请遵守 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。安全漏洞不要公开提交 Issue，请按 [SECURITY.md](SECURITY.md) 联系维护者。

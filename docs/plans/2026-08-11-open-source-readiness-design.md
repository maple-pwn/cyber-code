# cyber-code 开源化设计

日期：2026-08-11

## 目标

把 cyber-code 的仓库入口整理成一个可供新贡献者、用户和维护者直接使用的开源项目，同时保持现有实现和安全边界的事实准确性。此次改动只覆盖文档、许可证和 GitHub 社区协作元数据，不改变运行时行为。

## 设计决策

### README 入口

- 继续使用中文作为主要语言，保留英文命令、路径和 API 名称。
- 首屏明确项目定位、非官方声明、当前状态和验证边界。
- 以 CLI/classic TUI、Tactical Ops Demo、Desktop 三条路径组织快速开始，避免把 Demo 描述成完整运行时。
- 使用现有文档作为深入阅读入口，不复制长篇内部设计细节。
- 增加仓库结构、开发验证、贡献、报告安全问题和许可证入口。

### 社区文件

- 使用 Apache License 2.0，并在 README 中链接 `LICENSE`。
- `CONTRIBUTING.md` 说明分支、提交、测试、文档和 PR 预期。
- `SECURITY.md` 说明安全问题不要公开提交 issue，以及当前项目的授权测试边界。
- `CODE_OF_CONDUCT.md` 采用 Contributor Covenant 2.1。
- 使用 GitHub 表单 issue 模板收集可复现的 bug 和可验证的功能请求；配置文件引导用户先阅读文档。
- PR 模板要求说明范围、测试和安全影响，避免把本地凭据或真实目标数据提交到仓库。

## 不在本次范围内

- 不新增截图、发布二进制、云服务凭据或未经验证的平台承诺。
- 不重写现有 CI、产品代码、runtime 协议或 UI。
- 不清理与文档无关的用户工作区修改。

## 验收标准

1. 所有新增文档为 Apache-2.0 兼容内容，且 README 中的链接指向仓库内存在的路径。
2. README 能让新用户在 Linux/macOS/Windows 上找到构建、配置 Provider、启动 CLI 和 Desktop 的入口。
3. 文档明确 Tactical Ops Demo、可信 Runtime Source、原生平台和云 Provider 的验证限制。
4. `pnpm test`、`pnpm typecheck`、`pnpm build`、`go test ./...` 及现有仓库检查通过。
5. 只提交开源化相关文件，并通过 `cyber-code-private` 推送分支后创建针对 `main` 的 PR。

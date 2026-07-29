# cyber-code README 美化实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**目标：** 将当前线性排列的 README 重构为美观、准确的中文开源项目首页和快速上手指南。

**结构：** 保持 `README.MD` 作为简洁的项目入口，详细行为链接到现有权威文档。只重组已经实现并验证的内容，不增加产品行为或未经支持的宣传。

**技术：** GitHub Flavored Markdown、shields.io 徽章、现有 Cobra CLI、仓库验证脚本。

---

### Task 1：重构 README 首页

**文件：**
- 修改：`README.MD`
- 参考：`docs/capability-matrix.md`
- 参考：`docs/configuration.md`
- 参考：`docs/providers.md`
- 参考：`docs/security.md`
- 参考：`docs/sdk.md`

1. 将普通标题替换为居中的首屏区域，包含 cyber-code 名称、一句话定位、克制的静态徽章和紧凑导航。
2. 在首屏下方保留非官方研究项目免责声明。
3. 增加四组功能概览：Agent 体验、Provider 兼容性、扩展能力和安全性。
4. 将最短构建运行流程移动到详细 Provider 配置之前。
5. 使用可折叠的 Unix 与 Windows 区域展示 DeepSeek 配置，并保留凭据仅来自环境变量的说明。
6. 使用便于浏览的工作流表格和命令表格替代过长的命令块。
7. 增加权限、会话与上下文、MCP/插件/VS Code、可选工具和平台边界等简洁章节。
8. 以贡献者验证命令和文档导航收尾。

### Task 2：验证所有公开说明

**文件：**
- 必要时修改：`README.MD`

1. 运行 `go run ./cmd/cli --help`，将文档中的顶层命令与实际 Cobra 输出对照。
2. 运行 `go run ./cmd/cli config --help`、`go run ./cmd/cli sessions --help` 和 `go run ./cmd/cli plugins --help`，核对公开的子命令。
3. 使用本地链接扫描检查每个相对 Markdown 链接的目标。
4. 运行 `sh scripts/check-placeholders.sh`、`sh scripts/check-brand.sh` 和 `sh scripts/check-entrypoint-reachability.sh`。
5. 运行 `git diff --check`，并检查 Markdown 标题顺序、表格可读性和代码围栏是否配对。
6. 提交为 `docs: refresh README presentation`。

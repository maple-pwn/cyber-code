# cyber-code 基础加固实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**目标：** 固化 Compact 双触发器语义，清理并持续约束 TODO/FIXME，并建立基础设施无关的 Ed25519 签名发行 manifest 与构建产物工作流。

**架构：** 保持 Runtime/Agent 为 Compact 状态唯一所有者；TODO 检查以精确 allowlist 约束全仓；更新 metadata 使用一个自包含签名 envelope，由 `internal/update` 统一生成和验证，仓库工具与手动工作流只生产可部署 bundle，不上传或安装。

**技术：** Go、Ed25519、canonical JSON、Cobra、POSIX shell、GitHub Actions、现有 Runtime/Provider/CLI 测试体系。

---

### Task 42：固化 Compact 独立触发器语义

**文件：**
- 修改：`internal/agent/compact_test.go`
- 修改：`internal/runtime/runtime_test.go`
- 修改：`tests/integration/session_task_test.go`
- 修改：`docs/configuration.md`
- 修改：`README.MD`

1. 增加 Agent 失败测试：Context 比例低于阈值但绝对 Token 达标时仍触发一次 Compact；连续越界不重复，两个阈值都回落后重新武装。
2. 增加 Runtime 测试：独立触发产生 `warning/compacted/completed` canonical 顺序并持久化快照。
3. 运行 `go test ./internal/agent ./internal/runtime ./tests/integration -run 'Compact|Threshold' -count=1`，确认缺少的语义/文档契约先失败或由现有实现证明行为。
4. 仅在测试暴露差异时修改 `internal/agent/engine.go`；不得把两个触发器改成 AND。
5. 将 `docs/configuration.md` 中“同时达到”改为“任一达到”，明确自动/手动 Compact 的区别。
6. 检查 README 的简述与权威文档一致。
7. 运行 focused tests、`go test ./internal/session ./internal/agent ./internal/runtime ./tests/integration -count=1` 与 `git diff --check`。
8. 提交：`fix: align compact threshold semantics`。

### Task 43：清理并约束 TODO/FIXME

**文件：**
- 创建：`scripts/check-todos.sh`
- 创建：`scripts/check-todos_test.sh`
- 修改：`internal/constants/system.go`
- 创建或修改：`internal/constants/system_test.go`
- 修改：`internal/state/manager.go`
- 修改：`internal/state/manager_test.go`
- 修改：`internal/services/notifier.go`
- 修改：`internal/services/api/error_utils.go`
- 修改：`internal/services/api/logging.go`
- 修改：`internal/services/api/logging_test.go`
- 修改：`scripts/check-placeholders.sh`
- 修改：`.github/workflows/ci.yml`

1. 先写 `scripts/check-todos_test.sh`：临时 fixture 中普通 TODO/FIXME 必须失败，`internal/constants/output_styles.go` 中精确的 `TODO(human)` 教学文本必须允许。
2. 运行测试，确认 `scripts/check-todos.sh` 不存在而 RED。
3. 实现 POSIX `scripts/check-todos.sh`：扫描受版本控制文本；只允许指定文件中的 `TODO(human)`；输出文件、行号和违规内容；支持 `--self-test` 或调用独立测试脚本。
4. 增加 constants 测试：`GetUnameSR` 返回当前 `runtime.GOOS/runtime.GOARCH` 的稳定非空标识；SDK prefix 不再依赖已经实现的 Vertex TODO。
5. 将 attribution version 改为读取统一产品构建版本；把 attestation、workload、GrowthBook 明确标为当前不支持，不保留 TODO 词。
6. 给 StateManager 增加可测试的 `now` 来源，初始化与 Reset 写入 Unix session start；测试非零和重置更新时间。
7. 移除 notifier 的 analytics 占位，保留纯本地路由行为。
8. 将 API logger 定义为显式本地 debug logger：不实现遥测；实现非敏感 Anthropic 环境 metadata 读取；把 build age/last timestamp 兼容 API 标成明确 no-op 或使用受锁状态，并用测试固定选择。
9. 删除 `error_utils.go` 中无信息量 TODO 注释，不改变空消息行为。
10. 运行全仓 TODO 检查，确认除 allowlist 数据外无 TODO/FIXME。
11. 把检查接入 CI 的 Linux repository checks，并接入 placeholder 聚合检查。
12. 运行 `go test ./internal/constants ./internal/state ./internal/services ./internal/services/api -count=1`、脚本自测、`go vet` affected packages。
13. 提交：`chore: eliminate unclassified TODOs`。

### Task 44：实现签名发行 Manifest 核心

**文件：**
- 创建：`internal/update/manifest.go`
- 创建：`internal/update/manifest_test.go`
- 修改：`internal/update/checker.go`
- 修改：`internal/update/checker_test.go`
- 修改：`internal/cli/root.go`
- 修改：`internal/cli/utilities_cmd.go`
- 修改：`internal/cli/commands_test.go`
- 修改：`cmd/cli/main.go`

1. 写 manifest RED 测试，定义 `Envelope{SchemaVersion,Payload,Signature}`、`Payload{SchemaVersion,Product,Version,PublishedAt,Artifacts}` 和 `Artifact{GOOS,GOARCH,URL,SHA256,Size}`。
2. 测试 deterministic canonical payload：artifact 输入顺序不同仍产生相同签名字节和 envelope。
3. 测试校验失败：篡改 payload/签名、未知或重复平台、错误产品、未知字段、非 HTTPS/带 userinfo URL、无效 SHA-256、非正大小、无当前平台 artifact、超限内容。
4. 运行 `go test ./internal/update -run 'Manifest|Envelope|Artifact' -count=1`，确认 RED。
5. 实现 `BuildEnvelope` 与 `VerifyEnvelope`；先验证 envelope 大小/schema，再验 Ed25519，最后严格解析 payload 和选择平台 artifact。
6. 将 Checker 从自定义 HTTP signature header 迁移到单文件 envelope；保留显式 opt-in、10 秒上限、HTTPS redirect 校验和 64 KiB 上限。
7. 扩展 `update.Options` 以接收 GOOS/GOARCH；扩展 `Result` 返回 artifact SHA-256 与大小，但仍不下载文件。
8. 给 `ExecuteOptions` 增加默认 metadata URL/公钥；`version-check` flags 覆盖默认值，缺失时给出明确配置错误。
9. 将 `cmd/cli/main.go` 的 version、metadata URL、公钥改为可由 `-ldflags -X` 注入的变量，并传入 CLI。
10. 增加 CLI 测试：默认禁用不联网；启用时使用注入默认值；flags 可覆盖；输出摘要但不创建下载文件。
11. 运行 `go test ./internal/update ./internal/cli -run 'Version|Update|Manifest|Envelope|Artifact' -count=1`、race focused tests 与 `go vet`。
12. 提交：`feat: add portable signed release manifests`。

### Task 45：增加 Manifest 生成工具与 Bundle 工作流

**文件：**
- 创建：`scripts/release-manifest/main.go`
- 创建：`scripts/release-manifest/main_test.go`
- 创建：`.github/workflows/release-bundle.yml`
- 创建：`docs/releases.md`
- 修改：`README.MD`
- 修改：`docs/capability-matrix.md`
- 修改：`scripts/check-entrypoint-reachability.sh`

1. 写生成工具 RED 测试：解析 `goos/goarch=path` artifact、按平台排序、计算 SHA-256/大小、拼接 HTTPS base URL、读取 base64 Ed25519 seed/private key、输出 deterministic envelope，错误不得包含私钥。
2. 运行 `go test ./scripts/release-manifest -count=1`，确认 RED。
3. 实现仓库工具，支持 `manifest` 与 `public-key` 子命令；私钥只从指定文件或 `CYBER_CODE_UPDATE_SIGNING_KEY` 读取，默认 stdout 输出，不记录 secret。
4. 扩展入口检查，明确允许 `scripts/release-manifest` 作为开发工具，但继续保证唯一产品入口是 `cmd/cli`。
5. 创建手动 `workflow_dispatch` 工作流，输入 version/base URL；从 GitHub secret 读取签名 key；构建 Linux/Windows/macOS amd64 artifact；注入 update URL/公钥；生成 `latest.json`；上传一个 bundle artifact。
6. 工作流不得自动发布 Release、上传外部站点或打印私钥；权限保持 `contents: read`。
7. 编写 `docs/releases.md`：密钥生成/保管、手动工作流、静态部署、linker 注入、密钥轮换与离线验证。
8. 更新 README 与能力矩阵，只声明已经通过入口和测试的能力。
9. 运行工具 round-trip、脚本/品牌/入口检查、workflow YAML 解析（若本机无 actionlint，则用严格 YAML 解析和结构断言）。
10. 运行完整发布门禁：`go test ./... -count=1`、focused race、`go vet ./...`、覆盖率、VS Code 测试/编译、macOS smoke、Linux/Windows cross-build。
11. 请求最终代码审查，修复所有 Critical/Important 问题并重跑门禁。
12. 提交：`build: add signed release bundle workflow`。
13. 快进私有 `main`，推送、核对远端 SHA，删除临时 worktree/分支，保留用户根目录二进制。

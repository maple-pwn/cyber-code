# cyber-code 基础加固设计

## 目标

在不依赖外部云账号、静态托管域名或特定操作系统宿主的前提下，完成三项基础加固：统一 Compact 阈值语义、清理并约束待办标记、建立可部署到任意 HTTPS 静态站点的签名发行 manifest。

## Compact 语义

`context_compact_threshold` 与 `compact.json.threshold_tokens` 是两个独立安全触发器。任一阈值达到，并且已有足够历史可在保留近期消息和工具回合完整性的前提下压缩时，Agent 尝试一次自动 Compact。

连续处于任一阈值之上时不重复尝试；只有两个触发器都回落，或者 Runtime 明确替换/清空历史后，才重新武装下一次自动尝试。手动 `/compact` 继续只使用 `compact.json` 的绝对 Token 阈值，避免改变用户主动操作的既有含义。警告、压缩和完成事件继续通过 canonical Runtime 事件流输出并持久化。

文档必须明确“任一阈值触发”，并通过 Agent、Runtime 和 CLI 测试锁定恢复会话、失败重试边界、取消与工具上下文完整性。

## 待办标记治理

所有实现待办/修复标记分为四类：

1. 当前产品入口可达且需要修复的真实缺陷；
2. 不可达或重复实现中的遗留占位；
3. 明确不在范围内的 analytics、GrowthBook、私有 attestation 等能力；
4. 测试或教学提示中作为数据出现的人工贡献标记。

第一类使用测试驱动完成；第二类在证明不可达且无公开契约后删除或改为普通边界说明；第三类不得伪装成待实现产品承诺，应改为清晰的 out-of-scope/no-op 说明；第四类保留并加入精确 allowlist。新增 `scripts/check-todos.sh`，默认拒绝未分类待办/修复标记，并接入 CI 与本地验证。

不会为了清空计数而引入遥测、远程 feature flag、厂商私有 attestation 或不必要的全局状态。

## 签名发行 Manifest

### 格式

发行 metadata 使用单个 JSON envelope，适合普通 HTTPS 静态文件服务：

```json
{
  "schema_version": 1,
  "payload": "BASE64_CANONICAL_JSON",
  "signature": "BASE64_ED25519_SIGNATURE"
}
```

签名覆盖 payload 解码后的原始 canonical JSON 字节。Payload 包含：

- `schema_version`
- `product`
- `version`
- `published_at`
- `artifacts[]`，每项包含 `goos`、`goarch`、`url`、`sha256`、`size`

Artifact 平台键必须唯一，URL 必须为无用户信息的 HTTPS 地址，SHA-256 必须是规范小写十六进制，大小必须为正数。未知 schema、未知字段、重复平台、无当前平台 artifact、签名错误、响应超限和非 HTTPS 均 fail-closed。

### 客户端

`version-check` 保持显式 opt-in。客户端读取 envelope、验证 Ed25519 签名、解析 payload、进行语义版本比较，并选择当前 GOOS/GOARCH 的 artifact。结果只报告版本、URL、SHA-256 和大小；不会下载、执行或安装文件。

Metadata URL 和 Ed25519 公钥支持通过 Go linker variables 注入产品二进制；命令行参数仍可显式覆盖。未注入默认值且未传参数时给出明确配置错误。重定向、超时和响应大小继续受现有安全边界约束。

### 生成与工作流

仓库提供独立的 release-manifest 生成工具，位于非产品入口的 `scripts/` 下。工具从文件读取 Ed25519 私钥或受限环境变量，扫描显式 artifact 描述，输出 deterministic envelope，不打印私钥或原始敏感环境值。

手动 GitHub Actions 工作流负责：

1. 构建 Linux、Windows 和 macOS artifact；
2. 计算 SHA-256 与大小；
3. 使用仓库 secret 中的 Ed25519 私钥生成 envelope；
4. 上传包含二进制、manifest 和公钥信息的 workflow artifact bundle。

工作流不绑定发布商，不自动上传到外部站点，也不自动安装更新。部署者可以把 bundle 上传到任意 HTTPS 静态托管，再通过 linker variables 或显式 CLI 参数配置 URL 与公钥。

## 安全与兼容性

- 不把签名私钥写入仓库、日志、manifest 或二进制。
- 公钥不是秘密，可以编译进二进制或随 bundle 分发。
- 保留 `version-check` 的无网络默认行为。
- 旧的 HTTP header 签名格式尚未形成公开发行契约，可以迁移到 envelope；测试与文档同步更新。
- 不改变 Provider 凭据、Permission Broker、会话格式或 SDK 协议。

## 验证

- Compact：Agent、Runtime、session 与恢复会话集成测试。
- 待办治理：检查脚本自测、全仓扫描和入口可达性验证。
- Manifest：生成/解析 round-trip、篡改、错误签名、schema、平台选择、HTTPS、大小限制和 deterministic 输出测试。
- 发布：workflow 静态检查、Linux/Windows/macOS 构建、完整 Go 测试、race、vet、覆盖率与现有仓库脚本。

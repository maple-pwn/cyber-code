# cyber-agent Security Runtime

`cyber-code` 将 cyber-agent 作为显式 Security Runtime。编码 Runtime 和安全 Runtime 不会按
提示词自动切换；用户必须选择 `--runtime cyber-agent` 或在产品入口选择 Security Runtime。

## 连接模型

```text
CLI/TUI/Web/Desktop/VS Code
          -> SecurityRuntimeClient
          -> cyber-agent Session API
          -> Scope / Capability Lease / Tool Receipt / Evidence / Finding / Report
```

本地模式由 trusted host 启动 `cyber-agent serve` 并读取有界 readiness JSON；远程模式使用注入的
HTTPS/OIDC transport。Bearer 不进入 argv、产品事件、日志或报告。一次 cyber-code 任务只绑定一个
cyber-agent session，重连使用 `(sequence,event_id)` cursor 和原 idempotency key。

## 输入

`--input` 可重复使用，支持 ZIP/TAR/TGZ、TXT/Markdown/JSON/YAML/XML、PDF、DOCX、OpenAPI 2/3、
Swagger 和 Postman v2。上传返回 `InputManifest`，大文件只通过 upload reference 传输。解析目标
先显示为 Scope proposal，确认后才可进入 active tool execution。

## 运行与故障状态

客户端应显示 runtime name/version/location、connection state、session ID 和 authority。连接失败、
能力不兼容、过期凭据、cursor 冲突和 child crash 都进入显式状态；不得回退到 Demo 或 coding
Runtime。cursor 冲突先获取权威 snapshot，再从 committed cursor 恢复。

## 验收

使用 `scripts/cyber-agent-integration-smoke.sh` 验证 Go client、TypeScript source、产品输入和
Desktop trusted host 的集成测试。Linux/Windows 真机、云 Provider、OAuth 浏览器回调和 native
沙箱需在对应环境单独验收。

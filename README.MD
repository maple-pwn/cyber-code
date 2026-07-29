# cyber-code

非官方的 Go 编码 Agent，支持 Anthropic 原生 API 与 OpenAI-compatible API，并提供统一的 TUI、Print 模式、安全工具链和可恢复会话。

> 本项目基于公开发布物和 Source Map 的研究性还原，仅供研究使用，不代表 Anthropic 官方实现。重新分发或商业使用前必须自行完成许可证和权利审查。

## 支持范围

- Anthropic Messages API 与 OpenAI-compatible Chat Completions 流式协议
- DeepSeek 等自定义 Base URL、模型和凭据环境变量
- read/write/edit/search/shell 工具及默认拒绝的权限 Broker
- MCP HTTP/stdio、独立进程插件、LSP、Hooks、Tasks/子 Agent、Compact
- 会话事件日志、原子快照、恢复、导出和删除
- Bubble Tea TUI、纯文本/JSON Lines Print 模式、Vim 输入模式
- VS Code 扩展、Claude-compatible 插件市场和 PNG/JPEG/GIF/WebP 图片输入
- Linux 与 Windows 进程树管理、通知和语音能力检测

macOS 可用于开发和本地构建；正式平台门禁覆盖 Linux 与 Windows。

## 构建

需要 Go 1.26.5 或更高版本。

```bash
go mod download
go build -o cyber-code ./cmd/cli
```

Windows PowerShell：

```powershell
go build -o cyber-code.exe ./cmd/cli
```

## DeepSeek 配置

Linux/macOS：

```bash
export DEEPSEEK_API_KEY="your-key"
./cyber-code config profile set deepseek \
  --provider openai-compatible \
  --base-url https://api.deepseek.com \
  --model deepseek-v4-pro \
  --api-key-env DEEPSEEK_API_KEY \
  --activate
./cyber-code --print "Reply with exactly OK"
```

Windows PowerShell：

```powershell
$env:DEEPSEEK_API_KEY = "your-key"
.\cyber-code.exe config profile set deepseek --provider openai-compatible --base-url https://api.deepseek.com --model deepseek-v4-pro --api-key-env DEEPSEEK_API_KEY --activate
.\cyber-code.exe --print "Reply with exactly OK"
```

密钥只从所配置的环境变量读取，不写入 YAML。详细格式见 [docs/configuration.md](docs/configuration.md) 和 [docs/providers.md](docs/providers.md)。

## 常用命令

```bash
./cyber-code                         # 交互 TUI
./cyber-code --print "inspect repo" # 非交互文本
./cyber-code --print --json "..."   # JSON Lines
./cyber-code --print --verbose "..." # stderr 工具进度与 token usage
./cyber-code --image screenshot.png "inspect" # 首轮图片
./cyber-code --resume ID --print "continue"
./cyber-code doctor --json
./cyber-code config list
./cyber-code mcp list
./cyber-code plugins list
./cyber-code plugins marketplace search review
./cyber-code sessions list
./cyber-code serve                    # newline-delimited local SDK protocol
```

MCP 配置通过 `mcp add` 持久化；启用插件从状态目录的 `plugins/<name>/plugin.json` 加载。Skills、LSP、Hooks、Compact 和审计也从统一状态目录接入，格式见 [docs/configuration.md](docs/configuration.md)。模型可调用 `task_run`、`task_status` 和 `task_cancel` 执行受父级 turn budget 与权限约束的子 Agent。

`cmd/cli` 是唯一产品入口。旧的占位 CLI、工具集合、语音控制器和重复 ChatModel 已移除，并由 `scripts/check-entrypoint-reachability.sh` 阻止回归。VS Code 扩展位于 `editors/vscode`，构建与协议说明见 [docs/sdk.md](docs/sdk.md)。

VS Code 扩展从设置 `cyber-code.executable` 启动 `cyber-code serve`，Provider 凭据仍由 CLI 配置和环境变量管理。扩展不会保存 API Key，也不会直接写工作区；文件修改由 Runtime 经 Permission Broker 执行，扩展只预览结果 diff。

外部进程或网络在 headless 默认权限模式下会被拒绝，自动化场景必须显式选择经过审查的规则或 `--permission-mode bypass`。安全模型见 [docs/security.md](docs/security.md)。

## 验证

```bash
go test ./...
go test -race ./...
go vet ./...
scripts/check-placeholders.sh
scripts/check-entrypoint-reachability.sh
scripts/check-coverage.sh
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
```

交叉构建：

```bash
GOOS=linux GOARCH=amd64 go build -o cyber-code-linux-amd64 ./cmd/cli
GOOS=windows GOARCH=amd64 go build -o cyber-code-windows-amd64.exe ./cmd/cli
```

真实云服务验收是显式 opt-in，测试与日志不得保存密钥或完整敏感响应。

完整能力边界与发布门禁见 [docs/capability-matrix.md](docs/capability-matrix.md)。

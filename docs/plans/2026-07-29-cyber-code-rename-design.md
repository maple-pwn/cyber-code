# cyber-code 全量重命名设计

## 目标

将产品从 `claude-code-go` / `claude-go` 完整重命名为 `cyber-code`。模型、用户界面、命令、配置、状态目录、环境变量、协议客户端标识、构建产物和 Go module 均使用新名称。

本次采用不兼容的硬切方案，不读取、迁移或兼容旧配置目录、旧状态目录、旧环境变量和旧二进制名称。

## 身份与系统提示词

规范系统提示词必须明确：模型是 `cyber-code`，一个独立的编码 Agent；不得自称 Claude、ChatGPT、DeepSeek 或相应供应商产品。该提示词由正式 CLI 组合层注入每个 Provider 请求，而不是仅保存在未接线的常量中。

供应商协议所需的名称不属于产品品牌。Anthropic API 字段、Claude 模型名称和云服务协议名称保持不变。

## 命名边界

- CLI 命令和发布产物：`cyber-code`
- Go module/import path：`cyber-code`
- 用户配置和状态目录：`cyber-code`
- 项目配置：`.cyber-code.yaml`
- 环境变量：`CYBER_CODE_*`
- TUI、帮助、通知、User-Agent、MCP client info：`cyber-code`
- 临时文件、测试夹具、脚本和文档中的当前产品名称：`cyber-code`

历史设计文档可保留对上游 Claude Code 研究来源的事实说明；法律声明和第三方协议标识不得为了品牌一致性而失真。

## 失败行为

旧 `CLAUDE_GO_*` 变量和旧配置目录不会被读取。若新配置或凭据缺失，CLI 返回明确的配置错误，并只指向新的 `CYBER_CODE_*` 名称。

## 验证

先添加失败测试，覆盖 Provider 请求中的系统身份、CLI/TUI 可见品牌、新配置路径和环境变量。完成机械重命名后运行全量测试、竞态检测、静态检查、漏洞扫描、覆盖率门禁，以及 Linux/Windows 交叉构建。最终扫描当前产品代码和文档，旧名称只能出现在历史来源说明、Anthropic/Claude 协议语义或测试中的明确负例。

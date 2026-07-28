# Configuration

默认配置位于用户配置目录的 `cyber-code/config.yaml`。可用 `CYBER_CODE_CONFIG` 指定文件，用 `CYBER_CODE_STATE_DIR` 指定会话与扩展状态目录。项目根目录的 `.cyber-code.yaml` 会作为项目配置加载。

```yaml
active_profile: deepseek
permission_mode: default
profiles:
  deepseek:
    provider: openai-compatible
    base_url: https://api.deepseek.com
    model: deepseek-v4-pro
    api_key_env: DEEPSEEK_API_KEY
```

优先级从高到低为 CLI、环境变量、项目配置、用户配置、默认值。支持的权限模式为 `default`、`plan`、`accept-edits` 和 `bypass`；`bypass` 只能通过显式 CLI 参数启用。

管理命令：

```bash
cyber-code config list
cyber-code config get permission_mode
cyber-code config set permission_mode plan
cyber-code config profile set NAME --provider PROVIDER --model MODEL [flags]
cyber-code config validate
```

配置文件和状态文件使用跨进程事务锁与原子替换；Unix 权限限制为 `0600`，Windows 使用平台锁和替换语义。配置只保存环境变量名，不保存解析后的凭据。

持久会话采用单写者 lease：同一 session 同时只能由一个 cyber-code Runtime 打开。会话仍在运行时，第二次 `--resume` 以及 `sessions resume/export/delete` 会返回 active 错误；原 Runtime 关闭并释放 lease 后可重试。

## Context 指令

每次 Provider 请求都由统一 Context Builder 重新组装。系统段按以下顺序加载：

1. cyber-code 编译期身份与安全边界；
2. `<CYBER_CODE_STATE_DIR>/instructions.md` 用户指令；
3. Git 项目根的 `CYBER.md`；
4. 从 Git 项目根到 `--cwd` 当前目录逐层查找的 `.cyber-code/instructions.md`。

不在 Git 仓库时，`--cwd` 同时作为项目根和当前目录。不存在或仅包含空白的文件会被忽略。单个指令文件最多 256 KiB，全部自动发现的指令最多 1 MiB；符号链接解析后必须仍位于相应的用户状态目录或项目根，否则启动失败。

项目和用户指令会标记来源、路径、摘要、估算 Token 与“不可信策略输入”属性。它们可以指导编码行为，但不能修改权限模式、安全策略、凭据处理或沙箱状态。系统为 Context 保留 8192 个输出 Token，并以 128000 Token 作为当前保守窗口；低优先级非必需段超限时会产生确定性截断/排除诊断，身份、安全边界和未完成的消息/工具回合不会被静默删除。

Skill 正文不会自动进入每次请求。可用 Skill 由只读 `load_skill` 工具列出，模型显式调用后，其正文只作为该工具回合的结果进入会话。

## 扩展状态

以下路径相对于 `CYBER_CODE_STATE_DIR`。不存在的文件表示未配置对应能力。

- `mcp.json`：由 `cyber-code mcp` 命令管理的 MCP server。
- `plugins.json` 与 `plugins/<name>/plugin.json`：插件启用状态和 manifest。
- `skills/<name>/SKILL.md`：用户技能；项目技能位于 `<workspace>/.cyber-code/skills/<name>/SKILL.md`。项目同名技能优先。
- `lsp.json`：语言到 LSP 启动配置的映射；进程在首次工具调用时惰性启动。
- `hooks.json`：Hook event 到命令数组的映射。
- `compact.json`：压缩阈值和保留消息数。缺省使用 100000 tokens 与 8 条近期消息，并使用本地 token 估算避免额外计数请求。
- `audit.json`：最多 1000 条权限决策记录，由程序维护。

LSP 示例：

```json
{
  "go": {
    "command": "gopls",
    "args": ["serve"],
    "environment": {"LANG": "en_US.UTF-8"},
    "initialization_options": {}
  }
}
```

Hooks 示例：

```json
{
  "UserPromptSubmit": ["./scripts/prompt-context"],
  "PreToolUse": ["./scripts/check-tool"]
}
```

Hook 命令通过 stdin 接收 JSON，stdout 返回单个 Hook result JSON。命令使用当前平台的 shell 运行，工作目录为 `--cwd` 指定的 workspace，并受超时和输出大小限制。

Compact 示例：

```json
{
  "threshold_tokens": 80000,
  "keep_recent_messages": 10
}
```

LSP、Hooks 和插件配置中的命令会执行本地程序，应仅配置受信内容。插件 manifest 的能力声明用于 Broker 授权和工具注册，不限制子进程在操作系统层面的文件或网络访问。

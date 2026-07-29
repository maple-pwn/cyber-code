# cyber-code 本机集成协议

`internal/protocol` 提供 newline-delimited JSON 的本机协议，默认单帧上限为 1 MiB。每条消息必须包含 `version: 1` 和 `type`。

客户端消息：

- `start` 或 `input`：携带 `prompt`，启动一次串行 Runtime turn。
- `cancel`：取消当前 turn。
- `status`：返回 session、运行状态和历史消息数。
- `permission`：使用服务端发出的 `permission_id` 返回 `allow` 或 `deny`；未知 ID 会被拒绝。

`start`/`input` 还可以携带可选的 `ide_context`：

```json
{
  "version": 1,
  "id": "turn-1",
  "type": "start",
  "prompt": "修复当前错误",
  "ide_context": {
    "workspace": "/workspace/project",
    "focus": "internal/app.go",
    "selection": {
      "path": "internal/app.go",
      "start": { "line": 10, "character": 2 },
      "end": { "line": 12, "character": 8 },
      "text": "selected source"
    },
    "diagnostics": [
      { "path": "internal/app.go", "message": "undefined: value", "severity": "error" }
    ]
  }
}
```

位置使用从零开始的行号和字符号。上下文受协议单帧上限约束；服务端对旧 Runtime 使用额外的字段数量和文本长度限制，并明确把内容标记为不可信编辑器数据。实现 `protocol.IDERuntime` 的宿主可以直接接收结构化上下文。新增字段均为可选，旧版 v1 客户端和 Runtime 保持兼容。

服务端确认 `accepted` 已写出后才启动 Runtime，随后以相同 `id` 返回 `event` 消息；文件及 notebook 工具成功修改后还会发送 `diff`，其中包含工作区相对路径及完整的 `old_text`/`new_text`。diff 是 Runtime 已授权并执行修改后的只读通知，不授予客户端写权限；超过协议单帧上限时省略 diff，但 turn 继续完成。turn 结束时返回 `turn_finished`，取消完成时包含 `canceled: true`。权限请求通过 `permission` 消息发送，`request` 使用稳定的小写字段 `tool`、`action`、`workspace`、`command`、`paths`、`network`；响应必须匹配当前连接中的待处理 ID，断线会默认拒绝。

输出使用有界队列。客户端持续不读取时，服务端返回 slow-consumer 错误并取消当前 turn，避免无界内存增长。断开连接后，同一 Server 可以接受新连接。嵌入式宿主使用可取消 context 时，传给 `Serve` 的 input 必须实现 `io.Closer`；socket、pipe 或 stdio 被 `bufio.Reader` 等类型包装时，宿主仍需传入一个能关闭底层连接的 Reader/Closer。

`internal/bridge` 提供两个适配器：

- `MCPServer`：通过 MCP stdio 暴露 `cyber_code_start` 和 `cyber_code_status` 两个有限工具。
- `IDEMessage`：承载文件焦点、选择范围、诊断和 diff 的编辑器中立消息模型。

适配器不会写入用户工作区，也不会把凭据放进协议消息。生产入口可直接运行 `cyber-code serve`，它会创建 Runtime 并绑定本机 stdio；嵌入式宿主也可以绑定 `protocol.NewServer` 或 `bridge.NewMCPServer` 到自定义 stdio/socket。

## VS Code 扩展

`editors/vscode` 提供基于同一 JSONL 协议的扩展。它管理 `cyber-code serve` 子进程，发送工作区、当前文件、选区和诊断上下文，显示流式事件，处理取消及带 challenge ID 的权限确认，并以只读方式展示 Runtime 已执行的文件 diff。

扩展设置只包含 `cyber-code.executable`，不保存 Provider API Key。凭据继续由 cyber-code 的配置和环境变量解析。开发验证命令：

```bash
npm install --prefix editors/vscode
npm test --prefix editors/vscode
npm run compile --prefix editors/vscode
```

在 VS Code 的 Extension Development Host 中打开 `editors/vscode` 后，可运行 `cyber-code: Ask` 和 `cyber-code: Cancel`。`cyber-code.executable` 应指向已构建的本机二进制；扩展继承宿主进程环境，但不会把环境变量值写入设置或协议消息。扩展不直接写工作区；所有文件修改只由 Runtime 文件工具经 Permission Broker 授权后执行，diff 视图仅用于检查结果。

# cyber-code 本机集成协议

`internal/protocol` 提供 newline-delimited JSON 的本机协议，默认单帧上限为 1 MiB。每条消息必须包含 `version: 1` 和 `type`。

客户端消息：

- `start` 或 `input`：携带 `prompt`，启动一次串行 Runtime turn。
- `cancel`：取消当前 turn。
- `status`：返回 session、运行状态和历史消息数。
- `permission`：使用服务端发出的 `permission_id` 返回 `allow` 或 `deny`；未知 ID 会被拒绝。

服务端先返回 `accepted`，随后以相同 `id` 返回 `event` 消息；turn 结束时返回 `turn_finished`，取消完成时包含 `canceled: true`。权限请求通过 `permission` 消息发送，响应必须匹配当前连接中的待处理 ID，断线会默认拒绝。

输出使用有界队列。客户端持续不读取时，服务端返回 slow-consumer 错误并取消当前 turn，避免无界内存增长。断开连接后，同一 Server 可以接受新连接。

`internal/bridge` 提供两个适配器：

- `MCPServer`：通过 MCP stdio 暴露 `cyber_code_start` 和 `cyber_code_status` 两个有限工具。
- `IDEMessage`：承载文件焦点、选择范围、诊断和 diff 的编辑器中立消息模型。

适配器不会写入用户工作区，也不会把凭据放进协议消息。生产入口可直接运行 `cyber-code serve`，它会创建 Runtime 并绑定本机 stdio；嵌入式宿主也可以绑定 `protocol.NewServer` 或 `bridge.NewMCPServer` 到自定义 stdio/socket。

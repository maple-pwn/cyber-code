# cyber-code 本机集成协议

`internal/protocol` 提供 newline-delimited JSON 的本机协议，默认单帧上限为 1 MiB。每条消息必须包含 `version: 1` 和 `type`。

客户端消息：

- `start` 或 `input`：携带 `prompt`，启动一次串行 Runtime turn。
- `cancel`：取消当前 turn。
- `status`：返回 session、运行状态和历史消息数。

服务端先返回 `accepted`，随后以相同 `id` 返回 `event` 消息。Runtime 的权限确认仍由宿主 UI/权限 Broker 处理；协议拒绝外部注入 `permission` 响应，防止伪造授权。

`internal/bridge` 提供两个适配器：

- `MCPServer`：通过 MCP stdio 暴露 `cyber_code_start` 和 `cyber_code_status` 两个有限工具。
- `IDEMessage`：承载文件焦点、选择范围、诊断和 diff 的编辑器中立消息模型。

适配器不会写入用户工作区，也不会把凭据放进协议消息。生产入口可直接运行 `cyber-code serve`，它会创建 Runtime 并绑定本机 stdio；嵌入式宿主也可以绑定 `protocol.NewServer` 或 `bridge.NewMCPServer` 到自定义 stdio/socket。

# Security

所有文件、进程和网络操作先经过 Permission Broker。`default` 模式自动允许只读文件操作，写入和执行需要确认；headless 无确认 UI 时按拒绝处理。`plan` 禁止修改，`accept-edits` 允许工作区内编辑，`bypass` 仅接受显式 CLI 来源。

路径检查会解析现有符号链接祖先并拒绝工作区逃逸；文件工具在实际 I/O 前再次解析路径。Shell 重定向使用语法树提取，动态重定向目标被拒绝。Linux 使用进程组并可选 bubblewrap；启用 `sandbox_mode: required` 时缺少 bubblewrap 会 fail-closed。Windows 使用 Job Object 回收进程树，但当前不宣称完整文件系统/网络边界，因此 required 模式会拒绝启动。

集中 Redactor 处理 Authorization、API Key、Cookie、Bearer token 和已知 secret 值。Provider 错误、审计记录、doctor、CLI 和会话导出不得输出凭据。会话与配置状态使用受限权限和原子写入。

权限决策持久化到状态目录的 `audit.json`。记录只包含工具名、动作、决策、规则标识以及路径/网络目标数量，不包含命令、路径、prompt、文件内容或工具结果。审计写入失败时 Broker 不执行已请求的工具操作。

MCP、插件、LSP、Hooks 和子 Agent 不能通过 cyber-code 的工具调用提升父级权限。插件以独立 JSON-RPC 子进程运行，其 manifest 声明和工具入口需经过 Broker；但插件进程仍继承当前用户的操作系统权限，manifest 不会形成文件或网络沙箱。只能启用受信任插件；需要强隔离时，应由容器或操作系统沙箱承载整个 cyber-code 进程。

`--permission-mode bypass` 会跳过交互确认，只应在隔离环境和完全受信输入下使用。

# 子 Agent 实时可观测设计

## 目标

为 cyber-code 的子 Agent 提供类似 Claude Code 的完整实时观测体验。用户可以从父 Agent 主视图打开任务列表，进入任一子 Agent 的完整会话视图，观察流式文本、思考、工具活动、结果和 Token 用量，并通过快捷键返回父 Agent 或切换其他子 Agent。

## 方案选择

采用 Provider 无关的事件桥接方案。子 Agent 继续产生 canonical `core.Event`，Task Service 将安全事件包装为带任务身份的父级 Runtime 事件。TUI 消费同一事件流，并为父 Agent 与每个子 Agent 分别维护展示状态。

不采用定时轮询，因为轮询会引入延迟并丢失严格事件顺序；不采用子 Agent 直接调用 TUI，因为这会耦合 Runtime、Task 和交互前端，也无法复用于 Print、SDK 或 IDE。

## 事件模型

Core 增加子 Agent 生命周期事件：启动、嵌套事件、状态变化和结束。事件至少携带：

- 任务 ID、Agent 名称和描述；
- pending、running、completed、failed、cancelled 状态；
- 子 Agent 的 canonical 事件；
- 累计 usage、最近工具和安全错误摘要。

嵌套事件允许文本、思考、工具调用、工具结果、usage、warning、completed 和 error。不得转发 Provider 私有对象、凭证、原始请求头或未脱敏配置。

Task Service 为观察者提供有界发布通道。慢消费者不得阻塞子 Agent；缓冲溢出时记录截断状态，继续保留生命周期终态和最终结果。

## 数据流

1. 父 Agent 调用 `task_run`。
2. Task Service 创建任务并发布 started/running。
3. 子 Agent Engine 产生 canonical events。
4. Task Service 更新任务快照、进度和 usage，同时发布带 `task_id` 的嵌套事件。
5. 父 Runtime 将事件合并到当前输出流，但不写入父 Agent 对话历史。
6. TUI 按任务 ID 路由事件，更新对应子 Agent 视图。
7. 子 Agent 完成或失败后发布终态，父 Agent 仍通过原有 `task_run` 工具结果取得最终结果。

## TUI 交互

- 父视图保持现有主对话和 `task_run` 工具摘要。
- `Ctrl+T` 打开任务列表；若当前正在查看子 Agent，则返回父视图。
- 任务列表使用方向键选择，`Enter` 进入任务，`Esc` 关闭列表。
- 子 Agent 视图占用整个主消息区域，不与父对话混排。
- `[` 和 `]` 在任务间切换；`Esc` 返回父视图。
- 顶部显示任务描述、ID、Agent、状态、usage 和返回提示。
- 每个视图独立保存消息、工具状态和滚动位置。
- 用户停留在父视图时，后台任务仍持续更新；任务列表显示运行、完成、失败和取消状态。
- 已完成任务仍可进入查看保存的展示快照。

## 控制面

`/tasks` 返回真实任务列表，而不是固定的 `tasks: enabled`。每项包括任务 ID、状态、描述、Token、最近工具和是否发生展示截断。任务取消仍由受权限控制的 `task_cancel` 工具完成。

## 生命周期与错误处理

- 前台任务继承调用 turn 的取消上下文。
- 后台任务继续使用 Task Service 生命周期上下文。
- 子 Agent 错误更新任务终态并显示安全错误摘要，不终止 TUI。
- 父 Runtime 或 UI 消费者关闭后，Task Service 不得阻塞或泄漏 goroutine。
- 任务视图使用有界事件/文本容量；超限时保留最新活动、终态和最终结果，并显示截断警告。

## 测试策略

- Core：事件复制、序列化和嵌套事件校验。
- Tasks：订阅、事件顺序、慢消费者、缓冲截断、终态必达和并发任务隔离。
- CLI/Runtime：子 Agent 事件从 Engine 到父 Runtime 的桥接；OpenAI-compatible 与 Anthropic 工具循环。
- TUI：任务列表、进入/返回、Agent 切换、独立滚动、流式文本、工具状态、usage、失败和后台完成。
- Control plane：`/tasks` 输出真实快照。
- 回归：全量测试、race、vet、覆盖率门禁和 macOS/Linux/Windows 构建。

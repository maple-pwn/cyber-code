# Tactical Ops TUI

Phase 3 将 Tactical Ops 提升为 `cyber-code` 的默认交互界面。它是终端原生的任务控制客户端，使用与 Web 相同的 product event schema 和 deterministic projector。

## 当前边界

默认入口仍是明确标记的 `DEMO`：

```bash
cyber-code "评估 juice-shop.lab"
```

- 默认 source 是内置 `scenario`，也可显式写成 `--source=scenario`。
- Scenario 只接受包含 `juice-shop.lab` 的演示目标。
- Demo 不读取 Provider 凭据，不调用真实 Agent runtime，也不执行真实安全测试。
- 本地场景失败只进入 `task.blocked`，不会静默切换到远端 runtime。
- Phase 4 接入真实 local/remote EventSource 前，标题栏始终显示 `DEMO`。

需要当前完整的编码 Agent、斜杠命令、Provider 和开发工具时，使用：

```bash
cyber-code --ui=classic
```

`classic` 保留一个弃用周期。`--print`、`--print --json` 和 `serve` 不受默认交互界面变化影响。

## 快捷键

| 键 | 行为 |
| --- | --- |
| `Enter` | 无任务时创建 Demo 任务；有任务时发送 instruction |
| `F4` | 确认当前 Scope proposal |
| `F5` | 暂停任务；任务已暂停时继续 |
| `F6` | 按当前 lease revision 显式接管控制权 |
| `F8` | 请求取消任务 |
| `F2` | 打开或关闭全屏 Inspector |
| `Ctrl+T` | 打开 Agent 任务列表或返回 Narrative Stream |
| `Enter` | 在 Agent 列表中打开选中 Agent |
| `[` / `]` | 在 Agent 详情中切换 Agent |
| `Esc` | 返回父视图或恢复 composer 焦点 |
| `Tab` | 将焦点移入待处理 approval |
| `R` | Review approval 的不可变参数 |
| `C` | Review 后确认 `allow_once` |
| `D` | 立即拒绝 approval |
| `Ctrl+C` | 退出 Tactical Ops 并恢复终端 |

80 列终端使用紧凑键位标签，但 Pause/Resume、Cancel、Scope、Takeover、Inspector 和 Agents 均保留。

## 连接与恢复

Tactical adapter 按 cursor 接收严格校验的 schema v1 事件：

- cursor gap 会进入 `RESYNCING`，读取 snapshot 后从新 cursor 续订；
- unsupported schema 会进入 `INCOMPATIBLE`，冻结最后可信 cursor 并解除订阅；
- offline、resyncing 和 incompatible 状态只读；
- control lease 被其他客户端持有时只读，但仍允许显式 Takeover；
- Evidence 和已确认 Scope 只由 projector 更新，界面不直接修改权威状态。

Allow 与 Deny 场景的 terminal state SHA-256 在测试中与 Web canonical fixture manifest 精确一致。

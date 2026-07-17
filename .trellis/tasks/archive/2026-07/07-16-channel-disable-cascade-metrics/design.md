# Design: 渠道禁/启用级联 metrics

## Boundaries

| 层 | 职责 |
|----|------|
| `modelroute` | 批量对某 channel 的 metrics 施加 `EventManualDisable` / `EventRestoreAuto`（复用 `AdminForceState` / `ApplyTransition`） |
| `controller/channel.go` | 渠道页/批量/标签禁启用成功后调用级联 |
| `model.UpdateChannelStatus` | **不**直接 import modelroute；保持数据层纯净 |
| `service.DisableChannel` | 写 status=3，**不**调用级联 |

## API / 函数契约

```go
// modelroute: 按渠道目标状态级联 metrics。
// status==ChannelStatusManuallyDisabled → 该渠道全部 metrics EventManualDisable
// status==ChannelStatusEnabled          → 该渠道全部 metrics EventRestoreAuto（仅 MANUALLY_DISABLED 会变）
// 其它 status                           → no-op
// 返回处理的 metrics 行数；单行失败记日志并继续（或返回 error，实现时选「首错即返」更简单可测）
func CascadeMetricsForChannelStatus(channelID int64, status int) (int, error)
```

实现要点：

1. `ListChannelModelMetricsByChannel(channelID)`；空列表返回 `(0, nil)`。
2. 映射 status → event；无映射则 return 0,nil。
3. 每行 `AdminForceState(channelID, effectiveModel, event)`（或等价：LoadOrEnsure + ApplyTransition + SnapshotCritical）。
4. 可选：`InvalidateAllRoutePlans()` — 与删除渠道一致、偏保守；若 `ApplyTransition` 已清 Role 且候选链读 runtime state，可不 invalidate。**推荐首版不 invalidate**，与单条 metrics action 行为一致；若联调发现 plan 粘滞再加。

## 调用点

| 入口 | 何时调用 |
|------|----------|
| `controller.UpdateChannelStatus` | `changed == true` 且 status∈{1,2} |
| `controller.BatchUpdateChannelStatus` | 每个 `UpdateChannelStatus` 返回 true 的 id，status∈{1,2} |
| `controller` 标签禁用 | `DisableChannelByTag` 成功后，枚举 tag 下 channel id，每个 status=2 级联 |
| `controller` 标签启用 | `EnableChannelByTag` 成功后，枚举 tag 下 channel id，每个 status=1 级联 |

标签路径：`GetChannelsByTag` 取 id 列表再循环 `CascadeMetricsForChannelStatus`。

## 数据流

```
渠道页 禁用(status=2)
  → model.UpdateChannelStatus (abilities/cache)
  → modelroute.CascadeMetricsForChannelStatus(id, 2)
      → list metrics by channel
      → ApplyTransition(EventManualDisable) + SnapshotCritical per row
```

启用对称，event = EventRestoreAuto。

## 兼容与风险

- **与单条人工禁用叠加**：渠道启用会 `restore_auto` 所有 `MANUALLY_DISABLED` 行，包括此前仅在模型路由页手动禁用、渠道本身未禁用的行——但仅当渠道发生启用状态变更时才会触发；渠道一直为启用则无此副作用。
- **多 Key 渠道**：`UpdateChannelStatus` 在 multi-key 下可能只改 key 状态、渠道整体 status 未变 → `changed==false` 不级联，合理。
- **循环依赖**：级联只从 controller → modelroute → model，不反向。
- **失败策略**：渠道 status 已提交后 metrics 级联失败 → 记 SysLog，HTTP 仍 success（与现有「status 已改、旁路失败不回滚」一致）；测试覆盖成功路径与空列表。

## Rollback

去掉 controller 三处调用 + 删除 `CascadeMetricsForChannelStatus` 即可；无 schema 变更。

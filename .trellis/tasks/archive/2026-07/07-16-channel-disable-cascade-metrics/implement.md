# Implement: 渠道禁/启用级联 metrics

## Checklist

1. [ ] `modelroute`：新增 `CascadeMetricsForChannelStatus(channelID int64, status int) (int, error)`
   - status 2 → `EventManualDisable`；status 1 → `EventRestoreAuto`；其它 no-op
   - 列表 → 循环 `AdminForceState`
2. [ ] `controller.UpdateChannelStatus`：`changed && (status==1||status==2)` 时调用级联
3. [ ] `controller.BatchUpdateChannelStatus`：每个成功变更的 id 同上
4. [ ] 标签禁用/启用 handler：成功后 `GetChannelsByTag` 循环级联
5. [ ] 测试（`modelroute` 或 `controller` 包，按现有测试 DB fixture 风格）：
   - 禁用级联：seed 2 条 metrics → status 2 → 均为 MANUALLY_DISABLED
   - 启用恢复：从 MANUALLY_DISABLED → status 1 → PROBING
   - status 3：metrics 不变
   - 无 metrics：不报错
6. [ ] 跑相关测试：`go test ./modelroute/ ./controller/ -count=1 -run 'Cascade|ChannelStatus|ManualDisable'`（按实际测试名调整）

## Validation

```bash
go test ./modelroute/ -count=1 -run Cascade
go test ./controller/ -count=1 -run 'UpdateChannelStatus|BatchUpdateChannelStatus|ChannelTag'  # if added
```

## Risky files

- `controller/channel.go` — 多入口挂载
- `modelroute/migration.go`（或新建 `cascade.go`）— 批量 AdminForceState

## Rollback point

纯函数 + controller 调用；无迁移。

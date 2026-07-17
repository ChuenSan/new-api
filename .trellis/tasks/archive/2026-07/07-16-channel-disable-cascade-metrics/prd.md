# 渠道禁用级联模型路由指标人工禁用

## Goal

在渠道管理对渠道执行人工禁用时，将该渠道下全部 `channel_model_metrics` 的 `route_state` 统一置为 `MANUALLY_DISABLED`；渠道重新启用时对仍为 `MANUALLY_DISABLED` 的指标执行 `restore_auto`，与模型路由「指标」页人工禁用/恢复语义一致。

## Background

- 渠道状态：`1=启用` / `2=人工禁用` / `3=自动禁用`（`common.ChannelStatus*`）。
- 指标状态机已有 `EventManualDisable` → `MANUALLY_DISABLED`、`EventRestoreAuto` → `PROBING`（`modelroute.ApplyTransition` / `AdminForceState`）。
- 现状：渠道禁用不写 metrics；仅模型路由页可对单条渠道×模型人工禁用。

## Confirmed decisions

| 决策 | 结论 |
|------|------|
| 启用时 | 成对 `restore_auto`（仅作用于当前为 `MANUALLY_DISABLED` 的行） |
| status=3 自动禁用 | **不**级联 metrics |
| 按标签禁/启用 | **同样级联**（与 id 单条/批量一致） |
| 无 metrics 行 | 不预创建，跳过 |
| 状态语义 | 复用既有 transition，不新造状态 |

## Requirements

1. 人工禁用成功（`status → 2`）后，该 `channel_id` 下所有已存在 metrics 行 `route_state = MANUALLY_DISABLED`，并按现有 critical 路径持久化。
2. 覆盖路径：`UpdateChannelStatus`、`BatchUpdateChannelStatus`、`DisableChannelByTag`。
3. 重新启用成功（`status → 1`）后，该渠道下当前为 `MANUALLY_DISABLED` 的 metrics 执行 `restore_auto`。
4. 覆盖路径：`UpdateChannelStatus`、`BatchUpdateChannelStatus`、`EnableChannelByTag`。
5. `status → 3`（`service.DisableChannel` 等）不调用级联。
6. 运行时缓存 / Role / Snapshot 与单条 `AdminForceState` 一致。
7. 不改变删除渠道、模型路由单条 action、熔断/限流逻辑。

## Acceptance Criteria

- [ ] 单渠道禁用（status=2）后，该渠道全部 metrics 为 `MANUALLY_DISABLED` 且 DB 可读。
- [ ] 批量禁用多渠道后，各渠道 metrics 均为 `MANUALLY_DISABLED`。
- [ ] 按标签禁用后，标签下各渠道 metrics 均为 `MANUALLY_DISABLED`。
- [ ] 单条/批量/按标签启用后，原 `MANUALLY_DISABLED` metrics 进入 `PROBING`（`restore_auto`）。
- [ ] 自动禁用 status=3 不改变任何 metrics 的 `route_state`。
- [ ] 无 metrics 行时禁/启用不报错。
- [ ] 单元测试覆盖上述行为。

## Out of scope

- 前端改动。
- 无 metrics 时预创建再禁用。
- 修改 `MANUALLY_DISABLED` 状态机语义。
- 自动禁用级联 metrics。

## Technical notes (summary)

- 级联逻辑放在 `modelroute`（如 `CascadeMetricsForChannelStatus`），由 `controller` 在禁/启用成功后调用；避免 `model` → `modelroute` 循环依赖。
- 仅在渠道 status **实际变更成功**后触发；`UpdateChannelStatus` 返回 `false` 时不级联。
- 详见 `design.md` / `implement.md`。

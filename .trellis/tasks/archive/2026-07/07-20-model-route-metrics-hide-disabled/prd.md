# model-route metrics hide disabled channels

## Goal

模型路由 → 指标 页面只展示当前处于启用状态的渠道对应的渠道模型；已禁用渠道关联的渠道模型指标记录在前端被过滤，不再渲染。本次仅调整展示层，不动数据库、不动渠道数据、不动调度逻辑。

## Background

- 指标接口 `GET /api/model_route/metrics`（`controller/model_route.go` `ListModelRouteMetrics`）当前 `rowView` 只输出 `Role / IsStale / ChannelName / BaseURL / RequestedModels`，未携带渠道状态。
- 后端 `channelDisplayMap`（同文件）已查询并返回每渠道的 `Status` 与 `Exists`，但未被 `rowView` 使用。
- 前端 `ModelRouteMetrics` 类型（`web/default/src/features/model-route/types.ts`）无状态字段；`index.tsx` 的 `metrics` useMemo 仅按渠道名/ID 与模型关键词过滤，无渠道状态维度。
- 策略接口已通过 `channel_status` / `channel_exists` 暴露渠道状态（`ModelRoutePolicy`），本次让指标接口口径与之对齐。

## Requirements

- 后端 `ListModelRouteMetrics` 的响应行新增只读字段 `channel_status`（int）与 `channel_exists`（bool），取自已有的 `channelDisplayMap`；不新增数据库查询、不改渠道数据、不改调度。
- 前端 `ModelRouteMetrics` 类型补 `channel_status?: number`、`channel_exists?: boolean`。
- 前端 `metrics` useMemo 在现有渠道/模型关键词过滤之外，增加一条：渠道整体状态非启用（`channel_status !== ChannelStatusEnabled`，或 `channel_exists === false`）的行不渲染。
- 禁用判定口径：渠道整体 `Status`（与策略页 `channel_status` 一致），不涉及多 key 细分（`MultiKeyStatusList`）。
- 过滤在前端展示层完成；后端仍返回全部 metrics 行（不删除数据），渠道重新启用后前端自然恢复展示。
- 不影响「策略」页面与模型路由实际请求调度逻辑。

## Acceptance Criteria

- [ ] `ListModelRouteMetrics` 响应每行包含 `channel_status` 与 `channel_exists`，值与 `channelDisplayMap` 一致。
- [ ] 禁用某渠道后，进入/刷新 模型路由 → 指标 页面，该渠道对应的渠道模型不再出现。
- [ ] 同一模型存在多个渠道时，只过滤已禁用渠道，其他启用渠道正常展示。
- [ ] 将已禁用渠道重新启用后，其对应渠道模型重新出现在指标页面。
- [ ] 搜索、刷新、批量/单行 reset_unknown 等操作后，已禁用渠道数据仍不展示（过滤在 useMemo，覆盖所有渲染路径）。
- [ ] 不影响「策略」页面展示与模型路由实际请求调度逻辑。
- [ ] 后端现有测试通过；前端 `tsc`/lint 通过。

## Out of Scope

- 多 key 渠道的细粒度状态过滤。
- 后端在 SQL 层剔除禁用渠道 metrics（保留全量返回）。
- 指标数据的清理或重置逻辑变更。

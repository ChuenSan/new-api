# Journal - baizige (Part 1)

> AI development session journal
> Started: 2026-07-16

---



## Session 1: 渠道禁用级联模型路由指标人工禁用

**Date**: 2026-07-17
**Task**: 渠道禁用级联模型路由指标人工禁用
**Branch**: `feat/model-route-shadow-probe`

### Summary

渠道页单条/批量/标签人工禁用(status=2)时，将该渠道全部 channel_model_metrics 置为 MANUALLY_DISABLED；启用时 restore_auto→PROBING。自动禁用 status=3 不级联。实现 CascadeMetricsForChannelStatus + controller 挂载与单测，已 push 并触发 Docker verify Actions。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `2002753c` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete

---

## Session 2: 令牌级可用模式规划

**Date**: 2026-07-17
**Task**: 07-17-token-availability-mode
**Branch**: `feat/model-route-shadow-probe`

### Summary

令牌级可用模式规划完成：失败即按现有算法再选路，不设次数上限；prd/design/implement + jsonl 已齐，待用户审后 start。

### Status

[WIP] Planning — awaiting review before task.py start

---

## Session 3: 模型路由指标页隐藏禁用渠道

**Date**: 2026-07-20
**Task**: 07-20-model-route-metrics-hide-disabled
**Branch**: `feat/model-route-shadow-probe`

### Summary

模型路由 → 指标 页面前端过滤掉已禁用渠道对应的渠道模型。方案 A：后端 `ListModelRouteMetrics` 的 rowView 补只读 `channel_status`/`channel_exists`（复用已有 `channelDisplayMap`，零额外查询），前端类型补字段、`metrics` useMemo 增加状态过滤（口径与策略页一致：`channel_exists===false` 或 `channel_status!==ENABLED` 即不渲染）。仅展示层，不删数据、不改渠道、不影响调度；渠道重新启用后自然恢复展示。

### Main Changes

- `controller/model_route.go`: rowView 新增 `ChannelStatus`/`ChannelExists` 字段并从 info 填充。
- `web/default/src/features/model-route/types.ts`: `ModelRouteMetrics` 补 `channel_status?`/`channel_exists?`。
- `web/default/src/features/model-route/index.tsx`: import `CHANNEL_STATUS`；`metrics` useMemo 先按渠道状态过滤再叠关键词过滤。

### Testing

- `go build ./controller/...` 通过。
- `go test ./controller/... -run "ModelRoute|Metrics"` 通过。
- `go test ./model/... -run "Channel|Metrics"` 通过。
- `npm run typecheck`(tsgo -b) 通过。
- `npm run lint`: 改动文件无报错（既有报错均在未触碰文件）。

### Status

[OK] **Completed** — 待用户确认是否提交

### Next Steps

- 用户确认后按提交约定 commit。


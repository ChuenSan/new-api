# Journal - baizige (Part 1)

> AI development session journal
> Started: 2026-07-16

---

## Session 2: 模型路由页刷新强制重取

**Date**: 2026-07-22
**Task**: 模型路由页刷新功能优化
**Branch**: `feat/model-route-shadow-probe`

### Summary

模型路由后台页「刷新」按钮存在"复用进行中旧请求、不发新请求"的缺陷,导致刷新后仍显示旧数据,与重新进入页面/F5 不一致。三处根因(实测):(1) `handleRefresh` 对两个 query 用 `refetch({ cancelRefetch: false })`,in-flight 时直接复用旧 promise;(2) `lib/api.ts` 的 `inFlightGet` 传输层对相同 GET 并发去重,叠加根因1 将两次刷新合并为同一旧请求;(3) `isRefreshing` 仅依赖 `isFetching` 的异步翻转,极快连点可绕过 `if (isRefreshing) return`。

方案:改用 `qc.refetchQueries({ queryKey, exact: true }, { cancelRefetch: true, throwOnError: true })` 替代 `query.refetch({ cancelRefetch: false })`——`cancelRefetch: true` 取消 in-flight 查询再重发(顺带触发 `inFlightGet` 的 `.finally` 删 key,新请求正常发出,无需改传输层);新增 `refreshingRef = useRef(false)` 同步互斥堵连点漏拦;成功清空 `optimisticPolicyOrders` 防拖拽中途刷新冲突;失败用 `throwOnError: true` 让 `refetchQueries`(类型为 `Promise<void>`,经 `@tanstack/query-core@5.101.2` dts 核实)reject 走 catch,TanStack Query 保留旧 data。

实现时发现并修正一处设计偏差:原 design 误以为 `refetchQueries` 返回 `QueryObserverResult[]` 可逐项查 `status`,实际是 `Promise<void>` 且默认吞错,故改为 `throwOnError: true` + catch。`lib/api.ts` 按用户决定不改。

### Main Changes

- `web/default/src/features/model-route/index.tsx`:导入加 `useRef`;新增 `refreshingRef`;重写 `handleRefresh`(三处改动,~24 行)。`isRefreshing`、按钮渲染、所有 mutation 及 `stale_policy_snapshot` 恢复链路均未动。
- `.trellis/tasks/07-22-model-route-refresh-force-refetch/`:prd.md / design.md / implement.md 三件套。

### Git Commits

| Hash | Message |
|------|---------|
| — | 暂不提交(用户决定),改动已通过 typecheck + lint,留待手动 QA 后提交 |

### Testing

- `tsgo -b`:退出码 0(类型检查通过,`throwOnError` 经 `RefetchOptions extends ResultOptions` 合法)。
- `oxlint src/features/model-route/index.tsx`:退出码 0(无 warning/error)。
- 14 Case 手测:留待 baizige 在线上后台(jp279-cpa / sgp)手动验收,重点 Case 9(连点发新请求)、Case 13(刷新 vs 重新进入一致)。

### Status

[OK] **代码完成,待手动 QA + 提交**

### Next Steps

- baizige 手动验收 Case 1-14 后提交并归档任务。



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



## Session 2: 模型路由指标页隐藏禁用渠道

**Date**: 2026-07-20
**Task**: 模型路由指标页隐藏禁用渠道
**Branch**: `feat/model-route-shadow-probe`

### Summary

模型路由→指标页前端过滤已禁用渠道对应的渠道模型。方案A:后端 ListModelRouteMetrics 的 rowView 补只读 channel_status/channel_exists(复用已有 channelDisplayMap,零额外查询),前端类型补字段、metrics useMemo 增加状态过滤(口径与策略页一致:channel_exists===false 或 channel_status!==ENABLED 不渲染)。仅展示层,不删数据/不改渠道/不影响调度,渠道重新启用后自然恢复。go build+相关单测+前端 tsgo 类型检查均通过。一并归档 4 个 in_progress 任务(metrics-hide-disabled/reset-unknown/api-key-channel-whitelist/token-availability-mode)。

### Main Changes

- Detailed change bullets were not supplied; see the summary above.

### Git Commits

| Hash | Message |
|------|---------|
| `79eb6d52` | (see git log) |

### Testing

- Validation was not recorded for this session.

### Status

[OK] **Completed**

### Next Steps

- None - task complete

# 通用日志「快捷禁用」渠道模型

## Goal

在「使用日志 → 通用日志」每条请求日志上新增 **快捷禁用** 入口。管理员点击后，等价于在「模型路由 → 指标」页面定位到该日志对应的 `渠道 × 生效模型` 记录并执行现有的 **人工禁用**，无需跳转页面手动搜索。

禁用范围严格限定为：**当日志所属渠道下、该次请求实际使用的那一个模型**。不禁用整个渠道，不影响其它渠道下的同名模型。

## Background / Confirmed Facts

### 现有「人工禁用」链路（复用目标，不新造）

| 层 | 位置 | 事实 |
|----|------|------|
| 前端入口 | `web/default/src/features/model-route/index.tsx:1138-1164` | 指标表行内用 `Select` 作动作选择器，`t('Manual disable')` 在 `:245` |
| 前端 handler | `index.tsx:721-734` | 非 `reset_unknown` 路径直接 `actionMut.mutate(buildMetricsActionRequest(row, action))`，**行内路径无确认弹窗** |
| 请求构造 | `features/model-route/lib/metrics-reset.ts:65-74` | `buildMetricsActionRequest(row, action)` → `{channel_id, effective_model, action}`，纯函数可复用 |
| API | `features/model-route/api.ts:60-65` | `modelRouteMetricsAction` → `POST /api/model_route/metrics/action` |
| 路由与鉴权 | `router/api-router.go:231-243` | `modelRoute` 组 `middleware.RootAuth()` |
| 后端 handler | `controller/model_route.go:350-405` | `manual_disable → modelroute.EventManualDisable`（`:383`）→ `modelroute.AdminForceState`（`:391`）→ `recordManageAudit("model_route.metrics_action")`（`:401`） |
| 状态迁移 | `modelroute/route_state.go:209-212` | `EventManualDisable`：`RouteState = MANUALLY_DISABLED`、清空 `cooldown_until`、`GlobalRoles.Set(mk, RoleNone)` |
| 落库 | `modelroute/migration.go:155-162` | `AdminForceState` = `LoadOrEnsureMetrics` + `ApplyTransition` + `SnapshotCritical`；**指标行不存在时会 `EnsureChannelModelMetrics` 自动创建** |

结论：后端 `POST /api/model_route/metrics/action` 已完全满足需求，**本任务后端零改动或仅做只读辅助**；工作量集中在前端定位 `effective_model` 与入口 UI。

### 日志行可用字段

| 需要 | 日志字段 | 位置 |
|------|----------|------|
| `channel_id` | `log.channel`（number） | `features/usage-logs/data/schema.ts:40` |
| 渠道名（展示用） | `log.channel_name` | `schema.ts:41` |
| 请求模型 | `log.model_name` | `schema.ts:34` |
| 上游/生效模型 | `other.upstream_model_name`，**仅当 `other.is_model_mapped === true`** | `features/usage-logs/types.ts:164-165`；解析 `lib/format.ts:98-107`、`formatModelName` `:156-173` |

后端写入侧对齐：`service/log_info_generate.go:86-87` 只在 `relayInfo.IsModelMapped` 时写 `is_model_mapped` / `upstream_model_name`。

**关键事实**：日志行**没有** `effective_model` 字段。模型路由指标以 `effective_model` 为主键（`model/channel_model_metrics.go:16-17`），其解析规则是 `modelroute.ResolveEffectiveModel(requestedModel, modelMappingJSON)`（`modelroute/policy_key.go:39`，支持链式映射与自映射），前端不掌握渠道的 `model_mapping`。因此「日志 → 指标行」的定位存在映射缺口，必须显式解决（见 R2）。

### 权限落差（必须处理）

- 通用日志页：`useIsAdmin()`（`ROLE.ADMIN`）即可见，路由本身无角色守卫（`routes/_authenticated/usage-logs/$section.tsx:52-75`）。
- 模型路由页：前端要求 `ROLE.SUPER_ADMIN`（`routes/_authenticated/model-route/index.tsx:25-32`），后端 `RootAuth()`。

即普通 ADMIN 能看日志但调该接口必 403。入口必须按 SUPER_ADMIN 门控，不能沿用 `useIsAdmin()`。

### 通用日志表现状

- 列定义 `features/usage-logs/components/columns/common-logs-columns.tsx:294-890`，**当前没有任何行内操作列**（无 `id: 'actions'`）。
- 房内既有范式：`features/models/components/data-table-row-actions.tsx:50-140`（菜单 + `ConfirmDialog` + `queryClient` 失效）、列形态 `features/keys/components/api-keys-columns.tsx:346-350`（`meta: { pinned: 'right' }`，右钉由 `components/data-table/core/data-table-view.tsx:174-197` 生效）。
- 移动端卡片 `components/usage-logs-mobile-card.tsx:178-240`，cells map 由 `:359-361` 按列 id 构建，新增列自动可取 `cells.get('actions')`。
- i18n：单一 `translation` 命名空间、以英文原文为 key。已存在可复用 key：`Actions`、`Manual disable`（`zh.json:2571`）、`MANUALLY_DISABLED`（`zh.json:2573`）、`Action applied`、`Action failed`。

## Requirements

### R1 — 通用日志行内「快捷禁用」入口

- MUST 仅在 `logCategory === 'common'` 的通用日志表出现；绘图/任务日志不加。
- MUST 入口文案为「快捷禁用」（新增 i18n key `Quick disable`，中文「快捷禁用」）。
- MUST 桌面端以行内操作列呈现，列 id `actions`、右侧固定；移动端卡片同步可用。
- MUST 仅对 `ROLE.SUPER_ADMIN` 渲染；非 SUPER_ADMIN 不渲染该列/该入口（不做渲染后再报 403）。
- MUST 当 `log.channel <= 0`（无渠道信息，如系统日志/充值日志）或 `log.model_name` 为空时禁用该入口。
- MUST NOT 改动通用日志现有列的数据、排序、筛选与列可见性存储语义（列可见性 key 见 `usage-logs-table.tsx:59-64`）。

### R2 — 定位到正确的 `渠道 × 生效模型`

- MUST 提交的 `channel_id` 取 `log.channel`。
- MUST 提交的 `effective_model` 必须与模型路由指标页该行的 `effective_model` 一致。
- MUST 解析优先级：`other.is_model_mapped === true && other.upstream_model_name` 非空 → 用 `upstream_model_name`；否则回退 `log.model_name`。
- MUST 该回退在「渠道存在 `model_mapping` 但该次请求未标记 mapped」时仍需正确；若前端无法确定，MUST 由后端权威解析（后端已有 `modelroute.ResolveEffectiveModel` 与 `resolvePolicyEffectiveModel`，`controller/model_route.go:485-491`），不得在前端复制映射算法。
  - 落地形态由 `design.md` 决策：二选一 —— (a) 前端按上述优先级直接提交；(b) 新增只读解析或让后端在 action 接口按 `requested_model` 兜底解析。**禁止前端自行实现 `model_mapping` 链式解析。**
- MUST NOT 禁用整个渠道；MUST NOT 影响其它渠道下的同名模型；MUST NOT 影响同渠道下的其它模型。

### R3 — 复用既有禁用机制

- MUST 复用 `POST /api/model_route/metrics/action`，`action = 'manual_disable'`。
- MUST 复用 `buildMetricsActionRequest`（`lib/metrics-reset.ts:65-74`）构造请求体，不新写请求结构。
- MUST NOT 新增第二套禁用接口、第二套状态机或第二种禁用状态。
- MUST 结果与模型路由页人工禁用完全一致：`route_state = MANUALLY_DISABLED`、`cooldown_until` 清空、`role = NONE`、写 `model_route.metrics_action` 审计。
- MUST 目标指标行不存在时沿用后端 `EnsureChannelModelMetrics` 的自动创建行为，不在前端做存在性预校验。

### R4 — 确认流程与反馈

- MUST 点击后先弹确认，确认文案明确展示将被禁用的 `渠道`（`#id` + 名称）与 `模型`，避免误禁。
  - 说明：模型路由页行内路径无确认弹窗，但日志页密度高、误点成本高，此处按房内 `ConfirmDialog`（`components/confirm-dialog.tsx:33-48`）范式补确认，属于加固而非改动原有页面。
- MUST 成功提示复用 `t('Action applied')`，失败复用 `t('Action failed')` 并展示后端 message（可用 `getMetricsActionErrorMessage`）。
- MUST 请求进行中禁用重复提交。
- MUST 成功后失效 `['model-route-metrics']` 查询，使已打开的模型路由页刷新；日志列表本身无需失效（日志行内容不变）。

### R5 — 兼容与边界

- MUST NOT 改动模型路由页现有 UI 与行为。
- MUST NOT 改动日志写入侧（`service/log_info_generate.go` 等）。
- MUST NOT 改动 `/api/log/*` 的鉴权。
- MUST 保持 `/api/model_route/metrics/action` 的 `RootAuth()` 不放宽。

## Acceptance Criteria

- [ ] AC1: 通用日志表出现「快捷禁用」入口，绘图/任务日志无此入口
- [ ] AC2: 仅 SUPER_ADMIN 可见该入口；普通 ADMIN 完全不可见
- [ ] AC3: 对渠道 `#16` / 模型 `grok-4.5` 的日志执行快捷禁用后，模型路由指标页中 `#16 × grok-4.5` 行状态为 `MANUALLY_DISABLED`
- [ ] AC4: 同一操作后，`#16` 下其它模型状态不变；其它渠道下的 `grok-4.5` 状态不变
- [ ] AC5: 对发生了 model_mapping 的日志（`is_model_mapped=true`）执行后，被禁用的是上游生效模型对应的那一行，而非请求模型名
- [ ] AC6: 请求体为 `{channel_id, effective_model, action:'manual_disable'}`，走既有 `POST /api/model_route/metrics/action`，无新增禁用接口
- [ ] AC7: 操作写入 `model_route.metrics_action` 审计日志，内容含 channel_id / effective_model / action
- [ ] AC8: 点击后先出确认弹窗，弹窗展示渠道与模型；取消不发请求
- [ ] AC9: 成功 toast 为「操作已应用」，失败展示后端 message；进行中不可重复提交
- [ ] AC10: 无渠道信息或模型名为空的日志行，入口为禁用态
- [ ] AC11: 移动端卡片同样可执行快捷禁用
- [ ] AC12: 模型路由页原有人工禁用行为、日志页原有列与筛选均无回归

## Out of Scope

- 快捷「恢复自动」/ `restore_auto` 入口
- 批量快捷禁用（多选日志行）
- 日志页展示路由状态、熔断信息等指标列
- 修改模型路由页的确认流程或行内动作 UI
- 放宽 `/api/model_route/metrics/action` 权限、或为普通 ADMIN 提供该能力
- 在日志表新增除本入口外的其它行内操作
- 日志写入侧补充 `effective_model` 字段的数据迁移

## Notes

- 复杂度判定：涉及跨 feature 复用、权限落差、`effective_model` 映射缺口三处决策点 → 需 `design.md` + `implement.md` 后再 `task.py start`。
- 最大风险点是 R2 的映射定位：若前端简单用 `model_name` 提交，在渠道配置了 `model_mapping` 而该次请求未标记 mapped 的场景下，会创建/禁用一条**错误的**指标行（后端会 Ensure 出新行），造成「点了没生效 + 脏数据」。`design.md` 必须先定这一项。
- 次风险点是权限：沿用 `useIsAdmin()` 会给普通 ADMIN 一个必然 403 的按钮。

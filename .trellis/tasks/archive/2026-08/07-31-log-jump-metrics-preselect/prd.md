# 通用日志跳转模型路由指标并勾选目标行

## Goal

在「使用日志 → 通用日志」每条请求日志上新增一个快捷入口。点击后跳转到「模型路由 → 指标」页面，自动勾选与该日志 `渠道 ID × 请求模型` 对应的那一行，页面顶部出现「已选 1 条」的批量操作条。

入口的职责边界只有两件事：**跳转** + **勾选**。禁用、重置、恢复自动等实际动作，全部由用户在指标页面用现有 UI 手动完成。

## Background / Confirmed Facts

### 目标页面的选择机制（复用对象，不新造）

选择态是 `ModelRouteAdmin` 组件内的本地 `useState`，不在 URL、不在 store：

| 事实 | 位置 |
|------|------|
| `selectedMetricKeys: Set<string>` 组件内 state | `features/model-route/index.tsx:214-216` |
| 行 key = `` `${channel_id}:${effective_model}` `` | `features/model-route/lib/metrics-reset.ts:59-63`（`metricsRowKey`） |
| `selectedMetrics = metrics.filter(row => selectedMetricKeys.has(key))` — **只统计通过筛选后可见的行** | `index.tsx:612-615` |
| 「已选 {{count}} 条」批量条仅在 `selectedMetrics.length > 0` 时渲染 | `index.tsx:904-907`；i18n key `{{count}} selected` 已存在（`zh.json:62`） |
| 行内复选框 `onCheckedChange → toggleMetricSelected(key, ...)` | `index.tsx:1074-1082` |
| Tab 状态 `useState<'policies' \| 'metrics'>('policies')`，**默认 policies，不同步 URL** | `index.tsx:210`、`:843-845` |
| 路由 `/_authenticated/model-route/` **无 `validateSearch`**，当前不接受任何 search 参数 | `routes/_authenticated/model-route/index.tsx:25-33` |

结论：要落在指标 tab 且预勾选，必须新增一条跨页传参通道（URL search 参数是房内既有范式，见 `routes/_authenticated/channels/index.tsx:19-27` 与 usage-logs 的 `usageLogsSearchSchema`）。传参形态由 `design.md` 决定。

### 日志行可用字段

| 需要 | 字段 | 位置 |
|------|------|------|
| 渠道 ID | `log.channel`（number） | `features/usage-logs/data/schema.ts:40` |
| 渠道名（展示用） | `log.channel_name` | `schema.ts:41` |
| 请求模型 | `log.model_name` | `schema.ts:34` |
| 上游模型（仅 `other.is_model_mapped === true` 时有值） | `other.upstream_model_name` | `formatModelName`：`features/usage-logs/lib/format.ts:152-173` |

### 关键事实：`effective_model` 缺口已由 `requested_models` 闭合

日志行没有 `effective_model` 字段，而指标行以 `effective_model` 为主键；`effective_model` 的权威解析是后端 `modelroute.ResolveEffectiveModel(requestedModel, modelMappingJSON)`，支持链式映射，前端不掌握渠道的 `model_mapping`。

但指标列表接口已经把反查结果一并返回：`ListModelRouteMetrics` 为每行附加 `requested_models []string`（`controller/model_route.go:310`、`:319`、`:346`；前端类型 `features/model-route/types.ts:39`），且指标页搜索本来就同时匹配 `effective_model` 与 `requested_models`（`index.tsx:597-600`）。

因此「日志 → 指标行」的定位可以**纯前端、不复制映射算法**完成：在指标数据里找 `channel_id === log.channel` 且（`effective_model === log.model_name` **或** `requested_models` 包含 `log.model_name`）的行。这是本任务与已取消任务 `archive/2026-07/07-31-log-quick-disable-channel-model` 的核心差异——那次卡在映射缺口上，本次不需要后端改动。

### 指标页会隐藏部分行（必须处理）

`metrics` 的过滤链在渠道被禁用或不存在时直接丢弃该行：`channel_exists === false` 或 `channel_status !== CHANNEL_STATUS.ENABLED` → 不渲染（`index.tsx:585-590`）。同时 `selectedMetrics` 基于过滤后的 `metrics` 计算，所以**行被隐藏时即使 key 已在 selection 里，「已选」条也不会出现**。日志里的渠道完全可能已被禁用，这条路径必须有明确反馈，不能静默什么都不发生。

同理，若用户此前在指标页留有渠道/模型搜索关键词（`channelFilter` / `modelKeyword`，`index.tsx:212-213`），目标行也可能被筛掉。

### 权限落差（必须处理）

- 通用日志页：`useIsAdmin()` 即 `role >= ROLE.ADMIN` 可见渠道列（`components/usage-logs-table.tsx:77`、`columns/common-logs-columns.tsx:330`）；路由本身无角色守卫。
- 模型路由页：`beforeLoad` 要求 `role === ROLE.SUPER_ADMIN`，否则 `redirect('/403')`（`routes/_authenticated/model-route/index.tsx:26-32`）。

即普通 ADMIN 点这个入口必然被踢到 403 页。入口必须按 SUPER_ADMIN 门控，不能沿用 `useIsAdmin()`。

### 通用日志表现状

- 列定义 `features/usage-logs/components/columns/common-logs-columns.tsx`，当前**没有任何行内操作列**（无 `id: 'actions'`）。渠道列 `id: 'channel'` 在 `:333`，包在 `if (isAdmin)` 里（`:330`）。
- 移动端卡片按列 id 取 cell（`components/usage-logs-mobile-card.tsx:359-362`），新增列会自动进入 cells map。
- 列可见性持久化 key 见 `usage-logs-table.tsx:59-64`。
- i18n 单一 `translation` 命名空间、以英文原文为 key。可复用：`Metrics`（`zh.json:2631`）、`Model Route`（`:2715`）、`Select row`（`:4122`）、`{{count}} selected`（`:62`）。

## Requirements

### R1 — 通用日志行内快捷入口

- MUST 仅在 `logCategory === 'common'` 的通用日志表出现；绘图日志、任务日志不加。
- MUST 桌面端与移动端卡片均可用。
- MUST 仅对 `ROLE.SUPER_ADMIN` 渲染。非 SUPER_ADMIN 不渲染该入口（不做"渲染出来再跳 403"）。
- MUST 当 `log.channel <= 0`（无渠道信息，如充值/系统类日志）或 `log.model_name` 为空时，入口为禁用态或不渲染。
- MUST 入口的可视语义是「去模型路由指标里选中这一行」，不是「禁用」；文案与图标不得暗示会执行禁用动作。
- MUST NOT 改动通用日志现有列的数据、排序、筛选语义与列可见性存储 key 的含义。

### R2 — 跳转行为

- MUST 跳转到模型路由页并直接落在 **指标（metrics）** tab，不需要用户再点一次 tab。
- MUST 跳转参数至少携带渠道 ID 与请求模型（`log.model_name`）。
- MUST 目标页刷新（F5）后行为可重现，即跳转意图不能只存在于内存中的一次性事件。
- MUST NOT 改变模型路由页在无参数直接访问时的现有默认行为（默认落在 policies tab、无预选）。
- MUST 保持模型路由页的 SUPER_ADMIN 守卫不放宽。

### R3 — 行匹配规则

- MUST 匹配条件：`channel_id === log.channel`，且请求模型命中该行的 `effective_model` 或 `requested_models` 之一。
- MUST 请求模型以 `log.model_name` 为准；当 `other.is_model_mapped === true && other.upstream_model_name` 非空时，MAY 用 `upstream_model_name` 作为补充匹配候选，用于命中 `effective_model`。
- MUST NOT 在前端复制或近似实现 `model_mapping` 的链式解析。匹配只允许基于接口已返回的 `effective_model` / `requested_models` 字段。
- MUST 匹配到多行时（同渠道下多个 `effective_model` 都声明了该请求模型）全部勾选，并让「已选 N 条」如实反映数量。
- MUST NOT 在指标数据里凭空创建行。目标行不存在就是"未匹配"，走 R5 的反馈路径。

### R4 — 勾选结果

- MUST 勾选后页面顶部出现现有的批量操作条，「已选 1 条」（复用 `{{count}} selected`）。
- MUST 勾选态与用户手点复选框完全等价——同一个 `selectedMetricKeys`、同一个 `metricsRowKey` 形态，后续任何批量操作可直接使用。
- MUST NOT 自动执行 `manual_disable` 或任何其它 `metrics/action`。
- MUST NOT 自动修改目标行的 `route_state` / `role` / `cooldown_until`。
- MUST NOT 触发任何写接口。整个流程是只读 + 前端选择态。
- MUST NOT 仅做滚动定位或高亮而不勾选。滚动定位是可选加分项，勾选是必须项。

### R5 — 未命中与被隐藏时的反馈

- MUST 目标行不存在于指标数据时，给出明确提示（toast 或页面内提示），说明未找到该 `渠道 × 模型` 的指标记录；MUST NOT 静默无反应。
- MUST 目标行因渠道被禁用/不存在而被过滤隐藏时（`channel_exists === false` 或 `channel_status !== ENABLED`），给出可区分于"记录不存在"的提示，让用户知道原因是渠道不可用；MUST NOT 为此放宽指标页现有的隐藏规则。
- MUST 目标行因页面上遗留的渠道/模型搜索关键词被筛掉时，仍能让用户看到目标行（例如跳转时重置这两个筛选条件），或明确提示筛选正在遮挡。
- MUST 提示只发生一次，不得在指标数据每次 refetch 时重复弹出。

### R6 — 兼容边界

- MUST NOT 改动模型路由页现有的 policies tab、指标行内动作、批量动作行为。
- MUST NOT 改动日志写入侧（`service/log_info_generate.go` 等）与日志表结构。
- MUST NOT 改动 `/api/log/*`、`/api/model_route/*` 的鉴权与响应结构。
- SHOULD 后端零改动。`requested_models` 已随指标接口返回，无需新增只读解析接口。

## Acceptance Criteria

- [ ] AC1: 通用日志表出现该入口；绘图日志、任务日志无此入口
- [ ] AC2: 仅 SUPER_ADMIN 可见该入口；ADMIN 完全不可见，不存在"点了跳 403"的路径
- [ ] AC3: 对渠道 `#16`、请求模型 `grok-4.5` 的日志点击入口后，落在模型路由「指标」tab（无需手动切 tab）
- [ ] AC4: 落地后 `channel_id=16` 且请求模型为 `grok-4.5` 的那一行复选框为勾选态，顶部显示「已选 1 条」
- [ ] AC5: 该行以外的任何行均未被勾选
- [ ] AC6: 落地后目标行的 `route_state` / `role` / `cooldown_until` 与跳转前完全一致；无 `metrics/action` 等写请求发出（Network 面板可验证）
- [ ] AC7: 顶部批量操作下拉可正常对这一条已选记录执行「人工禁用」，行为与手动勾选后执行完全一致
- [ ] AC8: 对发生 model_mapping 的日志（`is_model_mapped=true`），勾选的是该渠道下声明了该请求模型的指标行（命中 `requested_models` 或上游 `effective_model`），而非新建/错行
- [ ] AC9: 无渠道信息（`channel <= 0`）或模型名为空的日志行，入口为禁用态或不渲染
- [ ] AC10: 指标中不存在该 `渠道 × 模型` 记录时，有明确提示，且不静默无反应
- [ ] AC11: 日志所属渠道已被禁用（指标页会隐藏该行）时，提示能让用户看出是渠道不可用导致，而非记录缺失
- [ ] AC12: 落地页面 F5 刷新后，勾选行为可重现（不依赖一次性内存事件）
- [ ] AC13: 直接访问 `/model-route`（无参数）时仍默认落在 policies tab、无任何预选
- [ ] AC14: 移动端卡片同样可完成跳转 + 勾选
- [ ] AC15: 提示不会随指标数据轮询/refetch 重复弹出
- [ ] AC16: 日志页原有列、筛选、列可见性无回归；模型路由页原有批量与行内动作无回归

## Out of Scope

- 在日志页直接执行禁用 / 恢复自动 / 重置（已取消任务 `archive/2026-07/07-31-log-quick-disable-channel-model` 的范围，本任务明确不做）
- 批量：多选日志行后一次性勾选多条指标
- 反向跳转：从指标页跳回相关日志
- 在日志表新增路由状态、熔断信息等指标列
- 修改指标页隐藏禁用渠道行的规则
- 修改指标页的确认流程、行内动作 UI、批量动作集合
- 为普通 ADMIN 开放模型路由页或其接口
- 日志写入侧补 `effective_model` 字段及历史数据迁移

## Notes

- 复杂度判定：需要 `design.md`。三个决策点必须先定：
  1. **跨页传参形态** —— 模型路由路由当前无 `validateSearch`，选择态是组件内 `useState`。需要定 URL search 参数的 schema（字段名、类型、是否 `replace`、落地后是否清参）以及它如何 seed `tab` 与 `selectedMetricKeys`。
  2. **seed 时机** —— 指标数据是异步 `useQuery`，参数到达时 `metrics` 可能还是空数组。需要定"数据到位后 seed 一次且只 seed 一次"的机制，避免 refetch 反复覆盖用户后续的手动勾选（直接关联 AC15、AC6）。
  3. **未命中/被隐藏的分支判定** —— "记录不存在" 与 "记录存在但被隐藏" 需要用未过滤的原始 `metricsQuery.data` 区分，不能只看过滤后的 `metrics`（AC10 / AC11）。
- 主要风险：seed 逻辑与 `metricsQuery` 生命周期耦合。若实现成"每次 `metrics` 变化就按参数覆盖 selection"，用户在落地页手动改选会被下一次轮询打回，且提示会重复弹。
- 次风险：入口文案若沿用"快捷禁用"之类措辞，用户会以为已经执行了禁用。文案必须表达"定位并选中"。

# 通用日志跳转模型路由指标并勾选目标行 — Implementation Plan

上下文阅读顺序：`implement.jsonl` → `prd.md` → `design.md` → 本文件。

## 0. 前置确认

- [ ] 确认工作分支为 `feat/model-route-shadow-probe`（`task.json.base_branch`）。
- [ ] 确认 `git status` 中 `web/default/src/features/model-route/` 与 `features/usage-logs/` 无未提交的他人改动。
- [ ] 确认 `controller/model_route.go:346` 仍返回 `RequestedModels`（本设计的地基；若该字段被移除，整个前端匹配方案失效，必须回到 planning）。

## 1. 纯函数层（先写，可单测，无 UI 依赖）

文件：`web/default/src/features/model-route/lib/metrics-reset.ts`

- [ ] 新增 `isMetricsRowVisible(row: ModelRouteMetrics): boolean`，条件与 `features/model-route/index.tsx:585-590` 逐字对齐：`row.channel_exists !== false && (row.channel_status === undefined || row.channel_status === CHANNEL_STATUS.ENABLED)`。
  - 注意 `CHANNEL_STATUS` 来自 `@/features/channels/constants`，`metrics-reset.ts` 目前未引入，需加 import。若不希望 lib 层依赖另一个 feature 的常量，可把状态值作参数传入——**但两处判定必须保持单一来源**，禁止复制条件。
- [ ] 新增 `findMetricsRowsForLog(rows, channelId, requestedModel)`：`requestedModel` 为空返回 `[]`；否则筛 `channel_id === channelId && (effective_model === requestedModel || (requested_models ?? []).includes(requestedModel))`。精确相等，不折叠大小写。
- [ ] 两个函数都 `export`，与 `metricsRowKey`（`:59-63`）同风格：纯函数、无 `t()`、无 toast。

文件：`web/default/src/features/model-route/lib/metrics-reset.test.ts`

- [ ] 追加 `describe('findMetricsRowsForLog')`：命中 `effective_model`、命中 `requested_models`、同渠道多行命中、`requestedModel` 为空、渠道 ID 不匹配。
- [ ] 追加 `describe('isMetricsRowVisible')`：`channel_exists === false` → false；`channel_status` 非 ENABLED → false；`channel_status === undefined` → true。
- [ ] 沿用文件现有范式：`node:test` 的 `describe/test` + `node:assert/strict`，不引入新测试框架。
- [ ] 保留文件头版权块（`scripts/add-copyright.mjs` 有 `copyright:check` 门禁）。

## 2. 路由 search schema

文件：`web/default/src/routes/_authenticated/model-route/index.tsx`

- [ ] 引入 `z`，新增 `modelRouteSearchSchema`：`tab: z.enum(['policies','metrics']).optional().catch(undefined)`、`channelId: z.number().optional().catch(undefined)`、`model: z.string().optional().catch('')`。
- [ ] 在 `createFileRoute` 里加 `validateSearch: modelRouteSearchSchema`，与现有 `beforeLoad` 并存（范式：`routes/_authenticated/channels/index.tsx:19-40`）。
- [ ] **不动** `beforeLoad` 的 SUPER_ADMIN 守卫（`:26-32`）。
- [ ] 验证：无参访问 `/model-route` 时三字段均为 `undefined`/`''`，不抛错、不 redirect。

## 3. 模型路由页 seed 逻辑

文件：`web/default/src/features/model-route/index.tsx`

- [ ] 取 search：用 `getRouteApi('/_authenticated/model-route/').useSearch()`（房内范式见 `features/usage-logs/components/usage-logs-table.tsx:48`）或 `Route.useSearch()`，二者择一，与文件现有 import 风格一致者优先。
- [ ] `tab` 初值改为读 search：`useState<'policies'|'metrics'>(search.tab === 'metrics' ? 'metrics' : 'policies')`（原 `:210`）。**必须保证无参时仍是 `'policies'`**（AC13）。
- [ ] 重构 `metrics` 的 filter（`:583-609`）：把行内的 `channel_exists` / `channel_status` 两行判定替换为 `isMetricsRowVisible(row)`。等价重构，不改行为，不动其余筛选条件。
- [ ] 新增 `seededRef = useRef<string | null>(null)`，放在现有 `refreshingRef`（`:227`）附近。
- [ ] 计算 `seedKey`：`search.channelId != null && search.model ? \`${search.channelId}:${search.model}\` : null`。
- [ ] 新增 seed `useEffect`，依赖 `[seedKey, metricsQuery.data]`：
  - [ ] `if (!seedKey) return`
  - [ ] `if (seededRef.current === seedKey) return`（挡 refetch 重复 seed → AC15）
  - [ ] `if (!metricsQuery.data) return`（数据未到位，等下次）
  - [ ] `seededRef.current = seedKey` —— **必须在任何副作用之前**
  - [ ] `const hits = findMetricsRowsForLog(metricsQuery.data.data, search.channelId, search.model)` —— 用**未过滤**的原始数据
  - [ ] `hits.length === 0` → `toast.error(t('No route metrics found for channel #{{channel}} / {{model}}', {...}))`，return（态 A）
  - [ ] `const visible = hits.filter(isMetricsRowVisible)`；`visible.length === 0` → `toast.warning(t('Channel #{{channel}} is disabled; its route metrics are hidden', {...}))`，return（态 B）
  - [ ] 态 C：`setTab('metrics')`；`setChannelFilter('')`；`setModelKeyword('')`；`setSelectedMetricKeys(new Set(visible.map(metricsRowKey)))` —— **替换**而非合并（AC4/AC5）
- [ ] 确认 effect 里**没有任何** `mutate` / `modelRouteMetricsAction` / 写接口调用（AC6）。
- [ ] 确认 `selectedMetrics`（`:612-615`）、批量条（`:904-907`）、行内复选框（`:1074-1082`）**均未改动**——勾选态必须与手点等价（R4/AC7）。
- [ ] eslint 依赖数组告警：`search.channelId` / `search.model` 已通过 `seedKey` 表达，若 lint 要求补齐依赖，用 `seedKey` 之外再补这两项也无害（指纹会挡重复执行）；不要用 `eslint-disable` 绕过。

## 4. 日志页入口

新文件：`web/default/src/features/usage-logs/components/metrics-preselect-action.tsx`

- [ ] 加版权头（复制同目录任一文件的头块）。
- [ ] 组件 `MetricsPreselectAction({ log }: { log: UsageLog })`：
  - [ ] `useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)`；非 SUPER_ADMIN 返回 `null`（范式：`features/channels/index.tsx:43`）。
  - [ ] `const disabled = log.channel <= 0 || !log.model_name`（AC9）。
  - [ ] `useNavigate()` → `navigate({ to: '/model-route', search: { tab: 'metrics', channelId: log.channel, model: log.model_name } })`。
  - [ ] 按钮沿用房内行内动作范式：`Button variant='ghost' size='icon-sm'` + `Tooltip`（参考 `features/keys/components/data-table-row-actions.tsx:194-214`）。图标选一个表达"定位/选中"语义的 lucide 图标，**不要**用 `PowerOff`/`Ban` 之类暗示禁用的图标。
  - [ ] `aria-label` 与 tooltip 用同一条新 key（文案禁含"禁用"）。
  - [ ] `onClick` 加 `e.stopPropagation()`（与日志表其他行内交互一致，见 `common-logs-columns.tsx:396`）。

文件：`web/default/src/features/usage-logs/components/columns/common-logs-columns.tsx`

- [ ] 在 `useCommonLogsColumns` 末尾（`:600` 之后的 push 块之后、`:890` 的 `return columns` 之前）追加 `{ id: 'actions', header: () => t('Actions'), cell: ({ row }) => <MetricsPreselectAction log={row.original} />, meta: { pinned: 'right' as const } }`。
- [ ] **列头方案二选一，实现时决定并在此打勾记录**：
  - [ ] 方案 A（默认，design.md 倾向）：给 `useCommonLogsColumns` 加 `isSuperAdmin: boolean` 参数，仅在 true 时 push 该列；`lib/columns.ts:33-42` 的 `useColumnsByCategory` 透传；调用点 `usage-logs-table.tsx:156` 传入。另两个 columns hook（drawing/task）签名可不动——只在 `useColumnsByCategory` 内部按需传。
  - [ ] 方案 B：列常在，cell 内部返回 null（空列头）。
- [ ] 不碰任何现有列的 `accessorFn` / `size` / `meta` / 排序 / 筛选（AC16）。

文件：`web/default/src/features/usage-logs/components/usage-logs-mobile-card.tsx`

- [ ] `CommonLogsCard`（`:178-240`）显式渲染 `cells.get('actions')`——该组件是白名单取 cell，新列**不会**自动出现（AC14）。位置按视觉密度定：建议首行 `modelCell`/`quotaCell` 那一行的右侧，或 `Details` 之后单独一行。

## 5. i18n

- [ ] 在 `web/default/src/i18n/locales/zh.json` 新增 3 个 key（英文原文为 key，按字母序插入对应位置）：
  - 入口文案（建议 `Select in model route` → 「在模型路由中选中」）
  - `No route metrics found for channel #{{channel}} / {{model}}`
  - `Channel #{{channel}} is disabled; its route metrics are hidden`
- [ ] 复用不新增：`Actions`、`Metrics`、`Model Route`、`{{count}} selected`、`Select row`。
- [ ] 运行 `bun run i18n:sync` 同步 en/fr/ja/ru/vi/zh-TW。
- [ ] 复核入口文案在 7 个 locale 里都不含"禁用/disable"语义。

## 6. 质量门禁

在 `web/default/` 下执行：

- [ ] `bun run typecheck`（`tsgo -b`）— 退出码 0，无新增类型错误。
- [ ] `bun run lint`（`oxlint`）— 改动文件无告警。
- [ ] `node --test src/features/model-route/lib/metrics-reset.test.ts`（或该文件在房内既有的运行方式）— 全绿。
- [ ] `bun run copyright:check` — 新文件版权头合规。
- [ ] `bun run format:check` — 格式合规（或跑 `bun run format`）。
- [ ] 后端未改动 → 不需要 `go build ./...`；若发现自己动了 `.go` 文件，说明偏离设计，停下复核。

## 7. 手工 QA（AC1-AC16）

无法自动化，需在真机（jp279-cpa / sgp）验证。

- [ ] AC1 通用日志有入口；绘图/任务日志无
- [ ] AC2 SUPER_ADMIN 可见；ADMIN 不可见、不存在跳 403 路径
- [ ] AC3 点击后落在「指标」tab，无需手动切
- [ ] AC4 `#16 × grok-4.5` 行勾选，顶部「已选 1 条」
- [ ] AC5 其余行未勾选
- [ ] AC6 **Network 面板确认只有 `GET /api/model_route/metrics`，无 `POST .../metrics/action`**；行状态与跳转前一致 ← 主检查点
- [ ] AC7 顶部批量下拉对这一条执行「人工禁用」，效果与手动勾选后一致
- [ ] AC8 拿一条 `is_model_mapped=true` 的日志点击，勾中的是声明了该请求模型的行
- [ ] AC9 `channel <= 0` 或模型名为空的日志行入口为禁用态
- [ ] AC10 指标中无该记录 → 明确提示，非静默
- [ ] AC11 渠道已禁用 → 提示可看出是"渠道不可用"，区别于 AC10 ← 主检查点
- [ ] AC12 落地页 F5 后勾选可重现
- [ ] AC13 直接访问 `/model-route` 仍默认 policies tab、无预选
- [ ] AC14 移动端卡片可完成跳转 + 勾选
- [ ] AC15 停在落地页等若干次 refetch，提示不重复弹；手动取消勾选后不被打回 ← 主检查点
- [ ] AC16 日志页列/筛选/列可见性无回归；模型路由页批量与行内动作无回归
- [ ] 补充：跳转前在指标页留搜索词 → 跳转后搜索词被清空，目标行可见（R5 第三条）
- [ ] 补充：同一会话内连续点两条不同日志 → 第二次也能正确 seed（验证 `seededRef` 存指纹而非 boolean）

## 8. 回滚点

- [ ] 纯函数层（第 1 步）单独提交：即使后续 UI 回滚，纯函数与测试无副作用可保留。
- [ ] 路由 schema + seed（第 2-3 步）为一个逻辑单元：回滚需同时撤，否则 search 参数无人消费。
- [ ] 日志页入口（第 4 步）可独立回滚：撤掉后模型路由页的 search 参数入口仍无害（无人调用）。
- [ ] i18n（第 5 步）回滚需一并撤 7 个 locale 文件。

## Notes

- 后端零改动是本设计成立的前提，源于 `controller/model_route.go` 已返回 `requested_models`。若实现中发现该字段为空数组导致匹配失败，**不要**在前端补映射解析（PRD R3 明确禁止），而应回到 planning 复核 `buildRequestedModelsByMetricsKey` 的反查覆盖面。
- 已取消的同方向任务 `.trellis/tasks/archive/2026-07/07-31-log-quick-disable-channel-model/` 的 PRD 记录了后端禁用链路的完整事实，若将来要做"日志页直接禁用"，从那份接着做，不要与本任务合并。
- 严禁扩大范围：本任务不引入日志页的任何其它行内操作，不做批量跳转，不改指标页隐藏规则。

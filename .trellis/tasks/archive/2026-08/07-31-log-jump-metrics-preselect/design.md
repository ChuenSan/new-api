# 通用日志跳转模型路由指标并勾选目标行 — Design

## 决策 1：跨页传参 = URL search 参数

模型路由页的选择态和 tab 都是 `ModelRouteAdmin` 组件内的 `useState`（`features/model-route/index.tsx:210`、`:214-216`），路由本身没有 `validateSearch`（`routes/_authenticated/model-route/index.tsx:25-33`）。传参有三种可选形态：

| 方案 | 判断 |
|---|---|
| Zustand store / 全局事件 | 否。刷新即丢，违反 R2「F5 可重现」/ AC12 |
| `sessionStorage` | 否。房内无此范式；且要自己管清理，比 URL 更易泄漏状态 |
| **URL search 参数** | **采用**。房内既有范式（`routes/_authenticated/channels/index.tsx:19-27`、usage-logs 的 `usageLogsSearchSchema`），刷新天然可重现，可分享 |

### schema

给 `/_authenticated/model-route/` 新增 `validateSearch`：

```ts
const modelRouteSearchSchema = z.object({
  tab: z.enum(['policies', 'metrics']).optional().catch(undefined),
  channelId: z.number().optional().catch(undefined),
  model: z.string().optional().catch(''),
})
```

- 三个字段全 optional + `.catch()`：无参访问时全为 `undefined`，组件保持现有默认（`tab` 初值 `'policies'`、selection 为空），满足 R2 / AC13。
- `channelId` 用 `number`：TanStack Router 的 search 序列化支持数字，与 `usageLogsSearchSchema` 的 `page: z.number()` 一致。
- 字段名 `model` 而非 `effectiveModel`：传的是**请求模型**（`log.model_name`），不是 `effective_model`。命名必须诚实，否则后续维护者会误以为可以直接当主键用。
- **`beforeLoad` 的 SUPER_ADMIN 守卫不动**，`validateSearch` 与它并存（房内 `channels/index.tsx` 就是这个组合）。

### 落地后是否清参

**不清**。理由：清参需要一次额外 `navigate({ replace: true })`，会与 seed 的一次性判定纠缠（清参触发 re-render，若 seed 条件写得不严，可能二次 seed）。保留参数则 F5 行为一致（AC12 免费达成），代价只是 URL 上留着两个参数——用户下次手动切 tab 时 `tab` 参数与实际 tab 不同步，但 `tab` 参数只在初始化时被读一次（见决策 2），不会把用户拽回 metrics。

## 决策 2：seed 时机 —— 「参数指纹 + 已消费 ref」

这是本任务的核心风险点。指标数据是异步 `useQuery`（`index.tsx:236-239`），参数到达时 `metricsQuery.data` 可能还是 `undefined`。若写成「`metrics` 变化就按参数覆盖 selection」，用户在落地页的手动改选会被下一次 refetch 打回，提示也会重复弹（违反 R4 / AC6 / AC15）。

### 机制

```ts
const search = Route.useSearch()          // 或 getRouteApi(...).useSearch()
const seededRef = useRef<string | null>(null)

const seedKey =
  search.channelId != null && search.model
    ? `${search.channelId}:${search.model}`
    : null

useEffect(() => {
  if (!seedKey) return
  if (seededRef.current === seedKey) return      // 该指纹已消费，永不重复
  if (!metricsQuery.data) return                 // 数据未到位，等下一次 effect
  seededRef.current = seedKey                    // 先落指纹，再做副作用
  /* ... 匹配 + setSelectedMetricKeys + 反馈 ... */
}, [seedKey, metricsQuery.data])
```

要点：
- **`seededRef` 存指纹字符串，不是 boolean。** 用 boolean 的话，用户从日志页点第二条不同的日志（同一次会话内，组件不卸载，只是 search 变化）就 seed 不了。存指纹后：同参数不重复 seed（挡住 refetch 与 AC15），换参数则重新 seed（第二次点击照常工作）。
- **`seededRef.current = seedKey` 必须在副作用之前赋值。** 保证即使后续逻辑抛错也不会因为 effect 重入而重复弹提示。
- **依赖用 `metricsQuery.data` 而不是过滤后的 `metrics`。** `metrics` 还依赖 `channelFilter` / `modelKeyword`（`index.tsx:583-609`），把它放进依赖会让用户输入搜索词时触发 effect；虽然指纹会挡住，但依赖链更长、更难推理。
- **`tab` 的 seed 用 `useState` 初值，不用 effect**：`useState<'policies'|'metrics'>(search.tab === 'metrics' ? 'metrics' : 'policies')`。只在挂载时读一次，之后 tab 完全由用户控制，URL 上残留的 `tab=metrics` 不会把用户拽回来。
  - 边界：同一次会话内从日志页点第二条日志时组件不重挂载，`useState` 初值不会重跑。因此上面的 seed effect 里 **也要 `setTab('metrics')`** ——即由 effect 负责"每次 seed 都切到 metrics"，`useState` 初值只负责"首次挂载即落在 metrics，避免闪一帧 policies"。两者不冲突：effect 里的 `setTab('metrics')` 对首次挂载是幂等的。

## 决策 3：匹配与三态反馈

### 匹配函数（新增纯函数，可测）

放在 `features/model-route/lib/metrics-reset.ts`（该文件已是本 feature 的纯函数集散地，且已有 `metricsRowKey`，匹配结果要直接喂给它）：

```ts
export function findMetricsRowsForLog(
  rows: readonly ModelRouteMetrics[],
  channelId: number,
  requestedModel: string
): ModelRouteMetrics[] {
  if (!requestedModel) return []
  return rows.filter(
    (row) =>
      row.channel_id === channelId &&
      (row.effective_model === requestedModel ||
        (row.requested_models ?? []).includes(requestedModel))
  )
}
```

- 精确相等，不做大小写折叠：`effective_model` 是路由主键（`model/channel_model_metrics.go`），大小写敏感；模糊匹配会选错行。
- 匹配 `requested_models` 是本设计的关键——它由后端 `buildRequestedModelsByMetricsKey` 反查生成（`controller/model_route.go:310`、`:346`），已经吃掉了 `model_mapping` 的链式解析。前端因此不需要、也禁止复制映射算法（R3）。
- 返回数组而非单个：同渠道下多个 `effective_model` 可能都声明了同一请求模型，R3 要求全选。

### 三态判定：必须用未过滤的原始数据

指标页会隐藏 `channel_exists === false` 或 `channel_status !== ENABLED` 的行（`index.tsx:585-590`），且「已选 N 条」批量条基于过滤后的 `metrics` 计算（`:612-615`、`:904`）。所以判定必须分两层：

```text
raw   = metricsQuery.data.data                       // 未过滤
hits  = findMetricsRowsForLog(raw, channelId, model)

hits.length === 0
  → 态 A「记录不存在」：toast.error，不改 selection
hits 全部会被隐藏（channel_exists === false || channel_status !== ENABLED）
  → 态 B「渠道不可用」：toast.warning（文案区别于 A），不改 selection
否则
  → 态 C「命中」：setSelectedMetricKeys(new Set(visibleHits.map(metricsRowKey)))
     + 清空 channelFilter / modelKeyword
```

- 态 A / 态 B 必须文案可区分（R5 / AC10 / AC11）。只查过滤后的 `metrics` 无法区分二者——两种情况都表现为"找不到"，用户会以为数据丢了。
- 态 C 用 `new Set(...)` **替换**而非合并 selection：跳转语义是"我要处理这一条"，若与上次残留的选择合并，「已选 1 条」会变成「已选 N 条」，违反 AC4 / AC5。
- 态 C 同时把 `channelFilter` / `modelKeyword` 置空（R5 第三条）：否则页面上遗留的搜索词会把目标行筛掉，`selectedMetrics` 算不出来，批量条不出现。这比"提示筛选正在遮挡"体验更好，且不触碰隐藏规则。
- 隐藏判定条件必须与 `index.tsx:585-590` **完全一致**。为避免两处漂移，把该判定抽成一个共用谓词（如 `isMetricsRowVisible(row)`），`metrics` 的 filter 与本处 seed 都调它。这是本任务唯一一处对现有过滤代码的改动，且是等价重构，不改变行为。

## 决策 4：日志页入口形态

### 列

在 `useCommonLogsColumns`（`features/usage-logs/components/columns/common-logs-columns.tsx:294`）末尾追加：

```ts
{
  id: 'actions',
  header: () => t('Actions'),
  cell: ({ row }) => <MetricsPreselectAction log={row.original} />,
  meta: { pinned: 'right' as const },
}
```

- 沿用 `features/keys/components/api-keys-columns.tsx:345-350` 的形态，`meta.pinned` 由 `components/data-table/core/data-table-view.tsx:197` 生效。
- 该列**只在通用日志**加（`useCommonLogsColumns` 本身只喂给 `logCategory === 'common'`，见 `lib/columns.ts:39-42`），绘图/任务日志走各自的 hook，天然满足 R1 / AC1。

### 权限门控

`useCommonLogsColumns(isAdmin)` 现有的 `isAdmin` 是 `role >= ADMIN`（`hooks/use-admin.ts:29-30`），**不能复用**。房内没有 `useIsSuperAdmin` hook，现有 SUPER_ADMIN 判定都是就地读 store（`features/channels/index.tsx:43`、`components/profile-dropdown.tsx:49`）。跟随该范式，在 `MetricsPreselectAction` 组件内读：

```ts
const isSuperAdmin = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
if (!isSuperAdmin) return null
```

放在组件内而不是列工厂里，是为了不改 `useCommonLogsColumns` 的签名（避免波及 `useColumnsByCategory` 及另两个 columns hook）。列本身始终存在但 cell 渲染 null——列头会露出一个空的「操作」列。若要连列头都不出现，则需给列工厂加参数；**倾向后者更干净**，但会动 `lib/columns.ts:33-37` 的签名。二选一在实现时定，判据是：改签名的波及面（3 个 hook + 1 个调用点）是否小于空列头的观感代价。默认选**改签名**（波及面明确且是纯增参）。

### 禁用条件

`log.channel <= 0 || !log.model_name` → 渲染禁用态按钮（R1 / AC9）。用禁用态而非 null，是为了让列宽稳定、且用户能通过 tooltip 知道为什么不可点。

### 传的模型值

主值取 `log.model_name`。`formatModelName(log)`（`features/usage-logs/lib/format.ts:152-173`）已经封装了 `is_model_mapped` + `upstream_model_name` 的解析，复用它拿 `actualModel` 作为**备用候选**，不重复解析 `other`。

URL 只传一个 `model` 参数（保持 schema 简单）。优先级：`log.model_name`。理由：`requested_models` 反查表就是按请求模型建的，直接命中；而 `upstream_model_name` 只在映射发生时有值，且它对应的正是 `effective_model`——匹配函数的第一个条件已经覆盖。因此 `log.model_name` 单值足以覆盖 AC8 的两条路径，**不需要**在 URL 上传第二个模型值。

### 移动端

`CommonLogsCard`（`features/usage-logs/components/usage-logs-mobile-card.tsx:178-240`）是**显式白名单**取 cell（`cells.get('model_name')`、`'channel'` 等），新列不会自动出现。必须显式加一处 `cells.get('actions')`（R1 / AC14）。位置建议：卡片首行右侧或 `Details` 之后，实现时按视觉密度定。

## 数据流

```text
[通用日志] 点击入口 (SUPER_ADMIN, channel>0, model_name 非空)
  → navigate({ to: '/model-route', search: { tab: 'metrics', channelId: log.channel, model: log.model_name } })
  → 路由 beforeLoad: SUPER_ADMIN 守卫（不变）→ validateSearch 解析
  → ModelRouteAdmin 挂载: useState tab 初值 = 'metrics'（不闪 policies）
  → metricsQuery 拉取中 → seed effect 因 !metricsQuery.data 直接 return
  → 数据到位 → effect 再跑:
       seededRef.current !== seedKey ✓
       → seededRef.current = seedKey（先落指纹）
       → hits = findMetricsRowsForLog(raw, channelId, model)
       → 态 A/B: toast, 不改 selection
       → 态 C: setTab('metrics') + 清 channelFilter/modelKeyword
               + setSelectedMetricKeys(new Set(visibleHits.map(metricsRowKey)))
  → 批量条渲染「已选 1 条」（现有逻辑，零改动）
  → 后续 refetch: seedKey 未变 → 指纹命中 → 不再 seed、不再弹提示
  → 用户手动改选/执行批量动作: 现有逻辑，完全不受 seed 影响
```

全程**零写请求**（AC6）：只有 `GET /api/model_route/metrics`（页面本来就会拉）+ 前端 state。

## 文件清单

| 文件 | 改动 |
|---|---|
| `routes/_authenticated/model-route/index.tsx` | 新增 `modelRouteSearchSchema` + `validateSearch`；`beforeLoad` 守卫不动 |
| `features/model-route/index.tsx` | `tab` 初值读 search；新增 `seededRef` + seed effect；`metrics` 的可见性判定抽成共用谓词 |
| `features/model-route/lib/metrics-reset.ts` | 新增 `findMetricsRowsForLog`、`isMetricsRowVisible` 纯函数 |
| `features/model-route/lib/metrics-reset.test.ts` | 补匹配与可见性判定的用例（`node:test` + `node:assert/strict`，同文件现有范式） |
| `features/usage-logs/components/columns/common-logs-columns.tsx` | 追加 `id: 'actions'` 列 |
| `features/usage-logs/components/columns/`（新文件） | `MetricsPreselectAction` 组件（入口按钮 + 权限 + 禁用态 + navigate） |
| `features/usage-logs/lib/columns.ts` | 若采用"改签名"方案：透传 super-admin 标志 |
| `features/usage-logs/components/usage-logs-mobile-card.tsx` | `CommonLogsCard` 显式渲染 `cells.get('actions')` |
| `i18n/locales/*.json` | 新增入口文案 + 两条反馈文案的 key |
| 后端 | **零改动** |

## i18n

已存在可复用：`Actions`、`Metrics`、`Model Route`、`{{count}} selected`（`zh.json:62`）、`Select row`（`:4122`）。

需新增 3 个 key（英文原文作 key，房内单一 `translation` 命名空间）：
- 入口文案：语义必须是「在模型路由中选中」，**禁止**出现「禁用」字样（R1 末条 / Notes 次风险）。建议 `Select in model route`。
- 态 A：`No route metrics found for channel #{{channel}} / {{model}}`
- 态 B：`Channel #{{channel}} is disabled; its route metrics are hidden`

新增后跑 `bun run i18n:sync` 同步全部 7 个 locale。

## 兼容与回滚

- 后端零改动，无 API 契约变化，无迁移。
- 模型路由页：新增的是 search 参数入口与一个 effect；无参访问路径上 `seedKey === null`，effect 首行即 return，行为与现在逐字节一致（AC13）。可见性谓词抽取是等价重构。
- 日志页：新增一列 + 一处移动端渲染，不碰现有列的 `accessorFn` / 排序 / 筛选 / 列可见性 key（`usage-logs-table.tsx:59-64`）。
- 回滚：按文件清单逐项 revert 即可，无数据侧残留。唯一需注意的是 i18n 新 key 会散在 7 个 locale 文件，回滚时一并撤。

## 验证方式

- `tsgo -b` 类型检查、`oxlint` 检查改动文件（房内既有门禁，见归档任务 `07-22-model-route-refresh-force-refetch/implement.md:22-23`）。
- 新增纯函数补 `node:test` 用例：匹配命中 `effective_model`、命中 `requested_models`、多行命中、空模型名、可见性判定三种组合。
- 手工 QA 覆盖 AC1-AC16，重点三条：
  - **AC6**：DevTools Network 确认落地过程只有 `GET /api/model_route/metrics`，无 `POST .../metrics/action`。
  - **AC15**：停在落地页等待若干次 refetch，提示不重复弹；手动取消勾选后不被打回。
  - **AC11**：拿一条渠道已禁用的日志点击，确认提示是"渠道不可用"而非"记录不存在"。

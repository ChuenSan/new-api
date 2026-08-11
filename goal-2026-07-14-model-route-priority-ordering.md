# Goal: 模型路由优先级拖拽排序与原子冲突处理

> **Codex `/goal` 命令**
>
> ```text
> /goal 在 web/default 的模型路由 Policies 中实现按 requested_model 分组的拖拽排序，
> 解决相邻整数无空位时无法插入的问题；保留手动优先级编辑，并将目标值冲突改为后端事务内原子 swap。
> 验证方式：后端优先级事务测试通过，前端拖拽/手动编辑行为测试通过，
> go test ./model ./controller、bun run typecheck、涉及文件 lint、bun run build 全部通过。
> 约束：不改变 modelroute 的状态优先排序、接管阈值和 manual_priority 数值语义；
> 不跨 requested_model 排序；不使用小数优先级、LexoRank 或每次全组重编号；
> 不影响 Metrics、渠道配置、计费、熔断与现有路由学习逻辑。
> 若阻塞：发现跨数据库事务不一致、优先级范围耗尽或现有重复优先级无法确定稳定顺序时，
> 停止扩大修改范围并报告 blocker、数据样例与备选处理方式。
> ```

---

## 1. 目标

模型路由的 `manual_priority` 同时承担两个职责：

1. 数值越大，同状态候选中的排序越靠前。
2. 数值差会参与 takeover 条件计算，人工拉大的差距具有业务含义。

本次改造必须让拖拽成为主要排序方式，同时保留精确数值控制，不能把优先级退化为纯展示序号。

### 成功标准

- 同一 `requested_model` 内可通过拖拽调整渠道顺序。
- `99、98、97` 等密集整数仍可插入新渠道或移动已有渠道。
- 拖拽只调整完成目标顺序所需的最小局部范围。
- 手动输入未占用值时直接更新。
- 手动输入已占用值时在后端事务中原子 swap。
- 支持通过手动输入或“设为第一”快捷操作制造较大的优先级差。
- 并发修改不会静默覆盖，旧快照提交返回 `409` 并刷新数据。
- 路由计划只在事务成功后失效一次，审计只记录一次完整操作。

---

## 2. 非目标

- 不修改 `SortCandidatesForProduction` 的状态优先规则。
- 不修改 `CanTakeOver` 对优先级差的解释。
- 不允许跨 `requested_model` 拖拽。
- 不把 `manual_priority` 改成浮点数或字符串排序键。
- 不增加数据库唯一索引强制 `(requested_model, manual_priority)` 唯一。
- 不在本次改造中新增 policy 删除能力。
- 不改变 configured/mapped policy 的创建、同步和清理机制。

---

## 3. 领域规则

### 3.1 排序作用域

优先级只在相同 `requested_model` 内比较。前端必须按模型分组，后端必须拒绝包含其他模型 policy 的重排请求。

### 3.2 当前稳定顺序

每个模型组的当前顺序定义为：

```text
manual_priority DESC → channel_id ASC
```

`channel_id ASC` 只用于处理历史重复优先级时的确定性展示，不代表重复优先级是推荐状态。

### 3.3 拖拽与手动编辑的区别

| 操作 | 语义 | 冲突处理 |
|---|---|---|
| 拖拽 | 指定最终相对顺序 | 中值插入或局部腾挪 |
| 手动输入 | 指定精确优先级数值 | 目标被占用时 swap |
| 设为第一 | 设置为组内最高值并保留指定领先幅度 | 计算新值后走手动更新语义 |

### 3.4 “第一”的准确含义

“设为第一”表示配置优先级在该模型组内最高。生产路由仍先比较健康状态，再比较 `manual_priority`，因此异常渠道不会因为数值最高而越过健康状态规则。

---

## 4. 拖拽优先级算法

### 4.1 基本流程

1. 从当前模型组中暂时移除被拖拽渠道，其旧优先级立即视为空闲值。
2. 根据拖拽后的上下邻居确定目标区间。
3. 上下优先级之间存在整数空间时，直接取中值。
4. 没有整数空间时，分别计算向上和向下腾挪到最近空闲整数所需修改的渠道数。
5. 选择修改数量更少的一侧；数量相同时优先向下腾挪，尽量保留上方人工设置的高优先级和领先差距。
6. 在一个数据库事务内校验快照并写入全部变更。

### 4.2 有空位示例

```text
拖拽前：
A  100
B   90

C 插入 A、B 之间：
A  100
C   95
B   90
```

只新增或修改 C，A、B 不变。

### 4.3 密集整数示例

```text
拖拽前：
A  99
B  98
D  97

新渠道 C 插入 A、B 之间。
```

候选方案：

```text
向上腾挪：A 99→100，修改 1 个已有渠道
向下腾挪：B 98→97、D 97→96，修改 2 个已有渠道
```

选择向上腾挪，事务结果为：

```text
A  100
C   99
B   98
D   97
```

### 4.4 边界插入

- 插入第一位：优先尝试 `currentMax + 1`。
- 插入最后一位：优先尝试 `currentMin - 1`。
- 达到上界或下界时只能向另一侧局部腾挪。
- 两侧均无空间时返回 `409 priority_range_exhausted`，不得溢出或静默全组重编号。

### 4.5 优先级范围

第一版沿用当前前端范围：

```text
-999 <= manual_priority <= 9999
```

后端新增同样的强制校验，避免仅依赖前端 `NumericSpinnerInput`。

---

## 5. 手动编辑与原子 swap

### 5.1 未冲突

```text
A  100
B   90

B 手动改为 500：
B  500
A  100
```

仅更新 B。

### 5.2 单一冲突

```text
A  100
B   90

B 手动改为 100：
B  100
A   90
```

后端读取 B 的旧值 `90`，在同一事务中把 A 改为 `90`、B 改为 `100`。

### 5.3 多重历史冲突

如果目标优先级已经被多个渠道占用，不能任意选择一个渠道交换。接口返回 `409 duplicate_priority_conflict`，前端刷新并提示先通过拖拽整理该模型组。

### 5.4 设为第一

优先级单元格保留数字输入，并增加“设为第一”快捷操作：

- 默认领先幅度：`100`。
- 建议值：`min(9999, currentMax + 100)`。
- 用户可在提交前继续手动修改建议值。
- 如果当前最高值已接近上限，不自动压缩其他渠道，提示用户输入可用值或先调整现有优先级。

---

## 6. API 设计

### 6.1 拖拽重排

```http
PUT /api/model_route/policies/reorder
```

请求：

```json
{
  "requested_model": "gpt-4.5",
  "ordered_channel_ids": [12, 19, 8, 3],
  "expected": [
    { "channel_id": 12, "manual_priority": 99 },
    { "channel_id": 8, "manual_priority": 98 },
    { "channel_id": 3, "manual_priority": 97 },
    { "channel_id": 19, "manual_priority": 0 }
  ]
}
```

要求：

- `ordered_channel_ids` 必须是该模型组完整 policy 集合的排列。
- `expected` 必须覆盖同一集合，用于乐观并发校验。
- 后端根据最终顺序计算最小局部优先级变更，不能信任前端提交的新优先级。
- 返回该模型组的最新完整 policy 列表和实际变更项。

成功响应：

```json
{
  "success": true,
  "message": "",
  "data": {
    "requested_model": "gpt-4.5",
    "changed": [
      { "channel_id": 12, "manual_priority": 100 },
      { "channel_id": 19, "manual_priority": 99 }
    ],
    "policies": []
  }
}
```

### 6.2 手动优先级更新

继续使用：

```http
PUT /api/model_route/policies/priority
```

请求扩展为：

```json
{
  "channel_id": 8,
  "requested_model": "gpt-4.5",
  "manual_priority": 100,
  "expected_manual_priority": 90,
  "conflict_strategy": "swap"
}
```

响应返回所有实际变化的 policy，前端不再自行发送第二次 swap 请求。

### 6.3 错误契约

| HTTP | Code | 含义 |
|---|---|---|
| 400 | `invalid_priority` | 超出范围或请求结构错误 |
| 400 | `invalid_order` | ID 缺失、重复或包含其他模型 policy |
| 404 | `policy_not_found` | policy 已被删除或同步移除 |
| 409 | `stale_policy_snapshot` | 提交期间数据已被其他管理员修改 |
| 409 | `duplicate_priority_conflict` | 手动目标值存在多个历史占用者 |
| 409 | `priority_range_exhausted` | 上下界均无可用腾挪空间 |

---

## 7. 后端事务与并发

### 7.1 事务要求

- 使用 GORM `DB.Transaction`，兼容 SQLite、MySQL 和 PostgreSQL。
- 不使用数据库方言专属锁语法。
- 每个更新必须带原优先级条件：

```text
WHERE channel_id = ? AND requested_model = ? AND manual_priority = ?
```

- 任意 `RowsAffected != 1` 时返回 stale 错误并回滚全部修改。
- 事务成功后再调用 `modelroute.InvalidateRoutePlan(requestedModel)`。
- 一个用户操作只生成一条管理审计记录，审计内容包含旧顺序、新顺序和实际修改项。

### 7.2 时间戳

不使用 `updated_at` 作为唯一并发版本，因为当前时间戳精度不足以可靠区分同一秒内的连续修改。并发校验以客户端读取到的优先级快照为准。

---

## 8. 前端设计

### 8.1 页面结构

Policies 从全局平铺表格改为按 `requested_model` 分组：

```text
gpt-4.5 · 4 routes
┌────┬──────────────┬──────────────┬────────┬────────┬────────┐
│    │ Channel      │ Effective    │ Priority│ Enabled│ Source │
├────┼──────────────┼──────────────┼────────┼────────┼────────┤
│ ☰  │ A (#12)      │ gpt-4.5      │ 100    │ Yes    │ ...    │
│ ☰  │ C (#19)      │ gpt-4.5      │ 99     │ Yes    │ ...    │
│ ☰  │ B (#8)       │ gpt-4.5      │ 98     │ Yes    │ ...    │
└────┴──────────────┴──────────────┴────────┴────────┴────────┘
```

### 8.2 拖拽交互

- 使用独立拖拽手柄，不把整行设为拖拽触发区。
- 限制为垂直方向和当前模型组。
- 支持鼠标、触摸和键盘操作。
- 键盘行为：Space/Enter 开始，方向键移动，Escape 取消。
- 拖拽结束后先乐观更新当前组；接口失败时恢复旧顺序。
- 只禁用正在提交的模型组，不再全局禁用所有 priority 输入。

### 8.3 筛选约束

- 模型搜索可以筛选模型组，但命中的模型组必须展示完整成员。
- 渠道名称/ID 筛选会隐藏组内成员，因此筛选生效时禁用拖拽并展示原因。
- 清除渠道筛选后恢复拖拽。

### 8.4 组件选择

- 继续使用项目现有 Base UI/shadcn 表单、按钮、Tooltip 和表格样式。
- 使用 dnd-kit 提供 sortable、拖拽手柄和键盘传感器能力。
- 新增依赖前锁定与 React 19 兼容的当前稳定版本，并通过 Context7 官方文档确认 API。

### 8.5 缓存更新

- API 成功后使用响应中的完整模型组更新 `model-route-policies` 查询缓存。
- 后台再执行一次 invalidate/refetch 校验权威状态。
- `409` 时放弃乐观状态、刷新数据并提示已被其他操作修改。

---

## 9. 文件计划

### 后端

| 文件 | 变更 |
|---|---|
| `model/channel_model_policy.go` | 增加原子 swap、重排事务、快照校验和局部腾挪逻辑 |
| `model/channel_model_route_dao_test.go` | 增加优先级事务与边界回归测试 |
| `controller/model_route.go` | 扩展手动更新请求，增加 reorder handler 和错误映射 |
| `controller/model_route_test.go` | 增加 API 校验、409 和响应契约测试；文件不存在时新增 |
| `router/api-router.go` | 注册 `PUT /model_route/policies/reorder` |

### 前端

| 文件 | 变更 |
|---|---|
| `web/default/src/features/model-route/types.ts` | 增加 reorder、expected snapshot 和响应类型 |
| `web/default/src/features/model-route/api.ts` | 增加 reorder API，扩展 priority API |
| `web/default/src/features/model-route/index.tsx` | 改为模型分组，移除前端双请求 swap |
| `web/default/src/features/model-route/components/policy-sortable-group.tsx` | 新增模型组和可拖拽行组件 |
| `web/default/src/features/model-route/lib/policy-order.ts` | 前端乐观排序和分组纯逻辑 |
| `web/default/src/features/model-route/lib/policy-order.test.ts` | 验证分组、移动和回滚所需的用户可见行为 |
| `web/default/src/i18n/locales/*.json` | 增加拖拽、并发冲突、范围耗尽等文案并同步所有支持语言 |
| `web/default/package.json` | 增加 dnd-kit 依赖 |
| `bun.lock` | 更新依赖锁文件 |

若 `index.tsx` 拆分后仍超过项目组件复杂度要求，优先继续拆出 Policies tab，不改动 Metrics tab 的业务逻辑。

---

## 10. 测试计划

### 10.1 后端模型测试

- 有空位插入：`100、90` 中插入得到 `100、95、90`。
- 密集插入：`99、98、97` 中插入得到 `100、99、98、97`。
- 下移成本更低时向下腾挪。
- 上下成本相同时优先向下，保留上方优先级。
- 被拖拽渠道旧优先级参与空位计算。
- 插入第一位和最后一位。
- 上界、下界和双侧范围耗尽。
- 手动未冲突更新。
- 手动单一冲突原子 swap。
- 多重历史冲突返回明确错误。
- expected 快照过期时事务完整回滚。
- 不同 `requested_model` 互不影响。
- 事务失败后路由数据保持原值。

### 10.2 Controller 测试

- reorder 缺少模型、ID 重复、集合不完整返回 400。
- 包含其他模型渠道返回 400。
- stale snapshot 返回 409。
- 成功响应包含权威 policies 和 changed。
- priority swap 响应包含交换双方。

### 10.3 前端测试

- 模型 policy 正确分组和排序。
- 拖拽只改变当前模型组。
- 渠道筛选生效时拖拽禁用。
- 乐观更新失败后恢复旧顺序。
- 409 后刷新并展示冲突提示。
- 手动编辑只发送一次请求，不再执行前端双请求 swap。
- “设为第一”生成可编辑建议值且不超过上限。

测试只保护用户行为、API 契约和事务不变量，不断言拖拽库内部实现。

---

## 11. 实施顺序

### Phase 1：纯后端优先级算法

1. 定义请求、结果和领域错误。
2. 实现中值插入、双向成本计算和边界处理。
3. 实现原子 swap。
4. 完成 model 层表驱动测试。

验收：

```bash
go test ./model -run 'Test.*ModelPolicy.*(Priority|Reorder|Swap)' -count=1
```

### Phase 2：后端 API

1. 扩展 priority handler。
2. 增加 reorder handler 和路由。
3. 接入事务成功后的缓存失效与审计。
4. 完成 Controller 契约测试。

验收：

```bash
go test ./controller ./model -count=1
go test ./modelroute -count=1
```

### Phase 3：前端分组和拖拽

1. 增加 dnd-kit 依赖。
2. 拆分 Policies 模型组组件。
3. 实现拖拽、键盘操作和组级 busy 状态。
4. 接入乐观缓存与失败回滚。

验收：

```bash
cd web/default
bun run typecheck
bun run lint -- src/features/model-route
```

### Phase 4：手动编辑、快捷置顶与 i18n

1. 删除前端双请求 swap。
2. 接入后端 changed 响应。
3. 增加“设为第一”建议值操作。
4. 使用项目 i18n 流程同步所有语言。

验收：

```bash
cd web/default
bun run i18n:sync
bun run typecheck
bun run build
```

### Phase 5：完整回归

```bash
go test ./model ./controller ./modelroute -count=1
go test ./... -count=1
cd web/default
bun run typecheck
bun run lint
bun run format:check
bun run build
```

---

## 12. 验收场景

### 场景 A：密集优先级插入

给定：

```text
A 99
B 98
D 97
```

当 C 被拖入 A、B 之间，最终必须为：

```text
A 100
C 99
B 98
D 97
```

页面刷新后顺序保持一致。

### 场景 B：保留人工大间距

给定：

```text
A 9000
B 100
C 99
D 98
```

在 B、C、D 的局部拖拽不得重置 A 的 `9000`，也不得把整组改写成固定步长序列。

### 场景 C：手动冲突

给定 A=100、B=90，当 B 手动输入 100，只发送一次 API 请求，成功后 A=90、B=100；任意数据库写入失败时两者都保持原值。

### 场景 D：并发修改

管理员甲读取 A=100、B=90；管理员乙先把 B 改成 80。管理员甲继续提交旧快照时必须收到 409，不能覆盖管理员乙的修改。

### 场景 E：筛选安全

渠道筛选隐藏同组部分 policy 时，拖拽手柄不可用；清除筛选后恢复。

---

## 13. 完成定义

- 所有成功标准与验收场景通过。
- 前端不再存在两次请求模拟 swap 的代码。
- 后端 mutation 均具备范围校验、快照校验和事务回滚。
- SQLite、MySQL、PostgreSQL 路径不包含方言专属 SQL。
- 新增文案完成项目支持语言翻译。
- TypeScript、lint、格式检查、前端构建和相关 Go 测试全部通过。
- 未修改 Metrics tab、路由状态排序、takeover 计算和其他无关功能。


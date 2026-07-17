# Design — 令牌级可用模式

## 1. 一句话

令牌开关打开后：客户端只发一次请求，New API 在内部重复发起全新的同请求执行轮次；失败不回客户端，每轮重新走现有选路，直到某一轮成功。

## 2. 边界

| 在范围内 | 不在范围内 |
|----------|------------|
| 令牌字段 + 鉴权 context | 新选路/评分算法 |
| `Relay` 主路径内部重放环 | 余额/预扣费语义改造 |
| 复用 `CacheGetRandomSatisfiedChannel` | 伪装 200 |
| 管理端令牌开关 UI | 多实例重放协调 |
| 现有熔断/auto-ban | 请求内容分类 |

Task relay（`RelayTask`）本迭代可不改；与聊天主路径独立。Realtime 已 upgrade 的 WS 路径保持现网。

## 3. 数据与契约

### 3.1 Token 字段

对齐 `CrossGroupRetry`：

| 层 | 内容 |
|----|------|
| DB / model | `Token.AvailabilityMode bool` `json:"availability_mode" gorm:"default:false"` |
| Update 白名单 | `model/token.go` `Update()` 字段列表增加 `availability_mode` |
| Create/Update API | `controller/token.go` 读写该字段 |
| Redis 缓存 | `Token` 整对象 HSet，加字段后自动进缓存；无单独 field 常量也可 |
| Context | `ContextKeyTokenAvailabilityMode`；`middleware/auth.go` `TokenAuth` 在已有 `CrossGroupRetry` 旁 `SetContextKey` |
| 前端 | `web/default` keys：types / form / mutate-drawer / 可选 columns 徽标；文案「可用模式」 |

默认 `false`；未迁移列由 GORM AutoMigrate 加列。

### 3.2 双层运行时语义（核心）

```
if !availabilityMode:
  完整保持现网循环与返回行为
else:
  while 请求仍可继续:
    创建一轮新的选路状态
    在本轮内按现有算法选择/切换候选渠道
    成功：return
    本轮失败或暂时无候选：按退避策略等待，再开启下一轮
```

**「当作客户端重新发送请求」的含义：**

- 请求体、模型名、用户与令牌不变
- 每轮的**选路入口仍是** `CacheGetRandomSatisfiedChannel`，不另写排序
- 单轮可维护已尝试渠道集合，避免一轮内反复抽中同一失败渠道
- 开启下一轮时必须重置轮次选路状态，允许再次选中前序轮次失败过的渠道
- **不**用全局 `RetryTimes` 截断
- 进入内部转发后，不用 `ShouldRetryByStatusCode` 决定是否进入下一轮；没有成功就继续
- `getChannel` 暂时无候选只结束本轮，不结束整个外层请求

### 3.3 硬边界（仅物理/现网不变量）

| 条件 | 行为 |
|------|------|
| `HasSendResponse()` | 停止重放（流式已写出） |
| `specific_channel_id` | 不跨渠（与现网一致） |
| 令牌鉴权未通过 | 无法识别令牌开关，按现有中间件直接返回 |
| 请求解析/校验未通过 | 尚未形成可重放的有效内部请求，按现有行为返回 |
| 渠道亲和强制 skip | 停止（`ShouldSkipRetryAfterChannelAffinityFailure`） |
| `getChannel` 无候选 / err | 结束本轮，退避后重开新轮 |
| 客户端断开 / request context 取消 | 停止内部重放，不再发起新上游请求 |

### 3.4 入口副作用边界

现有顺序是 `TokenAuth` → `ModelRequestRateLimit` → `Relay` 内请求解析/预扣费 → 现有重试循环。只把“选路 + 上游转发”放入无限重试轮次：鉴权、限流、请求解析和预扣费仅执行一次。客户端的一次请求只产生一次入口副作用。

## 4. 选路复用细节

### 4.1 model-priority（主路径）

已有 try-list / overflow + `usedChannelIDSet(use_channel)`。可用模式下该集合必须限定为单轮状态；链耗尽只结束本轮。下一轮清空后重新走同一链路。**零排序改动。**

### 4.2 channel-priority

完整保留现有 `retry` 映射优先级档与同档权重随机逻辑。可用模式不增加已用渠道过滤、不重算权重；现有有限内层结束后，外层新轮次把 `retry` 重置为 0，再按同一算法选择。

### 4.3 auto 分组 + CrossGroupRetry

保持现有 `cacheGetChannelPriorityChannel` 跨组逻辑；每轮复用同一组顺序，不改选路算法。

## 5. Relay 双层循环改造

`controller/relay.go` `Relay` 中，可用模式不能只放宽现有单层 `for` 上限，必须显式区分跨轮循环与单轮选路状态：

```go
if !availabilityMode {
    runCurrentRelayLoop()
    return
}

for requestCanContinue() {
    resetRoundSelectionState()
    outcome := runOneRoundWithCurrentSelector()
    if outcome.Success { return }
    if outcome.HasSentResponse { break }
    if outcome.NoCandidate { waitForContextOrRetryDelay() }
}
```

现有 `use_channel` 同时承担日志与 model-route 排除输入，不能作为永久跨轮集合直接累加。实现时应拆出单轮选择集合；如日志仍需展示全部尝试，则使用独立累计记录。

正常上游失败本身已有网络往返耗时，下一轮直接开始；仅在未发起上游请求且当前无候选时执行可取消的短暂等待，避免 CPU 忙循环。

## 6. 兼容与回滚

| 项 | 说明 |
|----|------|
| 默认关 | 全站行为不变 |
| DB | 仅加布尔列，可空默认 false |
| 回滚 | 关开关或回退二进制；列可保留 |
| 计费 | 预扣费仍在循环外一次；成功路径现网结算；失败退款现网 defer |

## 7. 风险

| 风险 | 缓解 |
|------|------|
| 无次数上限导致长连接与资源占用 | 尊重请求取消；轮次间退避；补充轮次/耗时日志 |
| 暂时无渠道导致 CPU 忙循环 | 无候选同样进入退避，不立即自旋 |
| channel-priority 卡在最低优先级 | 内层仍由现有 `RetryTimes` 结束；外层新轮次重置 retry |
| 流式半截 | `HasSendResponse` 硬停 |
| 前端 classic 主题 | 优先 `web/default`；classic 若仍用旧表可二期 |

## 8. 测试要点

- 关开关：`RetryTimes=0` 行为与现网一致
- 开开关 + model-priority：第一条失败，第二条成功，客户端只见成功
- 开开关 + 第一轮全失败、后续轮恢复：不返回中间错误，最终返回成功
- 开开关 + 同一渠道先失败后恢复：跨轮允许重选该渠道并成功
- 开开关 + 暂时无候选：退避而非忙循环或立即失败
- 开开关 + 流式已写出：不换渠
- Token CRUD 读写 `availability_mode`
- 单测：`shouldRetry` 分支 / 选渠跳过 used（可放 `service` 或 `controller` 包测）

## 9. 文件清单（预计）

| 文件 | 变更 |
|------|------|
| `model/token.go` | 字段 + Update 列表 |
| `constant/context_key.go` | context key |
| `middleware/auth.go` | SetContextKey |
| `controller/token.go` | create/update |
| `controller/relay.go` | 循环 + shouldRetry 分支 + HasSendResponse |
| `service/channel_select.go` | 仅提供轮次状态重置，不改 channel-priority 排序/权重 |
| `web/default/src/features/keys/*` | types/form/drawer |
| 测试 | token + relay/shouldRetry + 可选 channel_select |

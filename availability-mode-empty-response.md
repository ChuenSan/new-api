# 可用模式 · 空返回拦截与内部重试

## 一句话

可用模式开启时，流式请求先在网关内缓冲；上游必须给出「**终结事件 + 非空内容**」才算成功，才把内容一次性交给客户端。否则整次输出作废、计入「渠道 × 模型」路由的连续失败计数、换路内部重试。路由全挂则无限等待重放，**永不主动报错**。

## 现象与证据

```
logs 63964~63968   2026-09-21 22:37~22:41 (UTC+8)
channel 164   gpt-6-astra   /v1/responses   流式
prompt_tokens=0   completion_tokens=0   quota=0
frt      = 21.5 / 28.4 / 19.8 / 38.1 / 24.1 秒
use_time = 52   / 55   / 66   / 58   / 46   秒
stream_status = {"end_reason":"eof","status":"ok"}
admin_info.use_channel = ["164"]        ← 一次重试都没有
content = 上游没有返回计费信息，无法扣费（可能是上游超时）
```

关键推断：`prompt_tokens=0` ⟹ **`response.completed` 从未到达**（到达必带 upstream usage）。真相是上游在终结事件之前 eof 断流。
同渠道 3 小时内同形态 8 次（另有 13 次 `client_gone`）。

## 根因（三条，缺一不可）

1. **流式 handler 不认识失败事件**
   `relay/channel/openai/relay_responses.go:92-130` 只处理 `response.completed` / `response.output_text.delta` / `output_item.done`，**忽略 `response.failed` 与 `error`**，最后无条件 `return usage, nil`。
   反面参照（已经写对的）：`relay/channel/openai/chat_via_responses.go:224-235`。

2. **eof 被当成「正常结束」**
   `relay/common/stream_status.go:92-95` `IsNormalEnd()` 把 `eof` / `done` / `handler_stop` 都算正常；
   `controller/relay.go:756-763 relayProductionCompletedNormally` 只要无错 + 正常结束即成功 ⟹ **HTTP 200 + 记成功 + 不重试**。

3. **「已发给客户端」的判定发生在上游首字节，而不是客户端可见内容**
   `relay/helper/stream_scanner.go:245-246` 第一条上游 `data:` 就 `SetFirstResponseTime()`；
   `relay/common/relay_info.go:678 HasSendResponse()` 只看这个时间戳。
   于是 `controller/relay.go:288` 的 `if availabilityMode && streamInterrupted { break }` 与 `:296` 的轮次门控都以为「已经写出去了」，**重试被物理性封锁**。

## 已确认决策

| # | 决策点 | 结论 |
|---|--------|------|
| 1 | 放行时机 | **整条回复成功才放行**（可用模式下流式请求全部零流式） |
| 2 | 失败判据 | **终结事件 + 非空内容，缺一即失败** |
| 3 | 达到阈值（默认 3）之后 | **维持现状：永不主动报错**，含全挂 |
| 4 | 生效范围 | **所有格式的流式请求**（非流式不动） |
| 5 | 计数器 | 复用模型路由页「触发熔断前的 429 次数」（默认 3，可单路由覆盖） |
| 6 | 重试语义 | 复用现有实现，不新造 |

## 判据表

| 上游结果 | 判定 | 后果 |
|---|---|---|
| 终结事件 ✔ + 内容 ✔ | 成功 | commit 缓冲区 → 客户端一次性收到 |
| 终结事件 ✔ + 内容 ✘ | 失败 | 丢弃 → 重试（「空内容也不返回」） |
| 无终结事件（含 eof 断流） | 失败 | 丢弃 → 重试 |
| 流内 `error` / `response.failed` | 失败 | 丢弃 → 重试 |
| timeout / scanner_error / client_gone | 失败 | 丢弃 → 重试（现状即如此） |

终结事件映射：

| 格式 | 终结事件 | 现状 |
|---|---|---|
| Responses | `response.completed` | handler 未标记 |
| chat/completions | `[DONE]` | `relay/helper/stream_scanner.go:258` 已处理 |
| Claude | `message_stop` | `relay/channel/claude/relay-claude.go:509` 目前只 `return nil` |
| Gemini | 无 | 只按内容判定（兜底） |

内容判定优先用格式自带语义，不新增逐 delta 打点：
Responses 用 `response.completed` 的 `output` 非空 / `usage.output_tokens>0`；chat 用累积的 delta 与工具调用；Claude 用 `claudeInfo.ResponseText`。

## 改动点

### A. 缓冲层（新增）

`relay/helper/buffer_writer.go` —— 装饰 `gin.ResponseWriter`：嵌入原接口 + 覆写 `Write / WriteString / Flush / WriteHeaderNow / Status / Size / Written`，透传 `Unwrap / Hijack / CloseNotify`。

`controller/relay.go` `Relay` 中，条件 `可用模式 && relayInfo.IsStream && 非 realtime` 时装到 `c.Writer`。

- handler 的写入方式**完全不用改**（都走 `c.Render` / `FlushWriter`，见 `relay/helper/common.go:17,87`）
- 未 commit 前所有字节只进内存缓冲区
- commit 后关闭缓冲，之后字节直通（已发出不可撤销）
- 心跳 `: PING`（`relay/helper/common.go:110`）**直通不缓冲** → 保活客户端连接；代价是 HTTP 200 头提前发出（因决策 3，无副作用）
- 上限 `constant.StreamScannerMaxBufferMB`（`common/init.go:141`，默认 128）；超限降级为直通

### B. 状态标记

`relay/common/stream_status.go` `StreamStatus` 增两个字段：

- `SawTerminator bool` —— 终结事件到达
- `ContentCount int` —— 可见内容产出计数

`relay/common/relay_info.go:678 HasSendResponse()` 语义改为「**是否已向客户端 commit 过**」：
缓冲路径由 buffer writer 在 commit / discard 时置位；非缓冲路径（非可用模式）天然为 true，**行为不变**。

### C. 各 handler 补齐

| 文件 | 变更 |
|---|---|
| `relay/channel/openai/relay_responses.go:92` | `response.completed` → `sr.Done()`；新增 `response.failed` / `error` → `sr.Stop(err)`；内容判定 |
| `relay/channel/openai/relay-openai.go` | `responseTextBuilder` / `thinkingContent` / 工具调用非空即计内容 |
| `relay/channel/claude/relay-claude.go:509` | `message_stop` → `sr.Done()`；内容判定 |
| `relay/channel/gemini/relay-gemini.go` | 只计内容，不加终结事件要求 |
| `relay/helper/stream_scanner.go:258` | `[DONE]` 分支置 `SawTerminator` |

### D. 判据接线与熔断联动

- `controller/relay.go:756 relayProductionCompletedNormally` → 新判据：
  `正常结束 && 无错 && ContentCount>0 && (SawTerminator || 格式无终结事件)`
- `controller/relay.go:282` 的 `streamInterrupted` 随 `HasSendResponse()` 新语义自动变正确
  → `:288` 的 break 不再触发、`:296` 的轮次门控自动放行
- 失败路径 `:767 notifyModelRouteProduction(..., success=false, ...)`
  → `modelroute.ApplyProductionOutcome` 走失败分支
  → `applyTransitionLocked`（`modelroute/route_state.go:171-320`）`ConsecutiveFailures++`
  → 达 `GetRateLimitCircuitBreakerThreshold(m)`（`modelroute/rate_limit_threshold.go`，默认 3）
  → `openCircuit`（`modelroute/route_state.go:299`，RouteOpen + 冷却 + RoleNone）
  → `modelroute/candidate_chain.go:203` 把该路由剔出选路
- **这一步就是「计入重试次数」的落地位置**

### E. commit / discard

- 成功：缓冲区一次性写进真实 writer，`HasSendResponse` 置 true
- 失败：丢弃缓冲区（真实 writer 上只有心跳），`HasSendResponse` 保持 false → 内层 `shouldRetry` 与外层轮次门控照常工作
- `service/channel_select.go:52 ResetRound` 本就会清空 `use_channel`，跨轮可重选同一渠道，符合可用模式原语义

## 不做

- 非流式不动（保持现状）
- 不新增设置项、不改前端 / i18n（复用现有熔断阈值）
- 不改非可用模式下任何格式的行为
- 不实现「全挂报错」（决策 3：永不报错）

## 风险

1. 可用模式下所有流式请求零流式 —— 已确认接受
2. **上游不发终结事件的渠道会被全量误杀** —— 需观察期；channel 164 正常请求 `prompt_tokens>0`（近 3h 94/115）说明正常情况下 completed 是发的
3. 一次尝试 46~66s，多路由重试时客户端长时间空等 —— 决策 3 的必然结果
4. 大响应缓冲占用内存 —— 上限降级兜底

## 验证要点

- 复现点：`/v1/responses` 上游 eof 断流 → 不再记成功、不再返回空、进入重试
- 熔断：同一「渠道 × 模型」连续 3 次失败 → 路由变 OPEN 并被选路剔除
- 成功路径：正常 completed + 内容 → 客户端一次性收到完整响应
- 合规空回复（completed + 零输出）→ 判失败重试
- 非可用模式：全格式行为与现网逐字节一致
- Gemini 无终结事件 → 只按内容判定，不误杀
- 心跳：缓冲期间客户端仍收得到 `: PING`

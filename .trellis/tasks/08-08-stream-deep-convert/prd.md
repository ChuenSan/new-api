# PRD: 流式响应转换增强 — Anthropic Messages → OpenAI Chat 深度流式重组

## Goal

将 CC Switch 项目中成熟的生产级流式转换设计移植到 new-api，修复当前 `StreamResponseOpenAI2Claude` 在与严格校验 Anthropic SSE 协议的客户端（如 Claude Code）交互时的 8 个缺陷。

## Confirmed Defects

| ID | 描述 |
|----|------|
| D1 | `message_delta` 在 finish_reason chunk 时立即发送，usage 为 nil 时静默丢失 |
| D2 | 多 finish_reason chunk 无去重 → 重复 message_delta |
| D3 | `buildClaudeUsageFromOpenAIUsage` 不扣除 cache token |
| D4 | 无流内错误检测 |
| D5 | tool block 无 pending_args 缓冲 |
| D6 | 最后一个 chunk 漏失 + HandleFinalResponse 二次处理 |
| D7 | 无 finish_reason 正常 EOF 缺少 `content_block_stop` |
| D8 | `handleClaudeFormat` 用 nil usage 覆盖旧缓存 |

## Requirements

### R0: Event 责任单点化

**唯一规则**：`message_delta` + `message_stop` 仅由 `HandleFinalResponse` 发送。converter 的任何路径都只负责 block-level events，**绝不**输出 terminal events。

`content_block_stop` 幂等闭环：converter finish 路径执行 `stopOpenBlocksAndAdvance()` → 设 `LastMessagesType = "none"`。`HandleFinalResponse` 始终调用 `StopOpenBlocksForFinalize`（幂等），仅当 `LastMessagesType != "none"` 时才补关。

所有带 `finish_reason` 的 finish 路径（包括首 chunk finish、普通 finish、带 usage 的 finish chunk）都只关闭 block、缓存 finish reason/usage，**绝不**发送 `message_delta` 或 `message_stop`；仅有 usage 且没有 `finish_reason` 的 chunk 只缓存 usage，不改变 block 状态。最终 terminal events 仅由 `HandleFinalResponse` 发送。

### R1: message_delta 延迟 + 最后 chunk flush

`OaiStreamHandler` 在 scanner 返回后 flush `lastStreamData`（条件含 `ClaudeConvertInfo != nil && !StreamError`）。`handleLastResponse` 加 `lastStreamData != ""` guard。`containStreamUsage` 以 `streamResponse.Usage != nil` 判断真实 usage，不能用 token 数值是否为 0 判断。

**Flush 错误处理**：

| 场景 | 阶段 | 行为 |
|------|------|------|
| 上游错误 | Callback | `StreamError=true` + `event:error` + `sr.Stop()` → flush 跳过 |
| 上游错误 | Flush | `StreamError=true` + `event:error` + return error |
| JSON 解析失败 | Flush | 记录日志，继续 finalization |

### R2: FinishReason first-wins

首次非空 finish_reason 固定。后续 finish_reason chunk 只更新 usage。

### R3: cache token 扣除

匹配 `relay-claude.go:595` 的 max 语义：`cacheCreation = max(CachedCreationTokens, 5m+1h)`。`InputTokens`、`CacheCreationInputTokens`、分桶字段使用同一组解析结果。

解析顺序固定：`cachedRead = PromptTokensDetails.CachedTokens`，为 0 时回退 `PromptCacheHitTokens`；`cacheCreation = max(PromptTokensDetails.CachedCreationTokens, ClaudeCacheCreation5mTokens + ClaudeCacheCreation1hTokens)`。最终 `InputTokens = max(0, PromptTokens - cachedRead - cacheCreation)`，`CacheCreationInputTokens = cacheCreation`，5m/1h 分桶由同一个 `cacheCreation` 总量归一化，禁止分别扣除后再次相加。

### R4: 流内错误检测

`handleClaudeFormat` 入口 → `DetectUpstreamError` → hit：
- `if !StreamError` 才 emit `event:error`（**一次性**，防重复）
- `StreamError = true`
- return error

### R5: tool block pending_args 缓冲

`ToolBlockState` with `ID`, `Name`, `Started`, `PendingArgs`。
- 所有首 chunk `Delta.ToolCalls` 都必须先遍历并写入独立 `ToolBlockState`；只有 `ID` 和 `Name` 均非空时才 `emitToolBlockStart`，否则参数只追加到 `PendingArgs`，等待后续元数据或 finish 处理。
- **首 chunk 多 tool calls**：遍历全部 `Delta.ToolCalls`（不得使用 `GetFirstToolCall`），每个走 `assignToolBlockIndex` → 合并元数据/缓存参数 → 按完整性决定是否 `emitToolBlockStart`
- 非首 chunk：同现有逻辑
- late_tool_starts: 完整 `ID`+`Name` → start 并 flush；`ID` empty → 使用 placeholder 后 start；`Name` empty 或仅有 `PendingArgs` → discard

### R6: HandleFinalResponse 终端化

- `info == nil` guard
- `EnsureClaudeConvertInfo(info)`
- 始终 `StopOpenBlocksForFinalize`（幂等）
- 缓存式 terminal events
- Gemini path: `handleFinalStream` 先 `EnsureClaudeConvertInfo` + 预填充
- Responses path: `FinalizeResponsesToChatStream` 后调 `HandleFinalResponse`

### R7: usage 写入规则

**`HasUpstreamUsage` 标志**：收到真实 upstream usage 时设为 `true`；非 nil usage 即使所有 token 都是 0 也算真实 usage。估算 usage 永不设置该标志。

| 写入点 | 条件 | 行为 |
|--------|------|------|
| converter chunk | `streamResponse.Usage != nil` | 覆盖 + `HasUpstreamUsage = true`（last-wins） |
| OaiStreamHandler 写回 | 真实归一化 usage 且 `!HasUpstreamUsage && Usage == nil` | 仅填补空 canonical cache，并设 `HasUpstreamUsage = true`；估算 usage 不写入 |
| Responses upstream metadata | `response.Usage != nil` | 更新 state usage + `state.HasUpstreamUsage = true` |
| Responses 写回 Claude canonical | `state.HasUpstreamUsage` | 写入真实 usage；估算 usage 不写入 canonical |
| `BuildClaudeUsageFromOpenAIUsage(nil)` | — | 返回零值 `&ClaudeUsage{}`，非 nil |

估算 usage（`responseText2Usage`）**绝不**写入 `ClaudeConvertInfo.Usage`，也不设置 `HasUpstreamUsage`。Responses/Gemini/OAI 都必须把“真实 usage 是否收到”和“用于结算/返回的 usage”分开：无真实 usage 时返回值可以使用估算值，但 Claude terminal 仍从 nil canonical usage 生成非 nil 零值 usage。OAI、Responses、Gemini 均按 usage 字段是否存在判断真实性；Gemini 将 `GeminiChatResponse.UsageMetadata` 改为指针，`nil` 才表示上游未发送 metadata，因此真实全零 metadata 也能被识别。

### R8: 首 chunk 统一路径

1. `message_start`
2. 遍历**全部** `Delta.ToolCalls`（非 `GetFirstToolCall`），每个先写入 `ToolBlockState`；仅当 `ID`、`Name` 完整时 `emitToolBlockStart`，否则缓存 `PendingArgs`
3. finish_reason → first-wins + usage last-wins + `stopOpenBlocksAndAdvance()` + return（无 terminal）

## nil 契约

| 函数 | 可见性 | nil-safe |
|------|--------|---------|
| `EnsureClaudeConvertInfo` | 导出 | `info == nil` → no-op |
| `StopOpenBlocksForFinalize` | 导出 | `nil` → nil |
| `StreamResponseOpenAI2Claude` | 导出 | `nil` → nil |
| `BuildClaudeUsageFromOpenAIUsage` | 导出 | `nil` → 零值 |
| `HandleFinalResponse` | 导出 | `info == nil` → return |
| `handleClaudeFormat` | 内部 | 入口先 `EnsureClaudeConvertInfo(info)`；`info == nil` → error |
| `HandleStreamFormat` | 导出 | `info == nil` → error，不递增计数 |
| `OaiStreamHandler` | 内部 | `info == nil` → 返回 API error |
| `handleLastResponse` | 内部 | `lastStreamData == ""` → no-op；其余输出指针由入口校验后传入 |
| `handleFinalStream` | 内部 | `info == nil || resp == nil` → error |
| `OaiResponsesToChatStreamHandler` | 内部 | `info == nil` → 返回 API error；state 由构造函数保证非 nil usage |

## Acceptance Criteria

- [ ] AC1: flush → converter → `HandleFinalResponse` 发送一次 terminal events
- [ ] AC2: 多 finish_reason → 一次 terminal，reason 首次非空
- [ ] AC3: cache token 扣除正确（max 语义，含 `5m+1h` 兜底）
- [ ] AC4: 上游错误 → `event:error`（仅一次）+ `sr.Stop()` + 无 success terminal
- [ ] AC5: tool call args 先于 id/name → 缓冲 → start 后 flush
- [ ] AC6: 错误终止后无 success terminal
- [ ] AC7: 纯文本流完整 event 序列
- [ ] AC8: 无 finish_reason EOF → `StopOpenBlocksForFinalize` 补关 → `end_turn`
- [ ] AC9: 现有 tool index 映射测试通过
- [ ] AC10: `go build ./...` 通过
- [ ] AC11: `chat_via_responses.go` Claude 路径不 panic
- [ ] AC12: Gemini → Claude 工具调用 → 正确 stop_reason + content_block_stop
- [ ] AC13: Responses → Claude → `message_delta` + `message_stop`
- [ ] AC14: flush 阶段上游错误 → 无 success terminal
- [ ] AC15: callback 已设 `StreamError` → flush 跳过
- [ ] AC16: `HandleFinalResponse(nil)` → 不 panic
- [ ] AC17: `EnsureClaudeConvertInfo(nil)` → no-op
- [ ] AC18: `StopOpenBlocksForFinalize(nil)` → no-op
- [ ] AC19: `StreamResponseOpenAI2Claude(nil, ...)` → nil
- [ ] AC20: chunk usage last-wins（`HasUpstreamUsage = true`）
- [ ] AC21: 估算 usage 不写入 canonical（`HasUpstreamUsage` 阻止）
- [ ] AC22: `StopOpenBlocksForFinalize` 幂等
- [ ] AC23: `event:error` 仅发送一次（`StreamError` 已设则跳过）
- [ ] AC24: 首 chunk 多 tool calls → 全部遍历，每个独立 `ToolBlockState`
- [ ] AC25: 全程无 usage → 零值 `{InputTokens:0, OutputTokens:0}`
- [ ] AC26: `BuildClaudeUsageFromOpenAIUsage(nil)` → 零值非 nil
- [ ] AC27: Gemini 工具流由 finalizer 补发 `content_block_stop`，且 terminal 只发送一次
- [ ] AC28: `HandleStreamFormat(nil, ...)`、`OaiStreamHandler(nil, ...)`、`handleFinalStream(nil, ...)` 不 panic
- [ ] AC29: `handleLastResponse("")` 不解码、不修改输出参数
- [ ] AC30: Responses 无真实 usage 时只返回估算值，Claude terminal usage 为零值；真实零 usage 仍设置 upstream 标志
- [ ] AC31: 重复触发 upstream error 只发送一次 `event:error`
- [ ] AC32: 首 chunk 多 tool calls 按原始顺序独立创建 block；带 finish 时先完成全部 block，再关闭 block，最后由 finalizer 发送 terminal

## Out of Scope

- StreamScannerHandler 接口改造
- Claude channel 直接路径
- 请求转换层改动
- infinite-whitespace 防护

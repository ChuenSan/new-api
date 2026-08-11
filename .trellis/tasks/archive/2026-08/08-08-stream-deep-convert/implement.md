# Implement plan: 流式响应转换增强

## Step 1: 数据结构 + nil-safe 初始化 (`relay/common/relay_info.go`)

- [ ] `ToolBlockState` struct: `AnthropicIndex`, `ID`, `Name`, `Started`, `PendingArgs`
- [ ] `ClaudeConvertInfo`: 加 `HasFinishReason`, `HasEmittedMessageDelta`, `StreamError`, **`HasUpstreamUsage`**, `ToolBlocks`
- [ ] 三套 map 注释明确 key 类型
- [ ] `EnsureClaudeConvertInfo(info *RelayInfo)` — `info == nil` safe no-op
- [ ] `GenRelayInfoClaude`: 调用 `EnsureClaudeConvertInfo`

**Verify**: `go build ./relay/common/...`

## Step 2: Usage + helper 函数 (`service/convert.go`)

- [ ] `inputTokensExcludingCache(promptTokens int, usage *dto.Usage) int`:
  - cachedRead = `CachedTokens` → fallback `PromptCacheHitTokens`
  - cacheCreation = **max**(`CachedCreationTokens`, `5m+1h`) — 匹配 `relay-claude.go:595`
  - `InputTokens = max(0, promptTokens - cachedRead - cacheCreation)`
  - `CacheCreationInputTokens = cacheCreation`; derive 5m/1h split once with `NormalizeCacheCreationSplit`
- [ ] `buildClaudeUsageFromOpenAIUsage`: 调用 `inputTokensExcludingCache`
- [ ] `BuildClaudeUsageFromOpenAIUsage(u *dto.Usage) *dto.ClaudeUsage` (exported): `u == nil` → `&ClaudeUsage{}`（零值，非 nil）
- [ ] `DetectUpstreamError(data string) *types.ClaudeError`
- [ ] `StopReasonOpenAI2Claude(reason string) string` (exported)
- [ ] `StopOpenBlocksForFinalize(info *relaycommon.RelayInfo) []*dto.ClaudeResponse` — nil-safe, 补关 + 清理三套状态

**Verify**: `go build ./service/...`

## Step 3: StreamResponseOpenAI2Claude 改造 (`service/convert.go`)

- [ ] 入口: `openAIResponse == nil || info == nil || info.ClaudeConvertInfo == nil` → nil; `StreamError || Done || HasEmittedMessageDelta` → nil
- [ ] `assignToolBlockIndex`: create `ToolBlockState`（key = AnthropicIndex）
- [ ] `emitToolBlockStart(state)`: `Started=true` + `ToolBlockStarted[AnthropicIndex]=true` + emit start + flush pending + clear
- [ ] chunk usage 写入: `if streamResponse.Usage != nil { info.ClaudeConvertInfo.Usage = streamResponse.Usage; info.ClaudeConvertInfo.HasUpstreamUsage = true }`
- [ ] finish chunk: first-wins reason, usage last-wins, late_tool_starts, `stopOpenBlocksAndAdvance()` → `LastMessagesType="none"`, return（**无 terminal**）
- [ ] `stopOpenBlocks` 闭包: 清理 `ToolBlocks`
- [ ] usage-only chunk（无 `finish_reason`）: cache usage，不关闭 block，return nil
- [ ] **首 chunk 多 tool calls**: 遍历全部 `Delta.ToolCalls`（非 `GetFirstToolCall`），每个先创建/更新独立 `ToolBlockState`；`ID`、`Name` 均完整才 `emitToolBlockStart` 并 flush `PendingArgs`，否则继续缓存参数
- [ ] 首 chunk finish: 先完成全部 tool block start/delta 与 pending args，再执行统一 finish path（first-wins, `stopOpenBlocksAndAdvance`, 无 terminal）
- [ ] 所有带 `finish_reason` 的 finish 路径（首 chunk、普通 chunk、带 usage 的 finish chunk）只关闭 block；无 `finish_reason` 的 usage-only chunk 不改变 block；`message_delta`/`message_stop` 只能由 `HandleFinalResponse` 发送

**Verify**: `go build ./service/...`

## Step 4: Handler 层

### 4a: handleClaudeFormat (`helper.go:35`)

- [ ] `info == nil` 先返回 error；否则入口先 `EnsureClaudeConvertInfo(info)`，再访问 `ClaudeConvertInfo`
- [ ] `DetectUpstreamError` → hit → **`if !StreamError` 才 emit `event:error`** + `StreamError=true` + return error
- [ ] `streamResponse.Usage != nil` → 始终覆盖 + `HasUpstreamUsage = true`，即使 usage 全零
- [ ] `common.Unmarshal` error check

### 4b: OaiStreamHandler callback (`relay-openai.go:125`)

- [ ] OaiStreamHandler 入口 `info == nil` → 返回 API error；非 nil 才访问 `RelayFormat`/`ChannelSetting`
- [ ] `HandleStreamFormat` return err → `info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo.StreamError` → `sr.Stop(err)`; else `sr.Error(err)`

### 4c: Flush (`relay-openai.go:144`)

```go
if info.RelayFormat == types.RelayFormatClaude &&
    info.ClaudeConvertInfo != nil && !info.ClaudeConvertInfo.StreamError &&
    lastStreamData != "" {
    _ = HandleStreamFormat(c, info, lastStreamData, ...)
}
```

- [ ] flush 错误依赖 `StreamError` 做一次性 error event 防重；callback 已设置 `StreamError` 时跳过 flush

### 4d: handleLastResponse guard

- [ ] 加 `if lastStreamData == "" { return nil }` guard；不解码、不修改输出参数

### 4e: Usage 写回

```go
usageIsEstimated := !containStreamUsage
if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && usage != nil {
    if !usageIsEstimated && !info.ClaudeConvertInfo.HasUpstreamUsage && info.ClaudeConvertInfo.Usage == nil {
        info.ClaudeConvertInfo.Usage = usage
        info.ClaudeConvertInfo.HasUpstreamUsage = true
    }
}
```

- [ ] `containStreamUsage` 表示收到真实非 nil usage（全零 usage 也算真实），所有 OAI usage 检测点都不能用 `ValidUsage` 或 token 数值代替
- [ ] estimated `ResponseText2Usage` 只用于返回/结算，不写入 Claude canonical cache

**Verify**: `go build ./relay/channel/openai/...`

## Step 5: HandleFinalResponse (`helper.go:159`)

- [ ] `if info == nil { return }` before switch
- [ ] Delete old Claude case logic
- [ ] `EnsureClaudeConvertInfo(info)`
- [ ] `!HasEmittedMessageDelta && !StreamError`:
  - **先无条件** `StopOpenBlocksForFinalize(info)` → 幂等 emit stops（包括 Gemini open tool blocks）
  - stopReason = `HasFinishReason` ? cached : `"end_turn"`
  - `BuildClaudeUsageFromOpenAIUsage(ClaudeConvertInfo.Usage)` → message_delta（nil→非 nil 零值）
  - message_stop
  - `HasEmittedMessageDelta = true`
- [ ] `Done = true`
- [ ] `StreamError` 时只设 `Done = true`，不得发送 success terminal；不再解码 `lastStreamData` 或从函数参数 usage 覆盖 canonical

**Verify**: `go build ./relay/channel/openai/...`

## Step 6: Gemini path (`relay-gemini.go:1335`)

- [ ] `info == nil || resp == nil` 先返回 error；Claude 分支先 `EnsureClaudeConvertInfo(info)`
- [ ] 将 `dto.GeminiChatResponse.UsageMetadata` 改为指针；`UsageMetadata != nil` 即真实 upstream，即使所有 token 都是 0
- [ ] `geminiStreamHandler` 只在 `UsageMetadata != nil` 时写入 Claude canonical `Usage` 并设 `HasUpstreamUsage = true`
- [ ] `buildUsageFromGeminiMetadata` 及所有调用方适配指针 metadata；nil metadata 只能生成 fallback/估算 usage
- [ ] `ResponseText2Usage` 或图片 fallback 只用于返回/结算，不写入 canonical，也不设 `HasUpstreamUsage`
- [ ] `handleFinalStream` 只预填充 finish reason；不得从可能是估算值的 `resp.Usage` 写回 canonical
- [ ] 预填充 finish reason 遵守 first-wins；`HandleFinalResponse` 仍始终幂等补关 open blocks

**Verify**: `go build ./relay/channel/gemini/...`

## Step 7: Responses API path (`chat_via_responses.go`)

- [ ] `info == nil` 入口先返回 API error；Claude 分支调用 `EnsureClaudeConvertInfo(info)`，不得手写半初始化 struct
- [ ] `ResponsesToChatStreamState` 增加 `HasUpstreamUsage bool`；仅 `applyResponseMetadata` 收到 `response.Usage != nil` 时设为 true
- [ ] `response.Usage != nil` 即真实 upstream，即使转换后的 token 全为 0；无真实 usage 时才调用 `ResponseText2Usage`
- [ ] 估算值仅用于返回/结算和 OpenAI usage，不写入 `ClaudeConvertInfo.Usage`
- [ ] Claude 写回：仅 `state.HasUpstreamUsage` 时写入 `ClaudeConvertInfo.Usage` 并设 `ClaudeConvertInfo.HasUpstreamUsage = true`
- [ ] `FinalizeResponsesToChatStream` 后: `HandleFinalResponse(c, info, "", ...)`
- [ ] constructor 保证 `state.Usage != nil`；无真实 usage 时 terminal 通过 Claude canonical nil 生成零值 usage

**Verify**: `go build ./relay/...`

## Step 8: 现有测试更新

- [ ] `newClaudeRelayInfo()`: 调用 `EnsureClaudeConvertInfo`
- [ ] 断言: `Done` → `HasFinishReason`
- [ ] `driveStreamTrackMapping`: snapshot before clear

**Verify**: `go test ./service/ -run TestStreamResponseOpenAI2Claude`

## Step 9: 新增测试

### Converter 层 (`service/convert_test.go`)

- [ ] `TestBuildClaudeUsage_CacheDeduction` — max 语义
- [ ] `TestBuildClaudeUsage_CacheCreationMismatch` — `CachedCreationTokens` vs `5m+1h` 不一致
- [ ] `TestBuildClaudeUsage_CacheCreationFieldsShareMax` — InputTokens/CacheCreationInputTokens/5m/1h 使用同一 max 解析结果
- [ ] `TestBuildClaudeUsage_NilUsage` — nil → 零值
- [ ] `TestDetectUpstreamError`
- [ ] `TestStreamResponseOpenAI2Claude_MessageDeltaDeferred`
- [ ] `TestStreamResponseOpenAI2Claude_ToolPendingArgs`
- [ ] `TestStreamResponseOpenAI2Claude_FirstChunkMultiTools` — 多 tool calls 遍历
- [ ] `TestStreamResponseOpenAI2Claude_FirstChunkFinish`
- [ ] `TestUsageOnlyChunkAfterFinish`
- [ ] `TestStreamErrorNoTerminalEvents`
- [ ] `TestStreamErrorEventOnce` — 重复触发只发一次 event:error
- [ ] `TestFinishReasonFirstWins`
- [ ] `TestHasUpstreamUsage_PreventsEstimateOverwrite`
- [ ] `TestResponsesUsage_EstimateNotCanonical`
- [ ] `TestResponsesUsage_RealUsageCanonical`
- [ ] `TestResponsesUsage_ZeroRealUsageIsStillUpstream`
- [ ] `TestResponsesUsage_NoRealUsageReturnsEstimateButTerminalZero`
- [ ] `TestStopOpenBlocksForFinalize_*` (Text/Tool/StateCleanup/Idempotent/NilInfo)
- [ ] `TestEnsureClaudeConvertInfo_NilInfo`
- [ ] `TestStreamResponseOpenAI2Claude_Nil*`

### Handler 层 (`relay/channel/openai/*_test.go`)

- [ ] `TestHandleFinalResponse_Claude_FinishDone`
- [ ] `TestHandleFinalResponse_Claude_FinishUsageOnly`
- [ ] `TestHandleFinalResponse_Claude_NoFinishReason`
- [ ] `TestHandleFinalResponse_Claude_NoUsage` — 全程无 usage → 零值
- [ ] `TestHandleFinalResponse_Claude_LastChunkError`
- [ ] `TestHandleFinalResponse_Claude_PureText`
- [ ] `TestHandleFinalResponse_Claude_StreamErrorFlush`
- [ ] `TestHandleFinalResponse_Claude_StreamErrorSkipsFlush`
- [ ] `TestHandleFinalResponse_Claude_InfoNil`
- [ ] `TestHandleFinalResponse_Claude_UsageNormalized`
- [ ] `TestHandleFinalResponse_Claude_UsagePriority`
- [ ] `TestHandleStreamFormat_InfoNil`
- [ ] `TestHandleLastResponse_Empty`

### Gemini 层 (`relay/channel/gemini/relay-gemini_test.go`)

- [ ] `TestGeminiClaude_PureText`
- [ ] `TestGeminiClaude_ToolCallFinishReason` — 断言 `content_block_stop` 且 terminal 仅一次
- [ ] `TestGeminiClaude_ZeroRealUsage` — 全零 `UsageMetadata` 仍设置 `HasUpstreamUsage`

### Responses API (`relay/channel/openai/chat_via_responses_test.go`)

- [ ] `TestResponsesToChatStream_Claude_Finalization`

**Verify**: `go test ./service/... ./relay/channel/openai/... ./relay/channel/gemini/...`

## Step 10: 全项目质量闸

```bash
go build ./...
go test ./relay/common/... ./service/... ./relay/channel/openai/... ./relay/channel/gemini/...
```

审查闸:
- [ ] converter 任何路径不输出 terminal
- [ ] terminal 仅由 `HandleFinalResponse` 发送
- [ ] `HandleFinalResponse` 始终调 `StopOpenBlocksForFinalize`（幂等）
- [ ] `StopOpenBlocksForFinalize` 幂等: `LastMessagesType == "none"` → no-op
- [ ] converter finish → `LastMessagesType = "none"`
- [ ] `event:error` 仅一次（`StreamError` 已设则跳过 emit）
- [ ] 首 chunk 遍历全部 `Delta.ToolCalls`
- [ ] `HasUpstreamUsage` 正确管理：chunk 写入设 true，估算值不覆盖
- [ ] `BuildClaudeUsageFromOpenAIUsage(nil)` → 零值非 nil
- [ ] cache creation: max(`CachedCreationTokens`, `5m+1h`)
- [ ] `InputTokens`/`CacheCreationInputTokens`/分桶使用同一组解析结果
- [ ] nil 契约：导出函数 nil-safe，内部函数调用方保证
- [ ] AC1–AC32 covered

## Planning gate

- [ ] `prd.md`、`design.md`、`implement.md` 的 finish、usage、tool、多入口 nil 契约一致
- [ ] usage 真实性统一按字段存在性判断；Gemini metadata 使用指针；示例中的真实性变量均有明确定义
- [ ] 用户审阅并明确同意后，才执行 `task.py start`

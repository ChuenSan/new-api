# Design: 流式响应转换增强

## 终态状态机

```
HasFinishReason         — upstream 发送了非空 finish_reason（first-wins，不覆盖）
HasEmittedMessageDelta  — terminal events 已实际发送给客户端  
Done                    — stream 完全处理完毕
StreamError             — 流内错误，禁止 success terminal events
HasUpstreamUsage        — 已收到真实 upstream usage；全零 usage 也算真实，估算值不设此标志
```

## 事件责任单点化

**唯一规则**：`message_delta` + `message_stop` 仅由 `HandleFinalResponse` 发送。converter 的任何路径都只负责 block-level events，**绝不**输出 terminal events。

`content_block_stop` 幂等闭环：

```
converter finish 路径 → stopOpenBlocksAndAdvance()
  → emit content_block_stop events
  → LastMessagesType = "none"    ← 关键：标记"已关"
  → return (无 terminal events)

HandleFinalResponse → StopOpenBlocksForFinalize(info)
  → LastMessagesType == "none" ? → no-op (幂等)
  → LastMessagesType != "none" ? → emit content_block_stop + 清理状态
  → 发送 message_delta + message_stop   ← 唯一 terminal 发送点
```

任何带 `finish_reason` 的 finish 路径都遵循同一规则：先处理当前 chunk 的全部 block delta，再缓存首次非空 finish reason 和最新非 nil usage，关闭 open blocks，标记 `LastMessagesType = "none"`，返回 block events；不得在 converter 内发送 terminal events。首 chunk finish、普通 finish、带 usage 的 finish chunk 均适用。仅有 usage 且没有 `finish_reason` 的 chunk 只更新 usage，不关闭 block。

状态转换：

```
正常 block chunk           → block events (content_block_start/delta/stop)
finish chunk (首次）       → HasFinishReason=true, 缓存 reason(first-wins)+非 nil usage(last-wins, HasUpstreamUsage=true),
                             stopOpenBlocksAndAdvance() → LastMessagesType="none" → return (无 terminal)
finish chunk (重复）       → 仅更新 usage 缓存（reason 不覆盖）
usage-only chunk（无 finish_reason）→ 仅更新 usage 缓存，不改变 block 状态
usage-bearing finish chunk → 按 finish 路径关闭 block，不发送 terminal events
HandleFinalResponse        → 始终先 StopOpenBlocksForFinalize (幂等，已关则 no-op)
                              if !HasEmittedMessageDelta && !StreamError:
                               HasFinishReason=true  → 用缓存 reason+usage 发 terminal
                               HasFinishReason=false → end_turn + 缓存 usage 发 terminal
                               usage 为 nil → 零值 ClaudeUsage{InputTokens:0, OutputTokens:0}
                               HasEmittedMessageDelta=true
                              Done=true
流内错误 (StreamError）    → event:error (仅一次, StreamError 已设则跳过) + sr.Stop()/return error;
                            HandleFinalResponse 设 Done=true, 不发 terminal
```

## 核心数据流修正

### 1. 最后一个 chunk flush

```
上游 SSE → StreamScannerHandler → callback(n-1 chunks)
→ [DONE] → scanner return
→ FLUSH: if RelayFormatClaude && ClaudeConvertInfo != nil && !StreamError && lastStreamData != "":
       HandleStreamFormat(info, lastStreamData)
       // converter 执行 stopOpenBlocksAndAdvance → emit content_block_stop + LastMessagesType="none"
       // converter 不产生 terminal events
→ if lastStreamData != "":
       handleLastResponse(lastStreamData)  // metadata 提取
→ applyUsagePostProcessing(usage, lastStreamData)
→ 真实归一化 usage 仅在 `!HasUpstreamUsage && ClaudeConvertInfo.Usage == nil` 时写回 canonical；估算 usage 不写回。真实 usage 由 `lastStreamResponse.Usage != nil` 判定，即使 token 全为 0 也算真实。
→ HandleFinalResponse: StopOpenBlocksForFinalize(幂等) → terminal events
```

### 2. content_block_stop 责任归属

**统一规则**：`HandleFinalResponse` 始终调用 `StopOpenBlocksForFinalize`（幂等）。已关（`LastMessagesType == "none"`）→ no-op。

| 场景 | converter 已关？ | `StopOpenBlocksForFinalize` 行为 |
|------|:---:|------|
| OAI finish 流 | 是 | no-op |
| OAI 无 finish EOF | 否 | 补关 |
| Gemini 工具流 | 否 | 补关 |

**`StopOpenBlocksForFinalize`**（导出，`service/convert.go`）：

```go
func StopOpenBlocksForFinalize(info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
    if info == nil || info.ClaudeConvertInfo == nil {
        return nil
    }
    var responses []*dto.ClaudeResponse
    switch info.ClaudeConvertInfo.LastMessagesType {
    case relaycommon.LastMessageTypeText, relaycommon.LastMessageTypeThinking:
        responses = append(responses, generateStopBlock(info.ClaudeConvertInfo.Index-1))
    case relaycommon.LastMessageTypeTools:
        started := make([]int, 0, len(info.ClaudeConvertInfo.ToolBlockStarted))
        for idx := range info.ClaudeConvertInfo.ToolBlockStarted {
            started = append(started, idx)
        }
        sort.Ints(started)
        for _, idx := range started {
            responses = append(responses, generateStopBlock(idx))
        }
    }
    info.ClaudeConvertInfo.ToolBlockStarted = make(map[int]bool)
    info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex = make(map[int]int)
    info.ClaudeConvertInfo.ToolBlocks = make(map[int]*ToolBlockState)
    info.ClaudeConvertInfo.LastMessagesType = relaycommon.LastMessageTypeNone
    return responses
}
```

`LastMessagesType == LastMessageTypeNone` 时返回 nil 且不重复发 stop；无论当前 block 类型如何，关闭后都清空 `ToolBlockStarted`、`ToolBlockIndexByOpenAIIndex`、`ToolBlocks` 三套状态并保持 `LastMessagesType = none`。

## Data structures

### ClaudeConvertInfo (relay_info.go)

```go
type ToolBlockState struct {
    AnthropicIndex int
    ID             string
    Name           string
    Started        bool
    PendingArgs    string
}

type ClaudeConvertInfo struct {
    LastMessagesType            string
    Index                       int
    Usage                       *dto.Usage
    FinishReason                string
    Done                        bool
    HasFinishReason             bool
    HasEmittedMessageDelta      bool
    StreamError                 bool
    HasUpstreamUsage            bool    // converter/Responses path 已收到至少一次真实非 nil upstream usage

    // key = upstream OpenAI tool_calls[].index → AnthropicIndex
    ToolBlockIndexByOpenAIIndex map[int]int
    // key = AnthropicIndex → 是否已发送 content_block_start
    ToolBlockStarted            map[int]bool
    // key = AnthropicIndex → 完整 tool block 状态
    ToolBlocks                  map[int]*ToolBlockState
}
```

### EnsureClaudeConvertInfo (relay_info.go)

```go
func EnsureClaudeConvertInfo(info *RelayInfo) {
    if info == nil { return }
    if info.ClaudeConvertInfo == nil {
        info.ClaudeConvertInfo = &ClaudeConvertInfo{
            LastMessagesType:            LastMessageTypeNone,
            ToolBlockIndexByOpenAIIndex: make(map[int]int),
            ToolBlockStarted:            make(map[int]bool),
            ToolBlocks:                  make(map[int]*ToolBlockState),
        }
        return
    }
    if info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex == nil {
        info.ClaudeConvertInfo.ToolBlockIndexByOpenAIIndex = make(map[int]int)
    }
    if info.ClaudeConvertInfo.ToolBlockStarted == nil {
        info.ClaudeConvertInfo.ToolBlockStarted = make(map[int]bool)
    }
    if info.ClaudeConvertInfo.ToolBlocks == nil {
        info.ClaudeConvertInfo.ToolBlocks = make(map[int]*ToolBlockState)
    }
}
```

## Changed files

| File | Changes |
|------|---------|
| `relay/common/relay_info.go` | `ToolBlockState` + `ClaudeConvertInfo` 扩展（含 `HasUpstreamUsage`）+ `EnsureClaudeConvertInfo` |
| `service/convert.go` | `StreamResponseOpenAI2Claude` 重写 + `buildClaudeUsageFromOpenAIUsage` cache deduction; `DetectUpstreamError`; `inputTokensExcludingCache`; `StopReasonOpenAI2Claude`/`BuildClaudeUsageFromOpenAIUsage` exported（nil→零值）; `StopOpenBlocksForFinalize` exported |
| `relay/channel/openai/helper.go:35` | `handleClaudeFormat`: `EnsureClaudeConvertInfo` + error 检测（`StreamError` 已设则跳过 emit）+ usage 覆盖 |
| `relay/channel/openai/helper.go:146` | `HandleFinalResponse`: `info == nil` guard; 始终 `StopOpenBlocksForFinalize`; 缓存式终端化 |
| `relay/channel/openai/relay-openai.go:144` | `OaiStreamHandler`: nil 入口、flush 条件 + `handleLastResponse` guard + usage 写回 |
| `relay/channel/gemini/relay-gemini.go:1335` | `handleFinalStream`: nil guard + `EnsureClaudeConvertInfo` + finish reason 预填充；真实 Gemini metadata 与估算 usage 分离 |
| `dto/gemini.go:457` | `GeminiChatResponse.UsageMetadata` 改为指针，以区分缺失 metadata 与真实全零 metadata |
| `service/relayconvert/responses_to_chat.go:215` | `ResponsesToChatStreamState` 增加真实 usage 标志，并在 metadata 应用处按 `response.Usage != nil` 设置 |
| `relay/channel/openai/chat_via_responses.go:193` | `EnsureClaudeConvertInfo`; `FinalizeResponsesToChatStream` 后调 `HandleFinalResponse`; usage 写回设 `HasUpstreamUsage=true` |

## Key function specs

### OaiStreamHandler (relay-openai.go:144)

```go
// Flush
if info.RelayFormat == types.RelayFormatClaude &&
    info.ClaudeConvertInfo != nil &&
    !info.ClaudeConvertInfo.StreamError &&
    lastStreamData != "" {
    if err := HandleStreamFormat(c, info, lastStreamData, ...); err != nil {
        common.SysLog("error flushing last Claude stream chunk: " + err.Error())
    }
}

// Guard against empty lastStreamData
if lastStreamData != "" {
    if err := handleLastResponse(...); err != nil {
        common.SysLog("error handling last response: " + err.Error())
    }
}

applyUsagePostProcessing(usage, lastStreamData)

// Only fill an empty canonical cache with real normalized usage.
if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && usage != nil {
    // containStreamUsage is set only when an upstream Usage field is non-nil,
    // including an all-zero usage object; estimated usage never sets it.
    usageIsRealUpstream := containStreamUsage
    if usageIsRealUpstream && !info.ClaudeConvertInfo.HasUpstreamUsage && info.ClaudeConvertInfo.Usage == nil {
        info.ClaudeConvertInfo.Usage = usage
        info.ClaudeConvertInfo.HasUpstreamUsage = true
    }
}
```

### Error event 一次性

```go
func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
    relaycommon.EnsureClaudeConvertInfo(info)

    if errInfo := service.DetectUpstreamError(data); errInfo != nil {
        if !info.ClaudeConvertInfo.StreamError {
            info.ClaudeConvertInfo.StreamError = true
            _ = helper.ClaudeData(c, dto.ClaudeResponse{Type: "error", Error: errInfo})
        }
        return fmt.Errorf("upstream error: %s", errInfo.Message)
    }
    // ...
}
```

### StreamResponseOpenAI2Claude

```go
func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
    if openAIResponse == nil || info == nil || info.ClaudeConvertInfo == nil { return nil }
    if info.ClaudeConvertInfo.Done || info.ClaudeConvertInfo.StreamError || info.ClaudeConvertInfo.HasEmittedMessageDelta { return nil }
    // ...
}
```

### Usage 写入规则

| 写入点 | 条件 | 行为 |
|--------|------|------|
| converter chunk | `streamResponse.Usage != nil` | 覆盖 + `HasUpstreamUsage = true`（last-wins） |
| OaiStreamHandler 写回 | `!usageIsEstimated && !HasUpstreamUsage && Usage == nil` | 仅填补空 canonical cache，并设 `HasUpstreamUsage = true` |
| Responses upstream metadata | `response.Usage != nil` | 更新 `state.Usage` + `state.HasUpstreamUsage = true` |
| Responses 写回 Claude canonical | `state.HasUpstreamUsage` | 只写真实 upstream usage；估算值不写入 canonical |

### Cache token 扣除（匹配 `relay-claude.go:595` 的 max 语义）

```go
func inputTokensExcludingCache(promptTokens int, usage *dto.Usage) int {
    _, cachedRead, cacheCreation, _, _ := resolveCacheTokens(usage)
    return max(promptTokens-cachedRead-cacheCreation, 0)
}
```

`InputTokens`、`CacheCreationInputTokens`、5m/1h 分桶字段使用同一个 `cacheCreation` 总量；先算总量再用 `NormalizeCacheCreationSplit` 分桶，禁止用原始字段分别重复扣除。

`resolveCacheTokens(usage)` 是唯一解析入口，返回 `cachedRead`、`cacheCreation`、`creation5m`、`creation1h`；其中 `cachedRead` 在标准字段为 0 时才回退到 `PromptCacheHitTokens`，`cacheCreation = max(PromptTokensDetails.CachedCreationTokens, creation5mRaw+creation1hRaw)`。`buildClaudeUsageFromOpenAIUsage` 只使用该解析结果：`InputTokens` 使用扣除后的值，`CacheCreationInputTokens` 使用 `cacheCreation`，分桶使用 `NormalizeCacheCreationSplit(cacheCreation, creation5mRaw, creation1hRaw)`。

### BuildClaudeUsageFromOpenAIUsage — nil → 零值

```go
func BuildClaudeUsageFromOpenAIUsage(u *dto.Usage) *dto.ClaudeUsage {
    if u == nil {
        return &dto.ClaudeUsage{} // {InputTokens:0, OutputTokens:0}
    }
    // ... existing logic with inputTokensExcludingCache
}
```

### HandleFinalResponse

```go
func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, ...) {
    if info == nil { return }

    switch info.RelayFormat {
    case types.RelayFormatClaude:
        relaycommon.EnsureClaudeConvertInfo(info)
        // Always run the idempotent block finalizer before any terminal decision.
        // This also closes Gemini tool blocks whose finish_reason was cleared.
        stops := service.StopOpenBlocksForFinalize(info)
        for _, stop := range stops {
            _ = helper.ClaudeData(c, *stop)
        }
        if !info.ClaudeConvertInfo.HasEmittedMessageDelta &&
            !info.ClaudeConvertInfo.StreamError {
            stopReason := "end_turn"
            if info.ClaudeConvertInfo.HasFinishReason {
                stopReason = service.StopReasonOpenAI2Claude(info.ClaudeConvertInfo.FinishReason)
            }
            claudeUsage := service.BuildClaudeUsageFromOpenAIUsage(info.ClaudeConvertInfo.Usage)
            _ = helper.ClaudeData(c, dto.ClaudeResponse{
                Type:  "message_delta",
                Usage: claudeUsage,
                Delta: &dto.ClaudeMediaMessage{StopReason: &stopReason},
            })
            _ = helper.ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"})
            info.ClaudeConvertInfo.HasEmittedMessageDelta = true
        }
        info.ClaudeConvertInfo.Done = true
    }
}
```

### 首 chunk — 多 tool calls 遍历

```go
if info.SendResponseCount == 1 {
    // 1. message_start
    // 2. 遍历 ALL Delta.ToolCalls（非 GetFirstToolCall）。每个 tool call
    //    先创建/更新 ToolBlockState，并把参数写入 PendingArgs；只有
    //    ID、Name 均非空时才 start 并 flush PendingArgs。
    if openAIResponse.IsToolCall() {
        for i := range openAIResponse.Choices[0].Delta.ToolCalls {
            tc := &openAIResponse.Choices[0].Delta.ToolCalls[i]
            openAIIndex := i
            if tc.Index != nil { openAIIndex = *tc.Index }
            idx := assignToolBlockIndex(openAIIndex, tc)
            state := info.ClaudeConvertInfo.ToolBlocks[idx]
            mergeToolMetadataAndArgs(state, tc)
            if state.ID != "" && state.Name != "" {
                emitToolBlockStart(state)
            }
        }
    }
    // thinking/text → content_block_start + delta
    // 3. finish_reason → first-wins, usage last-wins, stopOpenBlocksAndAdvance, return (无 terminal)
}
```

### Gemini path

```go
func handleFinalStream(c *gin.Context, info *relaycommon.RelayInfo, resp *dto.ChatCompletionsStreamResponse) error {
    if info == nil || resp == nil {
        return errors.New("nil Gemini final stream input")
    }
    if info.RelayFormat == types.RelayFormatClaude {
        relaycommon.EnsureClaudeConvertInfo(info)
        if len(resp.Choices) > 0 && resp.Choices[0].FinishReason != nil && *resp.Choices[0].FinishReason != "" {
            if !info.ClaudeConvertInfo.HasFinishReason {
                info.ClaudeConvertInfo.FinishReason = *resp.Choices[0].FinishReason
                info.ClaudeConvertInfo.HasFinishReason = true
            }
        }
    }
    // geminiStreamHandler records real UsageMetadata before this finalizer.
    // resp.Usage may be an estimate and must not promote itself to canonical usage.
    streamData, _ := common.Marshal(resp)
    openai.HandleFinalResponse(c, info, string(streamData), ...)
    return nil
}
```

### Responses API path

```go
if info.RelayFormat == types.RelayFormatClaude {
    relaycommon.EnsureClaudeConvertInfo(info)
}
// ... stream processing ...
// applyResponseMetadata sets state.HasUpstreamUsage only when
// response.Usage != nil. An estimated usage is never marked as upstream.
usage := state.Usage
if !state.HasUpstreamUsage {
    usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
    // return/billing fallback only; do not place it in ClaudeConvertInfo.Usage
}
if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil && state.HasUpstreamUsage {
    info.ClaudeConvertInfo.Usage = state.Usage
    info.ClaudeConvertInfo.HasUpstreamUsage = true
}
for _, chunk := range relayconvert.FinalizeResponsesToChatStream(state) { ... }
if info.RelayFormat == types.RelayFormatClaude {
    // With no real upstream usage, canonical Usage remains nil and the
    // terminal message_delta carries zero usage; `usage` is still returned
    // for billing/settlement.
    HandleFinalResponse(c, info, "", responseId, createAt, state.Model, "", usage, true)
}
```

### nil 契约总结

`HandleStreamFormat`、`OaiStreamHandler`、`handleFinalStream`、`OaiResponsesToChatStreamHandler` all reject nil required inputs before dereference. `handleLastResponse("")` is a no-op. `EnsureClaudeConvertInfo(nil)` and all exported converter/finalizer helpers are safe no-ops.

| 函数 | 可见性 | nil-safe 策略 |
|------|--------|-------------|
| `EnsureClaudeConvertInfo` | 导出 | `info == nil` → no-op |
| `StopOpenBlocksForFinalize` | 导出 | `info == nil \|\| ccInfo == nil` → nil |
| `StreamResponseOpenAI2Claude` | 导出 | `resp == nil \|\| info == nil \|\| ccInfo == nil` → nil |
| `BuildClaudeUsageFromOpenAIUsage` | 导出 | `u == nil` → 零值 `&ClaudeUsage{}` |
| `HandleFinalResponse` | 导出 | `info == nil` → return |
| `handleClaudeFormat` | 内部 | `info == nil` → error；非 nil 入口先初始化 |
| `HandleStreamFormat` | 导出 | `info == nil` → error，不递增计数 |
| `OaiStreamHandler` | 内部 | `info == nil` → API error |
| `handleFinalStream` | 内部 | `info == nil \|\| resp == nil` → error |
| `OaiResponsesToChatStreamHandler` | 内部 | `info == nil` → API error；state usage 由构造函数保证非 nil |

## Test plan

### 修改现有测试

- `newClaudeRelayInfo()`: 调用 `EnsureClaudeConvertInfo`
- `driveStream`: 断言 `Done` → `HasFinishReason`
- `driveStreamTrackMapping`: snapshot before clear

### 新增 converter 层测试

- `TestBuildClaudeUsage_CacheDeduction` — cached read + creation（max 语义）
- `TestBuildClaudeUsage_CacheCreationMismatch` — `CachedCreationTokens` 与 `5m+1h` 不一致时取 max
- `TestBuildClaudeUsage_NilUsage` — nil → 零值 `{InputTokens:0, OutputTokens:0}`
- `TestDetectUpstreamError`
- `TestStreamResponseOpenAI2Claude_MessageDeltaDeferred`
- `TestStreamResponseOpenAI2Claude_ToolPendingArgs`
- `TestStreamResponseOpenAI2Claude_FirstChunkMultiTools` — 首 chunk 多 tool calls 遍历
- `TestStreamResponseOpenAI2Claude_FirstChunkFinish`
- `TestUsageOnlyChunkAfterFinish`
- `TestStreamErrorNoTerminalEvents`
- `TestStreamErrorEventOnce` — 重复触发只发一次 event:error
- `TestFinishReasonFirstWins`
- `TestHasUpstreamUsage_PreventsEstimateOverwrite`
- `TestStopOpenBlocksForFinalize_Text` / `_Tool` / `_StateCleanup` / `_Idempotent` / `_NilInfo`
- `TestEnsureClaudeConvertInfo_NilInfo`
- `TestStreamResponseOpenAI2Claude_NilOpenAIResponse` / `_NilInfo`

### 新增 handler 层测试

- `TestHandleFinalResponse_Claude_FinishDone`
- `TestHandleFinalResponse_Claude_FinishUsageOnly`
- `TestHandleFinalResponse_Claude_NoFinishReason`
- `TestHandleFinalResponse_Claude_NoUsage` — 全程无 usage → 零值 usage
- `TestHandleFinalResponse_Claude_LastChunkError`
- `TestHandleFinalResponse_Claude_PureText`
- `TestHandleFinalResponse_Claude_StreamErrorFlush`
- `TestHandleFinalResponse_Claude_StreamErrorSkipsFlush`
- `TestHandleFinalResponse_Claude_InfoNil`
- `TestHandleFinalResponse_Claude_UsageNormalized`
- `TestHandleFinalResponse_Claude_UsagePriority`

### Gemini 层

- `TestGeminiClaude_ToolCallFinishReason`
- `TestGeminiClaude_PureText`

### Responses API 路径

- `TestResponsesToChatStream_Claude_Finalization`

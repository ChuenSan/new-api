# 修复 OpenAI→Claude 流式转换 tool_calls index 错位 — Design

## 决策 1:用「OpenAI tool index → Claude block index」映射替代表达式 `base + offset`

### 现状的数学缺陷

当前实现把「Claude content_block index」当作 `ToolCallBaseIndex + toolCall.Index` 的代数和。这个公式在两个维度上都不可控:

- `ToolCallBaseIndex` 来自 `ClaudeConvertInfo.Index`(已被前置 text/thinking 块推进);
- `toolCall.Index` 来自上游,可能不从 0 开始、不连续。

两个不可控量相加,Claude 侧 index 就可能出现空洞(线上实锤:index 1 从未 start 却收到 stop)。**Claude 的 content_block index 是一个纯粹的本地序列号,它的正确值只取决于「这是第几个被打开的块」,与上游 index 无关。**

### 方案选型

| 方案 | 判断 |
|---|---|
| A. 上游 index 归一化(减去 min index) | 否。仍需假设上游 index 连续,且与 `ToolCallBaseIndex` 的耦合没断 |
| **B. 映射表 `map[openaiIdx]claudeIdx` + 独立计数器** | **采用**。CC Switch 的同构方案,彻底解耦,天然支持乱序/非连续/多并行 |
| C. 引入完整 SSE 状态机重写 | 否。超出本任务边界,R6 要求非流式/逆向/Gemini 零影响,重写风险不可控 |

### 采用 B 的状态模型

`ClaudeConvertInfo` 内(`relay/common/relay_info.go:37`)将工具 index 相关字段重构为:

```go
// 替换 ToolCallBaseIndex / ToolCallMaxIndexOffset
ToolBlockIndexByOpenAIIndex map[int]int  // openai tool index -> claude block index
ToolBlockStarted            map[int]bool // 已 start 的 claude block index(供 stopOpenBlocks 精确关块)
```

分配规则(在 `StreamResponseOpenAI2Claude` 内,`convert.go:480-529` 主工具循环):

1. 见到 `toolCall.Index = k` 且 `k` 不在 map → 分配 `claudeIdx = info.ClaudeConvertInfo.Index`,然后 `Index++`,`ToolBlockIndexByOpenAIIndex[k]=claudeIdx`,`ToolBlockStarted[claudeIdx]=true`,发 `content_block_start(claudeIdx)`。
2. `k` 已在 map → 取已分配的 `claudeIdx`,只发 `content_block_delta(claudeIdx, input_json_delta)`。
3. `stopOpenBlocks` 的 `LastMessageTypeTools` 分支改为遍历 `ToolBlockStarted` 的 key(排序后),逐个 `content_block_stop(idx)`,并清空 map。

### 关键不变式(实现必须保证)

- **INV-1** Claude index 分配单调递增、从 0 连续:`Index` 只在开新块时 +1,永不回退、永不跳号。
- **INV-2** 每个 `ToolBlockStarted[idx]=true` 的 idx 恰好收到一次 `content_block_start` 和一次 `content_block_stop`。
- **INV-3** `stopOpenBlocks` 只对 `ToolBlockStarted` 里存在的 idx 发 stop——这是 R4 的直接落地。
- **INV-4** text/thinking 块仍用 `Index` 单槽(现有逻辑不变),与工具块共享同一 `Index` 计数器——保证 text(0) → tool(1) 的连续性。

## 决策 2:`SendResponseCount==1` 首个 chunk 即工具调用的分支(`convert.go:318-355`)与主循环对齐

当前该分支硬编码 `resp.SetIndex(0)`。这在「首 chunk 就是 tool_call」时正确(0 是第一个块),但它**没有走映射分配**,如果后续主循环又在 `Index` 上推进,两条路径的 index 语义会分裂。

对齐方式:首 chunk 的工具块也走决策 1 的分配函数(`k=toolCall.Index` → `claudeIdx = Index`),`SetIndex(0)` 自然成立(此时 `Index==0`),但语义上不再是字面量 0,而是「第 0 个被分配的块」。这样 AC4 与主循环遵守同一不变式。

## 决策 3:`stopOpenBlocksAndAdvance` 的 tools 分支简化

现状(`convert.go:289-296`)在 tools 后要 `Index = ToolCallBaseIndex + MaxIndexOffset + 1` 再清零 base/maxOffset——这是代数分配的遗迹。改为映射模型后,`Index` 已在每次分配时正确推进,tools 分支只需:关所有已 start 工具块 → 清空 map → `LastMessagesType = none`(`Index` 不再手动重算)。

## 决策 4:测试策略——真实线上 fixture + 合成边界

- **fixture 测试**(AC1/R7):把线上 124 chunk 的 `tool_calls[].index=1` 特征抽象为最小合成流(role → 1 reasoning delta → tool index=1 的 start+args → finish=tool_calls → usage → DONE),断言产出事件的 index 序列。不必搬运完整 124 chunk(含敏感 prompt),提炼结构特征即可。
- **合成边界**(AC2/AC3):并行 3 tool 连续 index、tool 在 text 后、乱序 index(2 先于 0)。
- 测试位置:`service/` 包内新增 `convert_test.go`(若已有则追加),直接调用 `StreamResponseOpenAI2Claude`,无需起 gin——函数签名只依赖 `*dto.ChatCompletionsStreamResponse` 和 `*relaycommon.RelayInfo`,可纯单测。
- **回归保护**:运行 `go test ./service/ ./relay/...`,确认既有 Claude/Gemini 转换测试不因 `ClaudeConvertInfo` 字段重构而编译失败。

## 兼容性 / 回滚

- `ClaudeConvertInfo` 的 `ToolCallBaseIndex`/`ToolCallMaxIndexOffset` 删除:已确认仅 `service/convert.go` 与 `relay/common/relay_info.go` 引用,无外部消费方(见 prd「代码边界」表),属安全的包内重构。
- `Usage`/`Done`/`FinishReason` 字段不动,`helper.go:42/166/172`、`gemini/relay-gemini.go:1493`、`chat_via_responses.go:271` 全部不受影响。
- 回滚:单 commit,`git revert` 即恢复原代数逻辑;无 DB 迁移、无配置变更、无 API 变更。

## 明确的后续项(不在本任务)

- 「arguments 先于 id/name 到达」的乱序缓冲(CC Switch `pending_args`/`late_tool_starts`):当前 New API 实现里 `toolCall.Function.Name == ""` 时不发 start(`convert.go:504`),若 args 先于 name 到达会丢 args 或产出无 name 块。这是独立缺陷,需单独的 prd。
- usage cache 扣除、UTF-8 跨 chunk 安全:见会话对比结论,各自独立任务。

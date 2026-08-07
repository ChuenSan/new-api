# 修复 OpenAI→Claude 流式转换 tool_calls index 错位导致 Claude Code「Content block not found」

## Goal

修复 `service/convert.go` 中 `StreamResponseOpenAI2Claude` 的 content_block index 分配缺陷:当上游(OpenAI 兼容)流式响应的 `tool_calls[].index` 不从 0 开始、或 tool_use 块前已有 text/thinking 块占用 index 时,转换出的 Claude SSE 事件序列出现 index 空洞/失序,导致 Claude Code 客户端在本地解析 SSE 时抛 `RangeError("Content block not found")`(会话级 API Error,服务端无日志)。

## Background / Confirmed Facts

### 线上复现(2026-08-07,请求 ID `202608070314301688798028268d9d6FtA3eP9q`)

客户端:Claude Code 2.1.220,`POST /v1/messages?beta=true`,渠道 50(DeepSeek-V4-Flash-0731,`Claude Messages → OpenAI Compatible` 转换)。上游(`https://new-api.abrdns.com/v1/chat/completions`)返回的 124 个 SSE chunk 结构:

```
chunk 0      role=assistant
chunk 1      空 delta
chunk 2-94   reasoning(93 个 thinking delta,Claude index 0)
chunk 95     tool_calls[0]={index:1, id:toolu_55ac…, name:mcp__grok-search__plan_intent, args:""}
chunk 96-120 tool_calls[0]={index:1, args 增量}(Claude index 1)
chunk 121    finish_reason=tool_calls
chunk 122    usage-only
chunk 123    [DONE]
```

关键点:**上游 `tool_calls[].index` 从 1 开始,不是 0**(reasoning 占掉了 index 0)。

### 当前 index 分配逻辑(缺陷根因)

`service/convert.go` `StreamResponseOpenAI2Claude`(`:255`)的工具块分配:

| 位置 | 行为 | 问题 |
|---|---|---|
| `convert.go:484` | 进入工具段时 `ToolCallBaseIndex = info.ClaudeConvertInfo.Index`(当前空闲 index,本例 =1) | base 与 OpenAI index 无关 |
| `convert.go:493-497` | `offset = toolCall.Index`(OpenAI 原始 index,本例 =1) | offset 不从 0 归一 |
| `convert.go:501` | `blockIndex = base + offset`(= 1+1 = **2**) | 工具块开在 Claude index 2,**index 1 空洞** |
| `convert.go:272-276` | `stopOpenBlocks` 对 `base+0 … base+maxOffset` 逐个 `content_block_stop`(= 1,2) | 对 index 1 发了 stop,但 index 1 从未 start |

产出的 Claude SSE(简化):`message_start → cb_start(0,thinking) → 93×cb_delta(0) → cb_stop(0) → cb_start(2,tool_use) → 25×cb_delta(2,input_json) → cb_stop(1)† → cb_stop(2) → message_delta → message_stop`。

`cb_stop(1)†`:index 1 的 stop 没有任何对应的 start——Claude Code 收到 `content_block_stop`/`content_block_delta` 时在 `cr[pi.index]` 查不到块(`undefined`),抛 `RangeError("Content block not found")`(Claude Code 2.1.220 二进制内 `content_block_not_found_delta` 埋点),会话报 `⏺ API Error: Content block not found`。

### 为什么服务端无报错

错误由 Claude Code **客户端本地**在 SSE 解析阶段抛出,不形成 HTTP 错误回传。New API 侧自认转换成功:`stream_status:{status:"ok", end_reason:"done"}`、HTTP 200、正常计费(该请求扣 $2.3)。用户侧看到「API Error」但两侧服务端日志均无异常——这是该缺陷长期不可见的原因。

### 代码边界(修复的作用域)

| 事实 | 位置 |
|---|---|
| 唯一转换函数 | `service/convert.go:255` `StreamResponseOpenAI2Claude` |
| 状态结构体 | `relay/common/relay_info.go:37` `ClaudeConvertInfo`(含 `ToolCallBaseIndex` / `ToolCallMaxIndexOffset`,**仅这两个字段服务于工具 index 分配**) |
| 运行时调用点 | `relay/channel/openai/helper.go:44`(逐 chunk)与 `:168`(流尾最后一次) |
| 同语义非流式 | `service/convert.go:607` `ResponseOpenAI2Claude`(无 index 概念,不受影响) |
| 逆向转换 | `relay/channel/claude/relay-claude.go:442` `StreamResponseClaude2OpenAI`(Claude→OpenAI,与缺陷无关) |
| 其他 `ClaudeConvertInfo` 消费方 | `helper.go:42/:166` 只写 `Usage`;`helper.go:172` 只写 `Done`;`gemini/relay-gemini.go:1493` 只读 `Done`;`openai/chat_via_responses.go:271` 只写 `Usage`。**无人读 `ToolCallBaseIndex/MaxIndexOffset`,可安全改语义** |

### 触发条件(缺陷的输入域)

任一即可触发 index 错位:
1. 上游 `tool_calls[].index` 不从 0 开始(本例:reasoning 占 index 0,tool 从 1 开始)。
2. 同一流中 text/thinking 块先于 tool_use 块出现,使 `ToolCallBaseIndex > 0`,再叠加非零 OpenAI index → 空洞。
3. 多个并行 tool_call 且 index 不连续(如 0,2)→ `base+0..base+maxOffset` 的连续 stop 会命中未 start 的 index。

### 参照实现(CC Switch,Rust)

CC Switch(`docs/协议转换实现剖析.md`)的 `streaming.rs` 用 `tool_blocks_by_index: HashMap<OpenAI_index, ToolBlockState>` 把 OpenAI 的 tool index 映射到**独立、连续的 Claude content index**,乱序到达(`arguments` 先于 `id/name`)时用 `pending_args` 缓冲、finish 时 `late_tool_starts` 补发 start。这正是 New API 缺的解耦层。

## Requirements

- R1:Claude 侧 `content_block` index 必须从 0 连续分配,text/thinking/tool_use 共享同一递增序列,与上游 `tool_calls[].index` 的取值解耦。
- R2:同一 OpenAI tool index 的后续 args 增量必须落到**同一** Claude 块(需要 OpenAI index → Claude index 的映射)。
- R3:并行多 tool(index 0..n,不论是否连续)各自独立开块、独立关块。
- R4:关块(`content_block_stop`)只对**已 start** 的 index 发,且每个已 start 的块恰好 stop 一次。
- R5:不改 `ClaudeConvertInfo` 对外行为契约(`Usage`/`Done`/`FinishReason` 的读写方不受影响);`ToolCallBaseIndex`/`ToolCallMaxIndexOffset` 可重构或替换。
- R6:非流式 `ResponseOpenAI2Claude`、逆向 `StreamResponseClaude2OpenAI`、Gemini 路径零影响。
- R7:回归用真实线上流(chunk 序列见上)做 fixture,验证产出的 Claude SSE index 序列严格 `start(0)…stop(0), start(1)…stop(1)` 无空洞。

## Acceptance Criteria

- AC1:用 R7 的 fixture(reasoning 93 delta + tool index=1 25 args 增量)转换,产出事件序列的 index 集合为 `{0(thinking), 1(tool_use)}`,无 index 2,无对未 start index 的 stop。
- AC2:并行 3 个 tool(index 0,1,2)且 text 在前 → index 序列为 text=0, tool0=1, tool1=2, tool2=3,各自 start/stop 配对。
- AC3:tool index 乱序到达(index 2 先于 index 0)→ 按 OpenAI index 稳定映射,同一 OpenAI index 的 args 始终落同一块。
- AC4:`message_start` 首个 chunk 即带 tool_call(`SendResponseCount==1` 分支,`convert.go:318-355`)同样遵守 R1(当前硬编码 `resp.SetIndex(0)`,若此前已有 thinking 块则冲突——需确认该分支与主循环 index 分配一致)。
- AC5:`service/convert.go` 既有测试 + 新增 fixture 测试全绿;`go build ./...` 通过。
- AC6(部署验证):fix 上线后,同一客户端对同一上游重放该请求,Claude Code 不再报 `Content block not found`。

## Non-goals

- 不做「arguments 先于 id/name 到达」的乱序缓冲(CC Switch 的 `pending_args`)——那是另一个独立缺陷,超出本次线上事故的根因范围,仅在 design 中标注为后续项。
- 不动非流式、逆向、Gemini、AWS Bedrock 的转换路径。
- 不改计费、usage 语义(cache 扣除是另一个独立差距,不在本任务)。

## Out of Scope / Explicitly Excluded

- CC Switch 的其他硬化(UTF-8 跨 chunk、无限空白防护、SSE 嗅探)不纳入。
- usage cache 扣除(`input_tokens_excluding_cache`)不纳入。

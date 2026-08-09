# PRD: `/v1/messages` → `/v1/chat/completions` 深度转换

## Goal

让严格 Anthropic Messages 客户端通过 OpenAI、Azure、OpenRouter Chat Completions，以及 Advanced Custom 精确 converter `anthropic_messages_to_openai_chat_completions`，稳定完成文本、图片、thinking、工具调用、多轮 `tool_result`、非流式和流式请求/响应转换。

## Background

现有共享转换以入站 `RelayFormatClaude` 为判断，无法确认最终上游协议，因此会污染 Messages→Responses、Gemini 或原生 Messages 路径。请求转换存在工具、schema、混合内容和图片校验缺口；非流式转换缺少 Chat choice、refusal、legacy function call 与 usage 的严格处理；流式转换已有独立子任务，但必须受同一协议隔离契约约束。

## Scope

### R0: 目标协议隔离

在请求生命周期状态中建立 fail-closed 契约 `AnthropicMessagesToOpenAIChatCompletions`。仅当入站为 Anthropic Messages、未使用 pass-through，且最终上游已明确为 OpenAI Chat Completions 时设为 true；该状态在请求、非流式响应、流式 chunk、EOF finalizer 与 pseudo-SSE fallback 中一致可读。

启用：OpenAI、Azure、OpenRouter 的实际 Chat Completions endpoint；Advanced Custom `/v1/messages` route 且 converter 精确为 `anthropic_messages_to_openai_chat_completions`。

禁用：Messages→Responses（含 bridge）、Gemini、原生 Anthropic/DeepSeek/Moonshot Messages、Advanced Custom `none` 或其他 converter、任意 pass-through，以及非 Claude 下游的流调用者。

### R1: Chat 请求深转换

仅在 R0 true 时：

- 保留现有模型映射、token 规范化与标准采样/stream 字段适配。
- 转换有效的 `tool_choice`、`disable_parallel_tool_use` 与 tools；无有效 tools 时不得输出悬空工具选择字段。
- tools 解析、空 name、非 object schema、tool input/result 序列化与无效 image source 必须显式返回转换错误。
- 仅递归删除 schema 中值严格为 `"uri"` 的 `format` 字段，保留其他 JSON Schema 关键字。
- system array 的有效 text block 以换行拼接；维持 OpenRouter Claude 兼容路径的既有多模态/cache 行为。
- 同一 assistant message 可保留 text/image/thinking 与多个 tool calls；`tool_result` 转独立 tool message。
- 历史 thinking 仅在目标 adaptor 明确支持时输出 reasoning 字段；普通严格 Chat 不发送非标准字段。

### R2: 非流式 Chat 响应转换

仅在 R0 true 时：

- 空 choices 作为 bad response；仅使用 `choices[0]`。
- 按 thinking、text/refusal、tool_use 的固定顺序构造 Anthropic content blocks。
- 支持 message/part refusal、`reasoning_content`/`reasoning`、`tool_calls` 与 legacy `function_call`。
- 不合法、空或非 object 的 tool arguments 降级为 `{}` 并记录警告。
- 完整映射 finish reason；未知值降级为 `end_turn` 并记录警告。
- usage 始终非 nil、各 token 非负，cache token 不与 input token 重复累计。

### R3: 流式转换（子任务）

现有子任务 `.trellis/tasks/08-08-stream-deep-convert/` 负责 Chat SSE 到 Anthropic SSE 的状态机、pending tool、finish/usage/error/EOF finalizer 语义。该任务的新增行为必须由 R0 契约守卫，并与本任务的 DTO、隔离与最终集成测试一致。

### R4: 非流式 pseudo-SSE fallback

仅在 R0 true 且调用方请求非流式时，`OpenaiHandler` 在首次 JSON 解码失败后尝试 SSE 聚合：按顺序聚合 content/reasoning，按 upstream index 聚合 tool calls，finish reason first-wins，usage last-wins。SSE error、无有效 choice、缺少完成标识或截断结果必须返回上游/坏响应错误；非目标路径不得触发。

## Out of Scope

- 通用 IR 或通用协议转换框架。
- 改变任何非目标协议、pass-through 或非 Claude 下游调用者的字段、SSE terminal、usage 语义。
- 新增模型名、厂商名或 OpenAI 兼容名称推断。
- 改造原生 Anthropic Messages 渠道。

## Acceptance Criteria

- [ ] AC1：仅明确目标 Chat 路径启用深转换；所有列明非目标路径 fail-closed 并保留 legacy 行为。
- [ ] AC2：目标请求正确转换 system、tools/schema、tool choice、混合 text/image/tool_use、tool_result 与支持的 thinking；所有无效输入返回明确错误而不 panic。
- [ ] AC3：目标非流式响应严格处理 choice、thinking/refusal/tool calls/legacy function call、finish reason 和非负 usage。
- [ ] AC4：目标流式路径满足 content-block 生命周期、稠密索引、pending tools、唯一 terminal、usage-only、EOF 与 error 语义；非目标 stream 调用者不受影响。
- [ ] AC5：非流式 pseudo-SSE 在目标路径可靠聚合，且错误/截断不生成成功 Anthropic message。
- [ ] AC6：所有新增与回归测试通过，`go build ./...` 通过。

## Task Map

| Task | Responsibility | Dependency |
| --- | --- | --- |
| `08-08-stream-deep-convert` | R3 流式状态机与终结语义 | 依赖 R0 契约设计；集成前与本任务同步 |
| 本父任务 | R0、R1、R2、R4、DTO、目标 adaptor 接入、跨路径回归与最终集成 | 先完成或并行明确 R0 契约 |

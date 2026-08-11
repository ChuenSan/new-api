# PRD: /v1/messages → /v1/chat/completions 深度转换

> 分支：`codex/deep-convert-messages2chat`
> 参考：CC Switch 协议转换思路，仅按 new-api 现有 adaptor、RelayFormat、RelayInfo 链路适配
> 范围：仅 Anthropic Messages → OpenAI Chat Completions
> 策略：第一版无渠道级开关；不要求 cache token 计费精度，但协议 usage 必须完整可用

---

## 1. 目标

在 new-api 现有 Claude → OpenAI 转换骨架上补齐请求、非流式响应和流式响应的深度兼容，使严格 Anthropic Messages 客户端通过 `/v1/messages` 接入 OpenAI Chat Completions 上游时，可以稳定完成文本、多模态、thinking、工具调用和多轮工具回传。

本任务不新建通用协议转换层，不扩展其他协议方向。

## 2. 已确认的代码事实

| 环节 | 现状 |
|---|---|
| 路由 | `/v1/messages` 进入 `RelayFormatClaude` |
| 入口 | `relay/claude_handler.go` `ClaudeHelper` |
| 基础请求转换 | `service.ClaudeToOpenAIRequest` |
| OpenAI 请求适配 | `relay/channel/openai.Adaptor.ConvertClaudeRequest` → `ConvertOpenAIRequest` |
| URL 改写 | OpenAI adaptor 可将 Claude 入口改写为 Chat Completions；Azure 有独立分支 |
| 非流式响应 | `OpenaiHandler` → `ResponseOpenAI2Claude` |
| 流式响应 | `OaiStreamHandler` → `HandleStreamFormat` → `StreamResponseOpenAI2Claude` |
| 共享调用风险 | `ClaudeToOpenAIRequest` 同时被 Responses 桥接、Ali 及其他中间转换路径复用 |

当前链路已有骨架，但不能把所有深度规则无条件加入共享转换函数，否则会影响本任务范围外的 Responses、Gemini 和原生 Messages 路径。

## 3. R0 目标协议隔离

1. 调用点必须显式表达最终目标协议是否为 OpenAI Chat Completions。
2. Chat 专属深度规则仅在最终上游请求确认为 Chat Completions 时启用。
3. 不得仅根据模型名、厂商名或模糊的“OpenAI 系渠道”判断是否启用。
4. 可以扩展 `ClaudeToOpenAIRequest` 的 options/target 参数，或增加最小包装函数；不得新建独立通用转换层。
5. 以下路径行为必须保持不变：
   - `ClaudeHelper` 的 Chat Completions via Responses 分支
   - Claude → OpenAI → Gemini 中间转换
   - 原生 Anthropic adaptor
   - DeepSeek、Moonshot 等原生 Messages 路径
   - pass-through request body
   - Advanced Custom 未显式配置 Anthropic Messages → OpenAI Chat Completions 的路径
6. Advanced Custom 仅在 converter 明确为 `anthropic_messages_to_openai_chat_completions` 时启用。

## 4. R1 请求深度转换

### 4.1 顶层字段

| Anthropic 字段 | OpenAI Chat 字段 | 规则 |
|---|---|---|
| `model` | `model` | 使用模型映射后的上游模型名 |
| `max_tokens` | `max_tokens` | 基础转换保持 `max_tokens`；o-series/GPT-5 的 `max_completion_tokens` 切换复用 OpenAI adaptor 现有逻辑，不在转换器重复实现 |
| `temperature` | `temperature` | 透传，后续由目标 adaptor 处理模型限制 |
| `top_p` | `top_p` | 透传，后续由目标 adaptor 处理模型限制 |
| `top_k` | `top_k` | 保持现有兼容字段行为，不新增模型推断 |
| `stop_sequences` | `stop` | 单值输出 string，多值输出 array |
| `stream` | `stream` | 透传 |
| `tool_choice` | `tool_choice` | `auto`→`auto`；`any`→`required`；`none`→`none`；指定 tool → OpenAI function selector |
| `tool_choice.disable_parallel_tool_use` | `parallel_tool_calls` | 显式存在时取反映射 |
| `thinking` / `output_config.effort` | `reasoning_effort` 或 adaptor 已有 reasoning 字段 | 仅复用项目已有模型检测和 adaptor 兼容逻辑，不新增通用模型前缀表 |
| `metadata` | — | 第一版明确丢弃 |
| `service_tier` / `mcp_servers` / `context_management` / `container` 等 Anthropic 专有字段 | — | 不进入 OpenAI Chat 请求 |

`stream_options.include_usage` 继续由目标 adaptor 按现有能力注入；不得在不支持的渠道强制保留。

### 4.2 system

1. string system 转成单条 system message。
2. array system 仅提取 text block，跳过空 block。
3. 非 OpenRouter Claude 兼容路径使用 `"\n"` 连接多个 system text block，禁止无分隔直接拼接。
4. OpenRouter Claude 兼容路径保留现有 media content 与 `cache_control` 行为。
5. system/developer role 的模型适配继续由 OpenAI adaptor 负责。

### 4.3 tools 与 schema

1. `tools[].input_schema` → `tools[].function.parameters`。
2. 第一版仅递归移除值为 `"uri"` 的 `format` 字段。
3. 递归范围至少覆盖 `properties`、`items`。
4. 保留 `$schema`、`$defs`、`additionalProperties`、`patternProperties` 等其他关键字，不做激进清洗。
5. tools 解析失败、工具缺少 name 或 schema 无法转换时返回明确的请求转换错误，不得静默丢弃全部工具。
6. `tool_choice` 仅在转换后仍存在可用工具时输出；无工具时不得留下悬空的强制工具选择。

### 4.4 messages

1. string content 原样转换。
2. content array 中的 text、image、thinking、tool_use 必须允许共存。
3. 存在 tool_use 时不得丢弃同一 assistant message 中的 text/image content。
4. `tool_use` → `assistant.tool_calls[]`：
   - 保留 `id`
   - `name` → `function.name`
   - `input` 使用稳定 JSON 序列化写入 `function.arguments`
5. `tool_result` → 独立 `role:"tool"` 消息：
   - `tool_call_id=tool_use_id`
   - string content 原样使用
   - array/object content 序列化为 JSON 字符串
   - 序列化失败必须返回错误
6. image：
   - `source.type=base64` → `data:{media_type};base64,{data}`
   - `source.type=url` → 直接使用 URL
   - `source=nil`、空数据、空 URL、未知 source type 返回请求转换错误，禁止 panic
7. assistant thinking 历史块：
   - 仅在目标 Chat 上游现有兼容能力明确需要时写入 `reasoning_content`
   - 通用严格 OpenAI Chat 路径不发送非标准 reasoning 字段
   - `redacted_thinking` 的降级值必须稳定且有单测
8. 转换后空消息不发送；tool call assistant message允许 `content:null`。

## 5. R2 非流式响应深度转换

1. `OpenaiHandler` 在进入转换前校验响应结构：
   - `choices` 为空时返回 bad response
   - 本任务只转换 `choices[0]`，不得拼接多个 choice
2. content block 顺序固定：
   - thinking
   - text/refusal
   - tool_use
3. `message.reasoning_content` 或 `message.reasoning` → `{type:"thinking",thinking}`。
4. `message.content`：
   - string → text block
   - array 中 text/output_text → text block
5. 扩展 OpenAI response message DTO 以接收 `refusal`：
   - message-level refusal → text block
   - content part refusal → text block
6. `tool_calls[]` → tool_use block。
7. legacy `function_call` 必须与 `finish_reason:"function_call"` 成套支持并转换为单个 tool_use；不能只映射 stop reason。
8. tool arguments：
   - 合法 JSON object → `input`
   - 空或非法 JSON → `{}` 并记录 warn
9. finish reason：
   - `stop` → `end_turn`
   - `length` / `max_tokens` → `max_tokens`
   - `tool_calls` / `function_call` → `tool_use`
   - `content_filter` → 沿用项目现有 `refusal`
   - 未知值 → `end_turn` 并记录 warn
10. usage：
    - `prompt_tokens` 转换为 `input_tokens`
    - cache read/cache creation 字段不得与 `input_tokens` 重复计算
    - 缺失 usage 时返回非 nil 零值 usage
    - 不要求计费精度，但不得产生负 token

## 6. R3 流式响应状态机

### 6.1 终端事件单点化

1. `message_delta` 与 `message_stop` 只能由 finalizer 发送。
2. `StreamResponseOpenAI2Claude` 只产生 block-level events。
3. 每条成功流最多发送一次 `message_delta` 和一次 `message_stop`。
4. `finish_reason` 首个非空值生效，后续不得覆盖。
5. usage 使用最后一个非 nil 上游值。

### 6.2 最后 chunk 与 EOF

1. scanner 完成后必须 flush 尚未交给 converter 的最后一个 data chunk。
2. usage-only chunk 只更新 usage，不得提前关闭消息。
3. 正常 EOF 无 finish reason 时：
   - 幂等关闭未结束 content block
   - 使用 `end_turn`
   - 发送一次 terminal events
4. block 已关闭时 finalizer 不得重复发送 `content_block_stop`。

### 6.3 thinking、text 与工具块

1. thinking/text 类型切换前必须关闭旧 block。
2. 所有 `delta.tool_calls[]` 都必须处理，不得只读取第一个。
3. upstream tool index 映射到稠密 Anthropic content index。
4. 每个工具维护独立状态：ID、name、是否已 start、pending arguments。
5. arguments 先于 ID/name 到达时先缓存，信息完整后发送 start 再 flush。
6. finish 时：
   - 信息完整的 pending tool 补发 start
   - 缺少 ID 时生成稳定 placeholder
   - 缺少 name 的无效工具丢弃并记录 warn

### 6.4 流内错误

1. 检测普通 `data` chunk 中的 `{"error":...}`。
2. 错误仅发送一次 Anthropic `event:error`。
3. 错误后立即停止读取和转换。
4. 错误流不得补发成功的 `message_delta` 或 `message_stop`。

### 6.5 UTF-8

现有 `StreamScannerHandler` 使用按行扫描。第一版先增加多字节字符跨底层 read 边界测试；测试通过时不新增额外 UTF-8 缓冲层。

## 7. R4 非流式伪 SSE 兜底

1. 处理位置为 `OpenaiHandler`：完整读取 body 后，首次 JSON 解析失败时执行。
2. 仅在 `RelayFormatClaude` 且最终目标为 OpenAI Chat Completions 时启用。
3. SSE 嗅探成功后聚合：
   - content/reasoning 增量
   - tool_calls 增量
   - 首个非空 finish reason
   - 最后一个非 nil usage
4. 聚合结果构造成单个 `OpenAITextResponse`，再进入 `ResponseOpenAI2Claude`。
5. 流内 error 必须返回上游错误，禁止聚合为成功响应。
6. 无 choice，或既无 finish reason 也无完成标记时按截断响应处理。
7. 该兜底不得放入 `handleClaudeFormat`，因为错误标记为 JSON 的非流式响应不会进入流式 handler。

## 8. 生效矩阵

| 路径 | 深度转换 |
|---|---|
| `/v1/messages` → 明确的 OpenAI Chat Completions 上游 | 启用 |
| Azure Chat Completions | 启用 |
| OpenRouter Chat Completions | 启用 |
| Advanced Custom 显式 Messages → Chat converter | 启用 |
| Chat Completions via Responses | 禁用 |
| Claude → Gemini | 禁用 |
| 原生 Anthropic Messages | 禁用 |
| DeepSeek/Moonshot 原生 Messages | 禁用 |
| pass-through body | 禁用 |

最终实现必须由代码级目标协议标记落实该矩阵，不能只依赖文档约定。

## 9. 非目标

- Anthropic Messages → OpenAI Responses
- Anthropic Messages → Gemini
- OpenAI/Gemini/Responses → Anthropic 的其他入口扩展
- 渠道级、模型级开关
- 新建通用 IR 或转换框架
- cache token 计费精度改造
- 原生 Claude adaptor 行为调整

## 10. 测试矩阵

### 请求单测

- system string / array / 空 block
- tool_choice 四种类型
- `disable_parallel_tool_use`
- tools schema 递归清理
- tools 非法输入返回错误
- text + image + tool_use 混合 assistant message
- image base64 / URL / nil source / 未知 source type
- tool_use + tool_result 多轮
- thinking / redacted_thinking 保留与丢弃
- 无工具时不输出悬空 tool_choice
- o-series/GPT-5 复用 adaptor 的 max token 规范化

### 非流式响应单测

- thinking + text + 多 tool_calls 顺序
- refusal 两种形态
- legacy function_call
- finish reason 全映射
- 未知 finish reason 降级
- 空 choices
- 合法/非法 tool arguments
- cache usage 不重复计算
- usage 缺失返回零值

### 流式单测

- 纯文本完整事件序列
- thinking → text 切换
- text → 多 tool call
- arguments 早于 ID/name
- 首 chunk 多 tool call
- 多 finish reason first-wins
- finish 后 usage-only chunk
- 无 finish reason EOF
- 最后 chunk flush
- 流内错误只发送一次
- 错误后无成功 terminal events
- UTF-8 多字节字符跨底层 read 边界

### handler 与范围回归

- `stream:false` JSON 响应
- `stream:false` 伪 SSE 响应
- 伪 SSE error / 截断
- 最终上游 URL 为 Chat Completions
- 最终发送 body 不含 Anthropic 专有字段
- Responses 桥接行为零变化
- Gemini 中间转换行为零变化
- 原生 Anthropic、DeepSeek、Moonshot 行为零变化
- Advanced Custom 未指定 converter 时零变化
- pass-through 行为零变化

## 11. 验收标准

1. Claude Code 经 `/v1/messages` 接 OpenAI Chat Completions 上游，可完成含 thinking、图片、工具调用和 tool_result 的多轮对话。
2. 非流式响应符合 Anthropic Messages JSON 结构。
3. 流式响应满足 block 生命周期和 terminal event 单次语义。
4. 上游流内错误不会被包装为成功响应。
5. 伪 SSE 可以在非流式入口正确聚合或返回明确错误。
6. Responses、Gemini、原生 Messages、pass-through 路径行为零变化。
7. 新增测试覆盖 R0-R4 的正向和负向规则。
8. `go build ./...` 通过。
9. 受影响包测试通过。

# PRD: /v1/messages -> /v1/chat/completions 深度转换 v2

> 分支: `codex/deep-convert-messages2chat`
> 代码依据: 当前 new-api 仓库
> 范围: 仅 Anthropic Messages 入站、OpenAI Chat Completions 上游
> 原则: 复用现有 adaptor 与转换骨架，不新增通用协议转换层

---

## 1. 目标

在 new-api 现有 Claude -> OpenAI 转换骨架上，补齐请求、非流式响应、流式响应三条链路的深度字段映射和兼容性处理，使严格 Anthropic Messages 客户端可以通过 `/v1/messages` 调用最终协议为 `/v1/chat/completions` 的上游。

本任务只处理以下闭环：

```text
Anthropic Messages request
  -> OpenAI Chat Completions request
  -> OpenAI Chat Completions response/SSE
  -> Anthropic Messages response/SSE
```

不处理 Messages -> Responses、Messages -> Gemini 或 Messages -> 原生 Anthropic。

## 2. 已确认的代码事实

### 2.1 现有骨架

| 环节 | 现有实现 | 结论 |
|---|---|---|
| `/v1/messages` 入口 | `relay/claude_handler.go` `ClaudeHelper` | 已有 |
| Claude -> OpenAI 基础请求转换 | `service/convert.go` `ClaudeToOpenAIRequest` | 已有，字段不完整 |
| OpenAI Chat URL | `relay/channel/openai/adaptor.go` `GetRequestURL` | 已有 |
| OpenAI 请求模型兼容 | `relay/channel/openai/adaptor.go` `ConvertOpenAIRequest` | 已有 |
| 非流式 OpenAI -> Claude | `service/convert.go` `ResponseOpenAI2Claude` | 已有，字段不完整 |
| 流式 OpenAI -> Claude | `service/convert.go` `StreamResponseOpenAI2Claude` | 已有，终结和错误处理需增强 |
| OpenAI 非流式 handler | `relay/channel/openai/relay-openai.go` `OpenaiHandler` | 已有 |
| OpenAI 流式 handler | `relay/channel/openai/relay-openai.go` `OaiStreamHandler` | 已有 |

### 2.2 `ClaudeToOpenAIRequest` 是共享转换函数

当前直接调用点包括：

- OpenAI adaptor
- Ali adaptor 的 OpenAI compatible 路径
- `ClaudeHelper` 的 Chat Completions via Responses 路径

其他 adaptor 还可能通过 OpenAI adaptor 间接使用该函数，例如 Gemini、SiliconFlow 和 Advanced Custom。

因此，不能把 Chat Completions 专属规则无条件加入共享函数，否则会改变本次明确排除的协议路径。

### 2.3 已有模型兼容能力必须复用

OpenAI adaptor 已处理：

- o-series、GPT-5 的 `max_tokens -> max_completion_tokens`
- o-series、GPT-5 不兼容采样参数清理
- system -> developer role
- 模型后缀中的 reasoning effort
- OpenRouter `reasoning` 对象

本任务不得在 `ClaudeToOpenAIRequest` 中复制第二套模型注册表或相同适配逻辑。

## 3. 核心约束

### R0 最终目标协议隔离

所有新增深度规则必须由调用点显式声明最终目标为 OpenAI Chat Completions。

实现必须满足：

1. 基础 Claude -> OpenAI 结构转换可以继续被其他路径复用。
2. Chat Completions 专属增强只在明确的 Chat 目标模式下启用。
3. 不允许仅根据模型名称、`RelayFormatClaude` 或模糊的“OpenAI 系渠道”判断。
4. 不新增独立通用转换层；目标模式应作为现有转换链路的参数、选项或上下文契约。

### R0.1 必须启用的路径

| 路径 | 条件 |
|---|---|
| OpenAI adaptor | 最终 URL 为 Chat Completions，且未进入 Responses 桥接 |
| Azure | 由 OpenAI adaptor 将 `/v1/messages` 改写为 Azure Chat Completions |
| OpenRouter/Xinference | 使用 OpenAI adaptor 且最终协议为 Chat Completions |
| Advanced Custom | converter 明确为 `anthropic_messages -> openai_chat_completions` |
| 兼容 adaptor | 调用点明确声明最终上游请求和响应均为 OpenAI Chat Completions |

### R0.2 必须排除的路径

- `ShouldChatCompletionsUseResponsesGlobal(...) == true`
- Gemini adaptor 的 Claude -> OpenAI -> Gemini 中间转换
- 原生 Anthropic adaptor
- DeepSeek、Moonshot 等原生 Messages 路径
- Ali 原生 Anthropic Messages 路径
- Advanced Custom 的 native/其他 converter
- 全局或渠道级 pass-through body

## 4. 请求转换

### R1 顶层字段

| Anthropic 字段 | Chat Completions 字段 | 规则 |
|---|---|---|
| `model` | `model` | 沿用模型映射后的值 |
| `max_tokens` | `max_tokens` | 基础转换保留；最终模型规范化交给 adaptor |
| `temperature` | `temperature` | 基础转换保留；不兼容模型由 adaptor 清理 |
| `top_p` | `top_p` | 基础转换保留；不兼容模型由 adaptor 清理 |
| `top_k` | `top_k` | 保持现有兼容字段行为，不新增全局删除 |
| `stop_sequences` | `stop` | 单值为 string，多值为 array |
| `stream` | `stream` | 透传 |
| `metadata` | - | 第一版丢弃 |
| `service_tier` | - | 第一版不由本转换注入 |
| `mcp_servers` | - | 丢弃 |
| `context_management` | - | 丢弃 |
| `container` | - | 丢弃 |
| `output_format` | - | 丢弃 |

### R2 reasoning 映射

1. `output_config.effort` 使用现有 `ClaudeRequest.GetEfforts()` 解析。
2. o-series/GPT-5 判定复用 `dto.IsOpenAIReasoningOModel` 和 `dto.IsOpenAIGPT5Model`。
3. OpenRouter 保持现有 `thinking -> reasoning` 对象语义，不改成通用 `reasoning_effort`。
4. 标准 OpenAI reasoning 模型可以将明确的 effort 写入 `ReasoningEffort`。
5. `thinking.budget_tokens` 不在本任务中建立新的通用 low/medium/high 阈值表。
6. adaptor 已有 reasoning 逻辑优先于 Chat 深度转换，禁止重复覆盖。

### R3 tools 与 tool_choice

#### R3.1 tools

- `tools[].name -> tools[].function.name`
- `tools[].description -> tools[].function.description`
- `tools[].input_schema -> tools[].function.parameters`
- 递归清理 `properties` 和 `items` 中的 `format:"uri"`
- 第一版不扩展删除 `$defs`、`patternProperties`、`additionalProperties` 等关键字
- tools 解析失败必须返回转换错误，禁止静默变为空数组
- 空名称工具不得生成无效 OpenAI function

#### R3.2 tool_choice

| Anthropic | OpenAI |
|---|---|
| `auto` / `{type:"auto"}` | `"auto"` |
| `any` / `{type:"any"}` | `"required"` |
| `none` / `{type:"none"}` | `"none"` |
| `{type:"tool",name:"x"}` | `{type:"function",function:{name:"x"}}` |

若 `tool_choice.disable_parallel_tool_use` 存在：

```text
parallel_tool_calls = !disable_parallel_tool_use
```

没有有效 tools 时不得生成强制指定某个 function 的 `tool_choice`。

### R4 system 消息

1. string system 转为一条 system message。
2. array system 只消费 text block。
3. 空 text block 跳过。
4. 非 OpenRouter Claude 模型将多个 text block 使用 `"\n"` 连接。
5. OpenRouter Claude 模型沿用现有 media content 与 `cache_control` 行为。
6. 最终 system/developer role 调整继续由 OpenAI adaptor 负责。

### R5 content block

#### R5.1 text

- `text`、`input_text` 转为 OpenAI text content。
- 单一纯文本 block 可以简化为 string。

#### R5.2 image

| `source.type` | 转换 |
|---|---|
| `base64` | `data:{media_type};base64,{data}` |
| `url` | 直接使用 `source.url` |

必须校验：

- `source != nil`
- base64 source 的 `media_type` 和 `data` 非空
- URL source 的 URL 非空
- 未知 source type 返回转换错误

#### R5.3 tool_use

- 转为当前 assistant message 的 `tool_calls[]`
- `id`、`name` 必须保留
- `input` 序列化为 JSON arguments
- 序列化失败返回转换错误

#### R5.4 tool_result

- 每个 tool_result 转为独立 `role:"tool"` 消息
- `tool_call_id = tool_use_id`
- string content 原样使用
- 非 string content 序列化为 JSON string
- 序列化失败返回转换错误

#### R5.5 混合内容

同一 Anthropic message 同时包含 text/image 和 tool_use 时：

- OpenAI message 同时保留 `content` 与 `tool_calls`
- 不得因存在 tool call 丢弃 text/image
- 只有 tool call 且无媒体内容时，`content = null`

#### R5.6 thinking 历史块

- 通用 OpenAI Chat 路径默认不发送非标准 `reasoning_content`
- 明确要求保留 reasoning 的兼容路径，可将 assistant thinking block 合并到 `reasoning_content`
- `redacted_thinking` 不尝试解密
- 是否需要 reasoning 占位符必须由目标兼容策略显式声明，不能全局注入

## 5. 非流式响应转换

### R6 响应结构

1. 仅转换 `choices[0]`。
2. `choices` 为空时返回上游响应错误。
3. 不拼接多个 choice。
4. `message.reasoning_content` 或 `message.reasoning` 转为 thinking block，并位于 text block 之前。
5. string content 转为 text block。
6. array content 中的 text/output_text 转为 text block。
7. message-level refusal 和 content part refusal 转为 text block。
8. `tool_calls[]` 转为 tool_use block。
9. legacy `function_call` 在没有 tool_calls 时转为单个 tool_use block。

### R7 OpenAI response DTO

为支持 R6，OpenAI response message DTO 必须能够接收：

- `refusal`
- legacy `function_call`

新增字段不得改变现有请求序列化结果，必须使用 `omitempty` 或独立响应 DTO 结构避免无关输出。

### R8 finish_reason

| OpenAI | Anthropic |
|---|---|
| `stop` | `end_turn` |
| `length` / `max_tokens` | `max_tokens` |
| `tool_calls` | `tool_use` |
| `function_call` | `tool_use` |
| `content_filter` | `refusal` |
| 空值/未知值 | `end_turn` |

未知值必须记录日志，但不得将未知字符串直接返回给 Anthropic 客户端。

### R9 usage

1. `prompt_tokens -> input_tokens`
2. `completion_tokens -> output_tokens`
3. cache read/creation 字段写入对应 Claude usage 字段
4. 同一批 cache token 不得同时计入 input_tokens
5. 所有扣减结果不得小于 0
6. 无 usage 时仍生成非 nil 的零值 Claude usage
7. 本任务不要求计费精度变更，但转换后的 usage 必须结构合法

## 6. 流式响应转换

### R10 事件责任单点化

`message_delta` 和 `message_stop` 只能由最终收尾函数发送。

`StreamResponseOpenAI2Claude` 只负责：

- `message_start`
- `content_block_start`
- `content_block_delta`
- `content_block_stop`
- 缓存 finish reason 和 usage

converter 内任何路径不得发送成功 terminal events。

### R11 finish 与 usage 状态

- 首个非空 finish reason 生效，后续不得覆盖
- 非 nil upstream usage last-wins
- usage-only chunk 只更新 usage，不关闭消息
- finish chunk 关闭当前 block，但不发送 terminal events
- 无 finish reason 的正常 EOF 使用 `end_turn`
- 无 usage 时 finalizer 使用零值 usage

### R12 最后 chunk

`OaiStreamHandler` 在 scanner 结束后必须处理缓存的最后一个非空 data chunk，然后再执行最终收尾。

禁止：

- 最后一个 chunk 只用于 metadata 而未进入 Claude converter
- finalizer 再次重复转换同一 chunk
- 空 `lastStreamData` 被强制 JSON 解码

### R13 tool call 状态

每个 upstream `tool_calls[].index` 独立维护：

- Anthropic content block index
- id
- name
- started
- pending arguments

要求：

- 多 tool call 必须全部遍历
- upstream index 不要求从 0 开始或连续
- Anthropic index 按首次出现顺序稠密分配
- arguments 先于 id/name 时缓存
- id/name 完整后发送 start，再 flush pending arguments
- finish 时只关闭已经 start 的 block
- block stop 必须幂等

### R14 流内错误

- 检测普通 data chunk 中的 `{"error":...}`
- 同一流最多发送一次 Anthropic `event:error`
- 设置 stream error 状态并停止后续转换
- 错误后禁止发送 `message_delta` 和 `message_stop`
- 不得把流内错误包装成成功空响应

### R15 UTF-8

现有 `StreamScannerHandler` 使用按行 scanner。第一版先用测试验证中文、emoji 等多字节字符跨底层网络读取时不会产生非法 delta。

只有测试证明现有 scanner 会产生半截字符时，才增加额外 UTF-8 缓冲；不得预先新增第二套流切分器。

## 7. 非流式伪 SSE 兜底

### R16 触发位置

兜底必须位于 `OpenaiHandler`：

1. 读取完整 response body
2. 正常解析 OpenAI JSON
3. JSON 解析失败
4. `RelayFormat == RelayFormatClaude`
5. body 符合 SSE data 行特征
6. 聚合为单个 OpenAI Chat response
7. 调用现有非流式 Claude response converter

不得放在 `handleClaudeFormat`，因为错误标记为 `application/json` 的非流式响应不会进入流式 handler。

### R17 聚合要求

- 逐个解析 `data:` JSON chunk
- 忽略 `[DONE]`
- content 按顺序拼接
- reasoning 按顺序拼接
- tool_calls 按 index 聚合
- finish reason first-wins
- usage 使用最后一个非 nil 值
- 流内 error 返回上游响应错误
- 没有任何有效 choice 时返回错误
- 没有 finish reason 且没有 `[DONE]` 时按截断响应返回错误

## 8. 兼容性与回归约束

### R18 原生路径零变化

以下路径必须通过回归测试证明请求 body、URL 和响应处理未变化：

- Anthropic adaptor
- DeepSeek 原生 Messages
- Moonshot 原生 Messages
- Ali 原生 Messages

### R19 其他转换方向零变化

- Messages -> Responses
- Messages -> Gemini
- OpenAI -> Claude
- OpenAI Chat <-> Responses
- 其他 Advanced Custom converter

### R20 pass-through 零变化

全局或渠道 pass-through 开启时：

- 不执行深度转换
- 不清洗 schema
- 不重写字段
- 保持现有原始 body 行为

## 9. 测试矩阵

### 9.1 请求转换单测

- system string
- system array 多 block、空 block
- OpenRouter Claude system cache_control
- text only
- base64 image
- URL image
- image source nil/未知类型/空字段
- tool_use
- tool_result string
- tool_result array
- text + tool_use 混合内容
- image + tool_use 混合内容
- tool_choice 四种形态
- disable_parallel_tool_use
- schema 嵌套 `format:"uri"`
- tools 解析失败
- thinking preserve on/off
- metadata 与 Anthropic 专有字段丢弃

### 9.2 非流式响应单测

- reasoning + text 顺序
- string content
- array text/output_text
- message refusal
- content part refusal
- tool_calls
- legacy function_call
- malformed tool arguments
- empty choices
- finish reason 全映射
- unknown finish reason
- usage cache 扣减
- nil/缺失 usage

### 9.3 流式单测

- 纯文本完整事件序列
- thinking -> text 块切换
- text -> tool 块切换
- 多 tool call
- 非连续/out-of-order upstream index
- arguments 早于 id/name
- finish reason first-wins
- usage-only tail
- 无 finish reason EOF
- last chunk flush
- event:error 一次性
- error 后无成功 terminal
- block stop 幂等
- 中文和 emoji UTF-8

### 9.4 handler/集成测试

- `/v1/messages` 最终 URL 为 `/v1/chat/completions`
- Azure Chat Completions URL
- OpenRouter Chat Completions
- Advanced Custom 指定 converter
- 非流式伪 SSE
- 伪 SSE tool call 聚合
- 伪 SSE error
- 伪 SSE 截断
- 原生 Claude 路径零变化
- Responses 桥接零变化
- Gemini 路径零变化
- pass-through 零变化

## 10. 验收标准

- [ ] AC1: `/v1/messages` 在明确 Chat 目标路径下发往 Chat Completions 上游
- [ ] AC2: Chat 深度规则不会进入 Responses、Gemini 或原生 Messages 路径
- [ ] AC3: tool_choice、parallel_tool_calls、tools schema 转换正确
- [ ] AC4: text/image/tool_use 混合消息无内容丢失
- [ ] AC5: tool_result 独立消息和序列化正确
- [ ] AC6: reasoning、refusal、tool_calls、legacy function_call 非流式转换正确
- [ ] AC7: finish reason 只输出合法 Claude stop reason
- [ ] AC8: 流式 terminal events 仅发送一次
- [ ] AC9: 多工具和乱序参数可以完整重组
- [ ] AC10: 流内错误仅上报一次且无成功终结事件
- [ ] AC11: 最后 chunk 不丢失、不重复处理
- [ ] AC12: usage 非 nil，cache token 不重复计入 input_tokens
- [ ] AC13: 非流式伪 SSE 可以聚合，错误和截断不会伪装成功
- [ ] AC14: 原生 Claude、Responses、Gemini、pass-through 行为零变化
- [ ] AC15: 新增映射均有单测或 handler 测试
- [ ] AC16: `go build ./...` 通过
- [ ] AC17: 受影响 package 测试通过

## 11. 非目标

- 新建通用 IR 或独立协议转换框架
- Messages -> Responses 深度增强
- Messages -> Gemini 深度增强
- 原生 Anthropic 请求或响应增强
- 新增渠道级或模型级开关
- 建立新的全局 reasoning 模型注册表
- 完整映射 Anthropic metadata
- 修改计费价格或额度结算规则
- 处理其他入站协议方向

## 12. 预期修改边界

| 文件/模块 | 预期职责 |
|---|---|
| `service/convert.go` | Claude/OpenAI 请求、响应和流式转换 |
| `dto/claude.go` | 复用现有 Claude DTO，必要时补严格解析 helper |
| `dto/openai_request.go` 或响应 DTO | refusal、legacy function_call 等响应字段 |
| `relay/channel/openai/adaptor.go` | 目标模式传递、复用模型规范化 |
| `relay/channel/openai/helper.go` | 流式 Claude 格式处理和错误状态 |
| `relay/channel/openai/relay-openai.go` | last chunk、finalizer、伪 SSE 非流式兜底 |
| `relay/common/relay_info.go` | 必要的 Claude 流状态和显式目标标识 |
| 对应 `_test.go` | 映射、状态机和范围隔离测试 |

任何新增修改超出上述边界前，必须先证明其属于 `/v1/messages -> /v1/chat/completions` 闭环。

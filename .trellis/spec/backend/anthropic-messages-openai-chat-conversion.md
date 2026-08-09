# Anthropic Messages To OpenAI Chat Conversion

## 1. Scope / Trigger

Apply this contract only when an Anthropic Messages request is converted to a confirmed OpenAI Chat Completions upstream. The controlling field is `RelayInfo.AnthropicMessagesToOpenAIChatCompletions`.

Set it only in these adaptor paths:

- OpenAI, Azure, or OpenRouter with `RelayFormatClaude` and `RelayModeChatCompletions`.
- Advanced Custom `/v1/messages` route whose converter is exactly `anthropic_messages_to_openai_chat_completions`.

It must remain false for pass-through requests, Responses API bridges, Gemini, native Anthropic-compatible adaptors, and every other Advanced Custom converter.

## 2. Signatures

```go
func ClaudeToOpenAIRequest(dto.ClaudeRequest, *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error)
func ResponseOpenAI2Claude(*dto.OpenAITextResponse, *relaycommon.RelayInfo) *dto.ClaudeResponse
func StreamResponseOpenAI2Claude(*dto.ChatCompletionsStreamResponse, *relaycommon.RelayInfo) []*dto.ClaudeResponse
func BuildClaudeUsageFromOpenAIUsage(*dto.Usage) *dto.ClaudeUsage
func StopOpenBlocksForFinalize(*relaycommon.RelayInfo) []*dto.ClaudeResponse
```

`EnsureClaudeConvertInfo` initializes the state maps before a Claude stream is read. It is safe with a nil `RelayInfo`.

## 3. Contracts

- Request conversion preserves text, images, tool calls, and tool results. Invalid tool names, schemas, images, or tool choices return an error.
- Remove only JSON Schema `format: "uri"` values recursively; preserve all other schema keywords.
- Non-stream response conversion consumes only `choices[0]`, emits thinking, text/refusal, then tools, and returns nil for no choices.
- Pseudo-SSE fallback is enabled only for non-stream strict conversion. It must see a choice, a non-empty finish reason, and `[DONE]`; error and truncated streams fail.
- Strict stream converter emits only block events. `HandleFinalResponse` owns `message_delta` and `message_stop`, each at most once.
- Tool-call arguments may precede id/name. Buffer them in `ToolBlockState.PendingArgs`; start a block only after both id and name are available.
- `ClaudeToolChoice.DisableParallelToolUse` is a pointer: an omitted field leaves `parallel_tool_calls` omitted, while explicit `false` maps to `true` and explicit `true` maps to `false`.
- `output_config.effort` maps to `reasoning_effort` only where the existing upstream adaptor supports that field; OpenRouter uses its `reasoning` object with `effort`/`max_tokens`.
- Historical `thinking` and `redacted_thinking` blocks are dropped for ordinary strict Chat. OpenRouter Claude compatibility may preserve them as `message.reasoning_details` using `reasoning.text` and `reasoning.encrypted` entries.
- Response `Message.StringContent` and parsed media recognize both `text` and `output_text` parts; block order remains thinking, text/refusal, then tool use.
- EOF finalization assigns `tool_call_<upstream-index>` when a pending tool has a name but no ID, and logs/drops pending tools without a name.
- Strict stream finalization uses the strict stop-reason map: `function_call`/`tool_calls` become `tool_use`, and unknown values become `end_turn` with a warning.
- A real upstream usage object, including all-zero tokens, sets `HasUpstreamUsage`. Estimated usage is only for billing fallback and never overwrites canonical Claude stream usage.

## 4. Validation And Error Matrix

| Condition | Result |
| --- | --- |
| Invalid request tool/schema/image/tool choice | `ConvertRequestFailed` path |
| No Chat completion choices | bad upstream response, never `null` success |
| Pseudo-SSE error, no choice, no finish, or no `[DONE]` | bad upstream response |
| Strict stream error chunk | one Anthropic `error` event, stop scanner, no success terminal |
| No stream finish reason at EOF | close open blocks and emit `end_turn` terminal |

## 5. Good / Base / Bad Cases

- Good: a Chat chunk ends with `finish_reason: "tool_calls"`; close every started block, then emit one `message_delta` with `tool_use`.
- Base: a valid text stream reaches EOF without upstream usage; emit a zero-valued Claude usage object.
- Bad: set the strict flag from input format alone. A Messages-to-Responses bridge would then receive Chat-only conversion semantics.

## 6. Tests Required

- Adaptor tests cover Chat enabled and Responses disabled, plus the exact Advanced Custom converter.
- Service tests cover mixed request content, invalid inputs, first-choice response conversion, empty choices, and delayed tool metadata.
- OpenAI relay tests cover pseudo-SSE aggregation, truncated/error pseudo-SSE rejection, a single strict stream terminal, and stream error without success terminal.
- Run `go test ./...` and `go build ./...`.

## 7. Wrong Vs Correct

### Wrong

```go
info.AnthropicMessagesToOpenAIChatCompletions = info.RelayFormat == types.RelayFormatClaude
```

This leaks Chat-only semantics into Responses and native protocol paths.

### Correct

```go
info.AnthropicMessagesToOpenAIChatCompletions =
    info.RelayFormat == types.RelayFormatClaude &&
    info.RelayMode == relayconstant.RelayModeChatCompletions &&
    supportedChatChannel(info.ChannelType)
```

The flag is an output-protocol contract, not an input-format label.

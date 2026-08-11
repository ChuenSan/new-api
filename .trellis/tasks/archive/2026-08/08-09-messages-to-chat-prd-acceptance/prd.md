# Complete Messages to Chat PRD acceptance

## Goal

Fix remaining strict Messages-to-Chat semantic gaps and complete the PRD acceptance matrix.

## Requirements

- Preserve fail-closed activation of `AnthropicMessagesToOpenAIChatCompletions`.
- For strict non-stream responses, distinguish an absent upstream usage field from an explicit usage object. Missing usage produces a non-nil zero-valued Claude usage and must not be replaced by estimated billing usage.
- For strict stream tools, allocate Anthropic content indices only when a block becomes sendable. A pending tool dropped for a missing name must not leave an index gap. A missing ID with a valid name receives the stable `tool_call_<upstream-index>` placeholder at finalization.
- Accept a pseudo-SSE response as complete when it has a valid choice and either a finish reason or `[DONE]`. Reject responses with neither completion signal.
- Keep legacy and non-target request, response, stream, finalizer, and pseudo-SSE behavior unchanged.
- Add targeted acceptance tests for the protocol, request, response, stream, and pseudo-SSE cases listed in the source PRD.

## Acceptance Criteria

- [ ] Strict non-stream responses with no usage return zero-valued Claude usage while explicit usage, cache usage, and negative values remain normalized correctly.
- [ ] Strict stream output uses dense Anthropic indices when invalid pending tools are dropped.
- [ ] Strict stream tests cover multi-tool chunks, pending metadata, placeholder IDs, missing names, first-wins finish reasons, usage-only chunks, EOF, UTF-8 read boundaries, and terminal/error uniqueness.
- [ ] Pseudo-SSE accepts either completion signal and rejects no-choice, error, and truly truncated bodies.
- [ ] Protocol isolation tests cover OpenAI, Azure, OpenRouter, Advanced Custom, Responses, Gemini, native Messages, DeepSeek/Moonshot, pass-through, and non-Claude stream callers.
- [ ] Request tests cover tool-choice variants, system forms, schema preservation, image variants, tool results, thinking isolation, and o-series/GPT-5 adaptor normalization.
- [ ] Non-stream tests cover `output_text`, refusal forms, legacy function calls, tool argument fallbacks, stop reasons, and usage boundaries.
- [ ] `go test ./...`, `go build ./...`, `git diff --check`, and Trellis validation pass.

## Notes

- Source requirement: `PRD-20260809T165650-cf59e78a-2d55-449e-a8b0-179dbeeca59c.md`.
- Do not modify unrelated untracked workspace files.

# Design: Complete Messages to Chat PRD acceptance

## Boundaries

The strict path remains controlled by `RelayInfo.AnthropicMessagesToOpenAIChatCompletions`. Shared converters and handlers retain their legacy branches when the flag is false.

## Non-stream usage

Decode the upstream response with usage-presence information. Existing estimated usage remains available for billing and non-Claude outputs, but strict Claude response conversion receives the original absent usage as zero values. Explicit usage objects continue through normalization, including cache subtraction and non-negative clamping.

## Stream tool indices

Pending tools are keyed by upstream index before they become sendable. Anthropic indices are assigned when a tool has a valid name and either a real ID or a finalizer-generated placeholder. Started tools retain their assigned index. Dropped nameless tools never consume an Anthropic index.

## Pseudo-SSE completion

Aggregation requires at least one valid choice. Completion is established by either the first non-empty finish reason or `[DONE]`; only the absence of both is treated as truncation. Finish reason remains first-wins and usage remains last-wins.

## Verification

Tests are split between service-level conversion invariants and handler-level protocol behavior. Isolation tests exercise the actual flag-setting and fallback guards rather than only direct converter calls.

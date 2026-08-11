# Implement plan: Complete Messages to Chat PRD acceptance

## Step 1: Semantic fixes

- [ ] Preserve upstream usage field presence through `OpenaiHandler`.
- [ ] Prevent estimated usage from entering strict Claude non-stream output.
- [ ] Defer strict tool content-index allocation until the block is sendable.
- [ ] Accept pseudo-SSE completion from finish reason or `[DONE]`.

## Step 2: Acceptance tests

- [ ] Expand adaptor protocol-isolation cases.
- [ ] Expand strict request conversion validation and adaptor normalization cases.
- [ ] Expand strict non-stream response and usage cases.
- [ ] Add strict stream converter and handler lifecycle cases.
- [ ] Expand pseudo-SSE completion and non-target fallback cases.

## Step 3: Quality gate

- [ ] Run affected package tests without cache.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./...`.
- [ ] Run `git diff --check`.
- [ ] Update the backend conversion spec.
- [ ] Validate and archive the Trellis task after commit.

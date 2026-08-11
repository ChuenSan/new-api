# Token Availability Mode

## 1. Scope / Trigger

Use this contract when changing token CRUD, relay retry control, channel selection state, or the `web/default` token form for availability mode. The mode moves repeated client retries into one New API request without replacing the existing channel selector.

## 2. Signatures

- DB/API: `model.Token.AvailabilityMode bool` → `availability_mode`, default `false`.
- Request context: `constant.ContextKeyTokenAvailabilityMode` → `token_availability_mode`.
- Selection reset: `(*service.RetryParam).ResetRound()` resets retry and per-round selector state.
- Frontend: `ApiKey.availability_mode` and `ApiKeyFormData.availability_mode` are booleans.

## 3. Contracts

- Token authentication, rate limiting, request parsing, and pre-consumption run once.
- When disabled, relay behavior remains governed by the existing `common.RetryTimes` loop.
- When enabled, every failed relay round starts another round with no count limit.
- Every round reuses `service.CacheGetRandomSatisfiedChannel`; priority, weight, auto-group, and model-route ordering remain selector-owned.
- `ResetRound()` clears `use_channel`, auto-group indexes, and the cached model-route chain so a later round may select a previously failed channel.
- An unavailable selector result waits before the next round to avoid a CPU busy loop.
- Realtime, a canceled client request, or a response that has emitted bytes stops transparent replay.
- Authentication, validation, and billing failures before relay attempts keep their existing immediate error behavior.

## 4. Validation & Error Matrix

| Condition | Behavior |
|---|---|
| Token field absent or `false` | Existing finite retry behavior |
| Initial selector has no channel | Enter Relay and wait for another availability round |
| Upstream failure before response bytes | Process the channel error, then continue rounds |
| Successful upstream response | Return success immediately |
| Stream has emitted bytes | Stop; do not replay into an already-started response |
| Request context canceled | Stop and release request resources |
| Realtime request | Keep existing WebSocket behavior |
| Invalid token or request | Return the existing local error; do not enter relay rounds |

## 5. Good / Base / Bad Cases

- Good: the first round fails, model-route overflow or a later weighted selection succeeds, and the client receives only the successful response.
- Base: an old token has no stored field, migrates to `false`, and behaves exactly as before.
- Bad: all channels are temporarily unavailable; the request must wait with a cancelable delay instead of returning 503 or spinning.

## 6. Tests Required

- Token migration asserts the column exists and legacy rows default to `false`.
- Token update asserts `availability_mode=true` persists.
- Authentication asserts the token flag reaches Gin context.
- Round reset asserts retry index, used channels, auto-group state, and model-route chain are cleared.
- Retry control asserts a large number of rounds is still accepted and cancellation/response-started states stop.
- Full `go test ./...`, `go build ./...`, frontend type-check, and frontend build must pass.

## 7. Wrong vs Correct

### Wrong

```go
for retry := 0; retry <= common.RetryTimes; retry++ {
    tryUpstream()
}
return lastError
```

This is finite failover and returns an intermediate failure.

### Correct

```go
for requestIsActive() {
    runExistingRetryRound()
    if success || responseStarted {
        return
    }
    retryParam.ResetRound()
}
```

The outer loop has no count limit and every new round reuses fresh selector state.

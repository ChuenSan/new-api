# Error Handling

> How errors are handled in this project.

---

## Overview

Go's standard `error` is the universal currency. There is **no panic-driven control flow** except `common.FatalLog` at startup. Errors are produced in two tiers:

1. **Domain / sentinel errors** — plain `errors.New` values in `model/errors.go` and ad-hoc `fmt.Errorf` at call sites.
2. **API error payloads** — structured error objects in `service/error.go` + `types/error.go` + `dto/`, shaped per upstream protocol (OpenAI / Claude / Midjourney).

A Gin panic anywhere in a relay request is caught by `middleware/recover.go:RelayPanicRecover` and turned into a `500` JSON `{ "error": { "message", "type":"new_api_panic" } }`.

---

## Error Types

### Sentinel errors — `model/errors.go`

Plain package-level `error` vars grouped by domain:

```go
var ErrDatabase = errors.New("database error")
var (
    ErrInvalidCredentials   = errors.New("invalid credentials")
    ErrEmailAlreadyTaken    = errors.New("email already taken")
)
var ErrTokenNotProvided = errors.New("token not provided")
var ErrRedeemFailed      = errors.New("redeem.failed")
```

Match with `errors.Is`, never string comparison. Add new sentinels here when a caller needs to branch on a specific condition.

### `types.NewAPIError` — `types/error.go`

The error used on the relay path. Wraps an OpenAI-shaped error body + a `types.ErrorCode` (`types.ErrorCodeBadResponseStatusCode`, ...) + HTTP status. Constructors: `types.InitOpenAIError`, `types.NewOpenAIError`, `types.WithOpenAIError`. Supports `Unwrap()`, `MaskSensitiveError()` (redacts upstream URL/keys before returning to client), and status-code remapping via `service.ResetStatusCode`.

---

## Error Handling Patterns

- **Wrap, don't swallow**: return `fmt.Errorf("bad response status code %d, message: %s, body: %s", ...)` (`service/error.go` `buildErrWithBody`) — preserve context, let the caller decide.
- **Sanitize before surfacing**: `service/error.go` `ClaudeErrorWrapper` / `OpenAIErrorWrapper` inspect `err.Error()`, log the raw text via `common.SysLog` when it looks like a network/dial failure, then replace the user-facing message with `请求上游地址失败` so the upstream URL never leaks.
- **Log then map**: `service.RelayErrorHandler(ctx, resp, showBodyWhenFail)` parses the upstream body, attempts `dto.GeneralErrorResponse.TryToOpenAIError()`, and otherwise logs via `logger.LogError(ctx, ...)` with `common.LocalLogPreview` (truncated body) and returns a status-only error. Always pass `ctx` for request-id correlation.
- **Panic recovery**: `middleware/recover.go` defers `recover()`, logs `panic detected: %v` + `debug.Stack()` via `common.SysLog`, and writes the `500 new_api_panic` response.
- Use `errors.Is` / `errors.As` for sentinel/type checks; do not compare `.Error()` strings.

---

## API Error Responses

There is no single global error middleware that maps errors → responses. Two distinct error shapes, **split by surface**:

- **Admin / console endpoints (`controller/`)** use the shared helper `common/gin.go:199`:

  ```go
  common.ApiError(c, err)               // c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
  common.ApiErrorMsg(c, msg)            // fixed message
  common.ApiErrorI18n / ApiSuccessI18n // i18n variants
  ```
  **Frontend contract**: business failures return `http.StatusOK` with `{"success": false, "message": ...}` — **not** an HTTP error status code. Many handlers still write `c.JSON(http.StatusOK, gin.H{...})` inline (`controller/channel.go:126,164,180`); prefer the `common.Api*` helper in new code.

- **Relay endpoints** return protocol-shaped error wrappers from `service/error.go` — this is a **deliberately separate** path from `common.ApiError`:
  - `service.ClaudeErrorWrapper(err, code, statusCode)` → `*dto.ClaudeErrorWithStatusCode` (Claude)
  - `service.MidjourneyErrorWithStatusCodeWrapper(code, desc, statusCode)` (Midjourney)
  - `service.TaskErrorWrapper` / `TaskErrorFromAPIError` (async tasks)
  - `service.RelayErrorHandler(ctx, resp, showBodyWhenFail)` → `*types.NewAPIError` (central upstream-error normalizer, called explicitly by every relay handler: `relay/rerank_handler.go:91`, `relay/embedding_handler.go:79`, `relay/audio_handler.go:57`, `relay/gemini_handler.go:190`, `relay/compatible_handler.go:200`)
  - The `...Local` variants set `LocalError=true` to mark the error as originating locally vs upstream.
  - The legacy `OpenAIErrorWrapper` is **commented out** in `service/error.go:34-58` — kept for reference, do not restore.

- **Panic fallback** (`middleware/recover.go`): the only automatic error middleware; writes

  ```go
  c.JSON(http.StatusInternalServerError, gin.H{
      "error": gin.H{"message": ..., "type": "new_api_panic"},
  })
  ```
  then `c.Abort()`. Do **not** invent a `middleware/error.go` / `middleware/relay_error.go` — relay handlers convert errors manually via `service.RelayErrorHandler`, not via gin error middleware.

---

## Common Mistakes

- Comparing error strings (`if err.Error() == "..."` / `strings.Contains(err.Error(), ...)`) instead of `errors.Is(err, model.ErrTokenInvalid)` / `errors.Is(err, gorm.ErrRecordNotFound)` (the project even flags its own string-match debt in `service/billing_session.go:216` TODO).
- Leaking the upstream URL / API key in the response — always go through the `service/error.go` sanitizers, or use `types.NewAPIError.MaskSensitiveError()`.
- Logging the full upstream response body at INFO — use `common.LocalLogPreview` (truncated) and gate verbose logging behind `logger.LogDebug` / `common.DebugEnabled`.
- Calling `panic` inside a request path instead of returning an error; only `middleware/recover.go` is expected to handle panics, and only because it is the safety net.
- Creating a new sentinel in `model/errors.go` and forgetting to `errors.Is`-check it at the call site.

# Logging Guidelines

> How logging is done in this project.

---

## Overview

No third-party logging library. Logging is a **thin, in-house wrapper** over `fmt.Fprintf` into `gin.DefaultWriter` / `gin.DefaultErrorWriter`, with concurrent writer-swap protection for file rotation.

Two entry points, pick by call site:

- **`logger` package** (`logger/logger.go`) — context-aware, request-scoped. Takes `(ctx context.Context, msg string)`. Use inside request handlers / relay paths where a `context.Context` is available.
- **`common.SysLog` / `common.SysError` / `common.FatalLog`** (`common/sys_log.go`) — context-free. `[SYS]` / `[FATAL]` prefix. Use during startup, background tasks, and anywhere a request context does not exist.

---

## Log Levels

Four levels in `logger/logger.go`:

| Func | Tag | When |
|------|-----|------|
| `logger.LogInfo(ctx, msg)` | `INFO` | Normal lifecycle events (request start, channel selected, migration done). |
| `logger.LogWarn(ctx, msg)` | `WARN` | Degraded but functional state (fallback path taken, rate limit approaching). |
| `logger.LogError(ctx, msg)` | `ERR` | Operation failed but process continues (upstream call failed, DB error handled). |
| `logger.LogDebug(ctx, msg, args...)` | `DEBUG` | Verbose diagnostics; **no-op unless `common.DebugEnabled`** (env `DEBUG=true`). |

Context-free equivalents:

- `common.SysLog(s)` — `[SYS]`, startup / background.
- `common.SysError(s)` — `[SYS]` to stderr.
- `common.FatalLog(v...)` — `[FATAL]` then `os.Exit(1)`. Reserve for unrecoverable init failures only.

There is no separate WARN for `common`; use `common.SysError` or `logger.LogWarn`.

---

## Structured Logging

Format (single line, space-padded fields):

```
[LEVEL] YYYY/MM/DD - HH:MM:SS | requestID | msg
```

- `LEVEL` is one of `INFO` / `WARN` / `ERR` / `DEBUG` (`logger`), or `SYS` / `FATAL` (`common`).
- The middle field is the **request id** pulled from `ctx.Value(common.RequestIdKey)`, or the literal `SYSTEM` when ctx is nil / has no id. This is what ties a log line back to a single request — always pass `ctx` through so the id is preserved.
- INFO writes to `gin.DefaultWriter`; WARN/ERR/DEBUG write to `gin.DefaultErrorWriter`. Both may be a `MultiWriter` combining stdout + the rotating log file (`SetupLogger`).

### Request id propagation

`middleware/request-id.go` generates `common.NewRequestId()`, sets it on `c.Set(common.RequestIdKey, id)` and on `c.Request.Context()`, and emits response header (`common.RequestIdKey = "X-Oneapi-Request-Id"`, `common/constants.go:198`; the upstream id is `UpstreamRequestIdKey`). Any code that logs from a request must thread that `ctx` (or `c.Request.Context()`) into the logger call; dropping it loses the correlation id.

### Debug formatted logging

`logger.LogDebug(ctx, msg, args...)` accepts `fmt.Sprintf`-style args and only formats/expands them when `common.DebugEnabled` is true — cheap to leave in code. `logger.LogJson(ctx, msg, obj)` marshals to JSON under debug, **test-only**.

### Passing ctx

Inside a request: `logger.LogXxx(c.Request.Context(), msg)` (e.g. `controller/topup_creem.go:81,120`). Outside a request (background tasks): pass `context.Background()` explicitly rather than nil, so `id` falls back to `SYSTEM` predictably.

---

## What to Log

- Startup events: `common.SysLog("database migration started")` (`model/main.go`).
- Request lifecycle milestones for relay/admin actions.
- Upstream errors before they are sanitized for the client — `service/error.go` calls `common.SysLog(fmt.Sprintf("error: %s", text))` when it detects a dial/post failure and rewrites the user-facing message to `请求上游地址失败`.
- Panic recovery: `middleware/recover.go` logs `panic detected: %v` + full `debug.Stack()`.

---

## What NOT to Log

- **Secrets**: channel API keys, user tokens, passwords, `SESSION_SECRET` / `CRYPTO_SECRET`, DB/Redis DSNs. These live in `.env` and the `channels`/`tokens` tables; never echo them.
- **Full request/response bodies** at INFO level — only behind `logger.LogDebug` when `DEBUG=true`, and even then prefer truncation.

### Redaction before logging bodies

- `common.LocalLogPreview(content)` (`common/str.go:26`) truncates to `LocalLogContentLimit` **unless** `common.DebugEnabled`. Use it for any upstream response body / reason text before `logger.Log*` or `common.SysLog`. See `service/error.go:96`, `service/channel.go:20`.
- `service/task_polling.go:redactVideoResponseBody` strips base64 media before persisting task logs — domain-specific redactors live next to the producer, reuse them rather than logging raw payloads.
- `service/error.go:65-68` rewrites network-detail errors (`post`/`dial`/`http`) to the generic `请求上游地址失败` before returning to the client, on top of logging the raw text via `common.SysLog`.
- PII (raw emails, phone numbers) in `common.SysLog` lines that may be persisted to the log file.
- Don't log the original upstream error verbatim if it could leak the upstream URL/key; route through the `service/error.go` sanitization first.

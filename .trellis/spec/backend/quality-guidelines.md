# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

Standards enforced by convention rather than tooling. There is **no `.golangci.yml`** and no mandatory linter gate; correctness and consistency are enforced through code review and tests. The build target is `go build ./...`; tests live next to the code they exercise. Platform `darwin`/`linux` (deployed container), so keep Go std-lib-first and CGO-free where possible (SQLite uses `github.com/glebarez/sqlite`, the pure-Go driver — not `mattn/go-sqlite3`).

---

## Forbidden Patterns

- **Panic as control flow** inside request paths. Only `common.FatalLog` at startup and the `middleware/recover.go` safety net are expected. Return an `error` instead.
- **String-matching errors** (`if err.Error() == ...` / `strings.Contains(err.Error(), ...)` to branch). Use `errors.Is` / `errors.As` against sentinels in `model/errors.go`.
- **Hard-coded DB keywords** (`` `group` ``, `"group"`, `true`/`1` literals). Use `commonGroupCol` / `commonKeyCol` / `commonTrueVal` (see [Database Guidelines](./database-guidelines.md)).
- **Writing to global `model.DB` inside a `DB.Transaction`** instead of the `tx` arg.
- **Leaking secrets / upstream URLs** to the client or to INFO logs — route through `service/error.go` sanitizers and `types.NewAPIError.MaskSensitiveError()`.
- **Converting floats directly to int quota** without saturation — use `common/quota_math.go` `QuotaFromFloat` / `QuotaRound` (these clamp to `MaxQuota`/`MinQuota`, see `common/quota_math_test.go`).
- New `panic`/`os.Exit` outside `common.FatalLog`.

---

## Required Patterns

- **Context threading**: functions doing I/O, logging, or HTTP calls take `ctx context.Context` and propagate it — so the request id from `middleware/request-id.go` survives into `logger.Log*` calls and downstream clients. See `service/codex_oauth.go`, `service/error.go:RelayErrorHandler`.
- **Constants over magic values**: enums/enums-by-type live in `constant/` or as typed constants (`model/route_constants.go`, `types.ErrorCode`). Re-use existing ones before adding new.
- **Search before adding helpers** — per the project's [Code Reuse Thinking Guide](../guides/code-reuse-thinking-guide.md). Modifying a config/constant value requires a repo-wide `grep` first (the [Pre-Modification Rule](../guides/index.md#pre-modification-rule-critical)).
- **DB access only in `model/`**; business orchestration in `service/`; HTTP shaping in `controller/`. Respect the layer boundary — an inner package must not import an outer one.
- **Multi-DB-safe SQL**: every raw SQL path must be guarded by `common.UsingMainDatabase(...)` dialect checks.
- **Custom error shape per surface**: admin/console endpoints use `common.ApiError` / `ApiErrorMsg` with `http.StatusOK` + `{success:false}` (see [Error Handling](./error-handling.md)); relay endpoints use `service/error.go` protocol wrappers. Don't mix the two.
- **Custom `TableName()`** for newer tables, and `int64` Unix-millis timestamps via `BeforeCreate`/`BeforeUpdate` hooks (DB portability).
- Comments: terse, English, only where intent is non-obvious (`非必要不注释`). Keep the existing bilingual comments where present; don't strip them.

---

## Testing Requirements

- Tests are `_test.go` beside the package (`package model`, `package common`) — same-package so they can touch unexported symbols. Cross-package tests of internals use `_internal_test.go` (e.g. `controller/channel_test_internal_test.go`).
- Assertions via **`github.com/stretchr/testify`** (dominant; ~92 test files import it): `assert` for non-fatal checks (`common/quota_math_test.go`), `require` when preconditions must hold before proceeding (`model/locking_test.go`, `model/channel_delete_test.go`).
- **Test style is mixed, not mandated**: `common/` and `model/` lean toward free-form per-assertion tests (`TestQuotaFromFloat` — `assert.Equal` calls, no struct-slice table); `dto/` and `controller/` lean toward struct-slice table-driven tests (`dto/channel_settings_test.go`, `controller/model_route_test.go`). Match the neighbors in the package you're editing. Either way, give each test a **doc comment naming the invariant** it guards.
- Coverage is targeted, not blanket: numerical/billing (`common/quota_math`), DAO (`model/channel_*_test.go`), routing & shadow probe (`modelroute/`, 16 test files), and controller behavior (`controller/*_test.go`, 12 files) are the heavily-tested zones. New logic in those zones needs a test; pure glue may not.
- **Integration test** for cross-layer behavior: `integration/modelroute_chain_test.go`. Add here when validating a chain (request → route decision → relay → metric), not when a unit test suffices.
- Platform-conditional code uses `//go:build` tags (`common/system_monitor_windows.go` vs `system_monitor_unix.go`); mirror that split for any new OS-specific file.
- Validate before reporting "done": `go build ./...` and the package's tests must pass.

---

## Code Review Checklist

- [ ] Layering respected (no inner→outer import); DB writes only in `model/`, in a `tx` when transactional.
- [ ] Multi-DB safe: no hard-coded reserved words or dialect-specific SQL without a dialect guard.
- [ ] Errors handled, not swallowed; sentinel errors compared with `errors.Is`; secrets redacted before logging/responding.
- [ ] `ctx` threaded through logging and HTTP calls (request id preserved).
- [ ] No new panic/`os.Exit` in request paths; no string-matching on error messages.
- [ ] Quota / money math goes through `common/quota_math.go` with saturation.
- [ ] Tests added/updated for behavioral changes in the taxed zones; `go build ./...` + affected tests green.
- [ ] Touched a constant/config/option? Searched the repo for other consumers first.
- [ ] Comments/LF, file naming, package name match the neighbors (see [Directory Structure](./directory-structure.md)).

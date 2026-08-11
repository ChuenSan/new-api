# Model Route Reset to Unknown

## 1. Scope / Trigger

Use this contract when adding or changing the root-only operation that resets one `channel_id × effective_model` route to `UNKNOWN`. The operation spans the API, database snapshot, process-local routing state, audit log, and administration UI.

## 2. Signatures

```http
POST /api/model_route/metrics/action
Content-Type: application/json

{"channel_id": 1, "effective_model": "upstream-model", "action": "reset_unknown"}
```

```go
func ResetRouteToUnknown(channelID int64, effectiveModel string, requestedModels []string) error

func ResetChannelModelMetricsUnknown(
	channelID int64,
	effectiveModel string,
	runtime *ChannelModelMetrics,
) (*ChannelModelMetrics, error)
```

## 3. Contracts

- The router keeps `middleware.RootAuth()` on the `/api/model_route` group.
- `channel_id` must be positive; `effective_model` is trimmed and must be non-empty; the action is exactly `reset_unknown`.
- The metrics row must already exist. Never call `EnsureChannelModelMetrics` from the reset path.
- Hold the `MetricsKey` coordination lock while copying runtime learning, committing the database transaction, and invalidating runtime state.
- Preserve the latest learning snapshot and reset only `route_state`, `backoff_level`, `cooldown_until`, and `last_error_class`.
- After commit, clear only the target runtime metrics, temporary-failure window, dirty marker, and role. Invalidate every associated requested-model plan and clear only leases whose candidate matches the full `MetricsKey`.
- The UI action is row-only and dispatches immediately from the selector's `onValueChange` handler. It patches the matching query row immutably and owns exactly one success or backend-derived failure toast.
- Build the mutation payload only from the selected row: `{ channel_id, effective_model, action: 'reset_unknown' }`. Search text, aliases, requested models, and fuzzy matches must never determine the target.
- Base UI action selectors must receive localized `{ value, label }` entries through the root `items` prop and render menu entries from the same array. Otherwise `SelectValue` can expose the raw action value.
- Before the success patch, cancel the exact `['model-route-metrics']` query. Then write the immutable cache patch and invalidate the query in the background so an older in-flight response cannot overwrite the new state.

## 4. Validation & Error Matrix

| Condition | Response / effect |
| --- | --- |
| `channel_id <= 0`, blank model, or missing action | HTTP 400, `success=false`, no write or audit |
| Unsupported action | HTTP 400, `success=false`, no write or audit |
| Metrics row missing | Existing admin API error shape, no row creation, no cache eviction, no success audit |
| Database transaction fails | Failure response, all process-local state remains intact, no success audit |
| Reset succeeds | HTTP 200, `success=true`, targeted invalidation completes before audit |
| Reset is repeated | Success with the same four health-control values; learning remains intact |
| Localized row action is selected | Trigger displays the localized label, never `reset_unknown` |
| `reset_unknown` is selected | The frontend immediately posts the exact selected row key; no confirmation state may intercept the request |
| Metrics query is already in flight at reset success | Exact query is cancelled before the local patch; background invalidation fetches the current server state |

## 5. Good / Base / Bad Cases

- Good: selecting channel `35` or `104` with effective model `gpt-5.5` posts that exact pair immediately; runtime learning is preserved and only matching plans and leases are invalidated.
- Base: no runtime object exists; the transaction preserves the database learning snapshot and resets the four health controls.
- Bad: deriving the mutation target from a fuzzy search result, gating dispatch behind local confirmation state, creating a missing metrics row, evicting cache before commit, clearing calibration/EMA fields, or patching the query cache without first cancelling an older request.

## 6. Tests Required

- DAO: explicit zero/`NULL` writes, runtime-learning preservation, database-only fallback, idempotency, and missing-row no-create behavior.
- Runtime: target-only metrics/failure-window/dirty/role eviction; same-channel other-model and same-model other-channel isolation; multi-model plan invalidation; full-key lease comparison.
- Concurrency: a stale in-flight pointer reloads the committed reset before applying a later success or failure; snapshot/reset races cannot write an older state after reset.
- Controller: validation, unknown action, missing row, success audit fields, and no audit on failure.
- Frontend: row action order, batch exclusion, localized `{ value, label }` select items, immediate dispatch, exact `channel_id + effective_model` payloads, immutable target-only patch, absent-target reference preservation, row-key isolation, and backend error-message precedence.
- Async tests must wait until the worker lifecycle reaches zero before reading shared metrics. Waiting only for the executor to start races with result application and test cleanup.

## 7. Wrong vs Correct

Wrong: eviction before persistence can lose the newest runtime learning and leave the process inconsistent when the transaction fails.

```go
GlobalMetricsRuntime.Delete(mk)
if err := model.ResetChannelModelMetricsUnknown(channelID, effectiveModel, nil); err != nil {
	return err
}
```

Correct: serialize the full operation, commit the preserved snapshot first, then invalidate the target state.

```go
lock := metricsLockFor(mk)
lock.Lock()
defer lock.Unlock()

var runtime *model.ChannelModelMetrics
if current := GlobalMetricsRuntime.Get(mk); current != nil {
	copy := *current
	runtime = &copy
}
if _, err := model.ResetChannelModelMetricsUnknown(channelID, effectiveModel, runtime); err != nil {
	return err
}
GlobalMetricsRuntime.Delete(mk)
```

Frontend wrong: selection only stores confirmation state, so no request reaches the backend; omitting `items` also exposes the raw value.

```tsx
<Select onValueChange={() => setResetTarget(row)}>...</Select>
```

Frontend correct: bind localized items, dispatch from the selected row immediately, and cancel the exact query before patching.

```tsx
<Select
  items={localizedItems}
  onValueChange={(action) => {
    if (action === 'reset_unknown') {
      resetUnknown(buildResetUnknownRequest(row))
    }
  }}
>
  ...
</Select>
await queryClient.cancelQueries({
  queryKey: ['model-route-metrics'],
  exact: true,
})
queryClient.setQueryData(['model-route-metrics'], patchRow)
void queryClient.invalidateQueries({ queryKey: ['model-route-metrics'] })
```

# Model Route Refresh — Design

## Scope and ownership

One task: the refresh handler is the single observable surface. Request-issuance, the click guard, optimistic-order cleanup, and failure behavior form one coherent fix; splitting them would leave a window where refresh still reuses a promise or still loses the mutex.

The source requirement is `/Users/mac/Documents/newapi 需求.md`. Codegraph + Read inspection confirm its cited files and line numbers match the current tree.

## Contract: what refresh calls

Both queries are global, top-level hooks in `ModelRouteAdmin` (lines 231-239), no `enabled`/`staleTime`. The refresh handler must drive the same two `queryKey`s and treat them as one logical refresh.

```ts
const policyQuery = useQuery({ queryKey: ['model-route-policies'], queryFn: listModelRoutePolicies })
const metricsQuery = useQuery({ queryKey: ['model-route-metrics'], queryFn: listModelRouteMetrics })
```

Replace the per-query `refetch({ cancelRefetch: false })` with client-driven refetch using the already-available `qc = useQueryClient()` (line 209):

```ts
const [policyRes, metricsRes] = await Promise.all([
  qc.refetchQueries({ queryKey: ['model-route-policies'], exact: true }, { cancelRefetch: true, throwOnError: true }),
  qc.refetchQueries({ queryKey: ['model-route-metrics'], exact: true }, { cancelRefetch: true, throwOnError: true }),
])
```

Notes:
- `cancelRefetch: true` (the default, but stated explicitly) cancels an in-flight fetch for that key before issuing the new one. This is the core fix for root cause 1.
- `exact: true` prevents the call from cascading into other queries that share a `model-route-*` prefix (none currently do, but it is defensive and matches the existing `resetUnknownMut` usage of `exact: true` at line 504-507).
- `throwOnError: true` is required for failure detection: `qc.refetchQueries` is typed `Promise<void>` (verified against `@tanstack/query-core@5.101.2` dts) and resolves without surfacing per-query results. With `throwOnError: true`, a failed refetch rejects `Promise.all`, which the outer `catch` turns into the error toast. `throwOnError` is legal here because `RefetchOptions extends ResultOptions`, and `ResultOptions` declares `throwOnError`.

### Why not `query.refetch({ cancelRefetch: true })` (option B)

It also works and is a smaller diff. The client-driven form is preferred because (a) it keeps both keys under one cancellation contract, (b) it mirrors the existing `qc.cancelQueries`/`refetchQueries`/`invalidateQueries` calls already used by the mutations in this file (lines 265-267, 287-288, 363, 371, 450, 459, 492, 504-513, 539, 552, 684, 706-713), so it is the idiomatic pattern here, and (c) `exact: true` is only expressible through the client. If the team prefers the minimal diff, option B is an acceptable fallback; either way `cancelRefetch: false` must not remain.

## Click guard: synchronous mutex

`isRefreshing` (line 718) is derived from `isFetching`, which updates after a render commit. A rapid second click can enter `handleRefresh` before `isRefreshing` flips true. Add a synchronous ref mutex that is checked and set in the same tick:

```ts
const refreshingRef = useRef(false)

const handleRefresh = async () => {
  if (refreshingRef.current) return
  refreshingRef.current = true
  try {
    /* refetchQueries ... */
  } catch (err) {
    toast.error(err instanceof Error ? err.message : t('Refresh failed'))
  } finally {
    refreshingRef.current = false
  }
}
```

Keep `disabled={isRefreshing}` on the button (lines 790-799) for the perceived spin; the ref is the correctness guarantee, `isRefreshing` is the UX affordance. The two do not need to be unified.

## Optimistic order cleanup

`optimisticPolicyOrders` (line 226) holds in-flight drag orders. On a successful refresh, clear it so a stale optimistic order cannot override freshly fetched server ordering:

```ts
setOptimisticPolicyOrders(new Map())
```

Place this in the success branch only, alongside `setRowActionKey`/`setBatchActionKey` (lines 747-748). Do not clear it on failure — a failed refresh did not change server data, so the optimistic order stays valid.

## Failure handling

`qc.refetchQueries` is typed `Promise<void>` and, with the default `throwOnError: false`, swallows per-query failures (resolved to `undefined` via `.catch(noop)` in `#executeFetch`). To detect failure, pass `throwOnError: true`: any failed refetch rejects `Promise.all`, caught by the outer `try/catch`, which shows the error toast.

On failure, TanStack Query retains the previous `data` (it does not clear on error), so the list stays populated — satisfying Case 10/12. The `catch` skips the success side effects (`setOptimisticPolicyOrders`/`setRowActionKey`/`setBatchActionKey`), matching the current behavior.

Keep the outer `try/catch` (now the single failure path); the toast message fallback stays as-is.

## Transport-layer dedup interaction (root cause 2)

With `cancelRefetch: true`, the in-flight query is cancelled. Cancellation rejects the underlying axios promise, which triggers `originalGet(...).finally(() => inFlightGet.delete(key))` in `lib/api.ts` (line 71), deleting the dedup key. The subsequent refetch therefore passes the `inFlightGet.has(key)` check (line 68) and issues a real new request. No transport-layer change is needed.

The `disableDuplicate` opt-out (line 61-62) stays unused. It is documented in the PRD as a fallback only if a future regression shows the query-layer cancellation racing the transport-layer delete; do not pre-emptively touch `lib/api.ts`.

## Data flow

```text
click Refresh
  → refreshingRef.current guard (sync)
  → Promise.all([
      qc.refetchQueries(['model-route-policies'], { cancelRefetch: true, throwOnError: true }),
      qc.refetchQueries(['model-route-metrics'], { cancelRefetch: true, throwOnError: true }),
    ])
  → each refetch cancels its in-flight query → inFlightGet key deleted → new GET issued
  → any refetch rejects (throwOnError) → Promise.all rejects → catch → toast error, keep old data
  → else: clear optimisticPolicyOrders, bump row/batch action keys, toast success
  → finally: refreshingRef.current = false
```

## Files

| File | Change |
|---|---|
| `web/default/src/features/model-route/index.tsx` | Add `refreshingRef`; rewrite `handleRefresh` (lines 735-753) to use `qc.refetchQueries({ queryKey, exact: true }, { cancelRefetch: true })`, failure-check the result array, clear `optimisticPolicyOrders` on success. Keep `isRefreshing` and the button as-is. |
| `web/default/src/lib/api.ts` | No change (fallback-only, not applied). |

## Compatibility and rollback

- No backend or API contract change; no other query, mutation, or component is touched.
- Rollback is a single-file revert of `handleRefresh` and the `refreshingRef` line; `lib/api.ts` is untouched so there is no transport-layer rollback.
- No i18n keys added: `Refresh`, `Refreshed`, `Refresh failed` already exist and are reused.
- The existing `stale_policy_snapshot` recovery path (lines 367-371, 455-459) is untouched and still relies on its own `refetchQueries` calls.

## Verification approach

- Type-check and lint the web workspace.
- Manual QA per Cases 1-14; prioritize Case 9 (rapid double-click issues a new request, verified via DevTools network: two request cycles, not one reused) and Case 13 (refresh vs re-enter yield identical data).
- No backend tests; no frontend unit harness exists for this component (codegraph reports no covering tests). If desired, a small unit test for the failure-detection predicate is optional, not required.

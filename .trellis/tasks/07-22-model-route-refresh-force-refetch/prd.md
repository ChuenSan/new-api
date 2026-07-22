# Model Route Refresh — Force Refetch

## Goal

Make the Model Route admin page 「Refresh」 button always issue fresh network requests for `/api/model_route/policies` and `/api/model_route/metrics`, instead of reusing an in-flight promise, so refreshed data matches a fresh page load / F5 while preserving the operator's current search, exact-match, tab, and selection context.

## Background

- Source requirement: `/Users/mac/Documents/newapi 需求.md`. Repository inspection confirms every file/line it cites matches the current code.
- The page is `ModelRouteAdmin` in `web/default/src/features/model-route/index.tsx`. Two `useQuery` hooks (`['model-route-policies']`, `['model-route-metrics']`) load on mount with no `enabled`/`staleTime` gate (lines 231-239).
- Mount and refresh hit the same two GETs, so the original "N-vs-1 request gap" does not exist here. The real defect is request reuse, not interface count.
- `lib/api.ts` deduplicates concurrent GETs to the same `${url}?${params}` via `inFlightGet` (lines 57-74); a `disableDuplicate` opt-out already exists.

## Root cause (verified against code)

1. `handleRefresh` calls `policyQuery.refetch({ cancelRefetch: false })` and `metricsQuery.refetch({ cancelRefetch: false })` (lines 738-741). With `cancelRefetch: false`, when a request is in flight, TanStack Query returns the existing promise and issues no new request — refresh shows stale data.
2. `lib/api.ts` `inFlightGet` further collapses two rapid identical GETs into one in-flight request at the transport layer.
3. `isRefreshing = policyQuery.isFetching || metricsQuery.isFetching` (line 718) is the only click guard. `isFetching` flips from idle to fetching after a render cycle, so very fast double clicks can pass the `if (isRefreshing) return` guard before it becomes true. There is no synchronous mutex and no last-write-wins protection.

The backend is DB-fresh for `/policies`; `/metrics` overlays process-local runtime `role`/`is_stale` (lost on restart) but that is an inherent property, not the stale-data root cause. The axios default header already sends `Cache-Control: no-store`, so browser caching is not in scope.

## Requirements

### Refresh must issue new requests

- Every click must produce a new network request for both `/policies` and `/metrics`; it must not reuse an in-flight promise.
- Do not use `cancelRefetch: false` on the refresh path.
- Preferred approach: `qc.refetchQueries({ queryKey, cancelRefetch: true })` (or invalidate then refetch). `cancelRefetch: true` cancels the in-flight query; once cancelled, `inFlightGet`'s `.finally` deletes its key, so the new request proceeds without touching the transport layer.
- Do not globally disable `inFlightGet`; the dedup still protects against N+1 elsewhere. Only consider per-call `disableDuplicate` on the two model-route GETs if the query-layer fix proves insufficient — keep it off by default.

### Click guard and concurrency

- Add a synchronous `refreshingRef = useRef(false)` mutex: enter `handleRefresh`, if `refreshingRef.current` return, else set it true; clear it in a `finally` block. This closes the `isFetching` render-delay gap.
- Keep `disabled={isRefreshing}` and the button's `animate-spin` for perceived loading state.
- No mandatory `AbortController`; TanStack Query's built-in cancellation is sufficient.

### Preserve user context

- Refresh must not clear `channelFilter`, `modelKeyword`, `exactModelMatch`, `tab`, or `selectedMetricKeys` (all `useState`, decoupled from queries).
- On success, clear `optimisticPolicyOrders` (`setOptimisticPolicyOrders(new Map())`) so a mid-drag optimistic order cannot conflict with freshly fetched server data.
- Filtering/sorting (`policyGroups` line 560, `metrics` line 583) are `useMemo` over query data and recompute automatically when new data arrives; no extra handling needed.

### Loading and no-flicker

- During refresh, keep old data; do not clear the list or show a full-screen loading state. Only the refresh button shows loading (icon spin + disabled).
- A skeleton is acceptable only on first mount with empty data; do not introduce it as part of this change unless trivial.

### Failure handling

- If either query fails, keep pre-refresh data (TanStack Query retains old `data` on error), toast `Refresh failed`/`Refresh failed, please retry later`, and return without running success side effects (`setRowActionKey`/`setBatchActionKey`).
- Partial failure (one ok, one failed) must still preserve pre-refresh data; the existing `results.some(r => r.isError || r.error)` pattern covers it — adapt to whatever `refetchQueries` returns.

### Out of scope

- No backend changes, no new aggregate endpoint, no route/auth changes.
- No changes to mutations, search/filter, drag reorder, priority editing, metric row/batch actions, or the `stale_policy_snapshot` recovery path.
- No removal of the two-parallel-GETs design.

## Acceptance Criteria

- [ ] Case 1: backend priority 100→300, refresh shows 300 and the row re-sorts (via `policyGroups` useMemo) without F5.
- [ ] Case 2: backend policy enabled→disabled, refresh shows latest policy state from `/policies`.
- [ ] Case 3: backend channel available→unavailable, refresh updates channel status and the `16/22` count recomputes via `isPolicyRouteAvailable`.
- [ ] Case 4: new eligible channel added backend-side, refresh shows it immediately and total count changes.
- [ ] Case 5: channel deleted backend-side, refresh drops the row and count syncs.
- [ ] Case 6: searching "4.5", refresh keeps the keyword and still filters by it.
- [ ] Case 7: exact-match on, refresh keeps the toggle and queries by exact logic.
- [ ] Case 8: on the Metrics tab, refresh stays on Metrics (two queries cover both tabs).
- [ ] Case 9: rapid double-click does not flood requests (`refreshingRef` mutex + dedup), no stale-overwrite, ends with latest data. **Core regression: refresh must have actually issued a new request, not reused the old promise.**
- [ ] Case 10: a refresh request failing preserves pre-refresh data and shows a retry toast, no blank list.
- [ ] Case 11: during refresh the button shows clear loading (spinning icon + disabled) and is not clickable again.
- [ ] Case 12: no backend change, refresh is stable — no flicker, no list clear.
- [ ] Case 13: same query conditions, A clicks refresh and B re-enters the page → identical business data. **Directly verifies the root cause is gone.**
- [ ] Case 14: a channel going unavailable makes `16/22` correctly become `15/22`, consistent with the list.

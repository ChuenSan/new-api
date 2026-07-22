# Model Route Refresh — Implementation Plan

## 1. Rewrite the refresh handler

- [x] Add `const refreshingRef = useRef(false)` near the other refs/state in `ModelRouteAdmin` (`web/default/src/features/model-route/index.tsx`, alongside `optimisticPolicyOrders` ~line 226 or the hooks block).
- [x] Replace `handleRefresh` body (lines 735-753): use `qc.refetchQueries({ queryKey: ['model-route-policies'], exact: true }, { cancelRefetch: true, throwOnError: true })` and the same for `['model-route-metrics']`, wrapped in `Promise.all`.
- [x] Guard entry with `if (refreshingRef.current) return; refreshingRef.current = true`; reset in `finally`.
- [x] Detect failure via `throwOnError: true` so a failed query rejects `Promise.all` and enters the `catch` (return type is `Promise<void>`, so result-array inspection is not available — corrected from the original plan).
- [x] On failure: `catch` shows `toast.error(...)` (backend message or `Refresh failed`), TanStack Query retains prior `data`, success side effects are skipped.
- [x] On success: `setOptimisticPolicyOrders(new Map())`, then existing `setRowActionKey((v) => v + 1)` / `setBatchActionKey((v) => v + 1)`, then `toast.success(t('Refreshed'))`.
- [x] Keep outer `try/catch` toast fallback for synchronous throws.

## 2. Verify no surrounding regressions

- [x] Confirm `isRefreshing` (line 718) and the button (lines 786-802) are unchanged; the spin + disabled still reflect `isFetching`.
- [x] Confirm no other call site uses `policyQuery.refetch`/`metricsQuery.refetch` (search the file); the only refresh entry is `handleRefresh`.
- [x] Confirm the mutations' `invalidateQueries`/`refetchQueries`/`cancelQueries` calls are untouched.
- [x] Confirm `lib/api.ts` is not modified (no `disableDuplicate` added unless QA proves the query-layer fix insufficient).

## 3. Quality gates

- [x] Run `tsgo -b` type-check — no new type errors; `throwOnError` is legal via `RefetchOptions extends ResultOptions` (verified against `@tanstack/query-core@5.101.2` dts). Exit 0.
- [x] Run `oxlint` on the changed file — no warnings/errors. Exit 0.
- [ ] Run `bun run i18n:sync` only if a key was added (none expected); skipped — no keys added.

## 4. Manual QA — Cases 1-14 (user-verified)

Pending manual verification by baizige on a live backend (jp279-cpa / sgp). Code is feature-complete and passes typecheck + lint; this section is not gated on commit.

- [ ] Case 1: priority 100→300 refresh shows 300 and re-sorts.
- [ ] Case 2: policy enabled→disabled refresh shows latest state.
- [ ] Case 3: channel available→unavailable refresh updates status + `16/22` count.
- [ ] Case 4: new eligible channel appears, count changes.
- [ ] Case 5: deleted channel disappears, count syncs.
- [ ] Case 6: "4.5" search preserved across refresh.
- [ ] Case 7: exact-match toggle preserved.
- [ ] Case 8: Metrics tab preserved across refresh.
- [ ] Case 9: rapid double-click — DevTools shows real new requests (not reused promise), no flood, ends on latest data. **Primary regression check.**
- [ ] Case 10: failed refresh keeps pre-refresh data + retry toast.
- [ ] Case 11: button loading state clear and not re-clickable.
- [ ] Case 12: no backend change → no flicker / no clear.
- [ ] Case 13: refresh vs re-enter yield identical data. **Primary root-cause check.**
- [ ] Case 14: `16/22` → `15/22` consistent with list.

## 5. Rollback note

- [ ] Single-file revert of `handleRefresh` + the `refreshingRef` line restores prior behavior; `lib/api.ts` untouched so no transport-layer rollback.

## Notes

- Reuse existing `useQueryClient`/`useQuery` mechanisms only; no new data layer, no backend, no aggregate endpoint.
- Do not add `AbortController` unless QA reveals a stale-overwrite that query cancellation does not cover.
- Context order for implementation: `implement.jsonl` → `prd.md` → `design.md` → this file.

# Model Route Reset to Unknown — Design

## Scope and ownership

This remains one task because persistence, runtime invalidation, and the row-level UI patch form one observable operation. Splitting them would permit a successful API response while routing or the UI still uses stale state.

The source requirement is `/Users/mac/Documents/vps-conversation/模型路由-设为未知操作-设计文档.md` (v3). Repository inspection and the prior design-review session confirm that its final decisions match the current route-state, cache-key, RootAuth, and frontend query contracts.

## Request contract

```json
{
  "channel_id": 1,
  "effective_model": "upstream-model",
  "action": "reset_unknown"
}
```

The controller trims `effective_model`, validates a positive channel ID, rejects unknown actions, and derives related requested models from current policies plus channel mapping. It never trusts the UI to provide cache keys.

## Route-level coordination

Current metric objects are shared pointers and mutation relies on external discipline. Add one `MetricsKey`-scoped coordination mechanism in `modelroute` and use it for the complete mutation window, not only the final state transition.

The same coordination contract must cover:

- production outcome learning and state changes;
- transparent retry, cold-start, and emergency outcome transitions;
- probe and shadow-result learning/state changes;
- critical and periodic snapshot reads;
- `reset_unknown` snapshot preservation, persistence, and eviction.

Use locked internal helpers where nested operations would otherwise reacquire the same non-reentrant lock. Lock ownership stays in `modelroute`; database access stays in `model`.

## Backend data flow

```text
Root action handler
  → validate and reverse-map associated requested models
  → acquire MetricsKey coordination lock
  → copy latest runtime metrics when present
  → model transaction loads the existing persisted row
  → merge preserved learning snapshot
  → reset four health-control fields and update timestamp
  → commit
  → evict target runtime data + failWindow
  → clear target dirty marker and role
  → invalidate associated requested-model plans
  → conditionally clear matching overflow leases
  → release lock
  → write management audit
  → return success
```

### Persistence

- Add a model-layer transaction that first loads the target row and returns a not-found error without inserting.
- When a runtime copy exists for the same `MetricsKey`, use its persisted learning fields as the final snapshot; otherwise retain the database values.
- Explicitly select the snapshot columns so zero and `NULL` health values are written on every supported database.
- Overwrite `route_state`, `backoff_level`, `cooldown_until`, `last_error_class`, and `updated_at` after merging the learning snapshot.
- Do not persist runtime-only counters marked `gorm:"-"`.

### Runtime invalidation

- Give `RuntimeMetricsCache` a targeted eviction operation that deletes `data` and `failWindow` under its own mutex.
- Give `CalibrationPersister` a targeted dirty-marker clear operation. Snapshot collection must honor the same route coordination contract so a previously captured pointer cannot overwrite the reset afterward.
- Clear the role through `GlobalRoles.Set(mk, RoleNone)`.
- Reuse the existing policy-to-effective reverse mapping to enumerate all associated requested models and call `InvalidateRoutePlan` for each.
- Add an atomic `ClearLeaseIfMetricsKey(requestedModel, mk)` operation to `LeaseStore`; checking and clearing under one lease-store lock prevents deleting a concurrently replaced lease for another route.

The database transaction and process-local maps are deliberately not one rollback domain. Cache operations are synchronous in-memory mutations. If the process exits after commit, restart discards the stale maps and reloads the committed state.

## Frontend data flow

- Derive both selectors from one action list containing `reset_unknown` after `restore_auto`.
- Keep the existing row selector and add the same item to the batch selector.
- Dispatch `reset_unknown` directly from the row selector's `onValueChange` handler and maintain a set of pending row keys.
- Build the request only from the selected row's exact `channel_id` and `effective_model`; search text and fuzzy matching never participate in the mutation payload.
- Submit with `skipBusinessError` and `skipErrorHandler` so the mutation owns the single error toast for both business and HTTP failures.
- On success, use React Query `setQueryData`/`setQueriesData` to immutably patch the matching row before invalidating the metrics query in the background.
- Reset the row selector after the request settles so the same action can be retried after failure.
- Reuse the existing checked-row batch runner. Submit one exact request per selected row, patch each successful row immediately, count partial failures, and invalidate the metrics query once after the batch settles.

## Internationalization

All new source strings originate at `t(...)` call sites. Populate all six locales only through `web/default/scripts/add-missing-keys.mjs`, run it from `web/default`, then run `bun run i18n:sync`. Locale JSON files must not be edited directly.

## Compatibility and boundary behavior

- Existing four actions keep their current event-driven behavior.
- `UNKNOWN` only restores health eligibility. Policy enablement, channel status, token whitelist, group, request path, and concurrency constraints remain authoritative.
- Plan invalidation can rebuild other candidates for the same requested model but cannot change their persistent metrics or policies.
- An in-flight outcome that completes after reset is valid new state-machine input and may make the UI's later background refetch differ from its immediate `UNKNOWN` patch.
- A metrics row with no associated policy can be reset, but no plan or lease needs invalidation and the row does not become selectable by itself.

## Rollback

Code rollback removes the row action, direct event dispatch, i18n call sites, action union value, controller branch, reset orchestration, targeted cache APIs, route-coordination integration, and tests together. It does not reverse completed data resets. Existing actions and automatic state transitions remain intact.

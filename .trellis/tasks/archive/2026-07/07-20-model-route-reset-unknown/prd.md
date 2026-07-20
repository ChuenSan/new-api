# Model Route Reset to Unknown

## Goal

Add root-only row and batch actions that reset selected `channel_id × effective_model` routes to `UNKNOWN`, clear their health backoff controls, and make the reset visible to the current process and UI immediately without erasing learned metrics.

## Background

- Production routing accepts `HEALTHY`, `RECOVERING`, and `UNKNOWN`; `OPEN`, `RATE_LIMITED`, `PROBING`, and `MANUALLY_DISABLED` are excluded.
- Existing metric actions cannot restore an arbitrary route to a clean unknown state. A repaired upstream can therefore remain excluded by its state, cooldown, or stale process-local caches.
- Route health is keyed by effective model, while route plans and overflow leases are keyed by requested model. One effective route can be referenced by multiple requested models.
- Deployment scope is one VPS and one service process. Cross-instance cache broadcasts are not required.

## Requirements

### API and authorization

- Extend `POST /api/model_route/metrics/action` with the fixed action value `reset_unknown`.
- Keep the existing request fields: `channel_id`, `effective_model`, and `action`.
- Require `channel_id > 0` and a non-empty trimmed `effective_model`; reject unsupported actions.
- Keep the existing `RootAuth` boundary.
- Require the target metrics row to exist. Do not call `EnsureChannelModelMetrics` and do not create a row on failure.

### Reset semantics

- Serialize the reset with production outcomes, probe outcomes, and metric snapshots for the same `MetricsKey`.
- Preserve the latest runtime learning snapshot before evicting process-local metrics, including all EMAs, experience score, calibration, sample counts, and historical request/probe timestamps.
- In one database transaction, persist the preserved learning values and set only these health controls: `route_state = UNKNOWN`, `backoff_level = 0`, `cooldown_until = NULL`, and `last_error_class = ''`. Refresh `updated_at`.
- After the transaction commits, evict only the target route's runtime metrics and temporary-failure window, clear its completed dirty marker, and set its runtime role to `NONE`.
- Reverse-map every current `requested_model` associated with the target effective route, invalidate each route plan, and clear an overflow lease only when its candidate has the target `MetricsKey`.
- Treat repeated resets as successful idempotent operations.
- Do not cancel in-flight requests. Outcomes acquiring the route lock after the reset may advance `UNKNOWN` normally.

### Audit and failure behavior

- Record the existing management-audit shape after persistence and all process-local invalidation complete, including action, channel, effective model, operator, and time.
- Do not record a successful audit when persistence fails.
- A database failure must leave process-local caches untouched and return failure.
- Process restart between database commit and cache invalidation is acceptable because restart clears the single instance's process-local state.

### Administration UI

- Add “Reset to unknown” immediately after “Restore auto” in each metrics row action selector.
- Expose the same action list in the row and batch selectors so supported actions cannot drift between them.
- Apply the batch action only to checked rows, using each row's exact `channel_id + effective_model` as an independent request target.
- Allow the action from every current route state and submit it immediately when selected; do not gate the request behind a confirmation dialog.
- Identify pending rows by `channel_id + effective_model` and disable only the active row selector while its request is pending.
- On success, patch the matching React Query row locally to `route_state = UNKNOWN`, `role = NONE`, `backoff_level = 0`, `cooldown_until = null`, and `last_error_class = ''`, then allow a background invalidation/refetch.
- Show a dedicated success toast. On failure, keep the row unchanged and show exactly one toast containing the backend message.
- Add every new menu, dialog, success, and failure string through `t(...)` for `en`, `zh`, `fr`, `ja`, `ru`, and `vi`.

## Acceptance Criteria

- [ ] Both row and batch selectors contain “Reset to unknown” immediately after “Restore auto” and share one action definition.
- [ ] Batch reset processes every checked row and no unchecked row, using exact `channel_id + effective_model` request payloads.
- [ ] Selecting the action immediately sends `reset_unknown` for exactly the selected row's `channel_id + effective_model`, without fuzzy model matching.
- [ ] Only the pending row is disabled; other rows remain usable.
- [ ] Success immediately patches the five displayed/cache fields without a full-page refresh and shows the dedicated success toast.
- [ ] Failure preserves the original query data and shows one backend-derived error toast.
- [ ] The endpoint rejects invalid IDs, blank effective models, unknown actions, and missing metrics rows without creating data.
- [ ] Persistence resets the four health controls and preserves all learned and historical metric fields, including newer runtime values not yet snapshotted.
- [ ] Repeated resets remain successful and leave no intermediate state.
- [ ] Runtime metrics data, the temporary-failure window, role, and dirty marker are cleared only for the target `MetricsKey`.
- [ ] Every associated requested-model plan is invalidated; only leases pointing at the target `MetricsKey` are cleared.
- [ ] Other models on the same channel, other channels for the same model, policies, priorities, channel status, and channel configuration remain unchanged.
- [ ] With all other routing predicates satisfied, a request through an associated requested model can select the reset route.
- [ ] A successful post-reset request advances `UNKNOWN` to `HEALTHY`; a deterministic failure can advance it back to `OPEN` with backoff.
- [ ] A successful action creates one complete management-audit record; failed persistence creates no success audit.
- [ ] All new UI strings exist in all six supported locales and the i18n sync report remains valid.
- [ ] Targeted backend/frontend tests, frontend checks, `go build ./...`, and relevant package tests pass.

## Out of Scope

- Changing candidate ordering, priority, experience scoring, or automatic circuit-breaking rules.
- Bypassing policy, channel, token-whitelist, group, endpoint, or concurrency eligibility checks.
- Clearing learning data, policies, channel configuration, or in-flight requests.
- Cross-instance cache invalidation or an atomic multi-row database transaction.
- Reversing an already completed reset to its previous state.

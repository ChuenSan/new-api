# Model Route Reset to Unknown — Implementation Plan

## 1. Concurrency and persistence foundation

- [x] Add `MetricsKey`-scoped coordination in `modelroute` and locked internal helpers to avoid nested-lock deadlocks.
- [x] Apply the coordination contract to production, retry, cold-start, emergency, probe/shadow, and snapshot paths that mutate or copy route metrics.
- [x] Add a model transaction that requires an existing metrics row, merges a same-key runtime snapshot, explicitly writes all preserved persisted fields, and resets the four health controls.
- [x] Add tests for missing-row failure/no creation, four-field reset, zero/NULL writes, idempotency, runtime learning preservation, and database-only fallback.

## 2. Targeted runtime invalidation

- [x] Add target-key eviction for runtime metrics data plus `failWindow`.
- [x] Add target-key dirty-marker clearing and make snapshot collection obey route coordination.
- [x] Add atomic lease clearing by `requested_model + MetricsKey`.
- [x] Reuse current policy/channel mapping logic to enumerate all requested models for the target effective route.
- [x] Implement the reset orchestrator: commit first, then evict metrics/failure window, dirty marker, role, requested-model plans, and only matching leases.
- [x] Test one effective model mapped from multiple requested models, unrelated leases, same-channel/other-model isolation, same-model/other-channel isolation, and snapshot/reset races.

## 3. Controller and audit

- [x] Trim and validate action input; extend the supported action comment and switch with `reset_unknown`.
- [x] Route only `reset_unknown` through the dedicated reset orchestration; preserve all four existing event actions.
- [x] Keep `RootAuth`, return the existing API error shape, and record management audit only after full success.
- [x] Add controller tests for validation, unsupported actions, missing rows, success response, audit fields, and no success audit on persistence failure.

## 4. Administration UI

- [x] Use one action definition for row and batch selectors, including `reset_unknown` after `restore_auto`.
- [x] Insert the row menu item immediately after “Restore auto”.
- [x] Dispatch the reset request directly from the row selector with the selected row's exact `channel_id + effective_model`.
- [x] Track pending state by `channel_id + effective_model`; disable only the active row selector.
- [x] Submit with global business/HTTP toast handling disabled and emit exactly one local backend-derived failure toast.
- [x] On success, immutably patch the matching React Query row to `UNKNOWN`, `NONE`, zero backoff, null cooldown, and empty error class before background invalidation.
- [x] Add pure frontend helpers/tests for action-list separation, row keys, immutable success patching, pending isolation, and failure no-op.
- [x] Bind localized action items to Base UI selectors so the trigger never exposes `reset_unknown`.
- [x] Cancel the exact metrics query before applying the successful local patch, then reconcile from the server in the background.
- [x] Reuse the checked-row batch runner for reset requests, patch successful rows, report partial failures, and invalidate once after settlement.
- [ ] Cover direct selection dispatch and selector reset through browser QA. Blocked because the Browser plugin and a repository-installed Playwright runtime are unavailable.

## 5. Internationalization

- [x] Add menu, success, and failure fallback keys to `web/default/scripts/add-missing-keys.mjs` for `en`, `zh`, `fr`, `ja`, `ru`, and `vi`.
- [x] Run `node scripts/add-missing-keys.mjs` from `web/default`.
- [x] Run `bun run i18n:sync` and inspect the sync report for missing keys.
- [x] Do not edit locale JSON files directly; add the batch confirmation string through the translation script.

## 6. Verification

- [x] Run `gofmt` on touched Go files.
- [x] Run targeted `go test` commands for `model`, `modelroute`, and `controller`, including `go test -race ./modelroute`.
- [x] Run the frontend `node:test` files for model-route helpers through Bun's TypeScript-aware test runner.
- [x] Run `bun run typecheck` and `bun run build:check` from `web/default`.
- [x] Run targeted lint and format checks for every touched frontend source/script file.
- [x] Add and run the localized selector-item regression test; all 12 model-route frontend helper tests pass.
- [x] Add exact request-payload regression coverage for channels `35`, `104`, and effective model `gpt-5.5`.
- [x] Re-run `bun run typecheck`, `bun run build:check`, and `bun run i18n:sync` after the UI regression fix.
- [ ] Make the pre-existing full-repository `bun run lint` and `bun run format:check` baselines green. Unrelated existing source lint errors and `test-results` formatting artifacts remain out of scope.
- [x] Run `go build ./...` and `go test ./...`.
- [ ] Browser-check row/batch menu parity, checked-row targeting, immediate row request dispatch, exact payloads, pending state, success patches, partial failure toast, and background state reconciliation. Blocked by unavailable browser tooling.
- [x] Review the final diff to confirm no unrelated route ordering, learning, policy, channel, or bulk-action behavior changed.

## Risk and rollback points

- Route coordination is the first gate. Stop if any metrics mutation or snapshot path can still overwrite a committed reset with an older pointer.
- Persistence tests must prove runtime learning values are neither cleared nor regressed before cache eviction is enabled.
- Lease clearing must compare the full target `MetricsKey` atomically; clearing by requested model alone is a release blocker.
- The immediate UI patch is provisional by design; a later refetch may show a legitimate post-reset transition caused by live traffic.
- Roll back all reset-specific backend and frontend surfaces together. Do not delete data or attempt to reconstruct pre-reset state.

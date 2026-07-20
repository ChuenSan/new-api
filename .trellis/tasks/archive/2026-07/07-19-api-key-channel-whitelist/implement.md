# API Key Channel Whitelist — Implementation Plan

## 1. Model and API contract

- [ ] Add the nullable `ChannelIDList` storage type and `Token.AllowedChannelIds` field.
- [ ] Include the field in explicit token updates and verify AutoMigrate on SQLite.
- [ ] Add model queries for enabled channel summaries and submitted-ID validation.
- [ ] Enforce administrator-only writes, reject invalid/empty lists, preserve restricted policies on non-admin edits, and sanitize non-admin responses.
- [ ] Add the administrator-only enabled-channel summary route.

## 2. Routing enforcement

- [ ] Add the request context key and membership helper.
- [ ] Populate the restriction during token authentication.
- [ ] Enforce it for explicit channel IDs and affinity reuse.
- [ ] Filter model-priority try lists without reordering and reject disallowed emergency candidates.
- [ ] Filter channel-priority memory-cache and database candidates before priority/weight selection.
- [ ] Verify retry reset retains the token restriction while rebuilding only routing state.

## 3. Administrator UI

- [ ] Extend API key types, schema, defaults, detail normalization, and payload transformation.
- [ ] Add the enabled-channel summary query and administrator role gates.
- [ ] Build the all/specific selector with search and enabled-only multi-selection.
- [ ] Intersect historical values with enabled options before save.
- [ ] Add the administrator-only table summary column.
- [ ] Add all new strings to supported `en`, `zh`, `fr`, `ja`, `ru`, and `vi` locales following the project i18n workflow.

## 4. Verification

- [ ] Model tests: SQL `NULL`/array round-trip, malformed data, migration presence, update-to-NULL.
- [ ] Controller tests: create/update/detail/list semantics, `[]`, invalid/disabled IDs, administrator authorization, non-admin preservation and response sanitization, enabled-only summary endpoint.
- [ ] Middleware/service/model routing tests: unrestricted behavior, intersection order, affinity/direct/emergency denial, channel-priority filtering, disabled channels, corrupt empty list fails closed.
- [ ] Frontend unit/type checks for form transforms and selector filtering where the current test setup permits.
- [ ] Run `gofmt` on touched Go files.
- [ ] Run targeted Go tests for `model`, `controller`, `middleware`, and `service`.
- [ ] Run `go build ./...`.
- [ ] Run `bun run lint` and the relevant frontend test/type-check commands defined by `web/default/package.json`.

## Risk and rollback points

- Storage semantics are the first gate: stop if SQLite cannot preserve `NULL` versus `[]`.
- Routing tests must pass before UI work is considered complete; any bypass is a release blocker.
- Do not alter existing model-route ordering, health, priority, or weight code beyond predicate filtering.
- Do not remove or rewrite historical whitelist values when channels are disabled.

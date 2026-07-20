# API Key Channel Whitelist

## Goal

Add an optional per-API-key channel whitelist. Requests must keep the existing model/group routing behavior, then restrict the resulting candidates to channels allowed by the authenticated key. Keys without a whitelist must behave exactly as they do today.

## Background

- Token management endpoints are available to all authenticated users, while channel topology is administrator-only.
- A channel can be disabled after it was stored in a key whitelist. The stored relationship must remain until the next administrator save, but disabled channels must never be displayed or selected at runtime.
- Selection has several entry paths: model-priority routing, legacy channel-priority routing, affinity reuse, emergency recovery, retries, and administrator-specific channel overrides. The whitelist must not be bypassed by any path.

## Requirements

### Data and API contract

- Add `allowed_channel_ids` to the Token/API key contract.
- `null` means all channels; a non-empty integer array means only those channel IDs; `[]` is invalid.
- Token create, update, list, and detail responses must preserve this contract.
- The persisted representation must preserve SQL `NULL` versus a JSON array and remain portable across SQLite, MySQL, and PostgreSQL.
- Submitted IDs must be positive, unique after normalization, and reference currently enabled channels.
- Only administrators may submit a non-null whitelist or load channel summaries. Non-administrator updates must preserve any existing stored whitelist and must not expose channel IDs.

### Routing

- Apply the whitelist after existing model/group eligibility is established without changing candidate ordering, priority, weight, or retry semantics.
- Apply the same restriction in model-priority and channel-priority modes.
- Affinity reuse, emergency recovery, retry rounds, and explicit channel overrides must respect the whitelist.
- Disabled or missing channels must never match, including historically stored IDs.
- `null` must add no routing constraint. A corrupt historical empty array must fail closed and match no channels.

### Administration UI

- Show the channel setting only when the current user is an administrator.
- Add “Available channels” to the API key create/edit form, defaulting to “All channels”.
- Allow switching to “Specific channels”. In that mode, show search and a multi-select list of enabled channels.
- Each selectable row must contain only channel ID, channel name, and supported-model summary.
- Search must match channel ID, name, and supported models.
- Do not show disabled channels, including disabled IDs retained in an existing whitelist.
- On the next save, submit only selected IDs that are still present in the enabled-channel response.
- Reject saving “Specific channels” with no selection.
- Add an administrator-only API key list column that distinguishes all channels from a specific whitelist and summarizes currently enabled selections without showing disabled channel details.

## Acceptance Criteria

- [ ] Creating or editing a key with all channels persists and returns `allowed_channel_ids: null`.
- [ ] Creating or editing a key with enabled channel IDs persists and returns a normalized non-empty array.
- [ ] Frontend and backend both reject “Specific channels” with zero selections / `[]`.
- [ ] Backend rejects non-positive, missing, disabled, or unauthorized submitted channel IDs.
- [ ] Non-administrators cannot load channel summaries, cannot write a non-null whitelist, and do not receive stored channel IDs in token responses.
- [ ] Non-administrator edits preserve a pre-existing stored whitelist.
- [ ] A key with `null` has unchanged routing behavior.
- [ ] A restricted key can only select the intersection of existing routing candidates and its whitelist, in the original order.
- [ ] Legacy channel-priority, model-priority, affinity, retry, emergency recovery, and explicit-channel paths cannot bypass the whitelist.
- [ ] Disabled channels are absent from the selector and never selected at runtime.
- [ ] Editing a key with historical disabled IDs hides those IDs; saving retains only selected currently enabled IDs.
- [ ] The administrator table differentiates unrestricted and restricted keys using only enabled channel summaries.
- [ ] Targeted backend tests, frontend checks, and the repository build pass.

## Out of Scope

- Changing model-level route rules, priority, weight, health learning, or scheduling algorithms.
- Automatically deleting historical disabled IDs from stored whitelist values.
- Exposing channel topology or whitelist controls to non-administrators.
- Adding bulk whitelist editing for multiple API keys.

# API Key Channel Whitelist — Design

## Scope and ownership

This remains one task because storage, authentication context, routing, and UI all implement one atomic authorization contract. Splitting them would create intermediate states where the UI can save a policy that routing ignores, or routing expects a field the API cannot persist.

## Contract

The public field is `allowed_channel_ids`:

| JSON value | Stored value | Runtime meaning |
| --- | --- | --- |
| `null` or omitted by an administrator | SQL `NULL` | No channel restriction |
| `[1, 2, 3]` | JSON text array | Allow only those IDs |
| `[]` | Rejected | Invalid policy |

IDs are normalized to a sorted, duplicate-free list. Every submitted ID must be positive and currently enabled. Historical stored values are not rewritten merely because a channel becomes disabled.

## Backend data flow

```text
Admin form
  → token create/update validation
  → Token.AllowedChannelIds (nullable JSON text)
  → token cache
  → TokenAuth / SetupContextForToken
  → request-scoped allowed-ID set
  → every channel-selection path
```

### Persistence

- Define a small `ChannelIDList` custom type in `model/token.go` implementing `sql.Scanner` and `driver.Valuer`.
- Store it in a nullable `text` column. A nil slice returns SQL `NULL`; a non-nil slice returns JSON text. This follows GORM’s documented custom data type mechanism and avoids dialect-specific JSON column behavior.
- Add the field to `Token.Update()`’s explicit select list so clearing to `NULL` and non-zero arrays are both persisted.
- Existing rows migrate to `NULL` through GORM AutoMigrate, preserving unrestricted behavior.

### Authorization and validation

- Token handlers derive administrator status from the authenticated Gin role context.
- Administrators may set `null` or a valid non-empty list.
- Non-administrators submitting a non-null list receive an insufficient-privilege error.
- Non-administrator updates never assign the incoming field, so an existing stored policy is preserved.
- Token list/detail response copies clear `allowed_channel_ids` for non-administrators.
- Add an administrator-only `GET /api/token/available_channels` endpoint returning only `{id, name, models}` for enabled channels, ordered by ID.

### Runtime restriction

- Add a token allowed-channel context key. Presence means restricted, including an empty set; absence means unrestricted.
- Centralize the membership check so direct channel overrides, affinity reuse, model-priority selection, channel-priority selection, retries, and emergency candidates share one rule.
- In model-priority mode, filter the already group-matched try list while retaining its order.
- In channel-priority mode, filter candidate IDs/abilities before existing priority and weighted selection.
- Emergency recovery may return a candidate outside the whitelist; it must be discarded rather than used. Normal candidate construction remains unchanged.
- Existing enabled-status checks remain authoritative, so historical disabled IDs cannot match.

## Frontend data flow

- Extend the API key Zod schema and payload types with `allowed_channel_ids: number[] | null`.
- Add a form mode (`all` / `specific`) and selected ID array. The payload transformer emits `null` for all or the selected enabled IDs for specific.
- Fetch token detail and enabled channel summaries concurrently through TanStack Query while the administrator drawer is open.
- Intersect stored IDs with the enabled response before presenting/saving. This removes historical disabled IDs from UI state without mutating storage until save.
- Implement the selector from existing base-style shadcn components: radio group, command input/list/group/items, and checkbox state.
- Use the same query key for the drawer and table so TanStack Query deduplicates the enabled-channel request.
- Add a table cell for administrators only. It maps stored IDs to current enabled summaries and never renders disabled channel metadata.

## Compatibility and failure behavior

- Existing keys and callers omit the new field and remain unrestricted.
- Status-only token updates do not touch the whitelist.
- Redis token caching continues to serialize the slice value with the rest of the token.
- A malformed persisted JSON value surfaces as a database scan error instead of silently broadening access.
- A persisted non-nil empty array is treated as an empty allowed set at runtime, failing closed even though new writes reject it.
- If the enabled-channel summary request fails, the administrator cannot safely save a specific whitelist; the UI shows the load failure and leaves validation active.

## Rollback

Code rollback leaves an additive nullable column that older binaries ignore. No destructive schema rollback or data cleanup is required.

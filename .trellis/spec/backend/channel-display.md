# Channel Display Data for Administrator Log Lists

## Scenario: Current channel upstream links in usage logs

### 1. Scope / Trigger

- Trigger: an administrator-only log response needs display data from the
  current `Channel` configuration without storing a historical snapshot.
- This is a cross-layer contract: the Go list handlers augment paginated
  records, while `web/default` renders the optional `channel_base_url` as a
  safe new-tab link.

### 2. Signatures

```go
type ChannelDisplayInfo struct {
    Name    string
    BaseURL string
}

func GetChannelDisplayInfos(channelIDs []int) (map[int]ChannelDisplayInfo, error)
```

Administrator list responses may add the optional JSON field
`channel_base_url` to:

- `GET /api/log` (`model.Log`)
- `GET /api/mj` (`model.Midjourney`)
- `GET /api/task` (`dto.TaskDto`)

### 3. Contracts

- `channel_base_url` is derived from `Channel.GetBaseURL()` at read time; it
  is never persisted on logs, Midjourney rows, or tasks.
- `GetChannelDisplayInfos` filters non-positive IDs, deduplicates the page's
  IDs, uses `CacheGetChannel` while the memory cache is enabled, and makes one
  `GetChannelsByIds` query only for remaining IDs.
- A map entry means the channel still exists. Absent entries represent deleted
  or unavailable channels and must render without a link.
- `/api/log/self`, `/api/mj/self`, task self-service responses, and relay
  `TaskModel2Dto` responses must omit `channel_base_url` (`omitempty`).
- The frontend must call `normalizeExternalUrl` from
  `web/default/src/lib/external-url.ts`; it accepts HTTP(S), protocol-relative
  URLs, and matching bare host/path values only. Links use
  `target="_blank"` and `rel="noopener noreferrer"`.

### 4. Validation & Error Matrix

| Condition | Server behavior | Client behavior |
| --- | --- | --- |
| Positive ID resolves | Return its current `BaseURL` | Render a new-tab link only if normalization succeeds |
| Zero/negative/deleted ID | Omit the field | Keep the channel identifier non-clickable |
| Cache miss | Resolve all misses in one database query | No special handling |
| Channel lookup query fails | Log the error; return the page with any already-resolved entries | Missing address remains non-clickable |
| `javascript:`, `data:`, `file:`, malformed, or empty address | May be returned as configuration data | Never render an anchor |

### 5. Good / Base / Bad Cases

- Good: a page with repeated channel `12` returns one current base URL for all
  matching administrator rows, without N+1 queries.
- Base: a legacy row points to a deleted channel; the list succeeds and shows
  the existing `#12`-style label without an anchor.
- Bad: adding the field to `TaskModel2Dto` or calling the display resolver from
  a self-service list leaks an upstream address to an end user.

### 6. Tests Required

- Unit-test URL normalization for explicit HTTP(S), protocol-relative and bare
  host inputs, plus empty, malformed, and executable-protocol rejection.
- Unit-test `GetChannelDisplayInfos` with duplicate IDs and a deleted ID;
  assert only one channel query for an uncached page.
- Controller-test all three administrator list handlers for a populated
  `channel_base_url`, then assert every self-service counterpart omits it.

### 7. Wrong vs Correct

#### Wrong

```go
for _, log := range logs {
    channel, _ := GetChannelById(log.ChannelId, true)
    log.ChannelBaseURL = channel.GetBaseURL()
}
```

This creates an N+1 query pattern and risks a nil dereference for deleted
channels.

#### Correct

```go
display, err := GetChannelDisplayInfos(channelIDs)
for _, log := range logs {
    if channel, ok := display[log.ChannelId]; ok {
        log.ChannelBaseURL = channel.BaseURL
    }
}
```

This batches resolution, safely degrades missing channels, and keeps the
response field scoped to the administrator handler.

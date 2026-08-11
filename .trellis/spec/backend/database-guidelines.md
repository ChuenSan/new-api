# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

ORM is **GORM** (`gorm.io/gorm v1.25.2`). Four interchangeable backends, selected by env at startup: **MySQL**, **PostgreSQL**, **SQLite**, and a separate **ClickHouse** log DB. Drivers: `gorm.io/driver/{mysql,postgres,clickhouse}` + `github.com/glebarez/sqlite`. Multi-DB awareness is the central complication of every query here — code must stay portable across all backends.

Two package-level globals live in `model/`, set by `model/main.go`:

- `model.DB *gorm.DB` — the main DB (everything except call logs).
- `model.LOG_DB *gorm.DB` — the log DB (falls back to `DB` when `LOG_SQL_DSN` is unset).

`model` is the only package that owns these globals.

---

## Query Patterns

### Multi-DB column quoting — the `commonGroupCol` / `commonKeyCol` helpers

`group` and `key` are reserved words in MySQL/SQLite and must be quoted, but the quote char differs by backend. `model/main.go:initCol` sets:

- PostgreSQL: `commonGroupCol = "\"group\""`, `commonKeyCol = "\"key\""`; `commonTrueVal="true"`.
- MySQL / SQLite: `commonGroupCol = "\`group\`"`, `commonKeyCol = "\`key\`"`; `commonTrueVal="1"`.

Use them concat-style in raw `Where` clauses (see `model/ability.go:46,68,94`):

```go
DB.Table("abilities").
    Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
    Distinct("model").Pluck("model", &models)
```

Never hard-code `` `group` `` or `"group"` — it breaks on PostgreSQL. The log-DB equivalents are `logGroupCol` / `logKeyCol` (set for the *log* DB's dialect, which may differ from the main DB).

### GORM chain style

Queries are method-chained `.Where().First()` / `.Find()` / `.Pluck()`. Prefer parameterized `?` placeholders; never `fmt.Sprintf` user values into SQL.

### Transactions

Use `DB.Transaction(func(tx *gorm.DB) error { ... })` for multi-write atomicity. Do all writes against the passed `tx`, not the global `DB`. Heavy usage in `model/subscription.go` (13+ sites: `576,679,705,743,910,955,1072,1092,1139,1287,1381,1425,1492`), also `model/channel.go:459,905`, `model/user.go:426,500,534,757`, `model/channel_model_policy.go:209,332`, `model/checkin.go:96`. `tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", ...)` is the PG row-lock pattern (`model/user.go:271`).

### Locking & batch

`gorm.io/gorm/clause` is used for `clause.Locking`, `clause.OnConflict` (see `model/locking.go`, `model/ability.go`, `model/channel_model_policy.go`). Large writes use chunked iterations, not a single giant update.

### Timestamps via hooks, not DB `CURRENT_TIMESTAMP`

The codebase stores time as `int64` Unix millis (`gorm:"bigint"`) and stamps it in `BeforeCreate`/`BeforeUpdate` hooks using `common.GetTimestamp()` — see `model/channel_model_metrics.go:BeforeCreate`/`BeforeUpdate`. New tables with `created_at`/`updated_at` should follow this, so values are consistent across SQLite/MySQL/PG and not subject to per-DB clock/tz differences.

### Raw SQL

Allowed via `DB.Exec(...)` — used for backend-specific DDL (`ALTER TABLE ... TTL`, `ALTER TABLE ... ADD COLUMN`) that AutoMigrate can't express (`model/main.go:404,465,528,568`). Keep raw SQL guarded by `common.UsingMainDatabase(common.DatabaseTypeXxx)` checks so it only runs on the matching dialect.

### Cross-DB string concatenation

String ops also differ by dialect. `model/channel.go:140 channelGroupFilterCondition` branches on dialect: MySQL uses `CONCAT(',', group, ',')`, PG/SQLite use the `||` operator. Mirror this split for any inline string expression in a `Where` — and prefer a single guarded helper rather than inlining the branch at every call site.

---

## Migrations

### Primary mechanism: GORM `AutoMigrate`

`model/main.go:migrateDB()` calls `DB.AutoMigrate(&Channel{}, &Token{}, &User{}, ... &ChannelModelMetrics{}, ...)` over every registered struct. `migrateDBFast()` runs the same set concurrently (goroutine per model). Schema changes are expressed by **editing struct tags**, not by writing a migration — AutoMigrate only *adds* columns/tables; it does not drop or rename.

### Hand-written SQL in `migrations/*.sql`

`migrations/*.sql` (e.g. `20260711_add_channel_model_metrics.sql`) are **reference / idempotent DDL documented for operators** — they are *not* auto-executed at runtime via `go:embed`. The canonical schema source is the Go struct + AutoMigrate; the SQL mirrors it for manual provisioning and review (`IF NOT EXISTS`, multi-dialect-safe).

### Ad-hoc data migrations

Data-shape corrections live as named funcs called from `migrateDB()` before `AutoMigrate`: `migrateSubscriptionPlanPriceAmount()`, `migrateTokenModelLimitsToText()`, `ensureSubscriptionPlanTableSQLite()` (`model/main.go`). Add a new one when a column type/dtype changes and existing rows need conversion.

### Log DB

`migrateLOGDB()` handles ClickHouse log table DDL/TTL via raw `DB.Exec` (`model/main.go:404`).

---

## Naming Conventions

- **Table names**: Go struct → snake_case via `TableName()` overrides when the default (lowercased pluralized struct) is wrong. Examples: `ChannelModelMetrics.TableName()` → `"channel_model_metrics"`, `CasbinRule`, `AuthzRole`. Explicit `TableName()` is the project norm for newer tables.
- **Column names**: snake_case, driven by struct field `gorm:"column:..."` tags (or the field name lowercased). Quote-reserved words (`group`, `key`) with the `*Col` helpers, never in the `column:` tag.
- **Types in struct tags**: be explicit — `gorm:"type:varchar(64)"`, `gorm:"type:text"`, `gorm:"bigint;default:0"`, `gorm:"size:191"`, `gorm:"primaryKey;autoIncrement:false"`. Pointer types (`*string`, `*int64`, `*float64`) express nullable columns.
- **Runtime-only fields**: mark `gorm:"-"` so they are not persisted (see `ChannelModelMetrics.ConsecutiveFailures` etc.).
- **Indexes**: `gorm:"index"` on the field, or combined index via a separate gorm tag clause.

---

## Common Mistakes

- Hard-coding `` `group` `` / `"group"` or boolean `true`/`1` instead of the `commonGroupCol`/`commonTrueVal` helpers → breaks on PG or MySQL respectively.
- Writing to the global `model.DB` inside a `DB.Transaction(...)` instead of the `tx` argument → writes escape the rollback.
- Relying on AutoMigrate to drop/rename a column — it won't. Flag such a change to the team; it needs a manual data-migration func.
- Storing timestamps via a DB default (`DEFAULT CURRENT_TIMESTAMP`) — inconsistent across backends; use the `int64` + `BeforeCreate`/`BeforeUpdate` hook pattern instead.
- Treating `migrations/*.sql` as the source of truth — AutoMigrate + struct tags are. The SQL is documentation/idempotent DDL.
- Forgetting `TableName()` for a new table whose GORM-default pluralized name is wrong.

# Directory Structure

> How backend (Go) code is organized in this project.

---

## Overview

Module: `github.com/QuantumNous/new-api`. Web framework **Gin**, ORM **GORM**, multi-database (MySQL / PostgreSQL / SQLite / ClickHouse). Layout is **layer-by-package**: each top-level directory is one responsibility layer. There is no nested `internal/` boundary.

---

## Directory Layout

```
new-api/
├── main.go                 # entrypoint: init logger, model.InitDB, router.SetRouter, http.Server
├── router/                 # route registration only (api-router.go, relay-router.go, channel-router.go, ...)
├── controller/             # HTTP handlers: parse gin.Context -> call service/model -> c.JSON
├── middleware/             # Gin middlewares (request-id.go, auth.go, distributor.go, recover.go, rate-limit.go, ...)
├── service/                # business logic & cross-cutting orchestration (no DB structs, may call model)
├── model/                  # GORM models + DAO (DB access lives here; the only package that owns *gorm.DB)
├── modelroute/             # model routing subsystem (route decision, shadow probe, policy)
├── relay/                  # upstream relay adapters
│   ├── channel/            #   adapter.go interface + per-vendor packages (gemini/, claude/, openai/, ...)
│   ├── common/ constant/ helper/ common_handler/   # shared relay helpers
│   └── *_handler.go        # per-modality handlers (chat, embedding, image, rerank, ...)
├── dto/                    # request/response DTOs (OpenAIErrorWithStatusCode, ClaudeErrorWithStatusCode, ...)
├── types/                  # error types & SDK-shaped structs (NewAPIError, ClaudeError)
├── common/                 # cross-cutting utilities + globals (sys_log.go, redis.go, quota_math.go, gin.go, ...)
├── pkg/                    # reusable sub-packages with explicit boundaries (pkg/perf_metrics/, ...)
├── setting/                # runtime settings grouped by domain (setting/operation_setting/, ...)
├── constant/               # project-wide constants/enums
├── i18n/                   # internationalization resources
├── oauth/                  # OAuth provider implementations
├── migrations/             # hand-written SQL migrations applied by model/main.go migrateDB helpers
├── logger/                 # logging package: LogInfo/LogWarn/LogError/LogDebug
└── web/                    # frontend (out of scope for backend spec)
```

---

## Module Organization

### Layering rule

`controller -> service -> model`. Data flows inward only; an inner layer must not import an outer one.

- **`controller/`** holds Gin handlers. Parse `*gin.Context`, delegate logic to `service`/`model`, then shape the response with `c.JSON` and a `dto`/`gin.H`. Example: `controller/channel.go:101 GetAllChannels`.
- **`service/`** holds business logic that may span several models or external calls (`service/channel.go`, `service/quota.go`, `service/codex_oauth.go`). Does not define DB-mapped structs.
- **`model/`** is the **only** package that touches the global `model.DB *gorm.DB`. GORM structs + their DAO methods live here (`model/channel.go`, `model/ability.go`, `model/channel_model_policy.go`). `DB` and `LOG_DB` are package-level globals set by `model/main.go`.

### Relay adapters

Upstream vendor integration follows a fixed interface. `relay/channel/adapter.go` defines the adaptor contract; each vendor gets its own subpackage under `relay/channel/<vendor>/` (`gemini`, `claude`, `openai`, `aws`, `deepseek`, ...). Per-modality dispatch lives in `relay/*_handler.go` (`chat_completions_via_responses.go`, `embedding_handler.go`, `image_handler.go`).

### `common/` vs `pkg/`

- `common/` — project-wide utilities and singletons coupled to this app (logging writers, redis client, quota math, gin helpers, env, init). Imported freely across layers.
- `pkg/` — narrowly-scoped, more reusable sub-packages with a clear public API (e.g. `pkg/perf_metrics`). Prefer `pkg/` when a helper has no dependency on app globals and could be lifted independently.

### Routes

Route registration is split by surface in `router/` (`api-router.go`, `relay-router.go`, `channel-router.go`, `authz-router.go`, `video-router.go`, `web-router.go`) and wired from `router/main.go:SetRouter`. Middleware chains are attached per-group (see `router/relay-router.go` for the relay chain).

---

## Naming Conventions

- Package = directory name, all lowercase, single word where possible (`controller`, `model`, `relay`).
- File names: `snake_case.go` is the norm (`channel.go`, `channel_model_policy.go`); some older files use `kebab-case.go` (`channel-billing.go`, `request-id.go`). Match the neighbors in the same directory.
- Test files: `<name>_test.go`, same package (`package model`, `package common`). External-package tests that exercise unexported symbols use `<name>_internal_test.go` (e.g. `controller/channel_test_internal_test.go`).
- Handler funcs in `controller/` are verbs: `GetAllChannels`, `FetchUpstreamModels`, `FixChannelsAbilities`.

---

## Examples

- Layering: `controller/channel.go:101 GetAllChannels` -> `model` queries.
- Transactional DAO: `model/channel.go:459` and `model/channel.go:905` use `DB.Transaction(func(tx *gorm.DB) error {...})`.
- Multi-DB quoting in DAO: `model/ability.go:46,68` use `commonGroupCol` helper (see [Database Guidelines](./database-guidelines.md)).
- Adaptor contract: `relay/channel/adapter.go`; vendor impl `relay/channel/gemini/`.
- Route + middleware chain: `router/relay-router.go:13 SetRelayRouter`.

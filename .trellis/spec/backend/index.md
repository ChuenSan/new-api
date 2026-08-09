# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

Backend guidelines for `github.com/QuantumNous/new-api` (Go + Gin + GORM, multi-DB: MySQL / PostgreSQL / SQLite / ClickHouse). These reflect the **actual** conventions in the codebase — not ideals. Each file cites real `file:line` examples the team uses.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Layer-by-package layout, controller→service→model relay adapters | ✅ Filled |
| [Database Guidelines](./database-guidelines.md) | GORM patterns, multi-DB quoting, AutoMigrate, transactions | ✅ Filled |
| [Error Handling](./error-handling.md) | Sentinel errors, protocol error wrappers, panic recovery | ✅ Filled |
| [Quality Guidelines](./quality-guidelines.md) | Forbidden/required patterns, testify testing, review checklist | ✅ Filled |
| [Logging Guidelines](./logging-guidelines.md) | logger/common split, levels, request-id correlation, secrets | ✅ Filled |
| [Token Availability Mode](./token-availability-mode.md) | Token-scoped infinite relay rounds and cross-layer contract | ✅ Filled |
| [Model Route Reset to Unknown](./model-route-reset-unknown.md) | Root-only route health reset, learning preservation, runtime invalidation, and UI contract | ✅ Filled |
| [Anthropic Messages to OpenAI Chat Conversion](./anthropic-messages-openai-chat-conversion.md) | Fail-closed request, response, stream, and pseudo-SSE conversion contract | ✅ Filled |

---

## How to Use These Guidelines

Sub-agents (`trellis-implement`, `trellis-check`) auto-load the spec files listed in each task's `implement.jsonl` / `check.jsonl`. Keep these files accurate to the code — aspirational patterns that don't match the codebase will mislead sub-agents. When conventions change, update the spec here.

---

**Language**: All documentation is written in **English**.

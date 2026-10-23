---
name: goal-conventions
description: Non-negotiable rules for changing the Goal repository (engine, UI, infrastructure, factories). Use for any work item in the goal factory.
---

# Goal conventions

- **Config-driven.** Domain behavior lives in `factories/<name>/`, never in engine code. No hard-coded station, role or work type names in Go, SQL or TypeScript.
- **Factories are keyed by `metadata.id` (UUID).** Names and folders can change; database rows, S3 prefixes (`<id>/`) and AWS resources never use the name.
- **PostgreSQL is the only database.** No SQLite, Redis or other stores. Queue claims use `FOR UPDATE SKIP LOCKED`; realtime uses `LISTEN/NOTIFY`. Every schema change is a new numbered file in `migrations/`, forward-only.
- **Infrastructure** goes in `terraform/` only. Agents plan; a human applies.
- **UI** follows `design-system/goal/MASTER.md` exactly: user palette, Inter (opsz), radii 8–16 px, no gradients, no pink.
- **No employer or client names** in code, docs, examples or sample data.
- **Done means verified:** `go vet ./...`, `go test ./...`, `go run ./cmd/goal validate`, `terraform validate` all pass, and the change satisfies every acceptance criterion with evidence.
- Keep changes small: one spec, one branch (`goal/<item-key>`), one pull request.

## Engineering rules

`AGENTS.md` at the repo root is the full rulebook; `docs/engineering/architecture.md` and `docs/engineering/testing.md` explain it. The ones agents break most:

- **Hexagonal boundaries.** `internal/domain` and `internal/engine` import only ports; Postgres, Docker, ECS, secrets, Bedrock and HTTP are adapters. depguard fails the build otherwise; never add a `nolint` for it.
- **Manual constructor injection** in `cmd/*/main.go`. No DI frameworks, globals or `init()` side effects.
- **Stateless goald.** Anything another instance needs lives in PostgreSQL; schedulers take an advisory lock.
- **Cloud-ready.** Code must run unchanged on a laptop, on AWS ECS with managed PostgreSQL, and serve a UI that can deploy to Vercel.
- **Tests protect behavior.** No tests for constructors, getters, generated code or wiring; no mocks of our own types (use port fakes); every bug fix starts with a failing test.
- **`make check` passes** before a change is returned, and its output is attached as evidence.

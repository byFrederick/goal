---
paths:
  - "cmd/**"
  - "internal/**"
---

# Architecture

The design and its reasons are in [architecture.md](../../docs/engineering/architecture.md), [ADR 0003](../../docs/adr/0003-hexagonal-architecture-and-deployment.md), [ADR 0005](../../docs/adr/0005-factory-definitions-in-object-storage.md), [ADR 0008](../../docs/adr/0008-portable-runs-connections-and-knowledge.md) and [ADR 0013](../../docs/adr/0013-repository-layout.md).

## Ports and adapters

- Make `internal/domain` and `internal/engine` depend only on ports, so the engine runs unchanged on Docker Compose and on Cloudflare.
- Put every external system (Postgres, Docker, Cloudflare, code hosts, secrets, embedders, HTTP) in an adapter under `internal/adapters/`, because the engine must not know which one runs.
- Add a new system as an adapter behind an existing or new port, since a new import in the engine breaks the boundary for every target.
- Keep each package within its row of the import table in architecture.md ("Hexagonal core"), because depguard checks exactly that table.
- Treat depguard in `.golangci.yml` as the guard and never silence it with `//nolint`; a depguard finding means the design is wrong, not the linter.
- Model a provider as a connection kind (data in `internal/connkind/kinds/`), never as code in the engine, so adding a provider needs no deploy of engine logic.

## Config-driven

- Never write a factory, station, agent or work-type name in Go, SQL or TypeScript, because domain behavior lives in factory definitions.
- Key factories, rows and bucket prefixes by `metadata.id` (a UUID), never by name, since names change and ids do not.
- Keep only JSON Schemas and the `acme` fixtures in `internal/config/testdata` in the repository; definitions live in the bucket and PostgreSQL.

## State

- Keep goald stateless: anything another instance needs goes in PostgreSQL, because any instance may die at any time.
- Use PostgreSQL as the only database (no SQLite, Redis, queues or caches), so there is one thing to run and back up.
- Claim work with `FOR UPDATE SKIP LOCKED` and leases, so instances never take the same item and lost work returns to the queue.
- Use `LISTEN/NOTIFY` for realtime, so every instance streams to its own clients without sticky sessions.
- Run scheduled jobs under `pg_try_advisory_lock`, so exactly one instance runs each.
- Store only immutable content-addressed blobs in the bucket and never rely on object versioning, because R2 has none.

## Wiring

- Wire everything by manual constructor injection in `cmd/<binary>/main.go`, so every dependency is visible in one place.
- Use no DI container, no globals holding dependencies and no `init()` side effects, because they hide wiring and break tests.
- Read configuration from environment variables (listed in `cmd/goald/settings.go`) and platform secrets through the `Secrets` port, never from files a container would lose.

## Skills

Load `golang-project-layout`, `golang-design-patterns` and `improve-codebase-architecture` for structural changes.

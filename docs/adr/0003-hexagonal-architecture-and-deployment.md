# ADR 0003 — Hexagonal architecture, manual wiring, cloud-ready deployment

Status: accepted · 2026-10-23 · Decisions 4, 5 and 7 superseded by [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (phase 4: a Sandboxes port with Docker and Cloudflare adapters, local Compose and Cloudflare as the targets, model access as a connection); decisions 1–3 and 6 stand

## Context

Goal starts on a laptop but must be deployable to AWS (ECS Fargate and managed PostgreSQL) or with its UI on Vercel, scale horizontally, and stay clean while AI agents write most of its code.

## Decision

1. **Hexagonal architecture** (ports and adapters). `internal/domain` and `internal/engine` depend only on `internal/ports`. Postgres, Docker, ECS, secrets providers (environment now, AWS Secrets Manager, 1Password later), Bedrock knowledge bases, the MCP gateway and HTTP are adapters. `depguard` enforces the boundary in CI.
2. **Manual constructor injection.** Wiring lives only in `cmd/*/main.go`. No DI framework.
3. **Stateless `goald`.** All state in PostgreSQL; `SKIP LOCKED` leases for work, advisory locks for singleton jobs, `LISTEN/NOTIFY` for realtime fan-out per instance.
4. **Runner port with Docker and ECS adapters.** Agent runs are containers, never serverless functions.
5. **Deployment targets:** laptop (compose + Docker), AWS (ECS service + RDS/Aurora PostgreSQL + ECS RunTask runners + Secrets Manager), UI optionally on Vercel as a static site. Infrastructure for each target lives in `terraform/`.
6. **IDs generated in Go as UUIDv7** so the schema runs on managed PostgreSQL versions that lack `uuidv7()`.
7. **Model auth per factory:** subscription token only on a personal laptop; API key or Bedrock anywhere hosted or shared.

## Rejected

- **Clean architecture with use-case layers.** Same isolation, but many pass-through files and trivial tests per feature; the opposite of what agent-written code needs.
- **Flat packages.** Fast to start; hosting changes would ripple into the engine and tests would need real infrastructure everywhere.
- **DI frameworks (samber/do, wire, fx).** Indirection and generated code for wiring that fits in one readable file.
- **Agent runs on Vercel Functions or Lambda.** Runs last minutes to hours and need a container filesystem and long-lived MCP sessions.
- **A second datastore for queues or cache.** PostgreSQL covers both at this scale (ADR 0001).

## Consequences

- Adding AWS or a new runner is an adapter plus Terraform, with no engine changes.
- Engine tests are fast and meaningful with in-memory fakes; adapter tests are few and run against real dependencies.
- Contributors, human or agent, must respect the import rules; CI fails otherwise.

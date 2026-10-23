# Architecture

Goal is one Go daemon (`goald`), one PostgreSQL database, short-lived agent containers and a static React UI. The same binaries run on a laptop, on AWS and, for the UI, on Vercel. Decisions: [ADR 0001](../adr/0001-technology-stack.md) (stack), [ADR 0003](../adr/0003-hexagonal-architecture-and-deployment.md) (architecture and deployment).

## Hexagonal core

```
              ┌──────────────────────── goald ────────────────────────┐
  HTTP/SSE ──▶│ adapters/http ──▶  engine  ◀── domain                  │
  (UI, CLI)   │                      │                                 │
              │                    ports: Store  Runner  Secrets       │
              │                           Knowledge  ModelAuth  Clock   │
              │                      │                                 │
              │ adapters: postgres  docker | ecs  envsecrets | awssecrets
              │           bedrockkb  gateway (MCP)                      │
              └────────────────────────────────────────────────────────┘
```

| Package | May import | Never imports |
| --- | --- | --- |
| `internal/domain` | standard library | anything else in `internal/` |
| `internal/ports` | `domain` | adapters, engine |
| `internal/engine` | `domain`, `ports` | `adapters`, `config`, database or cloud SDKs |
| `internal/adapters/*` | `domain`, `ports`, their own SDK | `engine`, other adapters |
| `internal/config` | `domain` | adapters, engine |
| `cmd/*` | everything | — (wiring only) |

`depguard` in `.golangci.yml` enforces the engine and domain rows. A new external system is a new adapter behind an existing or new port; it never adds an import to the engine.

### Ports

| Port | Purpose | Adapters |
| --- | --- | --- |
| `Store` | Items, events, leases, factories, runs; transactions | `postgres` |
| `Runner` | Start, watch and stop one agent task | `docker` (laptop), `ecs` (AWS); a Vercel Sandbox adapter is possible later |
| `Secrets` | Resolve `env:`, ARN and (later) `op://` references at the edge | `envsecrets` (now), `awssecrets` (AWS), `onepassword` (last phase) |
| `Knowledge` | `kb_search`, `kb_write`, sync | `bedrockkb` |
| `ModelAuth` | Credentials for the model per factory | subscription token (laptop only), API key, Bedrock |
| `Clock` | Time, for deterministic tests | system, fake |

### Wiring

Manual constructor injection. `cmd/goald/main.go` reads the environment, builds each adapter, passes them to `engine.New(...)` and starts the HTTP server. No container, no globals, no `init()` side effects. A constructor takes its dependencies as interfaces and returns a concrete type.

## Scale and failure

| Concern | Approach |
| --- | --- |
| Horizontal scale | `goald` is stateless; run N instances behind a load balancer. |
| Work distribution | Each instance claims items with `FOR UPDATE SKIP LOCKED`; leases with heartbeats; expired leases return to the queue. |
| Singletons | Constraint detection, intake schedules, kaizen and Jonah schedules run under `pg_try_advisory_lock(<job key>)`, so exactly one instance runs each. |
| Realtime | Each instance `LISTEN`s on `goal_events` and streams to its own SSE clients; no sticky sessions needed. |
| Idempotency | Every engine step is keyed by (item, station, attempt) and safe to retry. |
| Connections | `pgxpool` per instance; RDS Proxy or PgBouncer in transaction mode when instances × pool size grows. |
| Event volume | `item_events` is append-only; partition by month when it passes ~50 M rows. |
| IDs | Generated in Go as UUIDv7, so the schema works on managed PostgreSQL versions without `uuidv7()`; the column default is a convenience for manual SQL only. |
| Shutdown | SIGTERM stops claiming, finishes or releases leases, then exits within the platform's grace period. |
| Observability | Structured `slog` JSON to stdout, OpenTelemetry traces and metrics; Claude Code's OTel events are joined to item traces. |

## Deployment targets

| Target | goald | PostgreSQL | Agent runners | UI | Secrets | Model auth |
| --- | --- | --- | --- | --- | --- | --- |
| Laptop | one process | `compose.yaml` | Docker | served by goald | `.env` now, 1Password in the last phase | Claude subscription token |
| AWS | ECS Fargate service, 2+ tasks behind an ALB | RDS or Aurora PostgreSQL, Multi-AZ | ECS `RunTask`, one task per agent run | served by goald or S3 + CloudFront | Secrets Manager | API key or Bedrock |
| Vercel UI | on AWS as above | as above, or a Vercel Marketplace Postgres (Neon, Supabase) | ECS | Vercel static deploy, calling goald's API | Vercel env for UI only | API key or Bedrock |

Agent runs cannot run as Vercel Functions: they last minutes to hours and need a container. The UI is static, so it deploys to Vercel without changes. All AWS infrastructure for these targets is added to `terraform/` as modules (`goald-service`, `postgres`, `runner`), each planned before it is applied.

The Claude subscription token is for one person on their own machine. Any shared or hosted deployment uses an API key or Bedrock (`runtime.auth` per factory).

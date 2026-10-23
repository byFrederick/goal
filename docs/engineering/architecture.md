# Architecture

Goal is one Go daemon (`goald`), one PostgreSQL database, one versioned object storage bucket for factory definitions, short-lived agent containers and a static React UI. The same binaries run on a laptop, on AWS and, for the UI, on Vercel. Decisions: [ADR 0001](../adr/0001-technology-stack.md) (stack), [ADR 0003](../adr/0003-hexagonal-architecture-and-deployment.md) (architecture and deployment), [ADR 0005](../adr/0005-factory-definitions-in-object-storage.md) (factory definitions in object storage).

## Hexagonal core

```
              ┌──────────────────────── goald ────────────────────────┐
  HTTP/SSE ──▶│ adapters/http ──▶  engine  ◀── domain                  │
  (UI, CLI)   │                      │                                 │
              │                    ports: Store  Runner  Secrets       │
              │                           ObjectStore  Clock  Knowledge │
              │                      │                                 │
              │ adapters: postgres  docker | ecs  envsecrets | awssecrets
              │           s3store (S3 | RustFS)  bedrockkb  gateway     │
              └────────────────────────────────────────────────────────┘
```

| Package | May import | Never imports |
| --- | --- | --- |
| `internal/domain` | standard library | anything else in `internal/` |
| `internal/ports` | `domain` | adapters, engine |
| `internal/engine` | `domain`, `ports` | `adapters`, `config`, database or cloud SDKs |
| `internal/adapters/*` | `domain`, `ports`, their own SDK | `engine`, other adapters |
| `internal/config` | `domain` | adapters, engine |
| `internal/factorystore` | `config`, `ports` | adapters, engine, cloud SDKs |
| `internal/gateway` | `domain`, `ports`, `config`, the MCP Go SDK | adapters, engine, `factorystore`, cloud SDKs |
| `internal/definitions` | `domain`, `ports`, `config`, `factorystore` | adapters, engine, cloud SDKs |
| `internal/api` (+ generated `api/gen`) | `domain`, `ports`, `config`, `engine` | adapters, `factorystore`, `gateway`, cloud SDKs |
| `cmd/*` | everything | — (wiring only) |

`depguard` in `.golangci.yml` enforces every row but the last. A new external system is a new adapter behind an existing or new port; it never adds an import to the engine.

### Ports

| Port | Purpose | Adapters |
| --- | --- | --- |
| `Store` | Items, events, leases, factories, runs; transactions | `postgres` |
| `Runner` | Start, watch and stop one agent task | `docker` (laptop), `ecs` (AWS); a Vercel Sandbox adapter is possible later |
| `Secrets` | Resolve `env:`, ARN and (later) `op://` references at the edge | `envsecrets` (now), `awssecrets` (AWS), `onepassword` (last phase) |
| `ObjectStore` | Versioned objects in one bucket: factory definitions now, knowledge documents from phase 3 | `s3store` (Amazon S3 on AWS, RustFS on a laptop) |
| `Knowledge` | `kb_search`, `kb_write`, sync | `bedrockkb` |
| `ModelAuth` | Credentials for the model per factory | subscription token (laptop only), API key, Bedrock |
| `Clock` | Time, for deterministic tests | `sysclock`, fake |
| `Workspaces` | A fresh clone of the factory's repository on the item's branch, per run | `gitworkspace` (host git) |
| `Checker` | A station's command checks against a run's workspace, isolated from the host | `docker` (laptop); ECS later |
| `Contracts` | Station outputs against the factory's JSON Schema contracts | `definitions` (over the bucket) |

#### Phase 1 signatures

`internal/ports/ports.go` is the source of truth; this is the shape adapters implement.

- `Store`: `CreateItem`, `Item`, `Waiting` (pull order), `Leases`, `ClaimNext(Claim{Station, Slot, Now, ExpiresAt})`, `HeartbeatLease`, `RequeueExpiredLeases`, `StartRun`, `FinishRun`, `Transition` (compare-and-set on station and state, fenced by the lease's `AcquiredAt`), `AppendEvents`, `ItemEvents(item, after, limit)` (paged history for the API). WIP is not counted: the unique `(factory_id, station, slot)` lease makes a claim beyond capacity fail with `ErrSlotTaken`.
- `Runner`: `Start(ctx, TaskSpec) (Task, error)`; `Task` has `Events() <-chan RunEvent` and `Wait() (RunResult, error)`. Cancelling `ctx` stops the task. `TaskSpec.WorkspaceDir` is the host path of the run's prepared git checkout. The `docker` adapter runs one hardened container of the worker image per task, stages the agent's prompt, skills and output schema through its `AgentFiles` dependency, and maps the worker's stdout protocol to events and a result; `worker/README.md` is the container contract.
- `Secrets`: `Resolve(ctx, ref)` for literals and `env:NAME`. `Contracts`: `Validate(ctx, factory, contract, output)`, wrapping `ErrContractViolation` for bad output.
- `Workspaces`: `Prepare(ctx, WorkspaceSpec{Run, Repository, Branch}) (Workspace, error)` and `Remove`. `Checker`: `Check(ctx, CheckSpec{Image, Command, WorkspaceDir, Timeout}) (CheckResult, error)`; a command that ran and failed is a result, not an error. [ADR 0006](../adr/0006-workspaces-and-command-checks.md) has the decisions behind both.
- `Clock`: `Now()` and `NewTicker(d)`.
- `ObjectStore`: `Get(ctx, key, version)` (empty version = current; `ErrNotFound` for a missing or deleted key or version), `Put(ctx, key, body) (ObjectInfo, error)`, `List(ctx, prefix) ([]ObjectInfo, error)` (current objects only, each with its version id), `Delete(ctx, key)` (a delete marker; earlier versions stay readable). `ObjectInfo` is `{Key, Version, Size}`; `Object` adds `Body`.
- Per-task MCP gateway tokens are HMAC-signed (`domain.SignTaskToken`), so any instance can verify them without stored state.
- `internal/adapters/postgres` implements `Store` with pgx and sqlc (`queries/` compiled against `migrations/` into the private package `postgres/db`; `task sqlc` regenerates it). Wiring: `postgres.Open(ctx, url)` returns a `*pgxpool.Pool`, `postgres.Migrate(ctx, pool)` applies the embedded goose migrations under a per-schema advisory lock (safe when several instances start at once), `postgres.New(pool)` returns the store, and `SaveFactory` registers each factory `goald` runs before work enters it (items reference their factory row). Every method is one transaction; locks are taken item first, then its lease or runs. `porttest/storetest` is the conformance suite both the fake and the adapter pass.
- `Knowledge` and `ModelAuth` arrive when the engine first needs them; until then model credentials reach tasks through `engine.Config.TaskEnv` secret references.

### MCP gateway

`internal/gateway` is an inbound HTTP component of `goald`, not an adapter: it reads factory definitions (`config.Factory`), which adapters may not. `gateway.New(Deps, Config)` returns an `http.Handler` that `goald` mounts at its MCP path. Its package comment is the reference; in short:

- **Auth.** Every request carries `Authorization: Bearer <task token>`; a missing, forged or expired token is 401, an agent the current definition no longer declares is 403, both without detail.
- **Stateless.** The SDK's stateless streamable-HTTP mode: each request is verified and served on its own and opens short upstream sessions, so any instance serves any request.
- **Connections.** A task gets the connections in both its token and its agent's `connections`. Remote MCP servers (`type: mcp`, `transport: http`, https or loopback http) are served, with header values like `Bearer env:NAME` resolved through `Secrets` on the host. Builtin (`knowledge`), stdio images and `aws` connections are reported unavailable (initialize instructions, the `goal/connections` `_meta` of `tools/list`, a logged warning) until their phase.
- **Tools.** Named `<connection>__<tool>`. `tools.allow` and `tools.deny` entries of the form `<connection>__<pattern>` (`path.Match`) apply to gateway tools; deny wins, and a connection with allow entries serves only what they match.
- **Audit.** Every `tools/call`, denied ones included, appends a `gateway_tool_call` event (connection, tool, outcome, duration, request and response sizes) through `gateway.Events`, which `ports.Store` satisfies. Arguments, results and credentials are never recorded; resolved credentials are scrubbed from results, errors and logs.
- **Bounds.** Per-call and list timeouts, a cap on upstream response and agent request sizes, no redirects for credentialed requests.

### HTTP API

`api/openapi.yaml` is the contract ([ADR 0004](../adr/0004-frontend-and-monorepo.md)); `task api:gen` regenerates the Go server interface, models and client in `internal/api/gen` (oapi-codegen, pinned in `Taskfile.yml`; generated code is committed). `internal/api` implements the strict server over three small interfaces (the engine, the store's item reads, the definitions cache) and answers errors as RFC 9457 problem details: 404 for unknown items and factories, 400 for invalid requests, 409 for a decision on an item that is not waiting for a human.

| Method and path | Does |
| --- | --- |
| `GET /v1/health` | liveness |
| `GET /v1/factories` | the loaded factories with their stations and work types |
| `POST /v1/factories/{id or name}/items` | submit `{title, type, body?, lane?, priority?}`; the type must be one of the factory's work types |
| `GET /v1/items/{item}` | the item, with each station's output |
| `GET /v1/items/{item}/events?after=&limit=` | one page of its history, oldest first, and `next_after` |
| `POST /v1/items/{item}/approve` | `{by}`: a human approves an item waiting for one |
| `POST /v1/items/{item}/reject` | `{by, reason}`: sends it back for rework |

Phase 1 has no API authentication: `goald` refuses to listen on anything but a loopback address. The CLI (`goal run`, `follow`, `item`, `approve`, `reject`) uses the generated client.

### Factory definitions in object storage

Definitions are content, not code ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md)). One versioned bucket holds every factory under its id:

```
s3://<bucket>/factories/<factory-id>/
  factory.yaml  agents/  skills/  contracts/  evals/     the definition
  knowledge/<collection>/                                reserved for the knowledge base (phase 3)
```

- `internal/config` loads a definition from any `fs.FS`; `LoadDir` wraps a local working copy. Paths in `factory.yaml` resolve inside that tree only.
- `internal/factorystore` owns the layout. `Push` validates a folder, uploads new and changed files (`factory.yaml` last), deletes files that were removed and never touches `knowledge/`. `Open` reads a factory's prefix at the listed object versions into an in-memory `fs.FS` (`Snapshot`), so a reader never mixes two pushes and can record exactly which versions it ran. `Pull` writes a snapshot into an empty folder; `List` validates every factory in the bucket.
- `internal/adapters/s3store` implements `ObjectStore` with the AWS SDK for Go v2: a custom endpoint with path-style addressing for RustFS, the standard credential chain on AWS. `EnsureBucket` (used by `goal storage init`) creates the bucket and turns versioning on.
- Rollback restores earlier object versions; the bucket keeps them for the lifecycle window.

### Wiring

Manual constructor injection. `cmd/goald/main.go` reads the environment (`cmd/goald/settings.go` lists every variable), builds each adapter, passes them to `engine.New(...)` and starts the HTTP server. No container, no globals, no `init()` side effects.

```
settings (env) ─┬─ envsecrets.New ───────────────────────────────┐ Secrets
                ├─ postgres.Open → Migrate → postgres.New ────────┤ Store (+ SaveFactory)
                ├─ s3store.New → factorystore.New → definitions.New(catalog, load) ─┐
                │      Contracts, gateway.Definitions, docker.AgentFiles ◀──────────┘
                ├─ gitworkspace.New(secrets) ─────────────────────┤ Workspaces
                ├─ docker client → docker.New(defs) / NewChecker ─┤ Runner, Checker
                └─ sysclock.Clock ────────────────────────────────┤ Clock
                                                                  ▼
                                        engine.New(Deps, Config{GatewayURL, GatewayKeyRef, TaskEnv})
   load hook: every refresh → store.SaveFactory(each) → engine.SetLines(lines)
   http.ServeMux: /v1/ → api.New(engine, store, defs)    /mcp → gateway.New(defs, secrets, store)
   goroutines: engine.Run (ticks + lease sweep), definitions refresh (GOAL_REFRESH_EVERY), HTTP
   startup: Refresh, then reap orphaned containers, run folders and workspaces
   SIGTERM: HTTP shutdown, engine context cancelled → runs stopped, items released to their queue
```

The definitions cache refreshes from the bucket at startup, every `GOAL_REFRESH_EVERY` (30s) and when the API is asked for a factory it does not know, so `goal factory push` followed by `goal run` works without a restart. A definition that stops loading keeps its last valid version in use.

### One run, end to end

```
goal run goal "Add a goal version subcommand"
  → POST /v1/factories/goal/items          engine.Submit: item #N queued at inbox
  → tick: inbox (queue) releases it to spec
  → tick: spec claims it (lease on a slot; heartbeat while the run lasts)
      StartRun
      Workspaces.Prepare   git clone of runtime.repository (token only in git's env)
                           checkout goal/<N>-<slug> (remote branch if it exists, else new)
      Runner.Start         container of runtime.image: /workspace = the clone, /goal = spec.json
                           and agent files (prompt, local skills, output schema), env =
                           CLAUDE_CODE_OAUTH_TOKEN + GOAL_GATEWAY_TOKEN; MCP: /mcp on the host
         agent → /mcp       gateway checks the task token, serves the agent's allowed GitHub
                           tools, adds GOAL_GITHUB_TOKEN upstream, appends gateway_tool_call
      worker stdout       → events (tool_call, agent_message, …) and the result
      FinishRun           cost and turns on the run and the item
      Contracts.Validate  output against the station's contract (retry once with the error)
      Checker.Check       each command check in its own container on the same workspace
                          (retry once with the failing output)
      Workspaces.Remove
  → Transition to the next station (approve-spec: waiting_human)
goal approve <item>       POST /v1/items/<id>/approve → build, then review, then merge (human)
``` A constructor takes its dependencies as interfaces and returns a concrete type.

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

| Target | goald | PostgreSQL | Object storage (definitions) | Agent runners | UI | Secrets | Model auth |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Laptop | one process | `compose.yaml` | RustFS in `compose.yaml` (`rustfs/rustfs`, S3 API on 127.0.0.1:9000) | Docker | served by goald | `.env` now, 1Password in the last phase | Claude subscription token |
| AWS | ECS Fargate service, 2+ tasks behind an ALB | RDS or Aurora PostgreSQL, Multi-AZ | Amazon S3, versioned, KMS-encrypted; task role through the standard credential chain | ECS `RunTask`, one task per agent run | served by goald or S3 + CloudFront | Secrets Manager | API key or Bedrock |
| Vercel UI | on AWS as above | as above, or a Vercel Marketplace Postgres (Neon, Supabase) | Amazon S3 as above | ECS | Vercel static deploy, calling goald's API | Vercel env for UI only | API key or Bedrock |

Agent runs cannot run as Vercel Functions: they last minutes to hours and need a container. The UI is static, so it deploys to Vercel without changes. All AWS infrastructure for these targets is added to `terraform/` as modules (`goald-service`, `postgres`, `runner`), each planned before it is applied.

The Claude subscription token is for one person on their own machine. Any shared or hosted deployment uses an API key or Bedrock (`runtime.auth` per factory).

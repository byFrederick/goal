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
| `internal/adapters/*` | `domain`, `ports`, their own SDK | `engine`, `api`, other adapters |
| `internal/config` | `domain` | adapters, engine |
| `internal/factorystore` | `config`, `ports` | adapters, engine, cloud SDKs |
| `internal/gateway` | `domain`, `ports`, `config`, the MCP Go SDK | adapters, engine, `factorystore`, cloud SDKs |
| `internal/definitions` | `domain`, `ports`, `config`, `factorystore` | adapters, engine, cloud SDKs |
| `internal/api` (+ generated `api/gen`) | `domain`, `ports`, `config`, `engine` | adapters, `factorystore`, `definitions`, `gateway`, cloud SDKs |
| `cmd/*` | everything | — (wiring only) |

`depguard` in `.golangci.yml` enforces every row but the last. A new external system is a new adapter behind an existing or new port; it never adds an import to the engine.

### Ports

| Port | Purpose | Adapters |
| --- | --- | --- |
| `Store` | Items, events, leases, factories, runs; transactions | `postgres` |
| `Runner` | Start, watch and stop one agent task | `docker` (laptop), `ecs` (AWS); a Vercel Sandbox adapter is possible later |
| `Secrets` | Resolve `env:`, ARN and (later) `op://` references at the edge | `envsecrets` (now), `awssecrets` (AWS), `onepassword` (last phase) |
| `ObjectStore` | Versioned objects in one bucket: factory definitions now, knowledge documents later (unscheduled) | `s3store` (Amazon S3 on AWS, RustFS on a laptop) |
| `Knowledge` | `kb_search`, `kb_write`, sync | `bedrockkb` |
| `ModelAuth` | Credentials for the model per factory | subscription token (laptop only), API key, Bedrock |
| `Clock` | Time, for deterministic tests | `sysclock`, fake |
| `Workspaces` | A fresh clone of the factory's repository on the item's branch, per run | `gitworkspace` (host git) |
| `Checker` | A station's command checks against a run's workspace, isolated from the host | `docker` (laptop); ECS later |
| `Sidecars` | A run's stdio MCP servers, each in its own hardened container, used by the gateway | `docker` (laptop); ECS later |
| `Contracts` | Station outputs against the factory's JSON Schema contracts | `definitions` (over the bucket) |
| `ItemNotices` | A notice of every committed item event, for the realtime stream | `postgres` (`LISTEN goal_events`) |
| `Tenancy` | The install's organizations: list, resolve by slug or id, create, update, soft delete, purge; each change audited in its transaction | `postgres` (cross-org reads through `SECURITY DEFINER` functions) |
| `Audit` | An organization's append-only audit log | `postgres` |
| `CodeHost` | Read, merge and clean up the pull requests of a factory's repository, on the host | `github` (REST API) |

#### Phase 1 signatures

`internal/ports/ports.go` is the source of truth; this is the shape adapters implement.

- `Store`: `CreateItem`, `Item`, `Waiting` (pull order), `Leases`, `ClaimNext(Claim{Station, Slot, Now, ExpiresAt})`, `HeartbeatLease`, `RequeueExpiredLeases`, `StartRun`, `FinishRun`, `Transition` (compare-and-set on station and state, fenced by the lease's `AcquiredAt`), `AppendEvents`, `ItemEvents(item, after, limit)` (paged history for the API). WIP is not counted: the unique `(factory_id, station, slot)` lease makes a claim beyond capacity fail with `ErrSlotTaken`. Phase 2 adds the reads behind the board and metrics: `ItemByNumber(factory, n)`, `ListItems(ItemQuery{States, Station, Before, Limit})` (newest first, paged by number), `StationLoads(factory)` (waiting and in progress per station, from the `station_wip` view) and `FlowMetrics(factory, since, until)` (done per UTC day, lead-time p50/p85 by linear interpolation, reworked, WIP now, spend of runs started in the window).
- `ItemNotices`: `Listen(ctx, deliver)` calls `deliver` for every committed item event (`ItemNotice{EventID, OrgID, FactoryID, ItemID, Number, Kind, Station, State, At}`, the state being the item's right after the event) until `ctx` ends. Every time it starts listening, the first time and after each reconnection, it first delivers a `Resync` notice, because events committed while it was disconnected are lost. The `postgres` adapter listens on a connection of its own (not a pooled one) and reconnects with backoff (100 ms doubling to 10 s); the `item_events` trigger (migration 00003) puts the item's number and state in the payload.
- `CodeHost`: `PullRequest(repo, url)`, `Merge(repo, url, Merge{Method, HeadSHA})` and `DeleteBranch(repo, branch)`. A pull request must belong to `repo` (the factory's `runtime.repository`) and refusals wrap `ErrNotMergeable`. The `github` adapter resolves `repo.AuthRef` through `Secrets` per call, sends it only as the `Authorization` header to the configured API (`GOAL_GITHUB_API_URL`), follows no redirects and scrubs it from every error.
- `Runner`: `Start(ctx, TaskSpec) (Task, error)`; `Task` has `Events() <-chan RunEvent` and `Wait() (RunResult, error)`. Cancelling `ctx` stops the task. `TaskSpec.WorkspaceDir` is the host path of the run's prepared git checkout. The `docker` adapter runs one hardened container of the worker image per task, stages the agent's prompt, skills and output schema through its `AgentFiles` dependency, and maps the worker's stdout protocol to events and a result; `worker/README.md` is the container contract.
- `Secrets`: `Resolve(ctx, ref)` for literals and `env:NAME`. `Contracts`: `Validate(ctx, factory, contract, output)`, wrapping `ErrContractViolation` for bad output.
- `Workspaces`: `Prepare(ctx, WorkspaceSpec{Run, Repository, Branch}) (Workspace, error)` and `Remove`. `Checker`: `Check(ctx, CheckSpec{Image, Command, WorkspaceDir, Timeout}) (CheckResult, error)`; a command that ran and failed is a result, not an error. [ADR 0006](../adr/0006-workspaces-and-command-checks.md) has the decisions behind both.
- `Sidecars`: `Start(ctx, SidecarSpec{Run, Connection, Image, Command, Env, Network}) (Sidecar, error)`; a `Sidecar` is an `io.ReadWriteCloser` over the server's stdout and stdin, and `Close` removes its container. `Env` carries resolved values that adapters never log. The `docker` adapter's sidecars carry the run's labels plus `goal.connection`, so `Runner.Reap` removes orphans with their run.
- `Clock`: `Now()` and `NewTicker(d)`.
- `ObjectStore`: `Get(ctx, key, version)` (empty version = current; `ErrNotFound` for a missing or deleted key or version), `Put(ctx, key, body) (ObjectInfo, error)`, `List(ctx, prefix) ([]ObjectInfo, error)` (current objects only, each with its version id), `Delete(ctx, key)` (a delete marker; earlier versions stay readable). `ObjectInfo` is `{Key, Version, Size}`; `Object` adds `Body`.
- Per-task MCP gateway tokens are HMAC-signed (`domain.SignTaskToken`), so any instance can verify them without stored state.
- `internal/adapters/postgres` implements `Store` with pgx and sqlc (`queries/` compiled against `migrations/` into the private package `postgres/db`; `task sqlc` regenerates it). Wiring: `postgres.Open(ctx, url)` returns a `*pgxpool.Pool`, `postgres.Migrate(ctx, pool)` applies the embedded goose migrations under a per-schema advisory lock (safe when several instances start at once), `postgres.CheckAppRole(ctx, pool)` refuses a runtime role that row-level security would not bind, `postgres.New(pool)` returns the store, and `SaveFactory` registers each factory `goald` runs before work enters it (items reference their factory row). Every method is one transaction; locks are taken item first, then its lease or runs. `porttest/storetest` is the conformance suite both the fake and the adapter pass.
- **Organizations.** Every org-owned table (`factories`, `work_items`, `leases`, `agent_runs`, `item_events`, `andon_pulls`, `audit_log`) carries `org_id` (default `goal_org()`), composite foreign keys to its parents in the same organization, and the `org_isolation` policy; `station_wip` runs with the caller's rights. Events are appended with one batched `INSERT` per event, because `COPY` is refused under row-level security. Org-owned rows are bound to the organization of their transaction by row-level security: `domain.WithOrg(ctx, id)` puts the org in the context, the adapter runs `set_config('goal.org_id', id, true)` first in each such transaction, and the policies compare with `goal_org()`, which fails when it is unset. `Tenancy`'s cross-org reads (`goal_orgs`, `goal_org_by_ref`, `goal_active_orgs`, the purge) are `SECURITY DEFINER` functions pinned to the schema that return organization rows and ids only. `postgres.DefaultOrgID` is the organization rows from before organizations moved into. `porttest/tenancytest` is the conformance suite of `Tenancy` and `Audit`.
- **Database roles.** goald never connects as the bootstrap superuser. `goal_owner` owns the schema and applies migrations (`GOAL_MIGRATE_DATABASE_URL`, on a pool closed before goald serves); `goal_app` runs everything else (`GOAL_DATABASE_URL`) and owns nothing, so row-level security binds it, and `CheckAppRole` refuses a superuser, a `BYPASSRLS` role, a member of `goal_owner` or a table owner at startup. `internal/adapters/postgres/roles.sql` creates both roles (idempotent, no `LOGIN`); `goal db init` (`postgres.InitRoles`, run by `goal up` every time, as the admin `GOAL_DATABASE_ADMIN_URL`, default compose's `goal` superuser) gives them `LOGIN` and the passwords `GOAL_PG_OWNER_PASSWORD`/`GOAL_PG_APP_PASSWORD` (dev-only defaults), grants the database and schema, and hands every object the bootstrap user owns to `goal_owner`, so a database from before the roles upgrades in place. There are no default privileges: each migration grants `goal_app` what it needs on the objects it adds (`00004_app_role.sql` for those of `00001–00003`). Integration tests connect as compose's superuser and switch roles with the `role` startup parameter, so they never touch the roles' passwords.
- `Knowledge` and `ModelAuth` arrive when the engine first needs them; until then model credentials reach tasks through `engine.Config.TaskEnv` secret references.

### MCP gateway

`internal/gateway` is an inbound HTTP component of `goald`, not an adapter: it reads factory definitions (`config.Factory`), which adapters may not. `gateway.New(Deps, Config)` returns an `http.Handler` that `goald` mounts at its MCP path. Its package comment is the reference; in short:

- **Auth.** Every request carries `Authorization: Bearer <task token>`; a missing, forged or expired token is 401, an agent the current definition no longer declares is 403, both without detail.
- **Stateless towards agents.** The SDK's stateless streamable-HTTP mode: each request is verified and served on its own and opens short sessions with remote servers. The one exception is the registry of running sidecars (below), keyed by run: it lives in the `goald` that started them, so a run's MCP traffic must reach that instance. Phase 2 runs one `goald`, so it always does; running several needs requests routed by run id (it is in the token) or sidecars that any instance can reach, such as ECS tasks with a network endpoint. A dead `goald`'s sidecars are orphans `Runner.Reap` removes.
- **Connections.** A task gets the connections in both its token and its agent's `connections`. Remote MCP servers (`type: mcp`, `transport: http`, https or loopback http) are served, with header values like `Bearer env:NAME` resolved through `Secrets` on the host. Stdio servers with an `image` are served as sidecars. Builtin (`knowledge`), `aws` connections and stdio commands without an image (they would run on the host) are reported unavailable (initialize instructions, the `goal/connections` `_meta` of `tools/list`, a logged warning) until their phase.
- **Sidecars.** A stdio connection's server runs in its own container per run (`ports.Sidecars`), started lazily by the run's first `tools/list` or `tools/call` that needs it, reused through one MCP session over its stdio for the rest of the run, and stopped when the engine reports the run ended (`engine.Deps.RunEnded` → `Gateway.EndRun`), after `SidecarIdle` (10 minutes) without requests, when its server exits, and at shutdown (`Gateway.Close`). `MaxSidecars` (16) caps one `goald`; past it, and when a sidecar fails to start, the connection is unavailable for that request only. The `env` values and `command` words that are secret references are resolved on the host and reach only the sidecar's environment and arguments, never the agent's container, and are scrubbed from results, errors, logs and events like header credentials. The image must be pinned (a version tag or a digest; `config.Pinned`): `goal factory validate` and `push` warn about an unpinned one, and the gateway reports it unavailable instead of starting it. The Docker adapter gives a sidecar the hardening of an agent task (read-only root with tmpfs `/tmp` as `HOME`, no capabilities, no new privileges, init, 1 CPU, 2 GiB, 512 PIDs), runs it as its image's user unless that is root (then as nobody), mounts nothing, and gives it no network unless the connection sets `network: true` (the default bridge, where `host.docker.internal` is the host). The first start pulls the image within `SidecarStartTimeout` (5 minutes) and outlives the request that began it; pull large images ahead of a run, since the worker lists tools once at start.
- **Tools.** Named `<connection>__<tool>`. `tools.allow` and `tools.deny` entries of the form `<connection>__<pattern>` (`path.Match`) apply to gateway tools; deny wins, and a connection with allow entries serves only what they match.
- **Audit.** Every `tools/call`, denied ones included, appends a `gateway_tool_call` event (connection, tool, outcome, duration, request and response sizes) through `gateway.Events`, which `ports.Store` satisfies. Arguments, results and credentials are never recorded; resolved credentials are scrubbed from results, errors and logs.
- **Upstream protocol.** Upstream sessions negotiate the latest MCP protocol. On the 2026 protocol a server may require `Mcp-Param-*` headers mirroring annotated arguments (`x-mcp-header`; GitHub's server does), which the SDK adds only for tools the session has listed, so a call lists the upstream's tools first. The gateway's own `tools/list` result is `cacheScope: private` (it depends on the task's token).
- **Bounds.** Per-call and list timeouts, a cap on upstream response and agent request sizes, no redirects for credentialed requests.

### HTTP API

`api/openapi.yaml` is the contract ([ADR 0004](../adr/0004-frontend-and-monorepo.md)); `task api:gen` regenerates the Go server interface, models and client in `internal/api/gen` (oapi-codegen, pinned in `Taskfile.yml`) and the TypeScript types in `packages/api-client/src/schema.ts` (openapi-typescript); both are committed, and `task check` fails when the TypeScript side is stale. `internal/api` implements the strict server over small interfaces (the engine, the store's reads, the definitions cache, the stream feed, the clock) and answers errors as RFC 9457 problem details: 404 for unknown items and factories, 400 for invalid requests, 409 when the item's state does not allow the action (a decision on an item not waiting for a human, a retry of an item that is not blocked, a merge away from a merge station or one the pull request refuses).

`{item}` is the item's id or `<factory>#<number>` (`goal#2`, the factory by name or id; `#` percent-encoded as `%23`), and every item carries its `ref`. `{factory}` is a factory's id or name.

| Method and path | Does |
| --- | --- |
| `GET /v1/health` | liveness |
| `GET /v1/factories` | the loaded factories with their stations (and a station's `action`) and work types |
| `POST /v1/factories/{factory}/items` | submit `{title, type, body?, lane?, priority?}`; the type must be one of the factory's work types |
| `GET /v1/factories/{factory}/items?state=&station=&limit=&after=` | one page of item cards, newest first, and `next_after` (the last number, or null) |
| `GET /v1/factories/{factory}/board` | the stations in line order with their agents, `wip_limit`, `waiting`, `in_progress` and cards (every open item up to 500, the 20 newest done), and the `constraint` |
| `GET /v1/factories/{factory}/metrics?window=7d` | throughput per UTC day, lead-time p50/p85 in hours, WIP, rework rate, spend and items done over the last N days |
| `GET /v1/stream?factory=` | Server-Sent Events of item events (below) |
| `GET /v1/items/{item}` | the item, with each station's output |
| `GET /v1/items/{item}/events?after=&limit=` | one page of its history, oldest first, and `next_after` |
| `POST /v1/items/{item}/approve` | `{by}`: a human approves an item waiting for one |
| `POST /v1/items/{item}/reject` | `{by, reason}`: sends it back for rework |
| `POST /v1/items/{item}/retry` | `{by}`: a blocked item goes back to its station's queue; its rework count is unchanged |
| `POST /v1/items/{item}/merge` | `{by, method?: squash}`: merge the item's pull request, then approve it (below) |

An item card is `{id, ref, number, title, type, lane, station, state, class_of_service, rework_count, cost_usd, created_at, updated_at, age_seconds, agent, pr_url}`: `agent` is the lease holder's agent while the item is in progress, `pr_url` the `pull_request` field of the item's latest station output, `age_seconds` the time since creation (created to done for a done item).

The board's `constraint` is the agent station with the most work per slot right now, `(waiting + in_progress) / min(agents, wip)`, ties to the upstream station, null when no agent station has work. It is a snapshot heuristic; constraint detection over time (later, unscheduled) replaces it. Metrics count items that reached done in the window (the last N UTC calendar days, today included, 1d to 90d) and the cost of agent runs started in it; WIP is the items now neither done nor cancelled, the rework rate the share of the items done in the window that were sent back at least once.

No API authentication yet: `goald` refuses to listen on anything but a loopback address. The CLI (`goal run`, `follow`, `item`, `approve`, `reject`, `retry`, `merge`) uses the generated Go client; a bare item number resolves when exactly one factory has an item with it (`--factory` otherwise). The web UI uses `@goal/api-client` (`packages/api-client`): `createGoalClient(baseUrl)` (openapi-fetch over the generated types) and `subscribe(baseUrl, handlers, factory?)` over the stream.

#### Realtime stream

`GET /v1/stream` is `text/event-stream`. Each committed item event is `event: item`, `id:` the event id, `data:` `{item_id, ref, factory_id, kind, station, state, at}` (`StreamEvent` in the contract); `?factory=` keeps one factory's. `event: resync` (`data: {}`) says events may have been missed, so the client re-reads what it shows; a `: heartbeat` comment comes every 15 s and `retry: 3000` opens the stream. There is no replay (`Last-Event-ID` is ignored): a client re-reads on every (re)connection, which `subscribe` signals as a resync.

```
item_events INSERT ── trigger notify_item_event() ── pg_notify('goal_events', {org_id, id, factory_id, item_id, number, kind, station, state, at})
        │  (any goald instance, in the transaction that changes the item)
        ▼
goald: postgres.Notices.Listen (one connection, reconnect + Resync) ──▶ api.Feed (fan-out) ──▶ each SSE client of this instance
```

Each instance listens once and fans out to its own clients, so `goald` stays stateless and any instance serves any client. A client that falls 256 notices behind has its stream ended (it reconnects and re-reads) so it never holds up the others; at shutdown the feed ends every stream before HTTP drains.

#### Merge stations

A human station may declare `action: merge` (schema: human stations only; the factory needs `runtime.repository`). `POST /v1/items/{item}/merge` (`goal merge <item>`) is `engine.Merge`:

1. The item must be waiting at that station (409 otherwise), and one of its outputs must name a `pull_request` URL on the factory's repository (409 otherwise). The latest station on the line that names one wins.
2. Through `CodeHost` with the repository's token: read the pull request; refuse (409, nothing changed) unless it is open and GitHub reports it mergeable. Then merge it with `merge_method` squash and the head SHA just read, so GitHub refuses if the branch moved; never forced. Read it again and require `merged`.
3. Delete the pull request's head branch, only when it lives on the same repository and is not its base branch; a failure there is logged, not fatal. Nothing else is ever deleted.
4. Approve the item in one transition with a `merged` event (`pull_request`, `sha`, `method`, `branch_deleted`) and an `approved` event by `by`.

A pull request that is already merged (a merge whose approval failed) is approved again, so the step is safe to retry. No credential reaches events, errors or logs.

### Factory definitions in object storage

Definitions are content, not code ([ADR 0005](../adr/0005-factory-definitions-in-object-storage.md) and its 2.2 amendment). The content lives in the bucket; the versions and the pointer to the running one live in PostgreSQL:

```
s3://<bucket>/
  orgs/<org-id>/blobs/<sha256>          every definition file (factory.yaml, agents/, skills/, contracts/, evals/), once per org, never overwritten
  catalog/templates/<name>/<version>/   curated starting points; catalog/skills/ (D) and catalog/connectors/ (phase 3) beside it

PostgreSQL
  definition_versions   one row per draft or published version: manifest {path → sha256}, number, revision, author, publisher
  definition_blobs      the hashes each org has stored
  factories.current_version_id          the published version that runs; publish and restore move it in one transaction
  agent_runs.definition_version_id      the version a run started on, and finishes on
```

- **Content.** A blob's key is the lowercase hex SHA-256 of its bytes. It is written with a conditional put and never rewritten, so a version's tree is reproducible from its manifest alone and identical files are stored once across versions and clones. Readers verify the hash of every blob they fetch.
- **Versions.** A factory has at most one open draft, shared by its editors and saved with optimistic concurrency on its revision. Publishing makes it version N+1, moves `current_version_id` and writes the audit row in one transaction; a trigger NOTIFYs `goal_definitions`, and every goald reloads. Published versions never change; restore moves the pointer back, and numbers are never reused.
- **Runs.** A run keeps the version it started on (contract, checks, agent files, gateway tools); an item's next step uses the current version.
- `internal/config` loads a definition from any `fs.FS`, built from a manifest and its blobs or, for a local working copy, by `LoadDir`. Paths in `factory.yaml` resolve inside that tree only. `config.Check` reports every finding as a located `domain.DefinitionProblem` (stable kebab-case code, severity, the check that found it, the Visual section and model pointer, file, line and column, and machine-applicable fixes); `Load` fails on the blocking ones. `Factory.SecretUses` lists every secret reference with its place and purpose for the credentials check.
- `internal/adapters/s3store` implements `ObjectStore` with the AWS SDK for Go v2: a custom endpoint with path-style addressing for RustFS, the standard credential chain on AWS. `EnsureBucket` (used by `goal storage init`) creates the bucket and turns versioning on, which stays as protection against deletes, no longer as the version mechanism.
- Editing goes through goald's API: the builder and the CLI (`goal factory pull/push/validate/publish`) write the draft.
- **Transition (2.2-B).** The pre-2.2 layout, one prefix of mutable files per factory read by `internal/factorystore` (`factories/<id>/`, moved by 2.2-A to the interim `orgs/<org-id>/factories/<id>/`), is imported once as v1 of each factory and then ignored. Until that import ships (B7), goald and the CLI still read and write that prefix.

### Wiring

Manual constructor injection. `cmd/goald/main.go` reads the environment (`cmd/goald/settings.go` lists every variable), builds each adapter, passes them to `engine.New(...)` and starts the HTTP server. No container, no globals, no `init()` side effects.

```
settings (env) ─┬─ envsecrets.New ───────────────────────────────┐ Secrets
                ├─ postgres.Open(migrate URL, goal_owner) → Migrate → close
                ├─ postgres.Open(goal_app) → CheckAppRole → New ───┤ Store (+ SaveFactory)
                ├─ s3store.New → MigrateLegacy → factorystore.New → definitions.New(catalog, tenancy, load) ─┐
                │      Contracts, gateway.Definitions, docker.AgentFiles ◀──────────┘
                ├─ gitworkspace.New(secrets) ─────────────────────┤ Workspaces
                ├─ github.New(secrets, GOAL_GITHUB_API_URL) ──────┤ CodeHost
                ├─ docker client → docker.New(defs) / NewChecker ─┤ Runner, Checker
                │      docker.NewSidecars → gateway.New(defs, secrets, store, sidecars)
                └─ sysclock.Clock ────────────────────────────────┤ Clock
                                                                  ▼
                        engine.New(Deps{..., Orgs: tenancy, RunEnded: gateway.EndRun}, Config{GatewayURL, GatewayKeyRef, TaskEnv, MaxRuns})
   load hook: every refresh → store.SaveFactory(each, in its org) → engine.SetLines(lines, each with its OrgID)
   postgres.NewNotices(pool's conn config) → api.NewFeed ─────────── ItemNotices for /v1/stream
   http.ServeMux: /v1/ → api.New(engine, store, defs, feed, clock)    /mcp → gateway
   goroutines: engine.Run (ticks + lease sweep), definitions refresh (GOAL_REFRESH_EVERY), feed.Run, HTTP
   startup: Refresh, then reap orphaned containers (task, check, sidecar), run folders and workspaces
   shutdown: HTTP, then the engine, then gateway.Close (stops every sidecar)
   SIGTERM: HTTP shutdown, engine context cancelled → runs stopped, items released to their queue
```

The definitions cache refreshes from the bucket at startup, every `GOAL_REFRESH_EVERY` (30s) and when the API is asked for a factory it does not know, so `goal factory push` followed by `goal run` works without a restart. A definition that stops loading keeps its last valid version in use. It loads the factories under `orgs/<org-id>/factories/` of every organization `Tenancy.ActiveOrgs` lists (a factory's organization is where it is stored, `factorystore.Prefix(org, id)`), and every read (`Factories`, `Lookup`, `Factory`, contracts, agent files) sees only the organization in its context. At startup `factorystore.MigrateLegacy` copies the layout before organizations (`factories/<id>/`) into the default organization and, once every copy matches, deletes the legacy keys (delete markers; old versions stay readable); the CLI's `factory` commands run it too, so a push never lands before it.

**Engine across organizations.** Each `domain.Line` carries its `OrgID`, and every store call the engine makes for a line, the run's included, is bound to it (`domain.WithOrg`); `Submit` and the decisions find only factories of the organization in their context (another's is `ErrNotFound`). `Tick` releases queue stations line by line, then claims with two-level round-robin: in each pass every organization with work claims at most one item, trying its factories in turn and their agent stations in line order, until nothing more can be claimed or the instance runs `GOAL_MAX_RUNS` runs (0: no cap). The order of organizations and of each one's factories rotates every tick; the tick counter and the run count are local scheduling hints, not state. `SweepLeases` requeues expired leases once per organization `ActiveOrgs` lists, so leases on factories no longer loaded are swept too.

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
goal approve goal#N       POST /v1/items/goal%23N/approve → build, then review, then merge (human)
goal merge goal#N         POST /v1/items/goal%23N/merge → squash-merge the build's pull request, approve → done
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

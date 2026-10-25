# Goal — config-driven AI agent factories

Goal runs AI agent factories on your machine. A factory is a definition: `factory.yaml` declares its stations, agents, skills, connections, WIP limits and knowledge base; Markdown files hold the agent prompts and skills. The engine moves work through the factory as a pull-based Kanban flow, built on the Theory of Constraints (*The Goal*) and the Three Ways (*The Phoenix Project*).

Factory definitions are content, not code. goald keeps them, not this repository: file content in one versioned S3-compatible bucket, drafts and published versions in PostgreSQL (ADR 0005 and its 2.2 amendment). The first factory, `goal`, builds this repository; others (`sre`, `university`) run the same engine on other work.

```
cmd/goal/              CLI: goal up, run, follow, item, approve, reject, retry, merge; goal factory pull|validate|push|publish|list|versions|restore; goal org list|use|create; goal login|logout|whoami; goal key; goal admin; goal connection; goal status; goal storage init; goal catalog skills|templates; goal db init|migrate|recover-admin|recover-owner
cmd/goald/             daemon: flow engine, HTTP API (/v1/orgs/{org}/..., with an SSE stream per organization), MCP gateway (/mcp), Docker agent runs
api/                   openapi.yaml: the HTTP API contract (Go types in internal/api/gen, TypeScript in packages/api-client)
auth/                  identity service (Better Auth on Hono): sign-in, sessions, members and invitations, behind goald's /api/auth
internal/engine/       flow engine: pull, WIP, gates, workspaces and command checks per run
internal/config/       loader and cross-reference validation (reads a folder or a bucket prefix)
internal/authoring/    definition authoring behind the API: drafts, validation, publish, restore, the template catalog
internal/adapters/     postgres (store, LISTEN/NOTIFY), s3store (S3 and RustFS), docker (sandboxes), github (merges), envsecrets, sysclock
packages/api-client/   @goal/api-client: typed TypeScript client of the API (generated types + openapi-fetch) for the UI
schema/                factory.schema.json, evals.schema.json (editor autocompletion + validation)
migrations/            PostgreSQL schema (goose, forward-only)
queries/               SQL for sqlc (pgx/v5)
compose.yaml           local PostgreSQL 18 with pgvector, RustFS (S3-compatible object storage) and Mailpit (the dev inbox)
worker/                agent container images (TypeScript, Claude Agent SDK): runtime and dev (Go, task, Terraform, PostgreSQL)
web/                   UI (React)
terraform/             the platform's own Cloudflare infrastructure: R2 bucket, DNS, Worker route, Secrets Store secrets
design-system/         UI rules generated with ui-ux-pro-max, plus the user's overrides (MASTER.md)
docs/adr/              decisions (0001 stack, 0002 Jonah evals, 0003 hexagonal architecture, 0004 frontend, 0005 definitions in object storage, 0006 workspaces and command checks)
docs/engineering/      architecture and testing rules
docs/jonah/            evaluation and self-remediation plan
AGENTS.md              rules for every agent and human (CLAUDE.md imports it)
Taskfile.yml           task check = everything the pre-push hook and CI run
.githooks/             pre-push hook: task check (+ task migrate-check when migrations/ changed)
.github/               CI workflow, currently disabled (no Actions minutes); runs the same tasks
.claude/skills/        skills.sh skills used to build Goal itself, pinned in skills-lock.json
```

## Quick start

Tools: Go, [Task](https://taskfile.dev), Docker, Terraform, tflint, sops and golangci-lint v2 (`mise install` reads `.mise.toml`).

```bash
task hooks:install         # once per clone: pushes run task check first
cp .env.example .env       # optional; defaults already point at the local RustFS
task check                 # lint, race tests, factory validation, terraform
task migrate-check         # migrations apply from zero on PostgreSQL 18, as goal_owner
task storage:up            # RustFS on 127.0.0.1:9000 (console on :9001)
task storage:init          # bucket goal-local (no versioning needed)
task test-integration      # adapters against PostgreSQL 18 and RustFS
```

### Run a factory

```bash
# .env: GOAL_GATEWAY_KEY (openssl rand -hex 32) and GOAL_MASTER_KEYS=k1:<openssl rand -base64 32> (keep a copy offline)
task up                                            # the agent image (goal-agent, worker/README.md) and the egress proxy's, PostgreSQL + RustFS, bucket, then goald in the foreground
# in another terminal, once: the organization's connections a factory runs on (or the Connections page);
# each reads its secret from stdin: paste it, then Ctrl-D (task connection:import-env upgrades an older .env)
bin/goal connection create claude --kind claude-subscription --secret token=-   # the output of claude setup-token
bin/goal connection create github --kind github --secret token=-                # a fine-grained token, these repos only
task factory:push DIR=../goal-factories/goal -- --publish  # the definition goald runs: runtime.modelConnection: claude,
                                                   # runtime.repository.connection: github, connections: [github]
bin/goal run goal "Add a goal version subcommand"  # submit; prints the timeline until a human must act
bin/goal item goal#2                               # read the spec the architect wrote (or: goal item 2)
bin/goal approve goal#2                            # approve-spec → build → review → merge (you)
bin/goal follow goal#2                             # follow it again
bin/goal merge goal#2                              # at the merge station: goald merges the pull request, then approves
bin/goal retry goal#2                              # a blocked item goes back to its station's queue
```

### Sign in

People sign in (`GOAL_AUTH=session`, the default): by a six-digit code mailed to them, or with Google. The identity service (`auth/`, Better Auth) runs beside goald and is reached only through goald's `/api/auth`.

```bash
# .env: BETTER_AUTH_SECRET (openssl rand -hex 32, not GOAL_GATEWAY_KEY),
#       GOAL_BOOTSTRAP_OWNER_EMAIL (the first owner's address) and database passwords of your own
#       (GOAL_PG_*_PASSWORD; goal up refuses the published dev ones); .env.example lists every sign-in variable
task up                    # also builds auth/ and starts it and Mailpit (codes at http://127.0.0.1:8025,
                           # user goal, password in ~/.config/goal/mailpit-ui-auth, which goal up generates)
open http://127.0.0.1:8080 # sign in as the bootstrap owner with the code Mailpit received
bin/goal login             # the CLI: approve the code it shows in the browser; kept in the OS keychain
bin/goal whoami
```

The bootstrap owner becomes a platform admin and owner of every organization (`default` included), then invites people from Members; rows from before sign-in stay as they were and read "Local user (before sign-in)". For scripts and CI, create an API key or a personal token on the organization's API keys page and set `GOAL_API_KEY`, or store it with `goal login --token` (from stdin); either acts in its own organization. Locked out: `goal db recover-admin <email>` or `goal db recover-owner <org> <email>` need only the database. `GOAL_AUTH=none` turns sign-in off again (loopback only, one developer's laptop: every request, agents' included, is the single local user) without losing data. Upgrading a 2.2 install: `pg_dump` first, add the two variables, `goal up` ([spec 3 §14](docs/specs/3-identity-and-access.md#14-rollout-and-migration)).

Every factory, item and run belongs to an organization; what existed before organizations is in `default`. Commands act in one: `--org <slug>`, else `GOAL_ORG`, else `org` in `~/.config/goal/config.json` (`goal org use <slug>` writes it), else the install's only organization (a key's or token's own). `goal org list` shows them and `goal org create <slug> --name N` adds one (platform admins). Decisions are recorded as the person signed in.

An item is `<factory>#<number>` (`goal#2`), its id, or a bare number when only one factory of the organization has an item with it (`--factory` picks one otherwise). `goal merge` works at a human station marked `action: merge` in `factory.yaml`: goald squash-merges every pull request the item's outputs name (each station's latest `pull_request` and `pull_requests`), each through the factory's GitHub or GitLab connection for its host, only if the host reports it open and mergeable; it never forces, skips the ones already merged (so a retry after a refusal is safe), and deletes only each pull request's head branch afterwards.

`goal up` first runs `goal db init`: it creates the database roles goald uses (`goal_owner` runs migrations, `goal_app` everything else, under row-level security) and hands the existing tables to `goal_owner`; then `goal db migrate` applies the pending migrations as `goal_owner`, before the auth service starts on the tables they create. So a database from an earlier version upgrades on the next `goal up` with nothing to do by hand. (The knowledge base, phase 4, moved compose's PostgreSQL to the pgvector image of the same PostgreSQL 18: that `goal up` recreates the `postgres` container once on the same `goal-pg` volume, data kept, and `goal db init` creates the `vector` extension.) Should the auth service stop by itself, `goal up` starts it again after a backoff (1s, doubling, at most 5 times in a row) while goald runs on. Running `bin/goald` directly needs `bin/goal db init` once first (`task goald` does it). `GOAL_DATABASE_URL` must not name the superuser: goald refuses a role that bypasses row-level security.

`goal up` does not push factories: goald runs the published version of each factory (versions in PostgreSQL, file content in the bucket) and every goald reloads within a second of a publish. At its first start on an install from before versions, goald imports each factory of the bucket's per-factory layout as version 1 of itself, once; a push to that layout afterwards is ignored and logged, so definitions change through goald's API from then on. Ctrl-C stops goald and returns in-flight items to their queue; `docker compose down` stops PostgreSQL and RustFS (volumes stay). Each claim of an item runs in a sandbox of its own (one container from the agent image, deleted at the end) holding a fresh clone of the factory's `runtime.repository` on the item's branch (`goal/<number>-<slug>`), cloned with `runtime.repository.connection`; the agent gets its model connection's and its own connections' variables, and station command checks (`task check`, `task migrate-check`) run in the same sandbox with none of them ([ADR 0006](docs/adr/0006-workspaces-and-command-checks.md)'s fresh clone and branch per item still hold). Sandboxes run on compose.yaml's agents network, with no route to the host or a private network: they reach public hosts, and goald's gateway at `/mcp` and `/run`, only through its `goal-egress` service, which `goal up` starts toward this machine's goald ([ADR 0009](docs/adr/0009-agent-network-isolation.md) and its phase 4 amendment).

`goal up --compose` (`task up:compose`) runs goald and the auth service as compose services too (the `stack` profile, images built from this checkout, goald holding the Docker socket: one's own laptop only), with `.env`'s settings and the addresses of compose's network, and returns once everything is healthy; `docker compose logs -f goald` follows it and `docker compose stop goald auth` stops it. It needs `GOAL_AUTH=session`. Plain `goal up` keeps goald on this machine.

GitHub Actions minutes are not available for this private repository, so CI on GitHub is disabled for now and the pre-push hook is the gate. `git push` is blocked unless `task check` passes (and `task migrate-check` when the pushed commits touch `migrations/`).

Read `AGENTS.md` before changing anything.

## Where factories live

A definition is a file tree:

```
factory.yaml
agents/*.md
skills/<name>/SKILL.md
contracts/*.schema.json
evals/          evals.yaml, cases/, judges/, fixtures/
```

goald stores each file once per organization as a content-addressed blob in the bucket (`orgs/<org-id>/blobs/<sha256>`), and PostgreSQL holds every draft and published version as a manifest (path → sha256) with a pointer to the version each factory runs. Each factory has one shared draft (the builder and the CLI both write it); publishing makes it the next version, every goald reloads within a second, and `goal factory restore` points the factory back at an older version. The bucket is one, versioned: locally `goal-local` on RustFS (`compose.yaml`), on Cloudflare the R2 bucket `goal-production` (`terraform/`). Files written to the bucket directly change nothing goald runs.

The CLI reaches definitions through goald's API (`GOAL_API_URL`, default `http://127.0.0.1:8080`). Only the operator commands `goal storage init` and `goal catalog skills|templates` use the bucket directly, configured from the environment (`.env.example`): `GOAL_S3_BUCKET`, plus `GOAL_S3_ENDPOINT`, `GOAL_S3_ACCESS_KEY` and `GOAL_S3_SECRET_KEY` for a local S3-compatible server or R2 (its account endpoint and an R2 API token's keys).

## Edit a factory

Edit in the builder, or in a working copy outside this repository (by default `../goal-factories`, never committed); `goal factory push` is how a folder changes a definition:

```bash
task factory:list                                   # the organization's factories: vN, open draft, paused
task factory:pull -- sre ../goal-factories/sre      # pull the current version (by name or id) into a new folder
$EDITOR ../goal-factories/sre/agents/triager.md
task factory:validate                               # every folder in GOAL_FACTORIES_DIR, through goald
task factory:push DIR=../goal-factories/sre         # the folder becomes the draft
bin/goal factory publish sre --message "Triage by severity"   # validate and publish it (or push with --publish)
```

`goal factory validate` checks the schema and every cross-reference (agents, skills, connections, stations, contracts, files) and, through goald, the organization's connections and knowledge sources it names and the stations open items sit at; it prints each problem as `path:line:col severity code message`. `goal validate` (`--offline`) checks schema and references without goald. Pull refuses a folder that is not empty, so it never overwrites local edits, and writes `.goal/pulled.json` (hidden, never pushed) to record which version the folder came from. Push refuses an open draft with changes the folder was not pulled from (someone is editing it in the builder: pull it with `--version draft`, or `--replace-draft` archives it) and a version published after the pull (`--force` pushes over it). A publish expects the version that ran when the push read it, so two publishes never cross. `knowledge/` and hidden files are never pushed.

## Factories are keyed by UUID

Every factory has an immutable `metadata.id`. Its database rows and bucket prefixes use it, so you can rename a factory or its folder at any time. When you copy a factory, give the copy a new id (`uuidgen | tr A-Z a-z`); `goal factory validate` rejects duplicates among the folders it checks.

## Add a factory

1. Copy a folder in your working copy, set a new `metadata.id` and `metadata.name`, then edit `factory.yaml` (or start one in the builder from a template).
2. `task factory:validate`
3. `task factory:push DIR=../goal-factories/<name> -- --publish`: a push of an id the organization lacks creates the factory with that id.

Templates for the builder's New factory come from the bucket's catalog: `goal catalog templates push <dir> --version N` adds one (`<dir>/template.yaml` with a title, the definition in `<dir>/factory/` with `metadata.id` `00000000-0000-0000-0000-000000000000` and no `credential:` references); versions never change.

## Database

PostgreSQL 18 is the only database: work items, the event log, leases (`FOR UPDATE SKIP LOCKED`), agent runs and realtime (`LISTEN/NOTIFY`). Schema changes are new files in `migrations/`; applied migrations are never edited.

## Infrastructure

`terraform/` holds the platform's own infrastructure and nothing per organization: on Cloudflare the R2 bucket, the DNS record and Workers route of `goal.byfrederick.work`, the R2 and Workers AI tokens and every Secrets Store secret; on PlanetScale the database's branch (adopted) and its administrator; the Google Cloud project of the sign-in OAuth client. Terraform generates every secret it can; only outside values and the master keys are in the age-encrypted `terraform/secrets/production.sops.yaml`. One root module, one Terraform workspace, `production`; state in an R2 bucket. wrangler deploys the control Worker and the containers (`deploy/cloudflare/`).

```bash
task tf:check                 # fmt, validate, tflint, terraform test: offline, part of task check
task secrets:edit             # the production secrets, decrypted in memory only
task tf:plan                  # needs the operator's credentials (task tf:credentials lists them)
```

Bootstrap, deploys, master key escrow and rotation, and restores: [docs/engineering/cloudflare-operations.md](docs/engineering/cloudflare-operations.md). Agents format, check and plan; only the owner, or the coordinator the owner authorizes, applies.

## Connections and the model

A factory signs in with its organization's connections, never with goald's environment: owners and admins create them on the Connections page or with `goal connection create` (secret values from stdin or a file, sealed under `GOAL_MASTER_KEYS`, never shown again), and the definition names them. `runtime.modelConnection` gives every run its model: a `claude-subscription` connection holds the output of `claude setup-token` (for one person's own use on their own subscription), an `anthropic` one an API key, a `bedrock` one AWS credentials minted per run. `runtime.repository.connection` clones the repository, and the factory's connections whose kind has a code host (`github`, `gitlab`) merge its pull requests. A run's box gets each connection's variables as it needs them and uses them directly, MCP servers included.

Upgrading an install from before connections (spec 4 §14): goald ignores `GOAL_TASK_ENV`, `CLAUDE_CODE_OAUTH_TOKEN` and `GOAL_GITHUB_*` with a warning, and blocks the items of a version that still uses `env:` references, `runtime.image`, `runtime.auth` or inline connections, or names no model connection ("publish a version with connections"). With the old tokens still in `.env`, `task connection:import-env` creates `github` and `claude` from them once (names that exist are left as they are); then name them in each factory (`runtime.modelConnection: claude`, `runtime.repository.connection: github`, `connections: [github, …]`), apply the fixes `goal factory validate` or the builder shows, publish, `goal retry` the blocked items, and delete the old variables from `.env`.

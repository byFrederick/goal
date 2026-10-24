# Goal — config-driven AI agent factories

Goal runs AI agent factories on your machine. A factory is a definition: `factory.yaml` declares its stations, agents, skills, connections, WIP limits and knowledge base; Markdown files hold the agent prompts and skills. The engine moves work through the factory as a pull-based Kanban flow, built on the Theory of Constraints (*The Goal*) and the Three Ways (*The Phoenix Project*).

Factory definitions are content, not code. They live in one versioned S3-compatible bucket, not in this repository (ADR 0005). The first factory, `goal`, builds this repository; others (`sre`, `university`) run the same engine on other work.

```
cmd/goal/              CLI: goal up, run, follow, item, approve, reject, retry, merge; goal factory validate|push|pull|list; goal storage init
cmd/goald/             daemon: flow engine, HTTP API (/v1/, with an SSE stream), MCP gateway (/mcp), Docker agent runs
api/                   openapi.yaml: the HTTP API contract (Go types in internal/api/gen, TypeScript in packages/api-client)
internal/engine/       flow engine: pull, WIP, gates, workspaces and command checks per run
internal/config/       loader and cross-reference validation (reads a folder or a bucket prefix)
internal/factorystore/ bucket layout factories/<id>/: push, pull, list
internal/adapters/     postgres (store, LISTEN/NOTIFY), s3store (S3 and RustFS), docker (runner, checker), gitworkspace, github (merges), envsecrets, sysclock
packages/api-client/   @goal/api-client: typed TypeScript client of the API (generated types + openapi-fetch) for the UI
schema/                factory.schema.json, evals.schema.json (editor autocompletion + validation)
migrations/            PostgreSQL schema (goose, forward-only)
queries/               SQL for sqlc (pgx/v5)
compose.yaml           local PostgreSQL 18 and RustFS (S3-compatible object storage)
worker/                agent container images (TypeScript, Claude Agent SDK): runtime and dev (Go, task, Terraform, PostgreSQL)
web/                   UI (React) — phase 2
terraform/             AWS infrastructure: bucket, KMS, S3 Vectors, one Bedrock KB per factory (later, unscheduled)
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

Tools: Go, [Task](https://taskfile.dev), Docker, Terraform and golangci-lint v2 (`mise install` reads `.mise.toml`).

```bash
task hooks:install         # once per clone: pushes run task check first
cp .env.example .env       # optional; defaults already point at the local RustFS
task check                 # lint, race tests, factory validation, terraform
task migrate-check         # migrations apply from zero on PostgreSQL 18
task storage:up            # RustFS on 127.0.0.1:9000 (console on :9001)
task storage:init          # bucket goal-local with versioning
task test-integration      # adapters against PostgreSQL 18 and RustFS
```

### Run a factory

```bash
# .env: CLAUDE_CODE_OAUTH_TOKEN (claude setup-token), GOAL_GITHUB_TOKEN (fine-grained PAT, this repo),
#       GOAL_GATEWAY_KEY (openssl rand -hex 32)
task worker:image-dev                              # the agent image the goal factory names (runtime.image)
task up                                            # PostgreSQL + RustFS, bucket, then goald in the foreground
task factory:push DIR=../goal-factories/goal       # in another terminal: the definition goald runs
bin/goal run goal "Add a goal version subcommand"  # submit; prints the timeline until a human must act
bin/goal item goal#2                               # read the spec the architect wrote (or: goal item 2)
bin/goal approve goal#2 --by "$USER"               # approve-spec → build → review → merge (you)
bin/goal follow goal#2                             # follow it again
bin/goal merge goal#2                              # at the merge station: goald merges the pull request, then approves
bin/goal retry goal#2                              # a blocked item goes back to its station's queue
```

An item is `<factory>#<number>` (`goal#2`), its id, or a bare number when only one factory has an item with it (`--factory` picks one otherwise). `goal merge` works at a human station marked `action: merge` in `factory.yaml`: goald squash-merges the pull request named by the item's latest `pull_request` output on `runtime.repository`, with that repository's token, only if GitHub reports it open and mergeable; it never forces, and deletes only the pull request's head branch afterwards.

`goal up` does not push factories: goald runs what the bucket holds and picks up pushes within `GOAL_REFRESH_EVERY` (or at once when `goal run` names a new factory). Ctrl-C stops goald and returns in-flight items to their queue; `docker compose down` stops PostgreSQL and RustFS (volumes stay). Each agent run works in a fresh clone of the factory's `runtime.repository` on the item's branch (`goal/<number>-<slug>`); the GitHub token is used on the host only, for the clone and inside the MCP gateway, and never reaches a container. Station command checks (`task check`, `task migrate-check`) run in a separate hardened container of the same image ([ADR 0006](docs/adr/0006-workspaces-and-command-checks.md)).

GitHub Actions minutes are not available for this private repository, so CI on GitHub is disabled for now and the pre-push hook is the gate. `git push` is blocked unless `task check` passes (and `task migrate-check` when the pushed commits touch `migrations/`).

Read `AGENTS.md` before changing anything.

## Where factories live

One bucket, versioned. Locally it is `goal-local` on RustFS (`compose.yaml`); on AWS it is `goal-<account>-<region>`.

```
s3://<bucket>/factories/<factory-id>/
  factory.yaml
  agents/*.md
  skills/<name>/SKILL.md
  contracts/*.schema.json
  evals/          evals.yaml, cases/, judges/, fixtures/
  knowledge/      reserved for the knowledge base (<collection>/<doc>.md); later, unscheduled
```

Every push writes new object versions, so a previous definition is restorable by version id. `goal factory push` mirrors a folder to its prefix: changed files are uploaded (`factory.yaml` last), files you removed are deleted, unchanged files keep their version, and `knowledge/` is never touched. The CLI prints the object version of every file it wrote.

Configuration comes from the environment (`.env.example`): `GOAL_S3_BUCKET`, plus `GOAL_S3_ENDPOINT`, `GOAL_S3_ACCESS_KEY` and `GOAL_S3_SECRET_KEY` for a local S3-compatible server. On AWS leave those three empty; the standard AWS credential chain applies.

## Edit a factory

Work in a working copy outside this repository (by default `../goal-factories`, never committed):

```bash
task factory:list                                   # what the bucket holds
task factory:pull -- sre ../goal-factories/sre      # pull by name or id into a new folder
$EDITOR ../goal-factories/sre/agents/triager.md
task factory:validate                               # every folder in GOAL_FACTORIES_DIR
task factory:push DIR=../goal-factories/sre         # validate, then upload; prints versions
```

`goal factory validate` checks the schema and every cross-reference (agents, skills, connections, stations, contracts, files). A push of an invalid factory writes nothing. Pull refuses a folder that is not empty, so it never overwrites local edits.

## Factories are keyed by UUID

Every factory has an immutable `metadata.id`. Its bucket prefix (`factories/<id>/`), database rows and every AWS resource use it, so you can rename a factory or its folder at any time. When you copy a factory, give the copy a new id (`uuidgen | tr A-Z a-z`); `goal factory validate` rejects duplicates among the folders it checks.

## Add a factory

1. Copy a folder in your working copy, set a new `metadata.id` and `metadata.name`, then edit `factory.yaml`.
2. `task factory:validate`
3. `task factory:push DIR=../goal-factories/<name>`

## Database

PostgreSQL 18 is the only database: work items, the event log, leases (`FOR UPDATE SKIP LOCKED`), agent runs and realtime (`LISTEN/NOTIFY`). Schema changes are new files in `migrations/`; applied migrations are never edited.

## Infrastructure

`terraform/` holds the AWS knowledge layer; knowledge-base provisioning is later work, not yet scheduled.

```bash
cd terraform
cp backend.hcl.example backend.hcl        # set your state bucket
cp terraform.tfvars.example terraform.tfvars
terraform init -backend-config=backend.hcl
terraform plan
```

Creates: a versioned, KMS-encrypted bucket `goal-kb-<account>-<region>` with one prefix per factory id; an S3 Vectors bucket; per factory a vector index, a Bedrock Knowledge Base and S3 data source scoped to the prefix, an agent role limited to that prefix and KB, and an optional ingestion schedule.

Roles that agents use to investigate other AWS accounts (for example `goal-sre-readonly`) belong in those accounts' own Terraform, not here.

## Claude authentication

Run `claude setup-token` once, put the token in `.env` (copy `.env.example`; it is git-ignored), and Goal injects it as `CLAUDE_CODE_OAUTH_TOKEN` into agent containers. That token can call models only: claude.ai connectors are not available to it, which is why connections are declared per factory and served by Goal's MCP gateway. For personal use on your own subscription only; switch `runtime.auth` to `anthropic-api-key` if anyone else ever uses the platform.

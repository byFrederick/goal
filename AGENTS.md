# Goal — rules for every agent and human working in this repo

Goal is a config-driven platform for AI agent factories. It runs on a laptop today and must deploy unchanged to AWS (ECS + managed PostgreSQL) or with its UI on Vercel. These rules exist so the codebase stays small, scalable and clean while agents write most of it. They are not optional.

## Non-negotiables

1. **Config-driven.** Domain behavior lives in factory definitions in the bucket (`s3://<bucket>/factories/<id>/`), never in this repository. No station, agent, work-type or factory name is hard-coded in Go, SQL or TypeScript, and no factory definition is committed here: the repo keeps only the JSON Schemas and small test fixtures (`internal/config/testdata`, sample org `acme`).
2. **Factories are keyed by `metadata.id` (UUID).** Rows, S3 prefixes and AWS resources use the id, never the name.
3. **PostgreSQL is the only database.** No SQLite, Redis, queues or caches. The one versioned object storage bucket (S3; RustFS locally) holds factory definitions and, from phase 3, knowledge documents; nothing else. Work claims use `FOR UPDATE SKIP LOCKED`; realtime uses `LISTEN/NOTIFY`; scheduled jobs take a `pg_try_advisory_lock`.
4. **Stateless processes.** `goald` keeps no state in memory or on local disk that another instance would need. Any instance can die at any time; leases and idempotent steps recover the work.
5. **Hexagonal architecture.** The domain and engine depend only on ports. Postgres, Docker, ECS, secrets providers, AWS, Bedrock and HTTP are adapters. `.golangci.yml` (depguard) enforces this; never silence it.
6. **Manual constructor injection.** All wiring happens in `cmd/<binary>/main.go`. No DI containers, no globals holding dependencies, no `init()` side effects.
7. **12-factor config.** Configuration comes from environment variables and factory definitions. Secrets come through the `Secrets` port (environment variables and a git-ignored `.env` now, AWS Secrets Manager in the cloud, 1Password in the last phase) and are never logged, committed or put in agent prompts.
8. **Infrastructure only in `terraform/`.** Agents may `fmt`, `validate` and `plan`. A human applies.
9. **No employer or client names** anywhere: code, docs, examples, sample data. Use `acme` and `example.com`.
10. **UI follows `design-system/goal/MASTER.md` exactly.**

## Repository layout

```
cmd/goal/            CLI            (main: parse flags, wire, run)
cmd/goald/           daemon         (main: wire adapters into the engine)
internal/domain/     plain types and rules: factory, item, station, lease. Imports nothing internal.
internal/engine/     flow engine: pull, WIP, drum-buffer-rope, andon. Imports domain + ports only.
internal/ports/      interfaces: Store, Runner, Workspaces, Checker, Secrets, Contracts, ObjectStore, Clock (Knowledge, ModelAuth later)
internal/adapters/   s3store/ postgres/ docker/ gitworkspace/ envsecrets/ sysclock/ (ecs/ awssecrets/ bedrockkb/ later; onepassword/ in the last phase)
internal/api/        HTTP API handlers for api/openapi.yaml (generated types in internal/api/gen); no adapter imports
internal/gateway/    MCP gateway inside goald: task-token auth, per-agent tools, credentials added on the host
internal/definitions/ goald's cache of the bucket's factory definitions: contracts, agent files, gateway lookups
internal/config/     loads and validates one factory definition from an fs.FS (bucket prefix or local folder)
internal/factorystore/ bucket layout factories/<id>/: push, pull, list, fs.FS view. Imports config + ports only.
migrations/          forward-only SQL (goose); never edit an applied file
queries/             SQL for sqlc
api/                 openapi.yaml + events.schema.json: the only contract between goald and TypeScript
schema/              JSON Schemas for factory and eval files
web/                 React SPA (pnpm workspace app), embedded into goald, deployable to Vercel
worker/              agent container image (pnpm workspace app, Claude Agent SDK)
packages/            contracts (generated), api-client, ui (design system), config
terraform/           all AWS infrastructure
docs/                adr/, engineering/, jonah/
Taskfile.yml         task check = everything the pre-push hook and CI run
.githooks/           versioned git hooks (pre-push); `task hooks:install` once per clone
compose.yaml         local PostgreSQL 18 and RustFS (S3-compatible)
```

Factory definitions are not in this tree. They live in the bucket under `factories/<metadata.id>/` (`factory.yaml`, `agents/`, `skills/`, `contracts/`, `evals/`; `knowledge/` is reserved for the knowledge base). Edit them in a working copy outside the repo: `goal factory pull` → edit → `goal factory validate` → `goal factory push` (ADR 0005).

Create a package only when it has a clear single responsibility and at least one caller. No `util`, `common`, `helpers` or `misc` packages.

## Tests: protect behavior, nothing else

Read `docs/engineering/testing.md`. In short:

- Test the engine's decisions (claims, WIP, constraint detection, gates, andon) with in-memory port fakes. These are the most valuable tests.
- Test adapters with integration tests against real dependencies (PostgreSQL 18 and RustFS in Docker), tagged `integration`.
- Do not write tests for getters, constructors, generated code, simple wiring or the standard library. Do not mock what you own; use the in-memory fakes in `internal/ports/porttest`.
- Every bug fix starts with a failing test that reproduces it.
- Coverage is not a target. A change that adds lines without a test that would fail if the behavior broke is incomplete.

## Done means verified

Run `task hooks:install` once per clone. From then on `git push` is blocked unless the checks pass: the pre-push hook runs `task check` (`gofmt`, `go vet`, `golangci-lint`, unit tests with `-race`, factory validation, `terraform fmt -check` and `validate`) and, when the pushed commits touch `migrations/`, `task migrate-check`. If you touched SQL or an adapter, `task test-integration` passes too (PostgreSQL 18 and RustFS from `compose.yaml`). CI on GitHub is currently disabled for lack of Actions minutes; `.github/workflows/ci.yml` runs the same tasks and is kept for when it returns, so the hook is the gate. Never bypass it with `--no-verify` without saying so in the pull request. A pull request states which acceptance criteria it meets and how each was verified.

## Changes

- One work item, one branch (`goal/<item>`), one small pull request. Refactors and features go in separate PRs.
- Decisions that others must follow get an ADR in `docs/adr/`.
- Delete dead code in the same PR that makes it dead. Do not commit commented-out code, TODOs without an issue link, or throwaway scripts.

## Skills to use

Skills live in `.claude/skills` (installed from skills.sh, pinned in `skills-lock.json`). Load the ones that match the files you touch:

| Area | Skills |
| --- | --- |
| Go | golang-project-layout, golang-code-style, golang-design-patterns, golang-error-handling, golang-concurrency, golang-testing, golang-lint, golang-security, golang-performance, golang-observability, golang-database, golang-cli (cmd/goal; stdlib flags, no cobra, no `init()`) |
| PostgreSQL | supabase-postgres-best-practices, postgresql-table-design, postgresql-optimization, postgresql-code-review, sql-code-review, database-migration |
| UI | ui-ux-pro-max (strictly), frontend-design, web-design-guidelines, impeccable, shadcn, tailwind-design-system, vercel-react-best-practices, vercel-composition-patterns, accessibility, typescript-advanced-types, tanstack-query-best-practices, tanstack-router-best-practices, vite, vitest, playwright-best-practices, webapp-testing |
| Monorepo and API contract | turborepo, pnpm, monorepo-management, openapi-spec-generation |
| Agents and evals | claude-api, mcp-builder, skill-creator, llm-evaluation, eval-harness, eval-harness-first, error-analysis, write-judge-prompt, validate-evaluator, generate-synthetic-data, build-review-interface, agent-evaluation |
| Infra and delivery | terraform-style-guide, terraform-test, terraform-module-library, aws-containers, aws-iam, aws-observability, amazon-bedrock, multi-stage-dockerfile, docker-compose-patterns (compose.yaml), github-actions-templates, github-actions-hardening, ci-cd-and-automation, deploy-to-vercel |
| Quality | testing-strategy, test-driven-development, code-review, code-review-and-quality, improve-codebase-architecture, security-and-hardening, architecture-decision-records |

Update skills deliberately (`npx skills update`), in their own PR, and review the diff: skills run with full agent permissions.

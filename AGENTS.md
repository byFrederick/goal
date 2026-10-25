# Goal — rules for every agent and human working in this repo

Goal is a config-driven platform for AI agent factories. It runs on a laptop today and must run unchanged on local Docker Compose and on Cloudflare (Containers, R2, a managed PostgreSQL), the two deployment targets ([ADR 0008](docs/adr/0008-portable-runs-connections-and-knowledge.md)). These rules exist so the codebase stays small, scalable and clean while agents write most of it. They are not optional.

## Non-negotiables

1. **Config-driven.** Domain behavior lives in factory definitions (content in the bucket, versions in PostgreSQL; ADR 0005), never in this repository. No station, agent, work-type or factory name is hard-coded in Go, SQL or TypeScript, and no factory definition is committed here: the repo keeps only the JSON Schemas and small test fixtures (`internal/config/testdata`, sample org `acme`).
2. **Factories are keyed by `metadata.id` (UUID).** Rows and bucket prefixes use the id, never the name.
3. **PostgreSQL is the only database.** No SQLite, Redis, queues or caches. The one object storage bucket (S3 API: RustFS locally, R2 on Cloudflare) holds factory definitions and uploaded knowledge documents (phase 4), both as immutable content-addressed blobs; nothing else, and nothing may depend on object versioning (R2 has none; ADR 0005). Work claims use `FOR UPDATE SKIP LOCKED`; realtime uses `LISTEN/NOTIFY`; scheduled jobs take a `pg_try_advisory_lock`.
4. **Stateless processes.** `goald` keeps no state in memory or on local disk that another instance would need. Any instance can die at any time; leases and idempotent steps recover the work.
5. **Hexagonal architecture.** The domain and engine depend only on ports. Postgres, Docker, Cloudflare, code hosts, secret and key providers, embedding models and HTTP are adapters; nothing provider-specific is hard-coded in the engine (a provider is a connection kind, data). `.golangci.yml` (depguard) enforces this; never silence it.
6. **Manual constructor injection.** All wiring happens in `cmd/<binary>/main.go`. No DI containers, no globals holding dependencies, no `init()` side effects.
7. **12-factor config.** Configuration comes from environment variables and factory definitions. Platform secrets come through the `Secrets` port (environment variables and a git-ignored `.env` locally; on Cloudflare, Secrets Store values the control Worker passes to each container as environment variables). Organization credentials live in connections (phase 4): sealed in PostgreSQL under the platform master key, write-only, and handed only to the runs of factories that name them, as environment variables of the run's own container; no factory reads goald's environment. Secrets are never logged, committed, returned by the API or put in agent prompts. The identity service (`auth/`) reads its own secrets from its environment the same way; goald never holds them, and user sessions and API keys never reach an agent.
8. **Infrastructure only in `terraform/`, and only the platform's own** (Cloudflare: bucket, DNS, secrets, routes). Creating an organization, a connection, a knowledge source or a factory never needs Terraform or a deploy. wrangler deploys what Terraform cannot (`deploy/cloudflare/`). One root module, one workspace (`production`); platform secrets only in the age-encrypted `terraform/secrets/production.sops.yaml`, never decrypted into a committed file; provider tokens never in it. Agents may `fmt`, `validate`, lint, test and `plan` (`task tf:check`, `task tf:plan`); only the owner applies, or the coordinating agent the owner explicitly authorizes for that change (`task tf:apply`), never a subagent. Runbook: `docs/engineering/cloudflare-operations.md`.
9. **No employer or client names** anywhere: code, docs, examples, sample data. Use `acme` and `example.com`.
10. **UI follows `design-system/goal/MASTER.md` exactly.**

## Repository layout

```
cmd/goal/            CLI            (main: parse flags, wire, run)
cmd/goald/           daemon         (main: wire adapters into the engine)
cmd/goal-egress/     egress proxy   (the way out of every run's box; ADR 0009; phase 4: agents' only policy is public hosts, never private, as a compose service)
internal/domain/     plain types and rules: factory, item, station, lease. Imports nothing internal.
internal/engine/     flow engine: pull, WIP, drum-buffer-rope, andon. Imports domain + ports only.
internal/ports/      interfaces: Store, Sandboxes, Secrets, Contracts, ObjectStore, ItemNotices, CodeHost, Clock, Tenancy, Audit, DefinitionStore, DefinitionNotices, SkillSource, Identity, Access, Platform, Connections, MasterKey, Minter (phase 4: new Knowledge, Embedder, SourceReader)
internal/adapters/   s3store/ postgres/ docker/ github/ gitlab/ envsecrets/ sysclock/ skillssh/ masterkey/ awssts/ (phase 4: cloudflare/ openaiembed/ gitdocs/ confluence/)
internal/api/        HTTP API handlers for api/openapi.yaml (generated types in internal/api/gen); no adapter imports
internal/gateway/    MCP gateway inside goald: task-token auth, per-agent tools, credentials added on the host (phase 4: Goal's MCP server for runs, its own tools and `/run` only; connections go direct from the box)
internal/egress/     the egress proxy's policy: agents to the model API and the gateway's path, tools to public hosts, nothing private (phase 4: public hosts for every box, and the public-only dialer goald uses for connections). Standard library only
internal/definitions/ goald's cache of published definition versions (current and pinned by runs): lines, contracts, agent files, gateway lookups
internal/config/     loads and validates one factory definition from an fs.FS (bucket prefix or local folder)
internal/connkind/   connection kinds (phase 4): parses and validates kind files against schema/connection-kind.schema.json; embeds the platform's (kinds/*.yaml)
internal/factorymodel/ the Visual editor's model of factory.yaml: read, apply section edits keeping comments, format
internal/authoring/  definition authoring: create, the shared draft (sections, files, CAS), validation, publish, restore, templates
internal/retention/  purges deleted organizations after GOAL_ORG_RETENTION: their rows and bucket objects
internal/builder/    the factory editor's metadata and the skills.sh skills catalog in the bucket (catalog/skills/)
internal/jonah/      phase 5: Jonah, the continuous-learning loop in goald (retros, proposals, eval runs of a draft against the current version). Imports domain + ports only
migrations/          forward-only SQL (goose); never edit an applied file
queries/             SQL for sqlc
api/                 openapi.yaml + events.schema.json: the only contract between goald and TypeScript; permissions.yaml (phase 3): every permission, generating goald's table and auth/'s roles
auth/                identity service (phase 3, pnpm workspace app: Hono + Better Auth): sign-in, sessions, members and invitations; reached only through goald's /api/auth proxy, whose client contract is ADR 0007's one exception
schema/              JSON Schemas for factory, eval and connection kind files
web/                 React SPA (pnpm workspace app), embedded into goald
worker/              agent container image (pnpm workspace app, Claude Agent SDK; phase 4: the one goal-agent base image with git, gh, mise and toolchains)
deploy/cloudflare/   phase 4: the control Worker (TypeScript) and wrangler config, the only Cloudflare-specific code besides internal/adapters/cloudflare
packages/            contracts (generated), api-client, ui (design system), config
terraform/           the platform's own infrastructure only (Cloudflare: R2, DNS, the Worker route, Secrets Store secrets; the sign-in Google project), never per organization
docs/                adr/, engineering/, jonah/
Taskfile.yml         task check = everything the pre-push hook and CI run
.githooks/           versioned git hooks (pre-push); `task hooks:install` once per clone
compose.yaml         local PostgreSQL 18, RustFS (S3-compatible) and, from phase 3, Mailpit (dev inbox); phase 4 adds pgvector, goald, the auth service, goal-egress and the agents' network; ports on 127.0.0.1 only
```

Factory definitions are not in this tree. A definition is a file tree (`factory.yaml`, `agents/`, `skills/`, `contracts/`, `evals/`) whose files are stored in the bucket as content-addressed blobs (`orgs/<org-id>/blobs/<sha256>`); PostgreSQL holds each draft and published version as a manifest (path → sha256) and points each factory at the version that runs (`definition_versions`, `factories.current_version_id`). Edit them in the builder or in a working copy outside the repo: `goal factory pull` → edit → `goal factory validate` → `goal factory push` (the folder becomes the draft; `--publish` or `goal factory publish` makes it the version that runs). Every one of those goes through goald's API; goald ignores definition files written to the bucket directly (ADR 0005 and its 2.2 amendment).

Create a package only when it has a clear single responsibility and at least one caller. No `util`, `common`, `helpers` or `misc` packages.

## Tests: protect behavior, nothing else

Read `docs/engineering/testing.md`. In short:

- Test the engine's decisions (claims, WIP, constraint detection, gates, andon) with in-memory port fakes. These are the most valuable tests.
- Test adapters with integration tests against real dependencies (PostgreSQL 18 and RustFS in Docker), tagged `integration`.
- Do not write tests for getters, constructors, generated code, simple wiring or the standard library. Do not mock what you own; use the in-memory fakes in `internal/ports/porttest`.
- Every bug fix starts with a failing test that reproduces it.
- Coverage is not a target. A change that adds lines without a test that would fail if the behavior broke is incomplete.

## Done means verified

Run `task hooks:install` once per clone. From then on `git push` is blocked unless the checks pass: the pre-push hook runs `task check` (`gofmt`, `go vet`, `golangci-lint`, unit tests with `-race`, factory validation, `task tf:check`: `terraform fmt -check`, `validate`, tflint and `terraform test`) and, when the pushed commits touch `migrations/`, `task migrate-check`. If you touched SQL or an adapter, `task test-integration` passes too (PostgreSQL 18 and RustFS from `compose.yaml`). CI on GitHub is currently disabled for lack of Actions minutes; `.github/workflows/ci.yml` runs the same tasks and is kept for when it returns, so the hook is the gate. Never bypass it with `--no-verify` without saying so in the pull request. A pull request states which acceptance criteria it meets and how each was verified.

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
| Identity (phase 3: the `auth/` Better Auth service, sessions, API keys; phase 6: OAuth provider) | better-auth-best-practices, better-auth-security-best-practices, organization-best-practices, create-auth (scaffolding reference only: migrations still go through goose), security-and-hardening, golang-security |
| Monorepo and API contract | turborepo, pnpm, monorepo-management, openapi-spec-generation |
| Agents and evals (the worker and the gateway; phase 5: Jonah's retros and the definition's `evals/` cases) | claude-api, mcp-builder, skill-creator, llm-evaluation, eval-harness, eval-harness-first, error-analysis, write-judge-prompt, validate-evaluator, generate-synthetic-data, build-review-interface, agent-evaluation |
| Infra and delivery | terraform-style-guide, terraform-test, terraform-module-library, aws-containers, aws-iam, aws-observability, amazon-bedrock, multi-stage-dockerfile, docker-compose-patterns (compose.yaml), github-actions-templates, github-actions-hardening, ci-cd-and-automation, deploy-to-vercel |
| Quality | testing-strategy, test-driven-development, code-review, code-review-and-quality, improve-codebase-architecture, security-and-hardening, architecture-decision-records |

Update skills deliberately (`npx skills update`), in their own PR, and review the diff: skills run with full agent permissions.

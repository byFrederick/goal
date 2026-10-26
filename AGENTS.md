# Goal — the index for every agent and human working in this repo

Goal is a config-driven platform for AI agent factories: each factory's stations, agents and work types are data, and the engine runs them. The same code runs on local Docker Compose and on Cloudflare (Containers, R2, a managed PostgreSQL) ([ADR 0008](docs/adr/0008-portable-runs-connections-and-knowledge.md)).

This file is an index. Each rule lives in one place and is linked from here; the layout and its reasons are in [ADR 0013](docs/adr/0013-repository-layout.md).

## Repository map

```
.claude/        rules/ (path-scoped rules), skills/ (vendored skills and our goal-* procedures)
.githooks/      versioned git hooks (pre-push runs task check)
.github/        CI, deploy and Terraform workflows
apps/           auth/ (identity service, Hono + Better Auth, reached only through goald's /api/auth proxy), control-worker/ (the control Worker's code, the Cloudflare entry in front of goald, auth and the run boxes; its tasks in deploy/cloudflare/Taskfile.yml), web/ (React SPA, embedded into goald), worker/ (goal-agent, the one agent container image); each app's tasks in its Taskfile.yml
cmd/            binaries: goal (CLI), goald (daemon), goal-egress (egress proxy); each main.go only wires its dependencies
contracts/      contracts/api/ (openapi.yaml, permissions.yaml, sandbox.yaml: the only contract between goald and TypeScript), contracts/definitions/ (JSON Schemas for factory, eval and connection kind files), env.yaml (every environment variable a process reads, with env.schema.json); the API generators' tasks in Taskfile.yml
db/             the database: db/migrations/ (forward-only SQL, goose), db/queries/ (SQL for sqlc), db/sqlc.yaml and Taskfile.yml (sqlc, migrate-check)
deploy/         deploy/cloudflare/ (the control Worker's wrangler config and deploy scripts), deploy/terraform/ (the platform's own infrastructure, one root module), each with the Taskfile.yml of its tasks
docs/           adr/, design/ (MASTER.md: the UI's design system), engineering/, history/ (history/specs/ and history/jonah/: earlier plans), reference/ (glossary.md, and environment.md generated from the contracts)
internal/       the Go packages: domain, engine, ports, adapters, api, gateway, cli (the goal CLI's commands, which cmd/goal only wires)… (docs/engineering/architecture.md)
packages/       shared TypeScript: api-client/ (the API client and its generated types, its tasks in Taskfile.yml), config/ (the shared Biome config and the tsconfig bases for Node, the browser and Workers)
tools/          repository tooling that is not a shipped binary: layoutcheck/ (task layout:check), envcheck/ (task env:check), checkplan/ (task check:changed), permgen/ (task api:gen's permission tables)
```

Root files: the entry docs, `Taskfile.yml` (the cross-cutting tasks, dotenv, env and vars; it includes each area's `Taskfile.yml` flattened, so every task keeps its name, and `tools/layoutcheck/tasks.txt` lists the names: [ADR 0013](docs/adr/0013-repository-layout.md)), `compose.yaml`, the Go and pnpm manifests, and `skills-lock.json`. Factory definitions are not in this tree; they live in the bucket and PostgreSQL ([factory-definition.md](docs/engineering/factory-definition.md)).

## Non-negotiables

Other docs cite these as "AGENTS.md rule N", so the numbers stay fixed.

1. **Config-driven.** No factory, station, agent or work-type name in code, and no definition committed: [architecture rule](.claude/rules/architecture.md), [ADR 0005](docs/adr/0005-factory-definitions-in-object-storage.md).
2. **Factories are keyed by `metadata.id` (UUID)**, never by name: [architecture rule](.claude/rules/architecture.md).
3. **PostgreSQL is the only database**, and the bucket holds only immutable content-addressed blobs: [architecture rule](.claude/rules/architecture.md), [database rule](.claude/rules/database.md).
4. **Stateless processes.** Leases and idempotent steps recover any instance's work: [architecture rule](.claude/rules/architecture.md), [engine rule](.claude/rules/engine.md).
5. **Hexagonal architecture**, enforced by depguard, never silenced: [architecture rule](.claude/rules/architecture.md).
6. **Manual constructor injection** in `cmd/<binary>/main.go`, no `init()` side effects: [architecture rule](.claude/rules/architecture.md), [Go rule](.claude/rules/go.md).
7. **12-factor config.** Platform secrets through the `Secrets` port, organization credentials only in connections, no secret in logs, commits, API answers or prompts: [ADR 0008](docs/adr/0008-portable-runs-connections-and-knowledge.md), [worker rule](.claude/rules/worker.md), [auth rule](.claude/rules/auth.md).
8. **Infrastructure only in `deploy/terraform/`, and only the platform's own.** CI applies from main; agents never apply: [infra rule](.claude/rules/infra.md), [ADR 0010](docs/adr/0010-infrastructure-applied-from-main.md), [runbook](docs/engineering/cloudflare-operations.md).
9. **No employer or client names** anywhere; use `acme` and `example.com`: [docs rule](.claude/rules/docs.md).
10. **UI follows `docs/design/MASTER.md` exactly**: [web rule](.claude/rules/web.md).

## Rules, skills and the change map

- Rules are `.claude/rules/*.md`. Each has `paths:` frontmatter, and Claude Code loads a rule when you touch a file its globs match. Run boxes load the checkout's project settings, so the same rules load there.
- Each rule's "Skills" section names the vendored skills for its area. Vendored skills live in `.claude/skills`, pinned in `skills-lock.json`; add, update or remove them only with `npx skills`, in their own pull request, and review the diff: skills run with full agent permissions.
- Our own procedures are the `goal-*` skills in `.claude/skills` (an adapter, an ADR, an API change, a migration, a UI screen, delivering a change).
- A run box offers only the skills its factory definition lists, taken by name from the checkout's `.claude/skills`.
- The [glossary](docs/reference/glossary.md) defines each domain term (factory, station, item, run, andon…) and names the package where it lives.
- When you change one file, the [change map](docs/engineering/change-map.md) says what else changes with it and what to run.

## Changes

- One work item, one branch (`goal/<item>`), one small pull request; refactors and features go in separate ones ([goal-deliver](.claude/skills/goal-deliver/SKILL.md)).
- Decisions that others must follow get an ADR in `docs/adr/` ([goal-adr](.claude/skills/goal-adr/SKILL.md)).
- Executable source and manifests (code, `Taskfile.yml`, workflows, `.golangci.yml`, schemas, lock files) win over prose. An agent that finds them disagreeing says so in its pull request.

## Done means verified

- Run `task hooks:install` once per clone. The pre-push hook then runs `task check` before every push.
- When you change SQL or an adapter, `task test-integration` passes too.
- When you change `db/migrations/` or `apps/auth/`, `task migrate-check` passes too.
- CI (`.github/workflows/ci.yml`) gates every pull request the same way. Never push with `--no-verify` without saying so in the pull request.
- A pull request states which acceptance criteria it meets and how each was verified.

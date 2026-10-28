# ADR 0013 — Repository layout: a small root and one home per kind

Status: accepted · 2026-10-26 · Builds on [ADR 0004](0004-frontend-and-monorepo.md) (the pnpm workspace) and [ADR 0012](0012-assemblies-and-parts.md) (the assembly that carries this reorganization out)

## Context

The root of the repository has grown into a mix of entry docs, manifests and a dozen directories of different kinds: three pnpm apps (`web/`, `auth/`, `worker/`) beside Go's `cmd/` and `internal/`, the contracts split between `api/` and `schema/`, the database between `migrations/`, `queries/` and `sqlc.yaml`, the infrastructure between `terraform/` and `deploy/`, and the design system in its own `design-system/` tree. Agents write most of the code. An agent that has to guess where a file goes, or which files change together, puts it in a new place or forgets the file next to it, and the guesses add up. The rules agents follow live in one long AGENTS.md that every agent reads whole, whatever it touches.

The owner wants a small root, one home for each kind of file, rules that load only for the paths a change touches, and our own procedures as skills. This ADR is the decision the other parts of the reorganization follow. It moves nothing itself.

## Decision

1. **The target layout.**

   ```
   AGENTS.md CLAUDE.md README.md LICENSE SECURITY.md     entry docs, nothing else in prose at the root
   go.mod go.sum package.json pnpm-workspace.yaml        manifests
   pnpm-lock.yaml Taskfile.yml compose.yaml
   skills-lock.json .mise.toml and the dotfiles
   cmd/ internal/                                        Go: cmd/ wires each binary, internal/ holds its code (internal/core: the hexagon's core; internal/cli: the goal CLI)
   tools/                                                repository tooling that is not a shipped binary
   apps/web/ apps/auth/ apps/worker/ apps/docs/          the pnpm apps (docs/: the product docs site, Astro Starlight at /docs)
   apps/control-worker/                                  the control Worker's code (see the amendment below)
   packages/api-client/ packages/config/                 shared TypeScript packages (config: see the amendment below)
   contracts/api/                                        openapi.yaml, permissions.yaml, sandbox.yaml, and events.schema.json once it exists
   contracts/definitions/                                the factory, eval and connection-kind JSON Schemas
   contracts/env.yaml contracts/env.schema.json          the environment registry and its schema
   db/migrations/ db/queries/ db/sqlc.yaml               the database
   deploy/cloudflare/ deploy/terraform/                  how the platform is deployed (cloudflare/: wrangler.jsonc and the deploy scripts)
   docs/adr/ docs/engineering/                           decisions, engineering guides
   docs/reference/                                       the glossary and references generated from the contracts
   docs/design/                                          the design system (MASTER.md)
   docs/history/                                         earlier plans the code has replaced (specs/, jonah/)
   .claude/rules/ .claude/skills/ .claude/settings.json  agent rules, skills and settings
   .github/ .githooks/                                   CI workflows and versioned git hooks
   ```

   `.claude/rules/` holds our rules, each scoped to the paths it governs. `.claude/skills/` holds our own skills and the vendored skills we actually use.

   *Amended 2026-10-26: one Taskfile per area.* The root `Taskfile.yml` keeps the cross-cutting tasks (`default`, `build`, `up`, `check`, `check:changed`, `test`, `lint`, `fmt`, `gen`, `gen:check`, `layout:check`, `env:check`, `hooks:install`, `clean`, the egress and goald images, storage, connections and factories), and also `dotenv`, the global `env:` and `vars:`, because an included Taskfile cannot have its own dotenv and reads the root's vars. Each area's tasks live in its own `Taskfile.yml` next to its code: `apps/web/`, `apps/auth/`, `apps/worker/`, `db/`, `deploy/cloudflare/`, `deploy/terraform/`, `contracts/` and `packages/api-client/`. The root includes each with `flatten: true` and `dir: "{{.ROOT_DIR}}"`. Flattening keeps every task's full name (`web:check`, `tf:plan`, `migrate-check`), so CI, the git hooks, the docs, the rules and the skills keep calling the same names. A namespace would rename tasks such as `migrate-check` and `deploy:production`. Setting the include's dir to the root means each task runs where it did before, and its own `dir:` and `sources:` stay relative to the root. Run `task` from the repository root: from inside an area directory, Task would find that area's Taskfile first. `tools/layoutcheck/tasks.txt` lists every name `task --list-all` shows, and `task layout:check` fails when the Taskfiles and the list differ, so a name changes only on purpose. `tools/envcheck` scans every `Taskfile.yml` for `${NAME}` reads, and `tools/checkplan` treats any of them as shared by every part.

   *Amended 2026-10-26: thin binaries.* A binary's code lives in `internal/`, and `cmd/<binary>/main.go` only builds its dependencies by constructor and calls one entry point (AGENTS.md rule 6). The goal CLI's commands moved from `cmd/goal`'s package main to `internal/cli`, tests, testdata and the integration test included, with no change to their names, help text, output or exit codes. `cmd/goal/main.go` builds the signal context and calls `cli.Run(ctx, args, stdin, stdout, stderr, getenv, readBuildInfo)`, which returns the exit code. Golden files in `internal/cli/testdata/golden` pin the help and the output of the commands that run without a server. A depguard rule stops every other package under `internal/` from importing `internal/cli`, so only `cmd/goal` wires it. `tools/envcheck` counts reads under `internal/cli/` as the CLI's.

   *Amended 2026-10-26: `apps/control-worker`.* The control Worker is an app like the others (TypeScript, Vitest in the Workers runtime, its own package `@goal/cloudflare`), so its code moved from `deploy/cloudflare` to `apps/control-worker`: `src/`, `test/` (the `sandboxtest` and `topology` stand-ins and `dev.sh` included), `package.json`, `tsconfig.json`, `biome.json` and `vitest.config.ts`. `deploy/cloudflare` keeps only what deploys it: `wrangler.jsonc` (its `main` is `../../apps/control-worker/src/index.ts`), `scripts/deploy.sh`, `scripts/images.mjs` with its test, `sandboxtest.sh`, `topology.sh` and the `Taskfile.yml` of `cloudflare:*` and `deploy:*`, whose names did not change. `deploy/cloudflare` has no package of its own, so its scripts run wrangler from `apps/control-worker/node_modules`, and `task cloudflare:check` lints `deploy/cloudflare/scripts` with the shared Biome config. The deploy workflow counts a change under either folder as an app change.

   *Amended 2026-10-26: `packages/config`.* It is the shared TypeScript settings: `biome.json` (the Biome formatter and linter settings) and the tsconfig bases `tsconfig.base.json`, `tsconfig.node.json` (NodeNext, for the Node services), `tsconfig.browser.json` (DOM, bundled, no emit) and `tsconfig.workers.json` (the Workers runtime). It is the private workspace package `@goal/config`, and each project extends one variant and keeps only its own options: `include`, `rootDir`, `outDir`, `types` and each app's own checks stay in the project. It holds no Vitest preset. `packages/config` is not checked on its own; a change to it runs every TypeScript part's check (`tools/checkplan`). The `goald`, `auth` and `worker` Dockerfiles copy its manifest and sources, and their `.dockerignore` allowlists admit them.

   *Amended 2026-10-26: `internal/core` and adapters by port.* The hexagon's core moved under `internal/core/`: `internal/core/domain`, `internal/core/ports` (with `porttest` and its conformance suites) and `internal/core/engine`. Each adapter now sits under a group named after the port it implements: `codehost/{github,gitlab}`, `sandboxes/{docker,cloudflare}`, `sourcereader/{gitdocs,confluence}`, `secrets/env`, `store/postgres` (with its sqlc `db/` and `roles.sql`), `objectstore/s3`, `masterkey/masterkey`, `minter/awssts`, `embedder/openai`, `skillsource/skillssh` and `clock/sys`. A package's name is its leaf directory, so `envsecrets`, `s3store`, `openaiembed` and `sysclock` are now `env`, `s3`, `openai` and `sys`; no exported identifier changed. The feature packages (`api`, `gateway`, `authoring`, `knowledge`, `definitions`, `builder`, `config`, `connkind`, `egress`, `factorymodel`, `retention`, `cli`) stayed where they were. The core's boundary is one strict depguard rule: `internal/core` imports only the standard library, itself and `github.com/google/uuid`. This lets the domain import the ports and the engine, which the old domain-only rule refused; the code does not do it today.

2. **A new top-level directory needs an amendment to this ADR.** It names the directory, the kind of file it holds and why no existing home fits.

   *Amended 2026-10-26: `tools/`.* It holds repository tooling that is not a shipped binary: programs that check or generate the repository's own files and that only the Taskfile and CI run, starting with `tools/layoutcheck` (`task layout:check`). `cmd/` does not fit because it holds the binaries Goal ships (`goal`, `goald`, `goal-egress`), and mixing in tooling makes it harder to tell what is deployed. `task layout:check` checks that this layout, the AGENTS.md map and the `paths:` globs of `.claude/rules` still match the tracked files.

   *Amended 2026-10-26: `contracts/env.yaml` and `docs/reference/`.* Neither is a new top-level directory, but both are new kinds of file, so they are recorded here. `contracts/env.yaml` is the environment registry: every environment variable a Goal process reads, with the processes that read it, its class (public, config or secret), whether it is required and what it means, never a value. Its shape is `contracts/env.schema.json`. It sits in `contracts/` because it is a contract between the code and whoever runs it, like the API. `docs/reference/` holds reference pages generated from a contract and never edited by hand, starting with `docs/reference/environment.md`. `tools/envcheck` (`task env:check`, part of `task check`) fails when a read is not declared, a declared variable is never read, `.env.example` names an undeclared variable or shows a real-looking secret, or the generated page differs from the committed one.

3. **No process output is committed.** Run artifacts, reports, metrics, screenshots and generated evidence stay out of the repository. Work state lives in Goal (items, runs, events), not in files here.

4. **Generated code stays where it is generated and is never edited by hand.** That is `internal/api/gen` and the generated TypeScript contracts (`packages/contracts` once it exists; today the types in `packages/api-client`), along with the other generated files the Taskfile names. A change to generated code is a change to its source followed by its task.

   *Amended 2026-10-26: one name rule and one check.* Every generated file has `.gen.` before its extension (`api.gen.go`, `permissions.gen.go`, `schema.gen.ts`, `access.gen.ts`, `routeTree.gen.ts`, and sqlc's `db.gen.go`, `models.gen.go`, `batch.gen.go` and `<query>.sql.gen.go`), so lint configs, rules and tools find generated files with one glob, `*.gen.*`, and a reader knows from the name alone not to edit it. The one exception is `docs/reference/environment.md`: it is a reference page that other docs link to, and its first line says it is generated. `task gen` runs every generator, and `task gen:check`, part of `task check` and so of CI and the pre-push hook, fails when a generated file differs from the commit or is untracked, both before it runs `task gen` (so an uncommitted hand edit is reported, not overwritten) and after. The generated files, their generators, sources and tasks are listed in one place: the [change map](../engineering/change-map.md)'s "Generated files" table.

5. **Executable source and manifests win over prose.** When a doc, rule or skill disagrees with the code, the Taskfile, a workflow or a manifest, the executable side is what happens. An agent that finds the two disagreeing says so in its pull request instead of guessing which one is meant, and fixes the prose when the change is in its scope.

6. **Rules and procedures.** Rules live in `.claude/rules/*.md`, each with `paths:` frontmatter naming the globs it applies to, and each rule in exactly one place: a rule is never repeated in another rule, a skill or AGENTS.md, which links to it instead. Our procedures (how to verify, deliver, add a migration and the like) are skills named `.claude/skills/goal-*`.

7. **The moves, one kind per pull request, in this order:** `contracts/`, `db/`, `apps/`, `deploy/terraform/`, `docs/design/`. Each move updates every path that names the old location in the same pull request ([the change map](../engineering/change-map.md)'s row for moving a directory). Until a move merges, the old path is current: docs, rules and code keep using it, and nothing refers to the new path as if it existed.

   *Amended 2026-10-26: the `deploy/terraform/` move is complete.* `terraform/` moved first; the SOPS creation rules followed, from the root `.sops.yaml` to `deploy/terraform/.sops.yaml`, next to the only file they govern (`deploy/terraform/secrets/production.sops.yaml`). sops finds its config only from the current directory up, so every sops call in the Taskfile passes `--config`, and `task tf:sops-test` (part of `task tf:check`) proves with a throwaway age key that the creation rule still matches the secrets file.

   *Amended 2026-10-26: the repository tools and old plans.* `cmd/checkplan` and `cmd/permgen` are repository tooling, not shipped binaries, so they moved to `tools/checkplan` and `tools/permgen`; `cmd/` now holds only `goal`, `goald` and `goal-egress`. The phase specs and the Jonah plan were plans that the code has since replaced, so they moved to `docs/history/` (`docs/history/specs/`, `docs/history/jonah/`), which is a record and is not updated. The living parts of the specs' README (the migration rules and the tenancy convention) moved to `docs/engineering/architecture.md`, "Migrations and tenancy". The Jonah draft `schema.sql`, which no migration applies, was deleted; it stays in git history. `docs/reference/glossary.md` defines the domain terms.

   *Amended 2026-10-28: `apps/docs`.* The product documentation site is an app like the others: `apps/docs/` is an Astro Starlight site, a private pnpm workspace package (`@goal/docs`) served under the base path `/docs`. Its tasks are in `apps/docs/Taskfile.yml`, included as `docs-site` with the same flattening, so they are `docs:site:*`. The name `docs` stays with `docs/Taskfile.yml` (the generated docs check). The site reads `contracts/api/openapi.yaml` at build time to render the API reference and publish `/docs/openapi.yaml` and `/docs/openapi.json`; the contract is never copied into the app. `tools/checkplan` runs `docs:site:check` (the type check, Biome and the build) for any change under `apps/docs/` and for `packages/config/`.

## Consequences

- The root lists entry docs and manifests only; the place of a new file follows from its kind.
- Rules load for the paths a change touches, so an agent reads less and the rules it reads apply.
- Each move is a pull request that changes paths only, which a reviewer can check against the change map. The Terraform move must keep the state and the resources as they are.
- The [change map](../engineering/change-map.md) lists the files that change together; a pull request that adds a coupling adds its row.
- Until all five moves merge, the repository is partly in the old layout and partly in the new; the History below says which paths moved.

## History

The old path of each directory or file that moves. Once the moves are done, this is the only place the old paths may stay.

| Old path | New path |
| --- | --- |
| `api/` | `contracts/api/` |
| `schema/` | `contracts/definitions/` |
| `migrations/` | `db/migrations/` |
| `queries/` | `db/queries/` |
| `sqlc.yaml` | `db/sqlc.yaml` |
| `web/` | `apps/web/` |
| `auth/` | `apps/auth/` |
| `worker/` | `apps/worker/` |
| `terraform/` | `deploy/terraform/` |
| `.sops.yaml` | `deploy/terraform/.sops.yaml` |
| `design-system/goal/MASTER.md` | `docs/design/MASTER.md` |
| `cmd/checkplan/` | `tools/checkplan/` |
| `cmd/permgen/` | `tools/permgen/` |
| `docs/specs/` | `docs/history/specs/` |
| `docs/jonah/` | `docs/history/jonah/` (`schema.sql` deleted) |
| `cmd/goal/` (all but `main.go`) | `internal/cli/` |
| `deploy/cloudflare/` (all but `wrangler.jsonc`, `Taskfile.yml`, `scripts/`, `sandboxtest.sh` and `topology.sh`) | `apps/control-worker/` |
| `internal/domain/`, `internal/ports/`, `internal/engine/` | `internal/core/domain/`, `internal/core/ports/`, `internal/core/engine/` |
| `internal/adapters/{github,gitlab}/` | `internal/adapters/codehost/{github,gitlab}/` |
| `internal/adapters/{docker,cloudflare}/` | `internal/adapters/sandboxes/{docker,cloudflare}/` |
| `internal/adapters/{gitdocs,confluence}/` | `internal/adapters/sourcereader/{gitdocs,confluence}/` |
| `internal/adapters/envsecrets/` | `internal/adapters/secrets/env/` |
| `internal/adapters/postgres/` | `internal/adapters/store/postgres/` |
| `internal/adapters/s3store/` | `internal/adapters/objectstore/s3/` |
| `internal/adapters/masterkey/` | `internal/adapters/masterkey/masterkey/` |
| `internal/adapters/awssts/` | `internal/adapters/minter/awssts/` |
| `internal/adapters/openaiembed/` | `internal/adapters/embedder/openai/` |
| `internal/adapters/skillssh/` | `internal/adapters/skillsource/skillssh/` |
| `internal/adapters/sysclock/` | `internal/adapters/clock/sys/` |

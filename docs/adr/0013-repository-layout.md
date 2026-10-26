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
   cmd/ internal/                                        Go (unchanged)
   tools/                                                repository tooling that is not a shipped binary
   apps/web/ apps/auth/ apps/worker/                     the pnpm apps
   packages/                                             shared TypeScript packages (unchanged)
   contracts/api/                                        openapi.yaml, permissions.yaml, sandbox.yaml, and events.schema.json once it exists
   contracts/definitions/                                the factory, eval and connection-kind JSON Schemas
   contracts/env.yaml contracts/env.schema.json          the environment registry and its schema
   db/migrations/ db/queries/ db/sqlc.yaml               the database
   deploy/cloudflare/ deploy/terraform/                  how the platform is deployed (cloudflare/ unchanged)
   docs/adr/ docs/engineering/                           decisions, engineering guides
   docs/reference/                                       the glossary and references generated from the contracts
   docs/design/                                          the design system (MASTER.md)
   docs/history/                                         earlier plans the code has replaced (specs/, jonah/)
   .claude/rules/ .claude/skills/ .claude/settings.json  agent rules, skills and settings
   .github/ .githooks/                                   CI workflows and versioned git hooks
   ```

   `.claude/rules/` holds our rules, each scoped to the paths it governs. `.claude/skills/` holds our own skills and the vendored skills we actually use.

2. **A new top-level directory needs an amendment to this ADR.** It names the directory, the kind of file it holds and why no existing home fits.

   *Amended 2026-10-26: `tools/`.* It holds repository tooling that is not a shipped binary: programs that check or generate the repository's own files and that only the Taskfile and CI run, starting with `tools/layoutcheck` (`task layout:check`). `cmd/` does not fit because it holds the binaries Goal ships (`goal`, `goald`, `goal-egress`), and mixing in tooling makes it harder to tell what is deployed. `task layout:check` checks that this layout, the AGENTS.md map and the `paths:` globs of `.claude/rules` still match the tracked files.

   *Amended 2026-10-26: `contracts/env.yaml` and `docs/reference/`.* Neither is a new top-level directory, but both are new kinds of file, so they are recorded here. `contracts/env.yaml` is the environment registry: every environment variable a Goal process reads, with the processes that read it, its class (public, config or secret), whether it is required and what it means, never a value. Its shape is `contracts/env.schema.json`. It sits in `contracts/` because it is a contract between the code and whoever runs it, like the API. `docs/reference/` holds reference pages generated from a contract and never edited by hand, starting with `docs/reference/environment.md`. `tools/envcheck` (`task env:check`, part of `task check`) fails when a read is not declared, a declared variable is never read, `.env.example` names an undeclared variable or shows a real-looking secret, or the generated page differs from the committed one.

3. **No process output is committed.** Run artifacts, reports, metrics, screenshots and generated evidence stay out of the repository. Work state lives in Goal (items, runs, events), not in files here.

4. **Generated code stays where it is generated and is never edited by hand.** That is `internal/api/gen` and the generated TypeScript contracts (`packages/contracts` once it exists; today the types in `packages/api-client`), along with the other generated files the Taskfile names. A change to generated code is a change to its source followed by its task.

5. **Executable source and manifests win over prose.** When a doc, rule or skill disagrees with the code, the Taskfile, a workflow or a manifest, the executable side is what happens. An agent that finds the two disagreeing says so in its pull request instead of guessing which one is meant, and fixes the prose when the change is in its scope.

6. **Rules and procedures.** Rules live in `.claude/rules/*.md`, each with `paths:` frontmatter naming the globs it applies to, and each rule in exactly one place: a rule is never repeated in another rule, a skill or AGENTS.md, which links to it instead. Our procedures (how to verify, deliver, add a migration and the like) are skills named `.claude/skills/goal-*`.

7. **The moves, one kind per pull request, in this order:** `contracts/`, `db/`, `apps/`, `deploy/terraform/`, `docs/design/`. Each move updates every path that names the old location in the same pull request ([the change map](../engineering/change-map.md)'s row for moving a directory). Until a move merges, the old path is current: docs, rules and code keep using it, and nothing refers to the new path as if it existed.

   *Amended 2026-10-26: the `deploy/terraform/` move is complete.* `terraform/` moved first; the SOPS creation rules followed, from the root `.sops.yaml` to `deploy/terraform/.sops.yaml`, next to the only file they govern (`deploy/terraform/secrets/production.sops.yaml`). sops finds its config only from the current directory up, so every sops call in the Taskfile passes `--config`, and `task tf:sops-test` (part of `task tf:check`) proves with a throwaway age key that the creation rule still matches the secrets file.

   *Amended 2026-10-26: the repository tools and old plans.* `cmd/checkplan` and `cmd/permgen` are repository tooling, not shipped binaries, so they moved to `tools/checkplan` and `tools/permgen`; `cmd/` now holds only `goal`, `goald` and `goal-egress`. The phase specs and the Jonah plan were plans that the code has since replaced, so they moved to `docs/history/` (`docs/history/specs/`, `docs/history/jonah/`), which is a record and is not updated. The living parts of the specs' README (the migration rules and the tenancy convention) moved to `docs/engineering/architecture.md`, "Migrations and tenancy". The Jonah draft `schema.sql`, which no migration applies, was deleted; it stays in git history. `docs/reference/glossary.md` defines the domain terms.

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

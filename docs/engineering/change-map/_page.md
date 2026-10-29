# Change map

Keep this map accurate: every pull request that adds a new coupling between files adds or updates its row. Each row is one entry file in [change-map/](change-map/) (`NNN-<slug>.md`, with the sections `## When you change`, `## Also change` and `## Run`, one line each; rows follow the file names), and `task docs:gen` renders this page from them and from `change-map/_page.md`. A branch that adds or edits an entry file runs `task docs:gen` and commits this page; never edit it by hand.

When you change X, also change Y, and run Z. Paths are today's; the moves of [ADR 0013](../adr/0013-repository-layout.md) update them.

| When you change | Also change | Run |
| --- | --- | --- |
<!-- change-map rows -->

## Generated files

Every generated file has `.gen.` before its extension ([ADR 0013](../adr/0013-repository-layout.md), decision 4), and nobody edits one by hand: change its source and run its task. `task gen` runs every generator; `task gen:check` (part of `task check`) fails when any file below differs from the commit or is untracked, checked before the generators run (an uncommitted hand edit) and after. `.golangci.yml` and the `biome.json` files ignore `*.gen.*`. The change map and the ADR index are not part of `task gen`: `task docs:gen` writes them, and `task docs:check` (part of `task check`) fails when either is stale or missing. A conflict on either is resolved by running `task docs:gen`, never by hand.

| File | Generator | Source | Task |
| --- | --- | --- | --- |
| `internal/api/gen/api.gen.go` | oapi-codegen (pinned in `Taskfile.yml`, config `internal/api/gen/oapi-codegen.yaml`) | `contracts/api/openapi.yaml` | `task api:gen` |
| `internal/api/permissions.gen.go` | `tools/permgen` | `contracts/api/permissions.yaml`, the operations' `x-goal-scope` in `contracts/api/openapi.yaml` | `task api:gen` (`task api:permissions-check` checks it alone) |
| `apps/auth/src/access.gen.ts` | `tools/permgen` | `contracts/api/permissions.yaml` | `task api:gen` |
| `packages/api-client/src/schema.gen.ts` | openapi-typescript (api-client's `gen` script) | `contracts/api/openapi.yaml` | `task api:gen` (api-client's `check:gen` checks it alone) |
| `internal/adapters/store/postgres/db/db.gen.go`, `models.gen.go`, `batch.gen.go`, `<query>.sql.gen.go` | sqlc (`db/sqlc.yaml`; the names come from its `output_*_file_name` and `output_files_suffix` options) | `db/queries/`, `db/migrations/` | `task sqlc` |
| `apps/web/src/routeTree.gen.ts` | the TanStack router plugin in `apps/web/vite.config.ts`, during the web build | `apps/web/src/routes/` | `task web:build` (`task gen` runs the build directly) |
| `docs/reference/environment.md` (the one exception to the name rule: a reference page other docs link to) | `tools/envcheck -write` | `contracts/env.yaml` | `task env:check -- -write` |
| `docs/engineering/change-map.md` (an exception to the name rule, like `docs/reference/environment.md`: a page other docs link to) | `tools/gendocs` | `docs/engineering/change-map/` (`_page.md` and one entry file per row) | `task docs:gen` (`task docs:check` checks it) |
| `docs/adr/README.md` (an exception to the name rule: the index of `docs/adr/`) | `tools/gendocs` | the heading and `Status:` line of each `docs/adr/NNNN-*.md` | `task docs:gen` (`task docs:check` checks it) |

## When you change

Moving a directory

## Also change

The root `Taskfile.yml` and the area Taskfiles it includes (`apps/web/`, `apps/auth/`, `apps/worker/`, `db/`, `deploy/cloudflare/` (the control Worker's tasks, its code in `apps/control-worker/`), `deploy/terraform/`, `contracts/`, `packages/api-client/`, `docs/`): their paths, `dir:`, `sources:` and the root's `includes:`; `tools/checkplan`'s path table; the path filters and `working-directory` of `.github/workflows/ci.yml`, `deploy.yml`, `terraform-plan.yml` and `terraform-apply.yml`; Dockerfile `COPY` paths and `.dockerignore` files; `deploy/cloudflare/scripts/images.mjs`; `pnpm-workspace.yaml`; Go import paths, and for a Go package the depguard `files:` and `pkg:` paths in `.golangci.yml`, `db/sqlc.yaml`'s `out` (the postgres adapter) and the package's location in `docs/engineering/architecture.md` and the glossary; the `paths:` globs of `.claude/rules`; the repository map in `AGENTS.md`; the History of ADR 0013 (these three are `tools/layoutcheck`'s inputs, with the tracked files)

## Run

`task layout:check`, `task check`, `task migrate-check` when `db/migrations/` or `apps/auth/` moved

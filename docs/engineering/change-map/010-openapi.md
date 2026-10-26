## When you change

`contracts/api/openapi.yaml`

## Also change

The handler in `internal/api` (never the generated `internal/api/gen`); the client in `packages/api-client` (its generated `src/schema.gen.ts` comes from the task, routes and helpers by hand); the callers in `apps/web/`; the spec and docs that describe the endpoint

## Run

`task api:gen`, `task gen:check`, `task test`, `task api-client:check`, `task web:check`

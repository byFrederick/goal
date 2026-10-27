## When you change

The organization's run list: `GET /v1/orgs/{org}/runs` (`listRuns`) in `contracts/api/openapi.yaml`, its handler `ListRuns` in `internal/api/runs.go`, or the `ListOrgRuns` and `OrgRunsSummary` queries in `db/queries/runs.sql`

## Also change

`Store.OrgRuns` and `RunQuery` (`internal/core/ports/ports.go`), its postgres adapter (`internal/adapters/store/postgres/store.go`, which escapes `\`, `%` and `_`), its in-memory fake (`internal/core/ports/porttest/reads.go`) and the conformance case `OrgRuns_NewestFirstFilteredPagedAndSummed`; the `TestAPI_ListRuns*` tests in `internal/api/runs_test.go`; the callers of the list in `packages/api-client`, `apps/web` (the Runs page) and the CLI (`goal runs`). Keep both queries on `agent_runs_factory_started_idx` (`factory_id = ANY (…)` with a `started_at` range): check the plan with `EXPLAIN` after a change

## Run

`task api:gen`, `task sqlc`, `task test`, `task test-integration`

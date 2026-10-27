## When you change

The organization search: `GET /v1/orgs/{org}/search` in `contracts/api/openapi.yaml`, its handler `internal/api/search.go`, or the `SearchItems` query in `db/queries/work_items.sql`

## Also change

`Store.SearchItems` (`internal/core/ports/ports.go`), its postgres adapter (`internal/adapters/store/postgres/reads.go`, which escapes `\`, `%` and `_`), its in-memory fake (`internal/core/ports/porttest/reads.go`) and the conformance case `SearchItems_MatchesTitleAndNumber`; the tests in `internal/api/search_test.go`; the callers of the search in `packages/api-client`, `apps/web` and the CLI

## Run

`task api:gen`, `task sqlc`, `task test`, `task test-integration`

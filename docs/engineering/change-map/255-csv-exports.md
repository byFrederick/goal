## When you change

A CSV export (ADR 0020, decision 3): `GET /v1/orgs/{org}/runs/export` (`exportRuns`) or `GET /v1/orgs/{org}/spend/export` (`exportSpend`) in `contracts/api/openapi.yaml`, the shared writer `csvExport` in `internal/api/export.go`, the handlers `ExportRuns` (`internal/api/runs.go`) and `ExportSpend` (`internal/api/spend.go`), or the `CountOrgRuns`, `OrgSpendByAgentModel` and `CountAgentModelSpend` queries

## Also change

The list each export mirrors, and the export with it: a new filter or scope on `listRuns` or `getOrgSpend` goes on its export too (`TestExportScopesAreTheLists` checks the scopes); `Store.EachOrgRun` and `Spend.EachAgentModelSpend` (`internal/core/ports`), their postgres adapter (`store.go`, `spend.go`) and fake (`porttest/reads.go`, `porttest/spend.go`) with the conformance cases `EachOrgRun_*` and `EachAgentModelSpend_*`; `CountOrgRuns` repeats `ListOrgRuns`' filters, so change both; the `TestExportRuns` and `TestExportSpend` tests, the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden`

## Run

`task api:gen`, `task sqlc`, `go test ./internal/api -run TestExport`, `go test ./internal/api -run TestAuthorize_`, `task test`, `task test-integration`

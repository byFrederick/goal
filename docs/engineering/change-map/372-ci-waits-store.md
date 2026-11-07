## When you change

CI waits and their polls: `db/migrations/00139_ci_waits.sql` (or a later migration of `ci_waits` or `ci_polls`), `db/queries/ci_waits.sql`, the CI wait methods of `ports.Store` in `internal/core/ports/ports.go` (`OpenCIWait`, `ReplaceCIWaitSHA`, `LeaseDueCIPolls`, `RecordCIPoll`, `OpenCIWaits`, `EndCIWaits`, `CIRounds`), the domain types in `internal/core/domain/ciwait.go`, `internal/adapters/store/postgres/ciwaits.go` or the fake in `internal/core/ports/porttest/ciwaits.go`

## Also change

`task sqlc` after a query; the adapter and the fake together, with the storetest cases in `internal/core/ports/porttest/storetest/ciwaits.go`; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` for a new table; [ADR 0044](../adr/0044-goald-waits-for-ci.md) decision 9 by amendment when the polling model changes; the `Store` row of `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestStore_Conformance/CIWaits' ./internal/core/ports/porttest/`, `go test -run 'CI' ./internal/core/domain/`, `task migrate-check`, `task test-integration`, `task docs:check`

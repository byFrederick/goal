## When you change

Jonah's store: `db/migrations/00075_jonah.sql` (or a later migration of `jonah_reviews`, `jonah_proposals`, `jonah_observations` or `agent_runs.kind`), `db/queries/jonah.sql`, `ports.JonahStore` in `internal/core/ports/jonah.go`, the domain types in `internal/core/domain/jonah.go`, `internal/adapters/store/postgres/jonah.go` or the fake in `internal/core/ports/porttest/jonahstore.go`

## Also change

`task sqlc` after a query; the adapter and the fake together, with the storetest cases in `internal/core/ports/porttest/storetest/jonah.go`; `domain.ProposalPathAllowed` and 00075's `goal_jonah_paths_allowed` together when the allowlist changes, with [ADR 0034](../adr/0034-jonah-proposals.md) decision 4 by amendment; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` for a new table; the `JonahStore` row of `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestStore_Conformance/Jonah' ./internal/core/ports/porttest/`, `go test -run 'Proposal' ./internal/core/domain/`, `task migrate-check`, `task test-integration`, `task docs:check`

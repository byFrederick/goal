## When you change

A migration (a new file in `db/migrations/`, the highest number plus one)

## Also change

The queries in `db/queries/` and the generated `internal/adapters/store/postgres/db` (`*.gen.go`); the `Store` port (`internal/core/ports/ports.go`) and its in-memory fake in `internal/core/ports/porttest`; the conformance suite `TestStore_Conformance`, which runs against the fake and PostgreSQL (`internal/adapters/store/postgres/store_integration_test.go`); row-level security for an org-scoped table

## Run

`task sqlc`, `task gen:check`, `task migrate-check`, `task test`, `task test-integration`

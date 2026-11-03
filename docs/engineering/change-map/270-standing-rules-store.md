## When you change

The connections store's tool modes or standing rules: `connections.tool_modes`, `connections.last_test_tools` and the `standing_rules` table (`db/migrations/00075_connection_tool_modes_standing_rules.sql` and the migrations after it), their queries (`db/queries/connections.sql`, `db/queries/standing_rules.sql`) or the adapter (`internal/adapters/store/postgres/connections.go`, `internal/adapters/store/postgres/standingrules.go`)

## Also change

The `Connections` port (`internal/core/ports/connections.go`), its in-memory fake (`internal/core/ports/porttest/connections.go`, `internal/core/ports/porttest/standingrules.go`) and the conformance suite `porttest/connectiontest`, which runs against both; `Connections.Delete` in the adapter and the fake together with the standing rules, since deleting a connection revokes its active rules ([ADR 0031](../../adr/0031-tool-calls-ask-first-and-standing-rules.md) decision 7); `internal/moveover/run.go` (`rekey`), which copies connection rules onto the server key when a factory moves over to secrets ([ADR 0037](../../adr/0037-secrets-and-environment-files.md) decision 8)

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

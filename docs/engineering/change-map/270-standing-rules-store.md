## When you change

The connections store's tool modes or standing rules: `connections.tool_modes`, `connections.last_test_tools` and the `standing_rules` table (`db/migrations/00075_connection_tool_modes_standing_rules.sql` and the migrations after it), their queries (`db/queries/connections.sql`, `db/queries/standing_rules.sql`) or the adapter (`internal/adapters/store/postgres/connections.go`, `internal/adapters/store/postgres/standingrules.go`)

## Also change

The `StandingRules` port (`internal/core/ports/standingrules.go`: the list, the server-keyed rule and revoking), its in-memory fake (`internal/core/ports/porttest/standingrules.go`, `porttest.NewStandingRules` over a fake `Connections`' data) and the conformance suite `porttest/standingruletest`; the `Connections` port (`internal/core/ports/connections.go`: the connection-keyed `CreateStandingRule`), its fake (`internal/core/ports/porttest/connections.go`) and the conformance suite `porttest/connectiontest`, which reads rules through a `StandingRules` value; each suite runs against the fake and the adapter; `Connections.Delete` in the adapter and the fake together with the standing rules, since deleting a connection revokes its active rules ([ADR 0031](../../adr/0031-tool-calls-ask-first-and-standing-rules.md) decision 7)

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

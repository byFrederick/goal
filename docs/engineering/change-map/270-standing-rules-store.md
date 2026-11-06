## When you change

The standing rules: the `standing_rules` table (`db/migrations/00075_connection_tool_modes_standing_rules.sql` and the migrations after it), its queries (`db/queries/standing_rules.sql`) or the adapter (`internal/adapters/store/postgres/standingrules.go`)

## Also change

The `StandingRules` port (`internal/core/ports/standingrules.go`: the list, the server-keyed rule and revoking), its in-memory fake (`internal/core/ports/porttest/standingrules.go`, `porttest.NewStandingRules`) and the conformance suite `porttest/standingruletest`, which runs against the fake and the adapter ([ADR 0031](../../adr/0031-tool-calls-ask-first-and-standing-rules.md) decision 7)

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

## When you change

The intake store: the `webhook_endpoints`, `webhook_deliveries` and `trigger_firings` tables or `work_items.trigger` and `work_items.source` (`db/migrations/00068_intake.sql` and the migrations after it), their queries (`db/queries/intake.sql`) or the adapter (`internal/adapters/store/postgres/intake.go`)

## Also change

The `Intake` port (`internal/core/ports/intake.go`) and its domain types (`internal/core/domain/intake.go`), its in-memory fake (`internal/core/ports/porttest/intake.go`) and the conformance suite `porttest/intaketest`, which runs against both; `goal_connection_key_ids` and `postgres.Rekey` when the sealed secret's columns change, so goald's start-up key check and `goal db rekey` still cover endpoints

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

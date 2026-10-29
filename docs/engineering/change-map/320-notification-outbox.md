## When you change

The notification outbox ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decisions 2 and 3): the table of `db/migrations/00083_notification_outbox.sql`, `db/queries/notification_outbox.sql`, the adapter `internal/adapters/store/postgres/notification_outbox.go` (who receives which event), the calls in `appendEvents` (`convert.go`) and `PullAndon` (`andon.go`), the port `ports.NotificationOutbox`, or the engine's budget event (`warnBudget` in `internal/core/engine/budget.go`)

## Also change

`task sqlc`'s generated files; the fake `NotificationOutbox` in `internal/core/ports/porttest/notifications.go`; `internal/adapters/store/postgres/notification_outbox_integration_test.go`; `internal/core/engine/budget_test.go`; the wiring in `cmd/goald/main.go`; `docs/engineering/architecture.md` ("Ports"); the email and Slack sender job when a row's shape or state changes

## Run

`task sqlc`, `task migrate-check`, `task test` (`TestWeeklyBudget_`), `task test-integration` (`TestNotificationOutbox_`, `TestSchema_` and `TestStore_Conformance`) and `task lint`

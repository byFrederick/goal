## When you change

Notification email ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decisions 3 and 4): the port `ports.Mailer` (`internal/core/ports/mailer.go`), the SMTP adapter `internal/adapters/mailer/smtp`, the sender job and its messages in `internal/notify` (`sender.go`, `render.go`), the sender's side of the outbox (`ports.NotificationDeliveries`, the `ClaimNotifications` and `FinishNotification` queries in `db/queries/notification_outbox.sql` and their adapter in `internal/adapters/store/postgres/notification_outbox.go`), or goald's `GOAL_SMTP_*` settings (`smtpSettingsFrom` in `cmd/goald/settings.go`)

## Also change

`task sqlc`'s generated files; the fakes `porttest.Mailer` (`internal/core/ports/porttest/mailer.go`) and `porttest.NotificationDeliveries` (`internal/core/ports/porttest/notifications.go`); `internal/adapters/store/postgres/notification_outbox_integration_test.go`; the wiring in `cmd/goald/main.go` (`emailSender`); `contracts/env.yaml` and `go run ./tools/envcheck -write` when a `GOAL_SMTP_*` reader or meaning changes; the auth service's `parseSMTP` (`apps/auth/src/settings.ts`) when the SMTP rules change, so both keep the same rules; the depguard row `notify` in `.golangci.yml` and `docs/engineering/architecture.md` ("Hexagonal core", "Ports")

## Run

`task sqlc`, `task env:check`, `task test` (`TestSMTP_`, `TestRender_`, `TestSender_`, `TestSettings_SMTP`), `task test-integration` (`TestNotificationOutbox_`) and `task lint`

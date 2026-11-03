## When you change

The organization's notification settings store ([ADR 0037](../adr/0037-secrets-and-environment-files.md) decision 14): the `org_notification_settings` table (`db/migrations/00092_org_notification_settings.sql` and the migrations after it), its queries (`db/queries/org_notification_settings.sql`) or the adapter (`internal/adapters/store/postgres/notification_settings.go`)

## Also change

`domain.NotificationSettings` (`internal/core/domain/notificationsettings.go`); the `NotificationSettings` port (`internal/core/ports/notificationsettings.go`), its in-memory fake (`internal/core/ports/porttest/notificationsettings.go`) and the conformance suite `porttest/notificationsettingtest`, which runs against both; the secrets store's deletion check (`SecretNotifiesSlack` in `secretTable.delete`, and the `OrgSecrets` fake's `notifies` hook); the Slack checks of `db/queries/notification_outbox.sql` and `db/queries/digests.sql`, with `TestNotificationOutbox_*` and `TestDigests_Recipients`

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

## When you change

Each person's notification preferences ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decision 1): `domain.NotificationPreferences` in `internal/core/domain/notifications.go`, the port `ports.NotificationPreferences`, the tables of `db/migrations/00080_notification_preferences.sql`, `db/queries/notification_preferences.sql`, the adapter `internal/adapters/store/postgres/notifications.go`, or `getNotificationPreferences`, `putNotificationPreferences`, `NotificationPreferences` and the `notifications:manage` permission in `contracts/api/openapi.yaml`, `contracts/api/permissions.yaml` and `internal/api/notifications.go`

## Also change

`task sqlc`'s and `task api:gen`'s generated files; the fake `internal/core/ports/porttest/notifications.go` and the suite `internal/core/ports/porttest/notificationtest`; `withoutOrgID` in `internal/adapters/store/postgres/schema_integration_test.go` for a new table; `internal/api/notifications_test.go`, the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`-update`); `docs/engineering/architecture.md` ("Ports" and "HTTP API")

## Run

`task sqlc`, `task api:gen`, `task api:permissions-check`, `task migrate-check`, `task test`, `task test-integration` (`TestNotificationPreferences_` and `TestSchema_`) and `task api-client:check`

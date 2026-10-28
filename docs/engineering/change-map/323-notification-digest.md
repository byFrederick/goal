## When you change

The daily digest ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decision 6): the `digests` table (`db/migrations/00082_digests.sql`), its queries (`db/queries/digests.sql`), the port `ports.Digests` (`internal/core/ports/notifications.go`), its adapter `internal/adapters/store/postgres/digests.go`, or the job and its message in `internal/notify/digest.go` (`DigestJob`, `RenderDigest`, `DigestHour`)

## Also change

The fake `porttest.Digests` (`internal/core/ports/porttest/digests.go`); the `digests` entry in `withoutOrgID` (`internal/adapters/store/postgres/schema_integration_test.go`) when the table's tenancy changes; the shared send rules in `internal/notify/sender.go` (`sendOutcome`, `logOutcome`, `tokenOnce`), which the digest reuses; the wiring in `cmd/goald/main.go` (`notificationJobs`, `digestEvery`); `docs/engineering/architecture.md` ("Ports" and "Scale and failure")

## Run

`task sqlc`, `task migrate-check`, `task test` (`TestDigest_`, `TestSender_`), `task test-integration` (`TestDigests_`, `TestSchema_EveryOrgTableHasRLS`) and `task lint`

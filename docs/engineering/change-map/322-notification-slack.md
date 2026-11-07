## When you change

Slack notifications ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decision 5): the port `ports.DirectMessenger` (`internal/core/ports/directmessage.go`), the Slack adapter `internal/adapters/directmessage/slack`, or the Slack side of the sender in `internal/notify` (`sendSlack`, `slackTokenOf`, the `slack-*` reasons in `sender.go`)

## Also change

The fake `porttest.DirectMessenger` (`internal/core/ports/porttest/directmessage.go`); the outbox's rule that queues Slack rows only while the organization's notification setting names a secret (`db/queries/notification_outbox.sql`); the wiring in `cmd/goald/main.go` (`notificationJobs`); the depguard row `notify` in `.golangci.yml` and `docs/engineering/architecture.md` ("Ports")

## Run

`task test` (`TestSlack_`, `TestSender_`, `TestRender_`) and `task lint`

## When you change

Slack notifications ([ADR 0023](../../adr/0023-notifications-and-sign-up-by-domain.md) decision 5): the connection kind `internal/connkind/kinds/slack.yaml`, the port `ports.DirectMessenger` (`internal/core/ports/directmessage.go`), the Slack adapter `internal/adapters/directmessage/slack`, or the Slack side of the sender in `internal/notify` (`sendSlack`, `slackTokenOf`, the `slack-*` reasons in `sender.go`)

## Also change

The fake `porttest.DirectMessenger` (`internal/core/ports/porttest/directmessage.go`); `TestPlatform`'s list of kinds (`internal/connkind/connkind_test.go`) when a kind is added or renamed; the outbox's rule that queues Slack rows only while the organization has a connection of kind `slack` (`internal/adapters/store/postgres/notification_outbox.go`) when the kind's name changes; the wiring in `cmd/goald/main.go` (`notificationJobs`); the depguard row `notify` in `.golangci.yml` and `docs/engineering/architecture.md` ("Ports")

## Run

`task test` (`TestPlatform`, `TestSlack_`, `TestSender_`, `TestRender_`) and `task lint`

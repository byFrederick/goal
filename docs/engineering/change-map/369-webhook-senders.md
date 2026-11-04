## When you change

The webhook sender catalog: a sender entry (`internal/catalog/senders/*.yaml`), its reader and `Check` (`internal/catalog/senders/senders.go`), or the receiver of `POST /hooks/{webhook id}` (`internal/api/hooks.go`)

## Also change

A new sender also needs its name in `domain.WebhookSenders` (`internal/core/domain/webhooks.go`), in 00115's `sender` and reserved-name CHECKs (a new migration), and its signature header in `domain.SignatureHeaders` (`internal/core/domain/intake.go`) and the headers CHECK of `webhook_deliveries` (a new migration, as 00117 does); the senders table under "Receiving deliveries" in `apps/docs/src/content/docs/concepts/access-and-secrets.md`; the `POST /hooks/{webhook id}` row in `docs/engineering/architecture.md` when an answer or a limit changes

## Run

`go test -race -run TestSenders ./internal/catalog/senders`, `go test -race -run TestHooks ./internal/api`, `go test -race -run TestStoredHeaders ./internal/core/domain`, and `task migrate-check` after a migration

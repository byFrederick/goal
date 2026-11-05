## When you change

Follow-ups: how they are read and kept (`internal/core/domain/followup.go`, `followupfile.go`), filed, delivered and dropped (`internal/core/engine/followupfiling.go`, `followupnotes.go`), or the store's follow-up queries (`db/queries/` and the `follow_ups` table)

## Also change

The Follow-ups, Closing an item sections of `docs/engineering/factory-definition.md` and the rules in `apps/docs/src/content/docs/concepts/work-items.md` and `guides/follow-ups.md`; the follow-up operations of `contracts/api/openapi.yaml` (then `task api:gen`); `apps/docs/src/content/docs/guides/act-on-an-item.md` when File or Drop change; the follow-up sweep in `docs/engineering/architecture.md`; the new SQL's migration when a column changes (a new migration, never an edit)

## Run

`go test -race -run FollowUp ./internal/core/domain ./internal/core/engine`, `task sqlc` after a query change, and `task migrate-check` after a migration

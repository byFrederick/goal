## When you change

The conversion from connections to secrets and environment files (`internal/moveover`: `Convert`, `Secrets`, `Mover.Run`) or what it reads (`internal/connkind` kinds, `internal/config`'s moved keys)

## Also change

`TestConvert`, `TestRun_Idempotent` and `TestConvert_GoalProductionFactory` in `internal/moveover`; `ports.FactoryConversions` with its fake, `porttest/conversiontest` and the postgres adapter (00087's `factory_conversions`) when what a conversion records changes; the conversion inbox entry (`internal/api/inbox.go`, `InboxConversion` in `contracts/api/openapi.yaml`); the "Moving to secrets and environment files" section of `apps/docs/src/content/docs/concepts/connections.md`

## Run

`go test -race -run 'TestConvert' ./internal/moveover, the same with -run TestRun_`, `go test -race -run TestInbox_Conversion ./internal/api` and, for the store, `task test-integration` (`TestFactoryConversions_Conformance`)

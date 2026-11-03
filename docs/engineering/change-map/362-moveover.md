## When you change

The conversion from connections to secrets and environment files (`internal/moveover`: `Convert`, `Secrets`, `Mover.Run`, and the test of a converted version before it publishes, `check.go`), the MCP server test it shares with the API (`internal/environmentcheck`) or what it reads (`internal/connkind` kinds, `internal/config`'s moved keys)

## Also change

`TestConvert`, `TestRun_Idempotent`, `TestRun_PointsKnowledgeSourcesAtSecrets`, `TestConvert_GoalProductionFactory` and the check tests (`TestRun_PublishesWhenChecksPass`, `TestRun_TokenWithoutPushNotPublished`, `TestRun_MCPServerFailureBlocksPublish`, `TestRun_RetriesAfterFix`) in `internal/moveover`; the environment test route (`internal/api/environmenttest.go`) when `internal/environmentcheck` changes; `ports.FactoryConversions` with its fake, `porttest/conversiontest` and the postgres adapter (00088's `factory_conversions`, 00090's UPDATE for a refusal replaced) when what a conversion records changes; the conversion inbox entry (`internal/api/inbox.go`, `InboxConversion` in `contracts/api/openapi.yaml`); the "Moving over from connections" section of `apps/docs/src/content/docs/concepts/access-and-secrets.md`

## Run

`go test -race -run 'TestConvert' ./internal/moveover, the same with -run TestRun_`, `go test -race -run TestInbox_Conversion ./internal/api`, `go test -race ./internal/environmentcheck` and, for the store, `task test-integration` (`TestFactoryConversions_Conformance`)

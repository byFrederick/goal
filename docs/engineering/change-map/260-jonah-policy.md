## When you change

The Jonah policy: `learning.jonah` in `contracts/definitions/factory.schema.json`, `config.Jonah` in `internal/config/config.go` or `jonahPolicy` in `internal/config/line.go`, or `domain.JonahPolicy` and `domain.DefaultJonahPolicy` in `internal/core/domain/line.go`

## Also change

The other three of those, so the schema's defaults and ranges, the Go defaults and the mapping agree; `TestDefaultJonahPolicy` in `./internal/core/domain` and `TestLine_JonahPolicyDefaults`, `TestLine_CarriesJonahPolicy` and `TestLoad_RefusesBadJonahPolicy` in `./internal/config`; the "Jonah: `learning.jonah`" section of `docs/engineering/factory-definition.md`; [ADR 0034](../adr/0034-jonah-proposals.md) by amendment when decision 2 or 5 changes

## Run

`go test -run 'Jonah' ./internal/config ./internal/core/domain`, `task docs:check`

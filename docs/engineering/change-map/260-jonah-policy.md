## When you change

The Jonah policy: `learning.jonah` in `contracts/definitions/factory.schema.json`, `config.Jonah` in `internal/config/config.go` or `jonahPolicy` in `internal/config/line.go`, or `domain.JonahPolicy` and `domain.DefaultJonahPolicy` in `internal/core/domain/line.go`

## Also change

The other three of those, so the schema's defaults and ranges, the Go defaults and the mapping agree; `TestDefaultJonahPolicy` in `./internal/core/domain` and `TestLine_JonahPolicyDefaults`, `TestLine_CarriesJonahPolicy` and `TestLoad_RefusesBadJonahPolicy` in `./internal/config`; `TestJonahPolicy_Budget` in `./internal/core/domain` when the budget or the per-review cap changes; `ignoredKeys` in `internal/config/checks.go` and `TestCheck_IgnoredFieldsWarnAndDoNotRefuse` when a key is retired (a retired key gets `field-ignored`, never `removedKeys`); the openapi `JonahPolicy` and `task api:gen` when a key the overview shows changes; the "Jonah: `learning.jonah`" section of `docs/engineering/factory-definition.md` and `task docs:ref:gen` for the definition reference; [ADR 0039](../adr/0039-jonah-learns-from-the-line.md) by amendment when decision 16 or 17 changes

## Run

`go test -run 'Jonah' ./internal/config ./internal/core/domain`, `go test -run 'IgnoredFields' ./internal/config`, `task docs:check`

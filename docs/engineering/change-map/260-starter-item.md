## When you change

The starter item: `starter` in `contracts/definitions/factory.schema.json`, `config.Starter` or its check in `internal/config/starter.go`, or `Model.Starter` in `internal/factorymodel/model.go`

## Also change

The rows of `TestCheck_Starter` and `TestLoad_ParsesStarter` in `internal/config/starter_test.go`; `internal/factorymodel/testdata/roundtrip/starter.yaml` and `TestFromYAML_ReadsTheStarter`; `starter` of `FactoryModel` in `contracts/api/openapi.yaml`, then `task api:gen`; the "Starter item: `starter.item`" section of `docs/engineering/factory-definition.md`; the Starter item entry of `docs/reference/glossary.md`; [ADR 0032](../adr/0032-self-serve-onboarding.md) by amendment when decision 3 changes

## Run

`task api:gen`, `task test` (the rows run as `TestCheck_Starter` in `./internal/config`, the round trip in `./internal/factorymodel`)

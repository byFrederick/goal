## When you change

The trigger format: the `trigger` definition and `board.triggers` in `contracts/definitions/factory.schema.json`, `config.Trigger` in `internal/config/config.go`, or its checks in `internal/config/triggers.go`

## Also change

The rows of `TestCheck_Triggers` and `TestLoad_ParsesTriggers` in `internal/config/triggers_test.go` and the triggers in `internal/config/testdata/support/factory.yaml`; `internal/factorymodel/testdata/roundtrip/triggers.yaml`; the "Triggers: `board.triggers`" section of `docs/engineering/factory-definition.md`; the Trigger entry of `docs/reference/glossary.md`; [ADR 0026](../adr/0026-intake-triggers.md) by amendment when a decision changes

## Run

`task validate`, `task test` (the rows run as `TestCheck_Triggers` in `./internal/config`)

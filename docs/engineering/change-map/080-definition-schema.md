## When you change

A definition schema in `contracts/definitions/` (factory, evals, connection kind)

## Also change

`contracts/definitions/schema.go`; the loader and checks in `internal/config`; `internal/connkind` and its kind files; the builder's metadata in `internal/builder`; the test fixtures in `internal/config/testdata`; the docs and spec that describe the field

## Run

`task validate`, `task test`, `task web:check` when the builder's editor shows the field

## When you change

A generator, its config or its output name (`tools/permgen`, `internal/api/gen/oapi-codegen.yaml`, api-client's `gen` script, `db/sqlc.yaml`, the web build's router plugin, `tools/envcheck`)

## Also change

The generated files below and their row; `task gen`; the `*.gen.*` exclusions of `.golangci.yml` and each `biome.json`; `tools/checkplan`

## Run

`task gen`, then `task gen:check`

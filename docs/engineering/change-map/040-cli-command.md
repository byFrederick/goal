## When you change

A command, its help or its output in `internal/cli` (the goal CLI; `cmd/goal/main.go` only wires it)

## Also change

The golden files in `internal/cli/testdata/golden` when the help or output changes on purpose (`go test ./internal/cli -run TestRun_Golden -update`, then review the diff); the reads it makes, in `contracts/env.yaml` (`tools/envcheck` counts reads under `internal/cli/` as the CLI's); the CLI docs: `README.md`'s layout and `docs/engineering/architecture.md`

## Run

`go test -race ./internal/cli ./cmd/goal`, `task env:check`, `task lint`

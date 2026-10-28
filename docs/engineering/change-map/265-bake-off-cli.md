## When you change

The goal CLI's bake-off commands: `goal bake-off run`, `show` and `apply` in `internal/cli/bakeoff.go`, which call `startBakeOff`, `getBakeOff` and `applyBakeOff`

## Also change

`internal/cli/bakeoff_test.go` and its golden files `internal/cli/testdata/golden/bake-off*.txt` (`go test ./internal/cli -run TestBakeOff -update` and `-run TestRun_Golden -update`, then review the diff), the bake-off lines of the usage in `internal/cli/main.go` (every group's golden file prints it), the bake-off rows of "HTTP API" in `docs/engineering/architecture.md`, and `README.md`'s layout

## Run

`go test -race -run TestBakeOff ./internal/cli`, `go test -race -run TestRun_Golden ./internal/cli`, `task lint`, `task docs:check`

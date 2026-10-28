## When you change

The run lifecycle (`engine.RunTask` and `runTask` in `internal/core/engine/runtask.go`, `domain.RunRequest` in `internal/core/domain/run.go`)

## Also change

The line's mapping of a claim to its request (`claimed.request` and `runOnce` in `internal/core/engine/work.go`); the sandbox's open and events (`internal/core/engine/sandbox.go`) and the run's connections (`runEnv` in `internal/core/engine/connections.go`); `RunRequest.Check` when a run kind may have something new, as ADR 0034 decision 1 says what a Jonah run leaves out; "One run, end to end" in `docs/engineering/architecture.md`

## Run

`go test -race -timeout 600s ./internal/core/engine`, `task lint`

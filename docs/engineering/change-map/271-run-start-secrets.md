## When you change

What a run of a version on secrets gets at its start (`secretEnv` and `openSecrets` in `internal/core/engine/environment.go`, `domain.Environment`, `SecretNames`, `FillSecrets` and `ModelKeyEnv` in `internal/core/domain/environment.go`, `Line.OnSecrets`), or the environment files' mapping into the line (`environments` in `internal/config/line.go`)

## Also change

`runEnv` in `internal/core/engine/connections.go`, which picks secrets or connections by the request's model key; `claimed.request` and `agentEnvironment` in `internal/core/engine/work.go`, `modelEnv` in `internal/core/engine/definitionrun.go` and the Jonah review's request in `internal/jonah/reviews.go`, which pick the same way; `RunRequest.Check` and `Line.Refusal`; `ports.OrgSecrets.Resolve` and its fake when what a use audits changes; "One run, end to end" in `docs/engineering/architecture.md`

## Run

`go test -race -timeout 600s -run TestRun ./internal/core/engine`, `go test -race -timeout 120s ./internal/core/domain ./internal/config ./internal/jonah`, `task lint`

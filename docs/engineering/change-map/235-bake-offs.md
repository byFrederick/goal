## When you change

The bake-off loop and operations (`internal/jonah/bakeoffs.go`, the bake-off branches of `internal/jonah/evals.go`), the draft write they apply through (`authoring.Service.SetAgentModel`, `internal/authoring/bakeoff.go`) or `GOAL_BAKEOFF_MODELS`

## Also change

`internal/jonah/bakeoffs_test.go`, `internal/authoring/bakeoff_test.go`, `TestSettings_BakeOffModels` in `cmd/goald/settings_test.go`, `contracts/env.yaml` and `.env.example` for the setting (then `task env:check -- -write`), `ports.EvalTask` and the `EvalRunner` adapter when a try's model override changes, and the Phase 5 paragraph of `docs/engineering/architecture.md`; ADR 0029 when a rule of the report, the arms or apply changes

## Run

`go test -race -run TestBakeOff_ ./internal/jonah`, `go test -race -run TestEvals_ ./internal/jonah`, `go test -race -run TestService_SetAgentModel ./internal/authoring`, `go test -race -run TestSettings_ ./cmd/goald`, `task env:check`, `task docs:check`

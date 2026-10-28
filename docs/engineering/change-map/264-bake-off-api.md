## When you change

The bake-off routes: the `listBakeOffs`, `startBakeOff`, `getBakeOff`, `rerunBakeOff` and `applyBakeOff` operations and the `BakeOff*` schemas in `contracts/api/openapi.yaml`, or their handlers in `internal/api/bakeoffs.go` and the bake-off methods of `api.EvalLoop` and `api.EvalRunReads` in `internal/api/evals.go`

## Also change

`task api:gen` (then commit `internal/api/gen`, `internal/api/permissions.gen.go` and `packages/api-client/src/schema.gen.ts`), `internal/api/bakeoffs_test.go` and the bake-off cases of `TestEvals_Unavailable` in `internal/api/evals_test.go`, the bake-off rows of "HTTP API" in `docs/engineering/architecture.md`, the Bake-off row of `docs/reference/glossary.md`, and the bake-off screen and the goal CLI's bake-off commands that call them

## Run

`task api:gen` (no diff), `task api:permissions-check`, `go test -race -run TestBakeOffs_ ./internal/api`, `go test -race -run TestEvals_ ./internal/api`, `task docs:check`

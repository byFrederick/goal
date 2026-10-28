## When you change

The bake-off store: the `eval_run_arms` table and the bake-off columns of `eval_runs` (`kind`, `station`, `agent`, `tries_per_case`) and `eval_results` (`arm`, `duration_ms`) (`db/migrations/00077_bake_offs.sql` and the migrations after it), their queries in `db/queries/evals.sql`, or the adapter's bake-off methods (`internal/adapters/store/postgres/bakeoffs.go`)

## Also change

The bake-off methods of the `Evals` port (`internal/core/ports/evals.go`) and its in-memory fake (`internal/core/ports/porttest/evals.go`, tested in `evals_test.go`); the bake-off types and `RecommendBakeOffArm` in `internal/core/domain/bakeoff.go` ([ADR 0029](../../adr/0029-bake-offs-are-eval-runs-of-one-agent-across-models.md)); the integration tests in `internal/adapters/store/postgres/bakeoffs_integration_test.go`, and `TestEvals_RLSAndGrants` and `TestPurgeOrg_RemovesEvals` in `evals_integration_test.go`; keep `LatestEvalRunForContent` and the case history queries to runs of kind `eval`

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

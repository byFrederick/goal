## When you change

The eval runs store: the `eval_runs` and `eval_results` tables (`db/migrations/00066_evals.sql` and the migrations after it), their queries (`db/queries/evals.sql`) or the adapter (`internal/adapters/store/postgres/evals.go`)

## Also change

The `Evals` port (`internal/core/ports/evals.go`) and the eval types in `internal/core/domain/eval.go` (`ManifestSHA256` is the hash the publish gate compares, so change it only with every run it names in mind); the integration tests in `internal/adapters/store/postgres/evals_integration_test.go`; `orgTables` in `schema_integration_test.go` for a new eval table

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

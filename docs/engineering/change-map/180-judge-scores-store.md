## When you change

The judge scores store: the `judge_scores` table (`db/migrations/00066_judge_scores.sql` and the migrations after it), its queries (`db/queries/judge_scores.sql`) or the adapter (`internal/adapters/store/postgres/judgescores.go`)

## Also change

The `JudgeScores` port (`internal/core/ports/judgescores.go`) and its domain types (`internal/core/domain/judgescore.go`), its in-memory fake (`internal/core/ports/porttest/judgescores.go`) and the conformance suite `porttest/judgescoretest`, which runs against both; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` when the table is renamed; `TestJudgeScores_RLS` when a key to factories, items or runs changes, so `goal_purge_org` still removes an organization's scores

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

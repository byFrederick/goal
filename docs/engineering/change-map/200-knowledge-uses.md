## When you change

Knowledge use and verdicts: the `knowledge_uses` and `knowledge_verdicts` tables (`db/migrations/00067_knowledge_uses.sql` and the migrations after it), their queries (the use and verdict queries at the end of `db/queries/knowledge.sql`) or the adapter (`internal/adapters/store/postgres/knowledge_uses.go`)

## Also change

`ports.Knowledge`'s `RecordUse`, `GiveVerdict`, `SourceUsage`, `DocumentUses` and `RunUses` (`internal/core/ports/knowledge.go`) and their domain types and limits (`internal/core/domain/knowledge.go`, ADR 0027), the in-memory fake (`internal/core/ports/porttest/knowledge_uses.go`) and the conformance suite `porttest/knowledgetest` (`uses.go`), which runs against both; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` when a table is renamed; ADR 0027 when the window's bounds, the verdict kinds or the note's length change

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

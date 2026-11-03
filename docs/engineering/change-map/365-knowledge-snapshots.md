## When you change

Knowledge as files: the `knowledge_snapshots`, `knowledge_files` and `knowledge_reads` tables and the `read_when` and `current_snapshot_id` columns of `knowledge_sources` (`db/migrations/00092_knowledge_snapshots.sql` and the migrations after it), their queries (the snapshot, file and read queries at the end of `db/queries/knowledge.sql`) or the adapter (`internal/adapters/store/postgres/knowledge_files.go`)

## Also change

`ports.Knowledge`'s `PutSnapshot`, `Snapshots`, `Files`, `AddReads` and `FileReads` (`internal/core/ports/knowledge.go`) and their domain types and limits (`internal/core/domain/knowledge_files.go`, ADR 0038 decisions 3, 4 and 15), the in-memory fake (`internal/core/ports/porttest/knowledge_files.go`) and the conformance suite `porttest/knowledgetest` (`files.go`), which runs against both; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` when a table is added or renamed; `ports.ObjectStore`'s `GetRange` and `PutStream` with the suite `porttest/objectstoretest` when the way snapshot zips are read or written changes

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

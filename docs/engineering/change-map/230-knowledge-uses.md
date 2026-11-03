## When you change

Knowledge reads: the `knowledge_reads` daily rollup (`db/migrations/00090_knowledge_snapshots.sql` and the migrations after it), its queries in `db/queries/knowledge.sql` or the adapter; how a run's touches count (`TouchOfKnowledge` and `KnowledgeTally` in `internal/core/domain/knowledge_files.go`, the engine's `countReads` in `internal/core/engine/runtask.go`); or the worker's `path` field of `tool_use` events (`toolPath` in `apps/worker/src/events.ts`). Until the cut-over (ADR 0038 decision 16) also the `knowledge_uses` and `knowledge_verdicts` tables (`db/migrations/00072_knowledge_uses.sql`) and their adapter (`internal/adapters/store/postgres/knowledge_uses.go`)

## Also change

`ports.Knowledge`'s `AddReads` and `FileReads` (`internal/core/ports/knowledge.go`), `KnowledgeFileReads.Check` (a whole-source search counts at `KnowledgeSourceRoot`, "."), the in-memory fake (`internal/core/ports/porttest/knowledge_files.go`) and the conformance suite `porttest/knowledgetest`, which runs against both; the tests `TestKnowledgeTouch_CountsAReadAndAGrep`, `TestRun_CountsKnowledgeReads` and `TestRun_KnowledgeReadsFailureLeavesTheRunAlone`, and the worker's case in `apps/worker/src/run.test.ts`; the `tool_use` row of `apps/worker/README.md`; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` when a table is renamed; ADR 0038 decision 10 when what counts as a read or a search changes. The old tables keep `RecordUse`, `SourceUsage`, `DocumentUses`, `RunUses` and `RunVerdicts` until software#532 drops them

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`, `pnpm --dir apps/worker test`

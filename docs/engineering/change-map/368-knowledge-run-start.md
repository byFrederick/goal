## When you change

How a run gets its knowledge (ADR 0038 decisions 7 to 9): the pins (`pinKnowledge` in `internal/core/engine/work.go`, `domain.KnowledgePin`, `TaskClaims.Knowledge`), the Knowledge section's template and its one builder (`domain.KnowledgeIndex` and `domain.IndexKnowledge` in `internal/core/domain/knowledge_section.go`), the bundle's `spec.knowledge` or `GET /run/knowledge/<source>` (`internal/gateway/knowledge.go`), or prepare's unpacking into `/knowledge` (`apps/worker/src/prepare.ts`)

## Also change

Their tests (`TestRun_KnowledgePins*`, `TestRunDefinition_KnowledgePinsOfTheRequestsAgent`, `TestKnowledgeIndex_Section*`, `TestBundle_Knowledge*`, `TestAPI_DraftKnowledgePreview*` (the editor's preview, `internal/api/knowledge_preview.go`, builds the same section), `TestServeKnowledge_*`, `prepare: knowledge` in `prepare.test.ts`, the Knowledge tests in `agent.test.ts`); the worker's `Knowledge` schema in `apps/worker/src/spec.ts` with the bundle; a new gateway path in `api.KnowledgePath`, `cmd/goald`'s routes, `internal/egress` (`KnowledgePrefix`) and their tests; `/knowledge` in `apps/worker/Dockerfile` and `contracts/api/sandbox.yaml`; `apps/worker/README.md` ("Sandbox contract", prepare's steps, spec.json); the "What agents do with it" section of `apps/docs/src/content/docs/concepts/knowledge.md`

## Run

`task test`, `pnpm --dir apps/worker test`, `task docs:gen`, `task docs:check`

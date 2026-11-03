## When you change

The knowledge files and reads API (ADR 0038 decision 13): `listKnowledgeFiles`, `getKnowledgeFile`, `uploadKnowledgeFiles`, `deleteKnowledgeUpload`, `checkKnowledgeSource`, `findKnowledgeFiles` and `listKnowledgeReads` in `contracts/api/openapi.yaml` (tag `knowledge`), `knowledge:read` or `knowledge:manage` in `contracts/api/permissions.yaml`, or their handlers (`internal/api/knowledge_files.go`, `internal/api/knowledge_check.go`). Until the cut-over also `getKnowledgeUsage`, `listKnowledgeDocumentUses` and `listRunKnowledge` (`internal/api/knowledge_uses.go`)

## Also change

The handler tests (`internal/api/knowledge_files_test.go`: `TestKnowledge_FileReadByRange`, `TestKnowledge_DeleteAnUpload`, `TestKnowledge_Reads`, `TestKnowledge_Check`, `TestKnowledge_PermissionMatrix`; `TestKnowledge_DeleteRefusedWhileAPublishedVersionListsIt` and `TestKnowledgeProblem_Codes` in `internal/api/knowledge_internal_test.go` for a new problem code); `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it), `TestPermissions_PersonalTokensCarryExactlyThese` and the sign-in `wantCan` lists when a permission changes, and the token form's scopes (`apps/web/src/shared/tokens/tokens.ts`); `knowledge.Ingestor.Check` (`internal/knowledge/check.go`) when what the Check lists changes; the ranged zip read follows `zipPaths` in `internal/gateway/knowledge.go`, so change both; `KnowledgeObjects` and `KnowledgeCheck` on `api.Deps`, wired in `cmd/goald/main.go`; the HTTP API table of `docs/engineering/architecture.md`; the `goal knowledge` and `goal memory` commands (`internal/cli/knowledge.go`, `internal/cli/memory.go`), their usage text and `task docs:ref:gen`

## Run

`task api:gen`, `task api:permissions-check`, `task test`, `task docs:gen`, `task docs:check`, `pnpm --dir apps/web run typecheck`

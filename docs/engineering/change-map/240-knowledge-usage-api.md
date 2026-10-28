## When you change

The knowledge usage and verdict API: `getKnowledgeUsage`, `listKnowledgeDocumentUses`, `giveKnowledgeVerdict` and `listRunKnowledge` in `contracts/api/openapi.yaml` (tag `knowledge`), `knowledge:verdict` in `contracts/api/permissions.yaml`, or their handlers (`internal/api/knowledge_uses.go`)

## Also change

The handler tests (`internal/api/knowledge_uses_test.go`: `TestKnowledgeUses_Usage`, `TestKnowledgeUses_DocumentAndRunUses`, `TestKnowledgeUses_Verdict`, `TestKnowledgeUses_Refused`) and `TestKnowledgeProblem_Codes` for a new problem code; the operations' probes in `internal/api/org_test.go`; `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it) and `TestPermissions_PersonalTokensCarryExactlyThese` when a permission changes; `ports.Knowledge`'s `GiveVerdict` and `RunVerdicts` with both implementations and `knowledgetest` when what a verdict audits or notifies changes; the HTTP API table of `docs/engineering/architecture.md`; the `goal knowledge` commands and the Knowledge page that call the operations ([ADR 0027](../adr/0027-knowledge-use-and-verdicts.md))

## Run

`task api:gen`, `task api:permissions-check`, `task test`, `task test-integration`, `task auth:check`

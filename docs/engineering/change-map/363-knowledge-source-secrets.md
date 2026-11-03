## When you change

What a synced knowledge source reads with: its secret (`knowledge_sources.secret_id`, 00088; `domain.SyncSpec.Secret`, `domain.SourceReaderKey`), how the ingestor opens it (`internal/knowledge/ingest.go` `secretReader`), or how `gitdocs` and `confluence` read a source on a secret

## Also change

`NewKnowledgeSource`, `KnowledgeSource` and `KnowledgeSourcePatch` in `contracts/api/openapi.yaml` and `internal/api/knowledge.go`; `secretUses` in `internal/api/secrets.go`; the knowledge conformance suite (`SecretReadBySource_KeptByIDAndDeleteIsAConflict`) with `porttest.Knowledge.ReadSecrets` and `postgres.Secrets`' delete check; moving over's `Mover.sources` in `internal/moveover/run.go`; `goal knowledge add --secret`; `apps/docs/src/content/docs/concepts/knowledge.md`

## Run

`go test -race -run 'TestKnowledge_Source' and -run TestAPI_SecretDeleteRefusedWhileASourceUsesIt ./internal/api`, `go test -race ./internal/knowledge ./internal/adapters/sourcereader/...`, `go test -race -run TestRun_PointsKnowledgeSourcesAtSecrets ./internal/moveover` and `task test-integration` (`TestKnowledge_Conformance`)

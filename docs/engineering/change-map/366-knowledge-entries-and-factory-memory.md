## When you change

A `knowledge.sources` entry's shape or its checks (`contracts/definitions/factory.schema.json`, `config.KnowledgeSource`, the `knowledge` check in `internal/config/checks.go`, or `knowledge()` in `internal/authoring/secrets.go`), or the factory memory (`contracts/definitions/memory.schema.json`, `internal/config/memory.go`, the limits in `internal/core/domain/knowledge_files.go`)

## Also change

Their tests (`TestCheck_KnowledgeEntries`, `TestCheck_FactoryMemory`, `TestSecretsCheck_KnowledgeSources`, `TestFromYAML_KeepsKnowledgeEntryObjects`); the "Knowledge" and "Factory memory" sections of `docs/engineering/factory-definition.md`; `task docs:ref:gen` for the docs site's definition and memory file references

## Run

`task test`, `task docs:gen`, `task docs:check`

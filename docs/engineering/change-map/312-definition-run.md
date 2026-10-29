## When you change

A draft request's box: `internal/core/engine/definitionrun.go` (`RunDefinition`: its env, task token and result), the definition bundle in `internal/gateway/bundle.go` (`definitionBundle`, `bundleDefinition`, `bundleDraftRequest`) or `TaskKindDraftRequest` in `internal/core/domain/token.go`

## Also change

goal-agent's definition mode on the other side: `apps/worker/src/definition.ts`, `apps/worker/src/spec.ts` (`DraftRequest`, `Bundle`'s `definition`), `apps/worker/src/prepare.ts` and `apps/worker/README.md` ("Definition mode"); `ports.DefinitionRunner` in `internal/core/ports/draftrequests.go` and its fake `porttest.DefinitionRunner`; `docs/engineering/architecture.md` ("One draft request, end to end")

## Run

`go test -race -run TestRunDefinition_ ./internal/core/engine`, `go test -race -run TestTaskToken_ ./internal/core/domain`, `go test -race -run TestServeRun_ ./internal/gateway` and `pnpm --dir apps/worker exec vitest run src/definition.test.ts src/prepare.test.ts`

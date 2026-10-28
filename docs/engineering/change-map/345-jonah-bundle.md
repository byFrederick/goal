## When you change

A Jonah review's bundle: `internal/gateway/jonah.go` (`jonahBundle`, `bundleJonah` and its file, event and rejected types, `gateway.Jonah`), the jonah token's claims in `internal/core/domain/token.go` (`TaskKindJonah`, `ws`, `we`, `jm`) or `domain.ProposalPathAllowed`, which picks the files it serves

## Also change

The worker's `Jonah`, `JonahFile`, `JonahEvent` and `JonahRejected` in `apps/worker/src/spec.ts` and its jonah mode in `apps/worker/src/jonah.ts` when a field changes; `goal_jonah_paths_allowed` in the migrations when the allowlist changes; `engine.gatewayToken` and `domain.RunRequest` when a claim changes; `docs/engineering/architecture.md` ("Jonah reviews" in the MCP gateway section)

## Run

`go test -race ./internal/gateway ./internal/core/domain ./internal/core/engine ./internal/definitions` and `pnpm --dir apps/worker exec vitest run src/jonah.test.ts`

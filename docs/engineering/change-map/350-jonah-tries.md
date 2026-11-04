## When you change

Tries of past items ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) decisions 9, 11 and 13): the try loop in `internal/jonah/tries.go` (`Tries.Sweep`, `Tries.Work`, `TryConfig`), `jonah.TriedOf` in `internal/jonah/tried.go`, Try again in `internal/jonah/tryagain.go`, Try another model in `internal/jonah/modelproposal.go`, `domain.PastTry` in `internal/core/domain/pasttry.go`, the `PastTries` port in `internal/core/ports/jonah.go` or its adapter (`internal/adapters/store/postgres/tries.go`, `db/queries/tries.sql`)

## Also change

`TestTries_*`, `TestTryAgain_*` and `TestModelProposals_*` in `internal/jonah`; the fake in `internal/core/ports/porttest/pasttries.go` and the storetest cases in `internal/core/ports/porttest/storetest/tries.go` with the adapter; a try's bundle in `internal/gateway` (`TestBundle_Try*`) when what a try gets changes; `jonahTryLoop` in `cmd/goald/jonah.go`, and `contracts/env.yaml` with `task env:check -- -write` for a `GOAL_JONAH_TRY_*` setting; the `ProposalTried` and `Tried*` schemas in `contracts/api/openapi.yaml` (then `task api:gen`) and the Tried on past items panel in `apps/web/src/features/jonah/tried-panel.tsx`; "Tried on past items", "Try again" and "Try another model" in `apps/docs/src/content/docs/concepts/jonah.md`, the Try row of `docs/reference/glossary.md` (then `task docs:ref:gen`) and "Jonah: learning from the line" in `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestTries_' ./internal/jonah`, `go test -race -run 'TestTryAgain_' ./internal/jonah`, `go test -race -run 'TestModelProposals_' ./internal/jonah`, `go test -race -run 'PastTry' ./internal/core/domain`, `go test -race -run 'TestBundle_Try' ./internal/gateway`, `task sqlc` and `task test-integration` after a query, `task docs:check`

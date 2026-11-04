## When you change

Jonah's evidence ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) decisions 1 and 2): `domain.BuildEvidence` and what makes an item evidence in `internal/core/domain/evidence.go` (`OutlierFactor`, `EvidenceMedianWindow`, `EvidenceTextLimit`, `PerTurnKinds`), `jonahEvidence` in `internal/gateway/jonah.go`, or the `EvidenceItems` and `StationRunMedians` queries in `db/queries/jonah.sql` and `internal/adapters/store/postgres/jonah.go`

## Also change

`TestEvidence_*` in `internal/core/domain/evidence_test.go`; the fake in `internal/core/ports/porttest/jonahstore.go` and the storetest cases in `internal/core/ports/porttest/storetest/jonah.go` with the adapter; the evidence paths in goal-agent's jonah prompt (`apps/worker/src/jonah.ts`) when a file of the folder is renamed; the Evidence section of `apps/docs/src/content/docs/concepts/jonah.md`, the Evidence row of `docs/reference/glossary.md` (then `task docs:ref:gen`) and "Jonah: learning from the line" in `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestEvidence_' ./internal/core/domain`, `go test -race -run 'TestServeRun_JonahBundle' ./internal/gateway`, `task sqlc` and `task test-integration` after a query, `task docs:check`

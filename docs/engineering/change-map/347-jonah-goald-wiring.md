## When you change

How goald runs the Jonah review loop: `cmd/goald/jonah.go` (`runJonahReviews`, `jonahRound`: the sweep under the schedule lock, the work on every goald), its wiring in `cmd/goald/main.go` (`jonah.NewReviews`, `gateway.Deps.Jonah`) or its settings in `cmd/goald/settings.go` (`GOAL_JONAH_REVIEW_EVERY`, `GOAL_JONAH_REVIEW_TIMEOUT`, `GOAL_JONAH_REVIEW_LEASE_MARGIN`)

## Also change

`contracts/env.yaml` and the generated `docs/reference/environment.md` (`task env:check -- -write`) for a setting; `internal/definitions` (`CurrentLines`, `Line`) when `jonah.ReviewDefinitions` changes; the storetest Jonah cases in `internal/core/ports/porttest/storetest/jonah.go` when a claim or lease rule changes; `docs/engineering/architecture.md` (Wiring, Jonah reviews)

## Run

`go build ./cmd/goald`, `go test -race -run 'TestJonahReviews_' ./cmd/goald`, `go test -race -run 'TestSettings_' ./cmd/goald`, `go test -race -run 'TestStore_Conformance/Jonah_' ./internal/core/ports/porttest` and `task env:check`

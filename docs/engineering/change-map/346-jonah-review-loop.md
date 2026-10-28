## When you change

The Jonah review loop: `internal/jonah/reviews.go` (`Reviews`, `ReviewDeps`, `ReviewConfig`, `ReviewDefinitions`, `TaskRunner`, the threshold and budget checks) or `internal/jonah/review_output.go` (the run's output shape and the checks that drop a proposal)

## Also change

`apps/worker/src/jonah.ts` (`JonahReply`) and `internal/gateway/jonah.go` (the event ids evidence names) when the output's shape changes; `domain.JonahReviewResult`, `NewProposal` and `NewObservation` when what is stored changes; `docs/engineering/architecture.md` (phase 5, the review loop)

## Run

`go test -race ./internal/jonah` and `golangci-lint run ./internal/jonah/...`

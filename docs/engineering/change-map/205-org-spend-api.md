## When you change

The organization's spend read: `GET /v1/orgs/{org}/spend` (`getOrgSpend`) in `contracts/api/openapi.yaml`, its handler `internal/api/spend.go`, or `goal spend` (`internal/cli/spend.go`)

## Also change

The tests in `internal/api/spend_test.go` (each window, over budget, the hidden factories) and `internal/cli/spend_test.go` with its golden file `internal/cli/testdata/golden/spend-results.txt`; `internal/api/testdata/access.golden` when its scope changes; the Spend and billing screen's calls in `apps/web` once it reads it. A budget's `week_usd` and `over_budget` follow the engine's rolling window (`domain.BudgetSince`, `Runtime.WeeklyBudget`): change them with the engine's budget, not alone

## Run

`task api:gen`, `go test ./internal/api -run TestGetOrgSpend_`, `go test ./internal/api -run TestAuthorize_TwoOrgMatrix`, `go test ./internal/cli -run TestSpend_`, `go test ./internal/cli -run TestRun_Golden`, `task test`

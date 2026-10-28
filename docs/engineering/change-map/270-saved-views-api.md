## When you change

The saved views routes: `internal/api/views.go`, their operations in `contracts/api/openapi.yaml` (tag `views`) or the `SavedView`, `NewSavedView`, `SavedViewChange` and `SavedViewList` schemas

## Also change

`task api:gen`'s generated files, `internal/api/views_test.go`, the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it), the saved views methods of the `Store` port and the `api.Items` interface with the fake, the postgres adapter and `porttest/storetest` when a route needs a new read, the Items page's calls in `apps/web` when an answer's shape changes, and the views row of "HTTP API" in `docs/engineering/architecture.md`

## Run

`task api:gen`, `go test -race -run 'TestSavedViews' ./internal/api`, `go test -race -run 'TestAuthorize_' ./internal/api`, `go test -race -run TestAPI_TwoOrgMatrixOverHTTP ./internal/api`, and `task test-integration` when the port changes

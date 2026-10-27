## When you change

The evals routes (`/v1/orgs/{org}/factories/{factory}/evals…` in `contracts/api/openapi.yaml`, `internal/api/evals.go`) or the publish eval gate (`internal/authoring/evalgate.go`, its call in `authoring.Service.Publish`)

## Also change

`task api:gen` and `testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`), the probes in `internal/api/org_test.go`, `internal/api/evals_test.go`, `internal/authoring/evalgate_test.go`, `goal eval` (`internal/cli/eval.go`, its usage and golden files), the route table in `docs/engineering/architecture.md` and "Publishing with eval cases" in `docs/engineering/factory-definition.md`; a change to how cases parse goes in `config.EvalCases` (`internal/config/evals.go`) and `jonah.(*Evals).Start` with it

## Run

`task api:gen`, `go test -race -run TestEvals_ ./internal/api`, `go test -race -run TwoOrgMatrix ./internal/api`, `go test -race -run 'TestPublish_' ./internal/authoring`, `go test -race -run TestEval_ ./internal/cli`, `task docs:check`

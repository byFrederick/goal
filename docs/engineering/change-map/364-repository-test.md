## When you change

The repository test that needs no factory (`POST /v1/orgs/{org}/repositories/test`, `internal/api/repositorytest.go`) or `CodeHost.RepositoryAccess` in `internal/adapters/codehost/github` and `gitlab`

## Also change

`RepositoryTestRequest` and `RepositoryTestResult` in `contracts/api/openapi.yaml`, then `task api:gen`; `porttest.CodeHost`'s `SetAccess` and `StallAccess`; the probe in `internal/api/org_test.go` and `testdata/access.golden`; `goal repository test` (`internal/cli/repository.go`) and its usage text, then `task docs:ref:gen`; the `goal repository test` line of `apps/docs/src/content/docs/concepts/connections.md`

## Run

`go test -race -run 'TestRepositoryTest_' ./internal/api`, `go test -race -run TestRepositoryAccess ./internal/adapters/codehost/...` and `go test -race -run Repository ./internal/cli`

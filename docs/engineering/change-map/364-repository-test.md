## When you change

A factory's repository test (`POST /v1/orgs/{org}/factories/{factory}/repositories/test`, `internal/api/factoryrepositorytest.go`) or `CodeHost.RepositoryAccess` in `internal/adapters/codehost/github` and `gitlab`

## Also change

`FactoryRepositoryTestRequest` and `FactoryRepositoryTest` in `contracts/api/openapi.yaml`, then `task api:gen`; `porttest.CodeHost`'s `SetAccess` and `StallAccess`; the probe in `internal/api/org_test.go` and `testdata/access.golden`; `goal repository test` (`internal/cli/repository.go`) and its usage text, then `task docs:ref:gen`; the `goal repository test` lines of `apps/docs/src/content/docs/concepts/access-and-secrets.md`

## Run

`go test -race -run 'TestFactoryRepositoryTest_' ./internal/api`, `go test -race -run TestRepositoryAccess ./internal/adapters/codehost/...` and `go test -race -run Repository ./internal/cli`

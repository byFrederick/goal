## When you change

The draft requests API: `createDraftRequest`, `listDraftRequests`, `getDraftRequest` and the `DraftRequest` schema in `contracts/api/openapi.yaml`, their handlers (`internal/api/draftrequests.go`) or `goal factory ask` (`internal/cli/ask.go`)

## Also change

The generated code (`task api:gen`: `internal/api/gen`, `packages/api-client`, `internal/api/permissions.gen.go`, `apps/auth/src/access.gen.ts`); the access golden `internal/api/testdata/access.golden` when an operation's scope changes (`go test ./internal/api -run TwoOrgMatrix -update`); the handler tests `TestAPI_DraftRequests` and `TestAPI_DraftRequestRefusals` (`internal/api/draftrequests_test.go`) and the CLI tests `TestFactoryAsk_*` (`internal/cli/ask_test.go`), with the help goldens when the usage changes; the draft requests row of `docs/engineering/architecture.md`'s API table, `README.md`'s CLI list and ADR 0021 when the limits, statuses or refusals change

## Run

`task api:gen`, `task api:permissions-check`, `go test -race ./internal/api ./internal/cli`, `task docs:check`, `task lint`

## When you change

The credentials API: its operations in `contracts/api/openapi.yaml` (tag `credentials`, `/v1/orgs/{org}/credentials`), `credential:read` or `credential:manage` in `contracts/api/permissions.yaml`, or the handlers (`internal/api/credentials.go`)

## Also change

The handler tests (`internal/api/credentials_test.go`, `TestAPI_CredentialResponsesHoldNoValue` among them: no answer holds a value); the HTTP API table of `docs/engineering/architecture.md`; `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it) and `TestPermissions_PersonalTokensCarryExactlyThese` when a permission changes; the `goal credential` commands and the Credentials screens that call the operations ([ADR 0017](../adr/0017-credentials.md))

## Run

`task api:gen`, `task api:permissions-check`, `task test`, `task auth:check`

## When you change

The secrets API: its operations in `contracts/api/openapi.yaml` (tag `secrets`, `/v1/orgs/{org}/secrets`), `secret:read` or `secret:manage` in `contracts/api/permissions.yaml`, the handlers (`internal/api/secrets.go`), or the deprecated credentials operations (tag `credentials`, `internal/api/credentials.go`) that the same permissions guard until the web moves to `/secrets`

## Also change

The handler tests (`internal/api/secrets_test.go`, `TestAPI_SecretResponsesHoldNoValue` among them: no answer holds a value; `internal/api/credentials_test.go` while the credentials operations stay); the probes of `internal/api/org_test.go`; the HTTP API table of `docs/engineering/architecture.md`; `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it) and `TestPermissions_PersonalTokensCarryExactlyThese` when a permission changes; the `goal secret` commands (`internal/cli/secret.go`, `internal/cli/secret_test.go`) and their usage text in `internal/cli/main.go`, then `task docs:ref:gen`; the screens that call the operations ([ADR 0037](../adr/0037-secrets-and-environment-files.md))

## Run

`task api:gen`, `task api:permissions-check`, `task test`, `task auth:check`, `task docs:ref:gen`

## When you change

The environment tests or the Access read model: the `environment_tests` table (`db/migrations/00086_environment_tests.sql`), its queries (`db/queries/environment_tests.sql`) and adapter (`internal/adapters/store/postgres/environmenttests.go`), the `EnvironmentTests` or `ToolLister` port (`internal/core/ports/environmenttests.go`), the MCP tool lister (`internal/adapters/toollister/mcp`), or the routes (`internal/api/environmenttest.go`, `internal/api/access.go`)

## Also change

The fakes (`internal/core/ports/porttest/environmenttests.go`) and the conformance suite `porttest/environmenttesttest`, which runs against both; `contracts/api/openapi.yaml` (`EnvironmentTestResult`, `Access` and their parts) and `access:read` in `contracts/api/permissions.yaml`; the CLI's `goal env test` and `goal access` (`internal/cli/env.go`, `internal/cli/access.go`) and their usage text; the scrubbing of opened values (`scrubValues`) whenever a test can return something new

## Run

`task sqlc`, `task migrate-check`, `task api:gen`, `go test -run TestEnvironmentTest_ ./internal/api`, `go test -run TestAccess_ ./internal/api`, `go test -run TestPermission ./internal/api`, `go test -run TestEnvTestCommand ./internal/cli`, `go test -run TestAccessCommand ./internal/cli`, `task test-integration`

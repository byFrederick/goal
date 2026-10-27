## When you change

Intake's read API: `getIntake` or `getIntakeTrigger` in `contracts/api/openapi.yaml` (tag `intake`), the `intake:read` permission in `contracts/api/permissions.yaml`, or their handlers in `internal/api/intake.go`

## Also change

`task api:gen` (the Go server, `internal/api/permissions.gen.go`, `packages/api-client/src/schema.gen.ts`), the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review), the key and token scope lists in `apps/web/src/features/api-keys` when a permission changes, the `Intake` port with its fake, adapter and `porttest/intaketest` when a read changes, and the intake rows of `docs/engineering/architecture.md` ("HTTP API")

## Run

`go test -race -run 'TestIntake' ./internal/api` and `go test -race -run TwoOrgMatrix ./internal/api`, `task api:permissions-check`, `pnpm --dir apps/web exec vitest run src/features/api-keys mock`, and `task test-integration` when the port changes

## When you change

`contracts/api/permissions.yaml`

## Also change

The operations' `x-goal-scope` in `contracts/api/openapi.yaml`; the generated table `internal/api/permissions.gen.go` and the auth roles `apps/auth/src/access.gen.ts` (both from the task); the permission tests (`internal/api/permissions_internal_test.go`, `tools/permgen`)

## Run

`task api:gen`, `task gen:check`, `task test`, `task auth:check`

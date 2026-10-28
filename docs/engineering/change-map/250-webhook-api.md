## When you change

The webhook endpoint routes and the delivery log: `internal/api/webhooks.go`, their operations in `contracts/api/openapi.yaml` (tag `webhooks`) or the `webhook:manage` permission in `contracts/api/permissions.yaml`

## Also change

`task api:gen`'s generated files, `internal/api/webhooks_test.go`, the probes in `internal/api/org_test.go` and `internal/api/testdata/access.golden` (`go test ./internal/api -run TwoOrgMatrix -update`, then review it), the `Intake` port with its fake, the postgres adapter and `porttest/intaketest` when a route needs a new read (`Delivery`, `DeliveryDays`), and the webhook row of "HTTP API" in `docs/engineering/architecture.md`. The secret's encoding (64 hex characters used as the HMAC key as written) binds the receiver at `POST /hooks/{endpoint}`, so change both together

## Run

`task api:gen`, `task api:permissions-check`, `go test -race -run 'TestWebhooks_' ./internal/api`, `go test -race -run 'TestPermissions_' ./internal/api`, `go test -race -run 'TestAuthorize_' ./internal/api`, `go test -race -run TestIntake_Conformance ./internal/core/ports/porttest`, and `task test-integration` when the port changes

## When you change

The credentials store: the `credentials` table (`db/migrations/00065_credentials.sql` and the migrations after it), its queries (`db/queries/credentials.sql`) or the adapter (`internal/adapters/store/postgres/credentials.go`)

## Also change

The `Credentials` port (`internal/core/ports/credentials.go`), its in-memory fake (`internal/core/ports/porttest/credentials.go`) and the conformance suite `porttest/credentialtest`, which runs against both; `goal_connection_key_ids` and `postgres.Rekey` when a sealed column changes, so goald's start-up key check and `goal db rekey` still cover credentials; when a connection's references change (`connections.secret_refs`, `db/migrations/00070_connection_secret_refs.sql`), the `Connections` and `Credentials` ports' stores and fakes together, since `Credentials.UsedBy` and `Credentials.Delete`'s conflict read them, and both `connectiontest` and `credentialtest`

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

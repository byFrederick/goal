## When you change

The credentials store: the adapter (`internal/adapters/store/postgres/credentials.go`), which reads and writes the `secrets` table (00065's `credentials`, renamed by `db/migrations/00085_secrets.sql`) through the secrets store's code, or its own queries over connections' references (`db/queries/credentials.sql`)

## Also change

The `Credentials` port (`internal/core/ports/credentials.go`), its in-memory fake (`internal/core/ports/porttest/credentials.go`) and the conformance suite `porttest/credentialtest`, which runs against both; `goal_connection_key_ids` and `postgres.Rekey` when a sealed column changes, so goald's start-up key check and `goal db rekey` still cover credentials; when a connection's references change (`connections.secret_refs`, `db/migrations/00070_connection_secret_refs.sql`), the `Connections` and `Credentials` ports' stores and fakes together, since `Credentials.UsedBy` and `Credentials.Delete`'s conflict read them, and both `connectiontest` and `credentialtest`

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

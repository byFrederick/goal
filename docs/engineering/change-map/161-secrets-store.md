## When you change

The secrets store: the `secrets` table (`db/migrations/00085_secrets.sql`, which renamed 00065's `credentials`, and the migrations after it), its queries (`db/queries/secrets.sql`) or the adapter (`internal/adapters/store/postgres/secrets.go`)

## Also change

`domain.Secret` and its checks (`internal/core/domain/secret.go`); the `OrgSecrets` port (`internal/core/ports/orgsecrets.go`), its in-memory fake (`internal/core/ports/porttest/orgsecrets.go`) and the conformance suite `porttest/secrettest`, which runs against both; `postgres.Credentials` and its fake, which serve the same table to the credentials API, and `credentialtest`; `goal_connection_key_ids` and `postgres.Rekey` when a sealed column changes, so goald's start-up key check and `goal db rekey` still cover secrets; never `domain.SecretAAD`'s bytes, which every sealed value is bound to

## Run

`task sqlc`, `task migrate-check`, `task test`, `task test-integration`

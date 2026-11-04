## When you change

An environment file's shape or checks: `contracts/definitions/environment.schema.json`, `internal/config/environment.go` (loading, `SecretRefs`, the `environments` check) or the secrets check (`internal/authoring/secrets.go`)

## Also change

Their tests (`internal/config/environment_test.go`, `internal/authoring/secrets_test.go`) and the fixture `internal/config/testdata/build`; the "Environment files" section of `docs/engineering/factory-definition.md`; `factory.schema.json` when a key that names an environment or takes a `{{secret}}` changes, and `factoryRefPlaces` or `environmentRefPlaces` with it; `task docs:ref:gen` for the docs site's definition reference

## Run

`task test`, `task validate`, `task docs:gen`

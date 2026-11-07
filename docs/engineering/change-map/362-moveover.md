## When you change

What the record of the one-time conversion to secrets and environment files keeps (`ports.FactoryConversions`: `Record` and `List`, 00088's `factory_conversions`, 00090's UPDATE for a refusal replaced) or how the inbox lists a refused conversion

## Also change

`ports.FactoryConversions` with its fake (`internal/core/ports/porttest/conversions.go`), `porttest/conversiontest` and the postgres adapter (`internal/adapters/store/postgres/conversions.go`); the conversion inbox entry (`internal/api/inbox.go`, `InboxConversion` in `contracts/api/openapi.yaml`); the "Moving over from connections" section of `apps/docs/src/content/docs/concepts/access-and-secrets.md`

## Run

`go test -race -run TestFactoryConversions_Conformance ./internal/core/ports/porttest`, `go test -race -run TestInbox_Conversion ./internal/api` and, for the store, `task test-integration` (`TestFactoryConversions_Conformance`)

## When you change

How an item is filed, whatever its origin (ADR 0040 decisions 1 and 2): `domain.File`, `domain.FileRequest`, `domain.Origin` or `domain.FiledRecords` (`internal/core/domain/file.go`), the store helper `fileItem` (`internal/adapters/store/postgres/file.go`), or the `work_items` columns `origin`, `via`, `created_by`, `generation` and `untrusted` (`db/migrations/00112_item_origins.sql`)

## Also change

Every caller of `domain.File` (`Engine.Submit`, `createFromTrigger` in `internal/core/engine/intake.go`, `checkPart` in `internal/core/engine/parts.go`), the memory store's `file` (`internal/core/ports/porttest/file.go`) and the conformance cases in `porttest/storetest/file.go`, which run against both stores, `itemFrom` (`internal/adapters/store/postgres/convert.go`), the API's `submittedFrom` and `itemOf` (`internal/api/api.go`) with the Item schema's `origin` in `contracts/api/openapi.yaml`, and the Origin row of `docs/reference/glossary.md`

## Run

`go test -race ./internal/core/domain ./internal/core/engine ./internal/core/ports/porttest ./internal/api`, `task sqlc` and `task migrate-check` when the columns change, and `task test-integration`

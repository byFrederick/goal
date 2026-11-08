## When you change

The build cache index: `db/migrations/00145_build_caches.sql` (or a later migration of `build_caches`), `db/queries/build_caches.sql`, the `ports.BuildCaches` port in `internal/core/ports/buildcaches.go` (`FindBuildCache`, `ReserveBuildCache`, `CommitBuildCache`, `PruneBuildCaches`), the domain types in `internal/core/domain/buildcache.go`, `internal/adapters/store/postgres/buildcaches.go` or the fake in `internal/core/ports/porttest/buildcaches.go`

## Also change

`task sqlc` after a query; the adapter and the fake together, with the storetest cases in `internal/core/ports/porttest/storetest/buildcaches.go`; `orgTables` in `internal/adapters/store/postgres/schema_integration_test.go` for a new table; [ADR 0045](../adr/0045-runs-share-a-per-repository-build-cache-in-the-bucket.md) by amendment when the reserve, commit or prune rules change; the `BuildCaches` row of `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestStore_Conformance/BuildCaches' ./internal/core/ports/porttest/`, `go test -run 'BuildCache' ./internal/core/domain/`, `task migrate-check`, `task test-integration`, `task docs:check`

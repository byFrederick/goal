## When you change

The watch on every change ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) decisions 14 and 15): `jonah.Watcher` in `internal/jonah/watch.go`, `domain.Watch`, its interval and verdicts in `internal/core/domain/watch.go` (`WatchMaxAge`, `WatchMaxItems`), the `Watches` port in `internal/core/ports/watches.go`, its adapter (`internal/adapters/store/postgres/watches.go`, `db/queries/watches.sql`, `db/migrations/00111_watches.sql`) or `watchEvery` and `runWatches` in `cmd/goald/jonah.go`

## Also change

`TestWatch_*` in `internal/jonah/watch_test.go` and `TestWatchVerdict`, `TestWatchInterval` and `TestWatchSideOf` in `internal/core/domain/watch_test.go`; the fake in `internal/core/ports/porttest` with the adapter, and `TestWatches_*` in `internal/adapters/store/postgres/watches_integration_test.go`; the watch schemas in `contracts/api/openapi.yaml` and `internal/api/watches.go` (then `task api:gen`) when what a version or a proposal shows changes; After publish and Undo this change in `apps/web/src/features/jonah/` and Versions in `apps/web/src/features/factory-editor/review/version-history.tsx`; "The watch on every change" in `apps/docs/src/content/docs/concepts/jonah.md`, the Watch row of `docs/reference/glossary.md` (then `task docs:ref:gen`) and "Jonah: learning from the line" in `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestWatch_' ./internal/jonah`, `go test -race -run 'TestWatch' ./internal/core/domain`, `go test -race -run 'TestWatches_' -tags integration ./internal/adapters/store/postgres` (or `task test-integration`), `task docs:check`

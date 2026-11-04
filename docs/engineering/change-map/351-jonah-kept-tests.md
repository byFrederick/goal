## When you change

Kept tests ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) decision 12): `domain.KeptTest` and what passed the first time means in `internal/core/domain/kepttest.go`, the `KeptTests` port in `internal/core/ports/jonah.go` or its adapter (`internal/adapters/store/postgres/kepttests.go`, `db/queries/kept_tests.sql`), kept tests at publish in `internal/jonah/keptdraft.go` (`KeptDrafts`), the routes in `internal/api/kepttests.go`, or `goal test` in `internal/cli/test.go`

## Also change

`TestNewKeptTest_*` in `internal/core/domain/kepttest_test.go`; the fake in `internal/core/ports/porttest/kepttests.go` and the storetest cases in `internal/core/ports/porttest/storetest/kepttests.go` with the adapter; the kept test tries in `internal/jonah/tries.go` when how one passes changes; the `KeptTest*` and `DraftKeptTest*` schemas in `contracts/api/openapi.yaml` (then `task api:gen`); `TestTest_*` in `internal/cli/test_test.go` and the usage text (then `task docs:ref:gen` for the CLI reference); the Tests tab (`apps/web/src/features/jonah/tests-page.tsx`), Keep as a test on the Teardown (`apps/web/src/features/items/keep-test-dialog.tsx`) and the publish dialog's kept tests (`apps/web/src/features/factory-editor/review/kept-tests-list.tsx`); "Kept tests" in `apps/docs/src/content/docs/concepts/jonah.md`, the Kept test row of `docs/reference/glossary.md` and "Jonah: learning from the line" in `docs/engineering/architecture.md`

## Run

`go test -race -run 'TestNewKeptTest_' ./internal/core/domain`, `go test -race -run 'KeptTests' ./internal/api`, `go test -race -run 'TestTest_' ./internal/cli`, `go test -race -run 'TestTries_.*KeptTest' ./internal/jonah`, `task docs:check`

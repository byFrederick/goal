## When you change

A check or quick fix of `config.Check` in `internal/config` (`checks.go`, `schema.go`, the other check files)

## Also change

The check's tests (`checks_test.go`, `schema_test.go`, or the file's own) and the fixtures in `internal/config/testdata`; the `config.Check` paragraph in `docs/engineering/architecture.md`; the builder's definition editor in `apps/web` when it shows the problem or applies the fix

## Run

`task test`, `task validate`

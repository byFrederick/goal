## When you change

The judge or eval case format: the `judge` or `case` definition in `contracts/definitions/evals.schema.json`, or their checks in `internal/config/evals.go`

## Also change

The table rows of `TestCheck_Judges` and `TestCheck_EvalCases` in `internal/config/evals_test.go`; the "Judges and eval cases" section of `docs/engineering/factory-definition.md`; the Judge and Eval case entries of `docs/reference/glossary.md`; [ADR 0023](../adr/0023-judges-and-evals-as-the-screens-show-them.md) by amendment when a decision changes

## Run

`task test` (the rows run as `TestCheck_Judges` and `TestCheck_EvalCases` in `./internal/config`)

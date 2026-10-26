## When you change

A new top-level directory, or a rule's `paths:` in `.claude/rules`

## Also change

The repository map in `AGENTS.md` and the layout of ADR 0013 (a new top-level directory also needs the ADR's amendment, its decision 2); a glob must match at least one tracked file

## Run

`task layout:check`

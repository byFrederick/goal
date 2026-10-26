## When you change

Adding, renaming or moving a task (in the root `Taskfile.yml` or an area's, such as `docs/Taskfile.yml`)

## Also change

`tools/layoutcheck/tasks.txt` (every name `task --list-all` shows, sorted); a new area Taskfile gets an `includes:` entry in the root with `flatten: true` and `dir: "{{.ROOT_DIR}}"`; for a rename, everything that calls it by name: `.github/workflows/`, `.githooks/`, `tools/checkplan`, the docs, `.claude/rules` and the `goal-*` skills

## Run

`task layout:check`, `task env:check` when it reads a variable

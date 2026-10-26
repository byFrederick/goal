## When you change

A new environment read (a variable any process, `compose.yaml` or a `Taskfile.yml` reads), or one that stops being read

## Also change

Its entry in `contracts/env.yaml` (readers, class, required, meaning; `indirect:` when the name is computed at run time); the generated `docs/reference/environment.md`. `tools/envcheck` finds reads by their syntax (`os.Getenv`, `os.LookupEnv`, `getenv`, `lookup`, `or`, `env:NAME` constants in Go; `process.env.X`, `env.X`, `env['X']`, `get('X')`, `required(env, 'X')` in TypeScript; wrangler.jsonc vars and Secrets Store bindings): a read through a helper with another name is not seen, so declare it with `indirect:`

## Run

`task env:check -- -write`, then `task env:check`

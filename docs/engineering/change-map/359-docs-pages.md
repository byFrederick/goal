## When you change

A CLI command, an API endpoint, a field of the factory definition or a screen: anything a person sees or calls

## Also change

Its docs page in `apps/docs/src/content/docs/`: a generated reference page (`reference/cli.md` from `internal/cli`'s usage text, `reference/definition.md` from `contracts/definitions/factory.schema.json`, `reference/environment.md` and `reference/glossary.md` from `docs/reference/`) by running `task docs:ref:gen`, never by hand; the API reference, which the site renders from `contracts/api/openapi.yaml` at build time; or the guide or concept page that describes it

## Run

`task docs:ref:gen`, then `task docs:check` (fails, naming the page, when a reference page is stale) and `task docs:site:check`

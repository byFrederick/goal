## When you change

The product docs site (`apps/docs/`): its pages, its theme (`src/styles/goal.css`), its Astro config, or the API contract it renders (`contracts/api/openapi.yaml`)

## Also change

The docs page a visible change touches (in `apps/docs/src/content/docs/`); the sidebar in `astro.config.mjs` when a section gains or loses a page; `docs/design/MASTER.md`'s tokens in `goal.css` when the design system changes. The API reference and `/docs/openapi.yaml` come from the contract, so a contract change needs no edit here

## Run

`task docs:site:check` (the type check, Biome and the build)

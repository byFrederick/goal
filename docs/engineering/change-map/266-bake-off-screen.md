## When you change

The bake-off screen: the routes `apps/web/src/routes/o.$org.f.$factory.jonah.bake-offs.*.tsx`, the bake-off page and its parts in `apps/web/src/features/jonah/bake-off-*.tsx`, or the bake-off wrappers and query keys in `apps/web/src/api/client.ts` and `queries.ts`

## Also change

The screen's picture `docs/design/screens/jonah/bake-off.{jpg,html}` when the design changes (the owner's canvas first), `apps/web/mock/bake-offs.ts` and `mock/bake-offs.test.ts` when the screen's figures change, `apps/web/e2e/bake-off.spec.ts`, the bake-off paragraph of `docs/engineering/frontend.md`, and the Jonah tab bar in `features/jonah/jonah-header.tsx` when a tab changes

## Run

`pnpm --dir apps/web run typecheck`, `pnpm --dir apps/web run lint`, `pnpm --dir apps/web test -- src/features/jonah/bake-off-page.test.tsx mock/bake-offs.test.ts src/ui/raw-colors.test.ts src/ui/arbitrary-values.test.ts`, `task docs:check`

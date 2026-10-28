## When you change

The organization's model and tool policy screen (`features/models/`, the route `routes/o.$org._org.models.tsx`, `api/policy.ts`), or the policy's shape in the contract (`OrgPolicyInput` and `OrgPolicyView` in `contracts/api/openapi.yaml`): the screen's sections, its form rules in `form.ts` and how a save reads the ETag ([ADR 0024](../../adr/0024-organization-policy.md))

## Also change

`apps/web/mock/policy.ts` (the acme policy the mock answers), the screen's row in `docs/engineering/frontend.md` ("Models and policies") and `apps/web/e2e/models.spec.ts`; the org menu's link in `apps/web/src/app/org-menu.tsx` and `screens.ts`; the registry's route id after a route moves

## Run

`pnpm --dir apps/web exec vitest run src/features/models src/app/org-menu.test.tsx mock/policy.test.ts`, `pnpm --dir apps/web run typecheck`, `pnpm --dir apps/web run lint`, and `pnpm --dir apps/web run test` (the arbitrary-value and raw-colour ratchets run with it)

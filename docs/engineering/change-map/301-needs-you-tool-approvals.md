## When you change

The Needs you page's tool-approval rows and detail, or its Approve and Deny for an Approval: `features/needs-you/` (`types.ts`, `actions.ts`, `detail.tsx`, `rows.tsx`, `needs-you-page.tsx`), or `decideToolApproval` in `apps/web/src/api/client.ts`

## Also change

The mock's waiting approval and its route: `apps/web/mock/fixtures.ts` (item 151's `tool_approval` step), `apps/web/mock/api.ts` (`inboxOf`, `POST .../tool-approval`) and `apps/web/mock/tool-approval.test.ts`; the e2e spec `apps/web/e2e/needs-you.spec.ts`; the Needs you section of [frontend.md](../frontend.md) ("Needs you")

## Run

`pnpm --dir apps/web run typecheck`, `pnpm --dir apps/web run lint`, `pnpm --dir apps/web exec vitest run src/features/needs-you mock`, and the Playwright spec `needs-you.spec.ts` where a browser is available

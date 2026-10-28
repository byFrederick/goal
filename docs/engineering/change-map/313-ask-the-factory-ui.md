## When you change

The Ask bar (`apps/web/src/features/factory-editor/yaml/ask-bar.tsx`), "Ask Jonah for a fix" (`apps/web/src/features/agents/ask-jonah-button.tsx`), the shared ask (`apps/web/src/features/factory-editor/draft-requests.ts`) or the draft request queries in `apps/web/src/api/queries.ts` (`defKeys.draftRequests`, `draftRequestsQuery`, `draftRequestQuery`)

## Also change

`docs/engineering/frontend.md` ("Asked definition changes (ADR 0021)"), `apps/web/mock/definitions.ts` (the draft request routes and their timers) with `apps/web/mock/definitions.test.ts`, the `e2e/definition.spec.ts` and `e2e/agent.spec.ts` flows, and the Vitest tests beside the changed components. A draft request's stream gap is closed on the backend first: a `draft_request` stream event changes the bar's queries, not the screen's keys

## Run

`pnpm --dir apps/web run typecheck`, `pnpm --dir apps/web run lint`, `pnpm --dir apps/web test -- src/features/factory-editor src/features/agents src/shared/invalidation.test.ts mock/definitions.test.ts`, and `pnpm --dir apps/web run baseline:arbitrary` after a change of arbitrary values

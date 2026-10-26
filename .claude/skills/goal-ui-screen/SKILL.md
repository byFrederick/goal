---
name: goal-ui-screen
description: Build or change a screen or component of the web UI from Goal's design system, with its loading, empty, error and filled states, its phone layout, dark mode and tests. Use for any change under apps/web/src that a person sees.
---

# Build a screen

The UI follows `docs/design/MASTER.md` exactly. How the app is laid out (routes, the API client, query keys, the mock, themes) is in [frontend.md](../../../docs/engineering/frontend.md). What moves with a UI change is the UI row of the [change map](../../../docs/engineering/change-map.md). Read both before you start, and [testing.md](../../../docs/engineering/testing.md) before the tests.

1. **Read the spec and the design.** Find the screen in its spec in `docs/specs/`. Then read the sections of `docs/design/MASTER.md` that apply: Global Rules, Component Specs, Dark theme and Anti-Patterns.
2. **Reuse the components.** The design system's components are in `apps/web/src/ui` (button, card, field, dialog, sheet, list-row, empty-state, page-header and others). Compose a screen from them, inside its feature folder in `apps/web/src/features/<area>`. Add a component to `apps/web/src/ui` only when two screens need it.
3. **Get the data through the client.** Requests live in `apps/web/src/api` and query keys in `apps/web/src/api/queries.ts`. No screen calls `fetch` or builds a query key by hand. A new endpoint comes first, through [goal-api-change](../goal-api-change/SKILL.md).
4. **Build all four states.** Every view that loads data has:
   - loading, with no placeholder shapes that shift when the data arrives;
   - empty, with `apps/web/src/ui/empty-state.tsx`, saying what to do next;
   - error, saying what failed and offering a retry where one helps;
   - filled.
   Controls the caller's permissions do not allow are off or hidden, as frontend.md describes.
5. **Make it work on a phone.** At 375 px nothing scrolls sideways, touch targets are at least 44 px, and long text is clamped or truncated with the full text in a `title`. Check 768 and 1440 px too.
6. **Make it work in dark mode.** Use the color tokens only. `apps/web/src/ui/raw-colors.test.ts` fails on a hex, `rgb()`, `rgba()` or `hsl()` value.
7. **Make it accessible.** Use labels, visible focus and keyboard paths. Icons beside words are `aria-hidden`. Respect `prefers-reduced-motion`.
8. **Feed the mock.** Give `apps/web/mock` the shapes the screen needs, so `GOAL_API=mock task web:dev` shows every state.
9. **Write the tests.** Write Vitest and Testing Library tests beside the component: each state, what each control does, and the permission cases. A critical flow also gets a Playwright spec in `apps/web/e2e`.
10. **Run the checks.** Run `task web:check`. Run `task web:e2e` where a browser is installed; where none is, say in the pull request that the Playwright suite was not run.
11. **Run MASTER.md's Pre-Delivery Checklist**, and fix what it finds.
12. **Docs as built.** Update `docs/engineering/frontend.md` and the spec for the screen.
13. **Deliver** with [goal-deliver](../goal-deliver/SKILL.md).

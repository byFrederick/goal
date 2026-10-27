---
paths:
  - "apps/web/**"
  - "docs/design/**"
---

# Web

The UI is one product, so its look comes from one design system: [MASTER.md](../../docs/design/MASTER.md). How the app is built and why: [frontend.md](../../docs/engineering/frontend.md).

## Design

- Follow `docs/design/MASTER.md` exactly: its tokens, Archivo, radii, spacing, vocabulary and components. Each screen's reference is `docs/design/screens/<flow>/<screen>.jpg` and `.html` ([index](../../docs/design/screens/README.md)): match it, and list any difference in the pull request ([ADR 0016](../../docs/adr/0016-ui-revamp.md)). Drift between screens is what users notice first.
- Use token names only, never a hex, `rgb()` or `hsl()` value, and no gradients. A theme switch then needs no edits, and `apps/web/src/ui/raw-colors.test.ts` enforces it.
- Support light and dark from the same tokens. People choose the theme, so both must read well.
- Lay out for a phone first and check 375, 768 and 1440 px by reasoning about the CSS. The app fills the viewport with no framed shell.
- Reuse the components already in `apps/web/src` before writing a new one. Two copies of a control drift apart.
- Run the ui-ux-pro-max pre-delivery checklist on every visual change. It catches what a reading of the code misses.

## The four states

- Every screen renders loading, empty, error and ready. A missing state shows as a blank page or a crash.
- Loading shows the app background with an sr-only "Loading" and `aria-busy`, never a gray placeholder shape. Placeholders flash and then shift when the page arrives.

## Data and routing

- Fetch server data only through `apps/web/src/api` and the generated types. The contract is the one source of shapes, so a hand-written type drifts from it.
- Keep server state in TanStack Query under the keys in `queries.ts`. One key scheme lets an event refresh every screen that shows the data.
- Keep filters, tabs and selections in the URL with TanStack Router. A linked view then shows the same thing for whoever opens it.
- Let stream events invalidate queries instead of polling. Polling wastes requests and lags behind the event.
- Load first-use views with `lazyRetrying` (`apps/web/src/shared/lazy.ts`), not bare `React.lazy`. A failed chunk otherwise stays broken until a reload.

## Accessibility

- Use semantic elements, a visible label for every control and a keyboard path to each action. Screen-reader and keyboard users depend on all three.
- Keep text contrast at WCAG 2.2 AA in both themes. The dark palette has its own values to check.
- Give every drag a menu alternative. Dragging alone excludes keyboard and switch users.
- Keep focus visible and in order after navigation and when a dialog opens or closes.

## Copy

- Write sentence case in plain words. The people reading are operators, not engineers.
- Name agents by their role or their name in the factory, never by a number. A number means nothing to the reader.

## Tests and checks

- Test what a person sees and does with Vitest and Testing Library. Tests of markup or internals break on every refactor.
- Write or update the Playwright specs when a flow changes. They cannot run in the box, so the pull request says so.
- Run `pnpm --dir apps/web run typecheck`, `lint` and `test` after the last edit.

## Skills

Vendored skills for this area: ui-ux-pro-max (strictly), frontend-design, web-design-guidelines, impeccable, shadcn, tailwind-design-system, vercel-react-best-practices, vercel-composition-patterns, accessibility, typescript-advanced-types, tanstack-query-best-practices, tanstack-router-best-practices, vite, vitest, playwright-best-practices, webapp-testing.

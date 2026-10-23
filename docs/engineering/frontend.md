# Frontend and monorepo

The UI is a static React single-page app. `goald` embeds it for the laptop; the same build deploys to Vercel or S3 + CloudFront. One OpenAPI contract connects it to the Go backend, and generated types keep both sides in step. Decision: [ADR 0004](../adr/0004-frontend-and-monorepo.md).

## Monorepo layout

One repository, two toolchains: a Go module at the root and a pnpm workspace for TypeScript, orchestrated by Turborepo. `make check` runs both.

```
factory/
  go.mod                       Go module (goald, goal CLI, engine, adapters)
  cmd/  internal/  migrations/  queries/
  api/
    openapi.yaml               THE contract between goald and every TypeScript client
    events.schema.json         SSE event payloads
  schema/                      factory.schema.json, evals.schema.json
  package.json                 private root: pnpm + turbo only, no app code
  pnpm-workspace.yaml          apps/* and packages/*
  turbo.json                   lint, typecheck, test, build pipelines with caching
  web/                         apps: the UI (React 19 + Vite)
  worker/                      apps: the agent container (Node 22 + Claude Agent SDK)
  packages/
    contracts/                 generated: TS types from api/openapi.yaml and schema/*.json
    api-client/                typed fetch client (openapi-fetch) + SSE subscription helper
    ui/                        design system: tokens from design-system/goal/MASTER.md + shadcn/Radix components
    config/                    shared tsconfig, Biome config, Vitest preset
```

Rules:

- `web` and `worker` depend on `packages/*`, never on each other.
- `packages/contracts` is generated and committed; CI fails if regenerating it produces a diff, so the Go and TypeScript sides cannot drift.
- `packages/ui` has no knowledge of Goal's API; it only renders. Domain components live in `web`.
- Exact versions in `package.json`, one lockfile (`pnpm-lock.yaml`), `pnpm` pinned via `packageManager` and `.mise.toml`.

## The contract

`api/openapi.yaml` (OpenAPI 3.1) is written first, by hand, and reviewed like code.

- Go: `oapi-codegen` generates a strict server interface that `internal/adapters/http` implements. Handlers call the engine through ports, as everywhere else.
- TypeScript: `openapi-typescript` generates types into `packages/contracts`; `openapi-fetch` gives a typed client with no hand-written fetch code.
- Realtime: `GET /v1/events` streams Server-Sent Events whose payloads are defined in `api/events.schema.json`. The client turns each event into a TanStack Query cache invalidation, so screens update without polling.
- Factory and eval files keep their JSON Schemas in `schema/`; the same schemas drive Go validation, TS types and the YAML editor's autocompletion.

## Web app architecture

| Concern | Choice | Why |
| --- | --- | --- |
| Build | Vite, TypeScript strict | Fast, static output, no server to run |
| Routing | TanStack Router, file-based | Type-safe routes and search params; every filter, tab and selection lives in the URL so any view can be linked |
| Server state | TanStack Query | Caching, retries, optimistic updates; SSE events invalidate queries |
| Local state | React state; no global store | Server state and URL state cover nearly everything; add a store only when a measured need appears |
| Styling | Tailwind v4 with `@theme` tokens generated from MASTER.md | One source of truth for palette, type, radii; no hex values in components |
| Components | shadcn/ui on Radix, in `packages/ui` | Accessible primitives we own and can restyle |
| Board drag and drop | dnd-kit with keyboard sensors, plus a Move menu on every card | WCAG 2.2 dragging alternative |
| Factory editor | Monaco with monaco-yaml bound to `schema/factory.schema.json`; xyflow for the line preview | Autocompletion and inline errors while editing YAML |
| Charts | Small SVG components in `packages/ui` (hatched bars, bridge, sparkline) | Exactly the design system's marks; no heavy chart library |
| Long lists | TanStack Virtual for traces and big boards | Keeps the main thread free |

Folder structure inside `web/src`:

```
app/          router, providers, error boundary, layout (sidebar on desktop, bottom tabs under 768 px)
features/
  floor/  board/  items/  agents/  knowledge/  jonah/  definition/
    routes/      route files for this feature
    components/  feature-only components
    queries.ts   TanStack Query hooks over packages/api-client
shared/       formatting (Intl), SSE wiring, auth session
```

Features never import each other; shared code goes to `shared/` or `packages/ui`. `dependency-cruiser` enforces this in CI the same way depguard guards the Go engine.

## Responsive and mobile

One app, not two: the layout switches to a bottom tab bar (Floor, Board, Items, Jonah, More) under 768 px, cards stack, the board becomes a station picker plus a list, and every action has a 44 px target. Later, the app becomes an installable PWA so approvals can arrive as push notifications.

## Hosting

| Target | How |
| --- | --- |
| Laptop | `web/dist` is embedded into `goald` with `go:embed`; one binary serves UI and API |
| AWS | `goald` serves it, or S3 + CloudFront with `/v1/*` routed to the ALB |
| Vercel | Static deploy of `web/`; `VITE_API_URL` points at goald on AWS; preview deployments per pull request |

The UI holds no secrets. In the cloud it signs in through goald (OIDC); on a laptop goald runs in single-user mode.

## Quality gates

- Biome for lint and format; `tsc --noEmit` for types; dependency-cruiser for boundaries.
- Vitest + Testing Library for components with real behavior (board moves, approval flow, editor validation), with MSW handlers generated from the OpenAPI contract. No snapshot tests of markup.
- Playwright for the critical flows at 375, 768 and 1440 px, with axe accessibility checks.
- Budgets: initial JS under 250 KB gzipped; routes are code-split.
- Every UI change follows ui-ux-pro-max's pre-delivery checklist and attaches screenshots at the three widths.

## Skills

ui-ux-pro-max (strictly), frontend-design, web-design-guidelines, impeccable, shadcn, tailwind-design-system, vercel-react-best-practices, vercel-composition-patterns, accessibility, typescript-advanced-types, tanstack-query-best-practices, tanstack-router-best-practices, vite, vitest, playwright-best-practices, webapp-testing, openapi-spec-generation, turborepo, pnpm, monorepo-management, deploy-to-vercel.

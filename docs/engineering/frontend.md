# Frontend and monorepo

The UI is a static React single-page app. `goald` embeds it for the laptop; the same build deploys to Vercel or S3 + CloudFront. One OpenAPI contract connects it to the Go backend, and generated types keep both sides in step. Decision: [ADR 0004](../adr/0004-frontend-and-monorepo.md).

## Monorepo layout

One repository, two toolchains: a Go module at the root and a pnpm workspace for TypeScript, orchestrated by Turborepo. `task check` runs both.

```
goal/
  go.mod                       Go module (goald, goal CLI, engine, adapters)
  cmd/  internal/  migrations/  queries/
  api/
    openapi.yaml               THE contract between goald and every TypeScript client (incl. the SSE StreamEvent)
  schema/                      factory.schema.json, evals.schema.json
  package.json                 private root: pnpm + turbo only, no app code
  pnpm-workspace.yaml          apps/* and packages/*
  turbo.json                   lint, typecheck, test, build pipelines with caching
  web/                         apps: the UI (React 19 + Vite)
  worker/                      apps: the agent container (Node 22 + Claude Agent SDK)
  packages/
    contracts/                 generated: TS types from api/openapi.yaml and schema/*.json
    api-client/                typed fetch client (openapi-fetch), errors as Problem, SSE subscription
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
- Realtime: `GET /v1/orgs/{org}/stream?factory=` streams one organization's Server-Sent Events: `event: item` (data: the `StreamEvent` schema in `api/openapi.yaml`, `id:` the event id) and `event: resync` (data `{}`: events may have been missed). There is no replay, so a client re-reads what it shows after a reconnect or a resync. The client turns each event into a TanStack Query cache invalidation, so screens update without polling.
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

Folder structure inside `web/src` (phase 2):

```
main.tsx      QueryClient, router, providers
routes/       file-based route files (TanStack Router plugin; routeTree.gen.ts is generated and committed)
                /                         → the floor of the factory used last (or the first)
                /f/$factory               → Floor        (layout route: shell + live stream)
                /f/$factory/board         → Board        (?station= picks the station on phones)
                /f/$factory/items         → Items list   (?state= filters)
                /f/$factory/items/$ref    → Item         ($ref: the number, goal#12 or the id)
api/          the only HTTP code: types.ts (generated contract shapes), client.ts (requests over @goal/api-client), queries.ts (keys, queryOptions)
app/          shell (sidebar from 768 px, icon rail 768–1023 px, bottom tabs below 768 px), loading/error/not-found screens
features/
  floor/  board/  items/        one folder per screen: page, cards, feature-only logic (items/timeline.ts)
shared/       code more than one feature uses: decisions (approve, send back, merge, retry), new item,
              live stream provider, factory helpers, formatting (Intl), state labels
ui/           design-system primitives on Radix: button, dialog, card, chip, hatch (SVG pattern marks), logo
```

Route files stay thin (loader + component); screens live in `features/`. Features never import each other; shared code goes to `shared/` or `ui/`. Boundaries are kept in review for now; `dependency-cruiser` arrives with `packages/`, the same way depguard guards the Go engine. `ui/` moves to `packages/ui` when a second app needs it.

### The API module and `@goal/api-client`

`packages/api-client` is the typed layer over the contract: `createGoalClient` (openapi-fetch over the types `openapi-typescript` generates into `src/schema.ts`, with `--default-non-nullable=false` so request fields with a default stay optional), `unwrap` (data on success, else it throws `ApiError`, which carries the RFC 9457 `Problem`, rebuilt from the status line when a proxy answers with something else), `subscribe(baseUrl, scope, handlers)` for the stream, and the schema types. Organization-owned routes are written relative to the organization with `orgPath("/factories/{factory}/board")`; `routes.ts` holds the one prefix (`orgRoutes`, `/v1/orgs/{org}`), and every such call passes `org` (slug or id) as a path parameter, as `StreamScope` does.

In `web`, `src/api/types.ts` re-exports the generated types under the names the screens use, and `client.ts` holds every request (`listOrgs`, `getOrg`, `createOrg`; then, each taking `org` first, `getAttention`, `getFactoryOverview`, `getInbox`, `listFactories`, `getBoard`, `getMetrics`, `listItems`, `getItem`, `listItemEvents`, `submitItem`, `approveItem`, `rejectItem`, `mergeItem`, `retryItem`, `subscribeToFactory`) over `createGoalClient`. Nothing else in `web/src` touches `fetch` or `EventSource`; `web/mock` is typed with the same generated shapes. Query keys (`queries.ts`) are rooted at the organization's id: `['orgs']`, `['org', org, 'detail']`, `['org', org, 'attention']`, `['org', org, 'overview', window]`, `['org', org, 'inbox', factory ?? 'all']`, `['org', org, 'factories']`, `['org', org, 'factory', f, 'board' | 'metrics' | 'items', …]`, `['org', org, 'item', ref]` and `['org', org, 'item', ref, 'events']`.

Until phase 2.2 C's `/o/:org` routes name the organization in the URL, the UI works in the first organization goald lists (`shared/org.ts`, the interim first-org shim: `interimOrg` in the `/f/$factory` and `/` loaders, `useOrg()` in components); C3 replaces it.

### Live updates

`shared/stream.tsx` opens one `EventSource` per open factory, on its organization's stream. Each `item` event invalidates the factory's board, metrics and lists and that item's queries within the organization (`shared/invalidation.ts`), batched over 300 ms so a burst of tool calls costs one refetch; the item's history refetch asks only for events after the last one cached. `resync`, and every reconnect after the first open, invalidate everything of the organization (`['org', org]`).

### Decisions

Approve and retry act at once (they only move work forward); send back asks for a reason in a dialog, and merge asks for confirmation. Merge replaces approve at a human station whose `action` is `merge`; nothing is keyed by station names. Decisions carry no actor: goald records the request's (the local user until phase 4 adds sign-in).

### Running it

```bash
task web:dev                     # Vite on :5173, /v1 proxied to goald on 127.0.0.1:8080 (GOAL_API_URL to change), Host kept for goald's cross-origin check
GOAL_API=mock task web:dev       # the same against the in-memory mock API (web/mock), no goald needed
GOAL_API=mock GOAL_MOCK_LIVE=1 task web:dev   # the mock's agents keep calling tools, to watch the stream
task web:check                   # tsc, Biome, Vitest
task web:e2e                     # Playwright (Chromium) at 375, 768, 1440 px with axe, on vite preview + the mock
task build                       # web:build, then go build -tags webui: goald serves the UI at /
```

`web/mock` is a stand-in for goald's API with the contract's shapes and realistic fixtures: two organizations, `acme` (a goal factory building itself, a small SRE factory, an empty support factory whose line is stopped by an andon pull) and `example-labs` (one empty factory), with weekly budgets for the Factories home. It serves the shell's attention, overview and inbox from the same state, keeps state per server, broadcasts stream events like goald, and `POST /__mock/reset` restores the seed (the e2e tests reset before each test).

### Embedding in goald

`web/webui.go` (Go package `web`) serves the build: files as they are (hashed `assets/` with a one-year immutable cache, the rest `no-cache`), `index.html` for every other path so client routes work on reload, and 404 for unknown `/v1`, `/mcp` and asset paths. `go build -tags webui` embeds `web/dist`; without the tag goald embeds `web/placeholder`, a page that says how to build the UI, so `go build ./...` works on a fresh clone without Node. `cmd/goald` mounts it with `mux.Handle("/", web.Handler())`.

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

- Biome for lint and format; `tsc --noEmit` for types; dependency-cruiser for boundaries (with `packages/`).
- Vitest for logic with real behavior (the trace built from events, which decisions an item allows); Testing Library for components when they carry such logic. No snapshot tests of markup.
- Playwright for the critical flows at 375, 768 and 1440 px, with axe accessibility checks.
- Budgets: initial JS under 250 KB gzipped; routes are code-split.
- Every UI change follows ui-ux-pro-max's pre-delivery checklist and attaches screenshots at the three widths.

## Skills

ui-ux-pro-max (strictly), frontend-design, web-design-guidelines, impeccable, shadcn, tailwind-design-system, vercel-react-best-practices, vercel-composition-patterns, accessibility, typescript-advanced-types, tanstack-query-best-practices, tanstack-router-best-practices, vite, vitest, playwright-best-practices, webapp-testing, openapi-spec-generation, turborepo, pnpm, monorepo-management, deploy-to-vercel.

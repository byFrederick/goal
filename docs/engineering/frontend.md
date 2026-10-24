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

Folder structure inside `web/src` (phase 2.2):

```
main.tsx      QueryClient, router, providers
routes/       file-based route files (TanStack Router plugin; routeTree.gen.ts is generated and committed)
                /                               → the organization used last (localStorage goal.org), else the first
                /f/*                            → old links: redirect to the factory's page under its organization
                /o/$org                         → layout: resolves the org (slug or id), its live stream, the rail
                  (_org)                        → pathless layout of the organization's own pages: org sidebar
                    /o/$org                     → Factories home
                    /o/$org/settings …          → Organization settings (C7), catalog and credentials (phase 3)
                  /o/$org/f/$factory            → layout: resolves the factory (name or id), factory sidebar,
                                                  phone crumb and tab bar; its index is the Floor
                                                  (?from&to&type: the range of UTC days and work type)
                  /o/$org/f/$factory/board      → Board        (?station= picks the station on phones)
                  /o/$org/f/$factory/items      → Items list   (?state= filters)
                  /o/$org/f/$factory/items/$ref → Item         ($ref: the number, goal#12 or the id)
                  /o/$org/f/$factory/definition → D's editor: frame + draft session; its index → /definition/line
                  /o/$org/f/$factory/definition/$section → basics|line|agents|connections|review|versions (?mode=yaml, ?station=, ?agent=, …)
                  /o/$org/new/$step             → D's New factory wizard (rail only, no sidebar; ?factory= from step 2)
api/          the only HTTP code: types.ts (generated contract shapes), client.ts (requests over @goal/api-client), definitions.ts and builder.ts (B's and D's routes), queries.ts (keys, queryOptions)
app/          the shell: AppShell and ShellFrame, Rail, PhoneTopRow, PhoneCrumb, TabBar, FactorySidebar,
              OrgSidebar, OrgMark (the organization menu's trigger: OrgMenu on the rail, OrgSheet on
              phones), CreateOrgDialog, AccountMenu, screens.ts (the screen registry), attention, route focus and titles,
              loading/error/not-found screens
features/
  factories/  floor/  board/  items/  org-settings/   one folder per screen: page, cards, feature-only logic (items/timeline.ts, org-settings/form.ts)
  factory-editor/   the wizard and Definition (D); draft/: the draft session (session.ts, use-draft.ts), merge.ts, ops.ts, pointer.ts;
                    frame/: the editor frame, status line, tiles, dialogs and unsaved guard; section-views.tsx: each section's view (basics/, …)
shared/       code more than one feature uses: decisions (approve, send back, merge, retry), new item,
              the andon cord (andon.tsx: the line status and the pull and clear dialogs, from Board.line),
              definition/ (typed views of B's FactoryModel, where an agent works, slots at full WIP, pointer ↔ line),
              the organization's live stream, org and factory helpers, marks, formatting (Intl), state labels, the address rule (org-rules.ts)
ui/           design-system primitives on Radix: button, dialog, confirm-dialog (typed confirmation), field and input, sheet, menu, tooltip,
              card (SectionCard, StatGrid), chip, count, mark, hatch (Meter, HatchBars), target-bar, bars (weekly bars with a target line and a hidden table), bridge (the lead-time waterfall, `bridgeGeometry`, with a hidden table), date-range, page-header, list-row, empty-state, logo
```

Route files stay thin (loader + component); screens live in `features/`. Features never import each other; shared code goes to `shared/` or `ui/`. Boundaries are kept in review for now; `dependency-cruiser` arrives with `packages/`, the same way depguard guards the Go engine. `ui/` moves to `packages/ui` when a second app needs it.

### The API module and `@goal/api-client`

`packages/api-client` is the typed layer over the contract: `createGoalClient` (openapi-fetch over the types `openapi-typescript` generates into `src/schema.ts`, with `--default-non-nullable=false` so request fields with a default stay optional), `unwrap` (data on success, else it throws `ApiError`, which carries the RFC 9457 `Problem` with its `code` (`slug-taken`, `confirmation-mismatch`, …) for screens to branch on, rebuilt from the status line when a proxy answers with something else), `subscribe(baseUrl, scope, handlers)` for the stream, and the schema types. Organization-owned routes are written relative to the organization with `orgPath("/factories/{factory}/board")`; `routes.ts` holds the one prefix (`orgRoutes`, `/v1/orgs/{org}`), and every such call passes `org` (slug or id) as a path parameter, as `StreamScope` does.

In `web`, `src/api/types.ts` re-exports the generated types under the names the screens use, and `client.ts` holds every request (`listOrgs`, `getOrg`, `createOrg`; then, each taking `org` first, `getAttention`, `getFactoryOverview`, `getInbox`, `getOrgSettings`, `patchOrgSettings`, `deleteOrg`, `listAudit`, `listFactories`, `getBoard`, `getMetrics` (a `FlowRange` of from, to and type, with the range before), `getWeekly`, `getLeadTime`, `getConstraint`, `pullAndon`, `clearAndon`, `listItems`, `getItem`, `listItemEvents`, `listItemRuns`, `noteItem`, `submitItem`, `approveItem`, `rejectItem`, `mergeItem`, `retryItem`, `moveItem`, `pauseItem`, `subscribeToOrg`) over `createGoalClient`, `definitions.ts` the definitions routes over the same client (`getFactory`, `createFactory`, drafts, sections, files, validation, publish, versions, compare, restore, templates; B §10), and `builder.ts` the builder routes (`getBuilderMeta`, `searchSkills`, `getSkillFiles`; D §8); factories are named by id. Nothing else in `web/src` touches `fetch` or `EventSource`; `web/mock` is typed with the same generated shapes. Query keys (`queries.ts`) are rooted at the organization's id: `['orgs']`, `['org', org, 'detail']`, `['org', org, 'attention']`, `['org', org, 'overview', window]`, `['org', org, 'inbox', factory ?? 'all']`, `['org', org, 'settings']`, `['org', org, 'audit', filter]`, `['org', org, 'factories']`, `['org', org, 'factory', f, 'board' | 'metrics' | 'items', …]` (f the factory's id; `metrics` takes the `FlowRange`), `['org', org, 'factory', f, 'weekly', to ?? 'today', type ?? 'all']`, `['org', org, 'factory', f, 'lead-time', range]`, `['org', org, 'factory', f, 'constraint', from ?? 'week', to ?? 'today']`, `['org', org, 'item', ref]`, `['org', org, 'item', ref, 'events']` and `['org', org, 'item', ref, 'runs']` (refreshed with the item); the definitions keys are `defKeys`, beside the work keys under the factory's prefix, and the builder keys `builderKeys`: `['org', org, 'builder', 'meta']`, `['org', org, 'builder', 'skills', q]`, `['org', org, 'builder', 'skill', key]`.

The URL names the organization (`/o/$org`, its slug or id) and the factory (`/f/$factory`, its name or id). The `/o/$org` and `/o/$org/f/$factory` layouts resolve both in `beforeLoad`, so every screen reads them from the route context (`useOrg()`, `useFactory()` in `shared/`): queries are keyed and requests made by their ids, and links use the slug and the name (`useFactoryParams()`).

### Live updates

`shared/stream.tsx` (`OrgStream`, mounted by the `/o/$org` layout) opens one `EventSource` per open organization, on its stream. Each `item` event invalidates what it can change (`invalidationsFor` in `shared/invalidation.ts`): the organization's attention counts and inboxes, the factory's work keys named one by one (board, metrics, items; only the board for tool calls; never the factory's bare prefix) and that item by ref and by id, batched over 300 ms so a burst of tool calls costs one refetch; the factory overviews at most every 5 s. The item's history refetch asks only for events after the last one cached. `resync`, and every reconnect after the first open, invalidate everything of the organization (`['org', org]`). A `factory` event (an andon pulled or cleared; the client's typed `onFactory`) invalidates the factory's work keys, the attention counts, the inboxes and the overviews (`factoryInvalidations`). Other event names are handled through the `streamHandlers` registry; names nobody registered are ignored. Switching organization closes the old stream and opens the new one. The shell says "Reconnecting" only while the stream is reconnecting; the board's line status ("Line running", "Line stopped: <reason>") adds "connecting…" or "reconnecting…" while the stream is not live.

### Decisions

Approve and retry act at once (they only move work forward); send back asks for a reason in a dialog, and merge asks for confirmation. Merge replaces approve at a human station whose `action` is `merge`; nothing is keyed by station names. Decisions carry no actor: goald records the request's (the local user until phase 4 adds sign-in).

### Moving work

Every board card has a Move button (32 px in the columns, the 44 px arrow on phones) and, when focused, the `M` shortcut (`aria-keyshortcuts`); both open the Move menu (`shared/move-menu.tsx`), a dialog listing the line's stations but the item's own and done stations (`shared/move-choices.ts`): upstream ones say they count as rework and require a reason, and an item in progress says its run stops. In the columns, dnd-kit drags a card by pointer (6 px before it starts, so clicks still open the item) or by keyboard (Space picks the focused card up, the left and right arrows carry it a column at a time, Space drops, Escape cancels; screen-reader announcements name the station); a drop on another column opens the same menu with that station chosen, so every move is confirmed. The board moves the card optimistically and puts it back with the problem's detail if goald refuses. The Item page adds Pause (a dialog with an optional reason), Resume (the retry of a paused item: its latest block is a `paused` event), `Send back to <rework_target>` at any state goald allows, and Move.

### Organization settings

One form (`/o/$org/settings`): "Save changes" is enabled while it differs from what goald last answered and sends `PATCH …/settings` with only the changed fields (`changedFields`; slots always carry the organization's cap, which the contract requires). Fields are checked against goald's own rules first; a `slug-taken` answer is the Address field's "Taken", other refusals show the Problem's detail. After a save the organization list is refetched (rail, sidebar, route context); a new address replaces the URL with the new slug, and links with the old one stop working (review decision 2). Leaving with unsaved changes asks first (`useBlocker`). Delete takes the typed name, then `/` opens the next organization or says there is none. Model access and Vault say "Arrives with credentials" until phase 3.

### Running it

```bash
task web:dev                     # Vite on :5173, /v1 proxied to goald on 127.0.0.1:8080 (GOAL_API_URL to change), Host kept for goald's cross-origin check
GOAL_API=mock task web:dev       # the same against the in-memory mock API (web/mock), no goald needed
GOAL_API=mock GOAL_MOCK_LIVE=1 task web:dev   # the mock's agents keep calling tools, to watch the stream
task web:check                   # tsc, Biome, Vitest
task web:e2e                     # Playwright (Chromium) at 375, 768, 1440 px with axe, on vite preview + the mock
task build                       # web:build, then go build -tags webui: goald serves the UI at /
```

`web/mock` is a stand-in for goald's API with the contract's shapes and realistic fixtures: two organizations, `acme` (a goal factory building itself, a small SRE factory, an empty support factory whose line is stopped by an andon pull, with an earlier cleared one) and `example-labs` (one empty factory), with weekly budgets for the Factories home. It serves the shell's attention, overview and inbox from the same state, keeps state per server, broadcasts stream events like goald, and `POST /__mock/reset` restores the seed (the e2e tests reset before each test). The definitions and builder routes are `mock/definitions.ts`: every factory has versions and goal an open draft (seeded in `mock/definition-seed.ts` with a template and skills catalog), Visual section saves are applied to `factory.yaml` by `mock/model.ts` on the `yaml` package's Document (comments, key order and flow style kept, items matched by identity, as `internal/factorymodel` does), writes are conditional (412 `draft-conflict` with the current value, 428 without a precondition), and every draft change is a `definition` event. The andon routes (`GET`/`POST …/andon`, `POST …/andon/clear`) keep each factory's pulls, refuse as goald does (`andon-pulled`, `andon-not-pulled`, reason and note limits), write the audit entries and send `event: factory`; goal's review gate has a seeded tally. `POST /__mock/fail {path, method?, status?, code?, times?, delay_ms?, problems?}` makes the next matching requests (path contains `path`; `times` 1 by default, 0 for all until a reset) wait and answer that status the way goald would (a 412 on a section save carries the section's value and etag, a 409 `definition-invalid` its problems); `DELETE /__mock/fail` clears them.

### Embedding in goald

`web/webui.go` (Go package `web`) serves the build: files as they are (hashed `assets/` with a one-year immutable cache, the rest `no-cache`), `index.html` for every other path so client routes work on reload, and 404 for unknown `/v1`, `/mcp` and asset paths. `go build -tags webui` embeds `web/dist`; without the tag goald embeds `web/placeholder`, a page that says how to build the UI, so `go build ./...` works on a fresh clone without Node. `cmd/goald` mounts it with `mux.Handle("/", web.Handler())`.

## Responsive and mobile

One app, not two, filling the viewport with no outer frame. From 768 px: a 72 px rail (organization mark, the organization's factory squares, New factory, then Connector catalog, Credentials and the account) and a 232 px sidebar (a factory's Work / Build / Learn, or the organization's own), both pinned to the left edge at full height, then `main`. Below 768 px: a top row with the organization mark and the factory squares, the crumb "Acme / sre", `main`, and on factory pages a bottom tab bar (Floor, Board, Items, Build, More). Since the sidebar takes room at every width from 768 px (`main` is 464 px wide at 768), screens lay out by the width of `main` with container queries (`@container` on `main`: `@[640px]:`, `@[720px]:`, `@[960px]:`), not viewport breakpoints; viewport breakpoints only switch the phone and desktop chrome. Cards stack, the board becomes a station picker plus a list, and every action has a 44 px target. Screens of later phases are listed in `app/screens.ts` with `available: false` and show as "Later" until the PR that adds their route flips them. Later, the app becomes an installable PWA so approvals can arrive as push notifications.

## Hosting

| Target | How |
| --- | --- |
| Laptop | `web/dist` is embedded into `goald` with `go:embed`; one binary serves UI and API |
| AWS | `goald` serves it, or S3 + CloudFront with `/v1/*` routed to the ALB |
| Vercel | Static deploy of `web/`, same-origin: rewrites send `/v1/*` and `/api/auth/*` to goald on AWS (phase 3, ADR 0007; no cross-site deployment); preview deployments per pull request run on the mock API and never sign in |

The UI holds no secrets. In the cloud it signs in through goald (OIDC); on a laptop goald runs in single-user mode.

## Quality gates

- Biome for lint and format; `tsc --noEmit` for types; dependency-cruiser for boundaries (with `packages/`).
- Vitest for logic with real behavior (the trace built from events, which decisions an item allows); Testing Library for components when they carry such logic. No snapshot tests of markup.
- Playwright for the critical flows at 375, 768 and 1440 px, with axe accessibility checks.
- Budgets: initial JS under 250 KB gzipped; routes are code-split.
- Every UI change follows ui-ux-pro-max's pre-delivery checklist and attaches screenshots at the three widths.

## Skills

ui-ux-pro-max (strictly), frontend-design, web-design-guidelines, impeccable, shadcn, tailwind-design-system, vercel-react-best-practices, vercel-composition-patterns, accessibility, typescript-advanced-types, tanstack-query-best-practices, tanstack-router-best-practices, vite, vitest, playwright-best-practices, webapp-testing, openapi-spec-generation, turborepo, pnpm, monorepo-management, deploy-to-vercel.

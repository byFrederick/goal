# Frontend and monorepo

The UI is a static React single-page app. `goald` embeds it for the laptop; the same build deploys to Vercel or S3 + CloudFront. One OpenAPI contract connects it to the Go backend, and generated types keep both sides in step. Decision: [ADR 0004](../adr/0004-frontend-and-monorepo.md).

## Monorepo layout

One repository, two toolchains: a Go module at the root and a pnpm workspace for TypeScript. The root `Taskfile.yml` and the area Taskfiles it includes (`apps/web/`, `apps/auth/`, `apps/worker/`, `packages/api-client/`… [ADR 0013](../adr/0013-repository-layout.md)) orchestrate both (Turborepo is not installed; it earns its place once workspace builds need caching), and `task check` runs both.

```
goal/
  go.mod                       Go module (goald, goal CLI, engine, adapters)
  cmd/  internal/  db/          Go code, and db/migrations/, db/queries/, db/sqlc.yaml
  contracts/api/openapi.yaml   THE contract between goald and every TypeScript client (incl. the SSE StreamEvent)
  contracts/definitions/       factory.schema.json, evals.schema.json
  package.json                 private root: pnpm only, no app code
  pnpm-workspace.yaml          the apps (auth, web, worker) and packages/*; the catalog pins better-auth once
  apps/
    auth/                      the identity service (Node 22, Hono + Better Auth; phase 3, ADR 0007)
    web/                       the UI (React 19 + Vite)
    worker/                    the agent container (Node 22 + Claude Agent SDK)
  packages/
    contracts/                 generated: TS types from contracts/api/openapi.yaml and contracts/definitions/*.json
    api-client/                typed fetch client (openapi-fetch), errors as Problem, SSE subscription
    ui/                        design system: tokens from docs/design/MASTER.md + shadcn/Radix components
    config/                    shared Biome config and tsconfig bases (Node, browser, Workers); no Vitest preset
```

Rules:

- `web` and `worker` depend on `packages/*`, never on each other.
- `packages/contracts` is generated and committed; CI fails if regenerating it produces a diff, so the Go and TypeScript sides cannot drift.
- `packages/ui` has no knowledge of Goal's API; it only renders. Domain components live in `web`.
- Exact versions in `package.json`, one lockfile (`pnpm-lock.yaml`), `pnpm` pinned via `packageManager` and `.mise.toml`.

## The contract

`contracts/api/openapi.yaml` (OpenAPI 3.1) is written first, by hand, and reviewed like code.

- Go: `oapi-codegen` generates a strict server interface that `internal/adapters/http` implements. Handlers call the engine through ports, as everywhere else.
- TypeScript: `openapi-typescript` generates types into `packages/contracts`; `openapi-fetch` gives a typed client with no hand-written fetch code.
- Realtime: `GET /v1/orgs/{org}/stream?factory=` streams one organization's Server-Sent Events: `event: item` (data: the `StreamEvent` schema in `contracts/api/openapi.yaml`, `id:` the event id) and `event: resync` (data `{}`: events may have been missed). There is no replay, so a client re-reads what it shows after a reconnect or a resync. The client turns each event into a TanStack Query cache invalidation, so screens update without polling.
- Factory and eval files keep their JSON Schemas in `contracts/definitions/`; the same schemas drive Go validation, TS types and the YAML editor's autocompletion.

## The design revamp

The user approved a revamp of the UI on 2026-10-26. [MASTER.md](../design/MASTER.md) now describes it: Archivo, the cobalt palette with one meaning per colour, one collapsible sidebar in place of the rail and the factory sidebar, and the vocabulary (Needs you, Intake and triggers, Jonah's proposals, judges, evals and bake-offs). Every screen is in [docs/design/screens/](../design/screens/README.md), binding by [ADR 0016](../adr/0016-ui-revamp.md). The app moves to it one work item at a time. Until an item lands, the sections below describe the screens as built.

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
| Factory editor | Monaco with monaco-yaml bound to `contracts/definitions/factory.schema.json`; the line preview is a plain HTML list, and stations reorder with dnd-kit plus ↑ ↓ buttons and Alt+Arrow | Autocompletion and inline errors while editing YAML; a linear line needs no graph canvas, and a list is accessible |
| Charts | Small SVG components in `packages/ui` (hatched bars, bridge, sparkline) | Exactly the design system's marks; no heavy chart library |
| Long lists | TanStack Virtual for traces and big boards | Keeps the main thread free |

Folder structure inside `apps/web/src` (phase 2.2):

```
main.tsx      QueryClient, router, providers
routes/       file-based route files (TanStack Router plugin; routeTree.gen.ts is generated and committed)
                /                               → the organization used last (localStorage goal.org), else the first
                /f/*                            → old links: redirect to the factory's page under its organization
                /sign-in?next=                  → Sign in (email code, Google), no rail; under GOAL_AUTH=none it opens `next` or /
                /sign-in/code?next=             → the emailed code; the address is in the history state, never the URL
                /device                         → Device-Approve: goal login's code, typed (never read from the URL), who asks and from where, Approve (a recent sign-in) or Deny; signed out, "Sign in to approve the Goal CLI" (back here after)
                /invite/$invitation             → an invitation email's link: signed out, "Sign in to see your invitation" (back here after); signed in, the invitation (Join, Decline; another address's, unusable, already a member)
                /o/$org                         → layout: resolves the org (slug or id), its live stream, the rail
                  (_org)                        → pathless layout of the organization's own pages: org sidebar
                    /o/$org                     → Factories home (?archived=true: the Archived filter, its list of archived factories with Restore)
                    /o/$org/members             → Members and roles (spec 3 §10.2): members, invitations, factory roles; read-only for members
                    /o/$org/settings            → Organization settings (C7): general, slots, model connections, secrets
                    /o/$org/connections         → Connections (spec 4 §10; connection:read): ?factory= (a factory's name: what its
                                                  current version names), ?kind=, ?q=
                    /o/$org/connections/new     → New connection (?kind=): kind picker, Define a kind…, the kind's fields
                    /o/$org/connections/$connection → One connection (?created after a create): fields, Test, Replace secret,
                                                  Delete, what a run gets, used by, recent use (audit:read)
                    /o/$org/knowledge           → Knowledge (spec 4 §10; knowledge:read): ?factory= (a factory's name: the sources its
                                                  current version lists), ?source= (whose documents show beside the list), ?q= and
                                                  ?as= (Try a search, as a factory)
                    /o/$org/knowledge/new       → New source (?kind=upload|synced|live): files dropped here go up once it exists;
                                                  a synced source's connection and what of it to read; a live source's connection
                    /o/$org/knowledge/$source   → One source (?created after a create, ?document= opens a document's indexed text):
                                                  Sync now, the drop zone (uploads), its documents a page at a time, settings,
                                                  used by, Delete (refused while a factory lists it)
                    /o/$org/api-keys            → API keys (spec 3 §10.2; in the sidebar and the organization menu with api_key:read
                                                  or personal_token:read): the organization's keys, the caller's personal
                                                  tokens (#personal-tokens, linked from the account menu) and, for admins,
                                                  members' tokens
                  /o/$org/f/$factory            → layout: resolves the factory (name or id), factory sidebar,
                                                  phone crumb and tab bar; its index is the Floor
                                                  (?from&to&type: the range of UTC days and work type;
                                                  ?touches=kind|reason|version groups the Touches panel)
                  /o/$org/f/$factory/board      → Board        (?station= picks the station on phones)
                  /o/$org/f/$factory/items      → Items list   (?state= filters; ?show=flat lists every item flat; the Inbox button in its header opens the Inbox drawer)
                  /o/$org/f/$factory/items/$ref → Item, or the Assembly page for state assembly ($ref: the number, goal#12 or the id)
                  /o/$org/f/$factory/definition → D's editor: frame + draft session; its index → /definition/line
                  /o/$org/f/$factory/definition/$section → basics|line|agents|connections|review|versions (?mode=yaml, ?station=, ?agent=, …)
                  /o/$org/f/$factory/settings   → Factory settings: Pause or Resume, and the Danger zone (archive, restore, delete)
                  /o/$org/f/$factory/agents/$agent → Agent     (E12: header, the last 7 days, send-back reasons; D12: Skills, Tool policy, Edit in Definition)
                  /o/$org/new/$step             → D's New factory wizard (rail only, no sidebar; ?factory= from step 2; start?factory= resumes a draft factory)
api/          the only HTTP code: types.ts (generated contract shapes), client.ts (requests over @goal/api-client), definitions.ts and builder.ts (B's and D's routes), connections.ts (spec 4's connections, kinds and their audit entries), knowledge.ts (spec 4's knowledge base), queries.ts (keys, queryOptions),
              session.ts (401 and 403 reauth-required handling around the client's fetch; safeNext), auth.ts (the identity service, /api/auth/*: Better Auth's
              browser client, loaded only by the sign-in pages, the re-auth dialog and the account's actions)
app/          the shell: AppShell and ShellFrame, Rail, PhoneTopRow, PhoneCrumb, TabBar, FactorySidebar,
              OrgSidebar, OrgMark (the organization menu's trigger: OrgMenu on the rail, OrgSheet on
              phones), CreateOrgDialog, account-menu.tsx (AccountProvider, AccountMenu, the sheet's AccountBlock), screens.ts (the screen registry), attention, route focus and titles,
              loading/error/not-found screens. The loading screen (PendingScreen, the router's default pending component) is the app background with an sr-only "Loading" and aria-busy: no placeholder shapes, so nothing shifts when the page arrives. After 1 s a thin line fixed to the top of the viewport says the load is still on (animated under motion-safe only).
features/
  factories/  factory-settings/  floor/  board/  items/  members/  org-settings/  sign-in/  connections/  knowledge/   one folder per screen: page, cards, feature-only logic (items/timeline.ts, org-settings/form.ts, connections/words.ts, knowledge/words.ts)
  factory-editor/   the wizard and Definition (D); draft/: the draft session (session.ts, use-draft.ts), merge.ts, ops.ts, pointer.ts;
                    frame/: the editor frame, status line, tiles, dialogs and unsaved guard; section-views.tsx: each section's view (basics/, line/, …)
                    agents/ (Agents section): the agents list and each agent's detail; skills-editor.tsx (chips and the catalog combobox); rules-editor.tsx: the Rules area below the agents (ADR 0014), listing each rule's name, description and who it applies to, with Add, Edit and Delete; rule-dialog.tsx: the one dialog for add and edit, the frontmatter as form controls (agents, stations, lanes and work types from this definition, unchecked = every agent; paths; description) and the body as a textarea with Write/Preview. A save is one agents edit: the rules entry plus rules/<name>.md (ops.ts addRule, setRuleText, removeRule), with the rule file read through the session first so undo can write it back. shared/definition/rule-md.ts reads and writes the file's frontmatter (the keys internal/config validates). The agent page's Rules section (agents/agent-rules.tsx, under the agent's skills and built-in tools) lists each rule that can apply to the agent: `effectiveRules` (shared/definition/effective-rules.ts, pure and tested) keeps a rule whose `agents` names it or is omitted, and one whose `stations` include a station the agent works at (`whereAgentWorks`), marking it conditional on those stations and any `lanes` or `workTypes` ("Only at Build · lanes backend"); a rule with no `appliesTo` is always. `paths` are not computed. Rule texts come through `useRuleTexts` (agents/use-rule-text.ts), shared with the Rules area.
shared/       code more than one feature uses: decisions (approve, send back, merge, retry), new item,
              the andon cord (andon.tsx: the line status and the pull and clear dialogs, from Board.line),
              what waits on you (inbox.ts: the Inbox drawer's groups and each row's action, a pure function; inbox-drawer.tsx: the Inbox button with its count and the drawer, in the Board, Floor and Items headers),
              definition/ (typed views of B's FactoryModel, where an agent works, the factory rules that can apply to an agent (effective-rules.ts), slots at full WIP, pointer ↔ line, the Agent page's cards and the tool policy),
              the organization's live stream, org and factory helpers, marks, formatting (Intl), state labels, the address rule (org-rules.ts), human touches in words (touches.ts: reasons, kinds, the Touches panel's groupings),
              signing in by code (code-sign-in.ts), the re-auth host and dialog (reauth.tsx), the account's actions (account.ts),
              connections in words (connections.ts: status, last test and use, the env names a run gets, the shell's "1 failing"),
              knowledge sources in words (knowledge.ts: each kind's name and icon, what a source reads),
              a form's edits over what goald last saved (use-edits.ts: a background change never throws typing away),
              "is created" once (created.ts: ?created leaves the URL as the page says it)
ui/           design-system primitives on Radix: button, dialog, confirm-dialog (typed confirmation), field and input, segmented-control, sheet, menu, tooltip,
              card (SectionCard, StatGrid), chip, count, mark, hatch (Meter, HatchBars), target-bar, kpi-tile (KpiTile: label, number, bar or sparkline, signed delta, from MASTER.md's KPI tile), stepper (Stepper: an item's progress, each step with its state in text), bars (weekly bars with a target line and a hidden table), bridge (the lead-time waterfall, `bridgeGeometry`, with a hidden table), date-range, page-header, list-row, empty-state, logo
```

Route files stay thin (loader + component); screens live in `features/`. Loaders load through `ensureLoaded` (`api/queries.ts`; `api/loaders.test.ts` refuses a plain `ensureQueryData` or `fetchQuery` in `routes/`), which loads again when the fetch it shares was cancelled by the page being left; the error screen does the same once for a cancelled load instead of saying goald is not answering. Features never import each other; shared code goes to `shared/` or `ui/`. Boundaries are kept in review for now; `dependency-cruiser` arrives with `packages/`, the same way depguard guards the Go engine. `ui/` moves to `packages/ui` when a second app needs it.

### The API module and `@goal/api-client`

`packages/api-client` is the typed layer over the contract: `createGoalClient` (openapi-fetch over the types `openapi-typescript` generates into `src/schema.gen.ts`, with `--default-non-nullable=false` so request fields with a default stay optional), `unwrap` (data on success, else it throws `ApiError`, which carries the RFC 9457 `Problem` with its `code` (`slug-taken`, `confirmation-mismatch`, …) for screens to branch on, rebuilt from the status line when a proxy answers with something else), `subscribe(baseUrl, scope, handlers)` for the stream, and the schema types. Organization-owned routes are written relative to the organization with `orgPath("/factories/{factory}/board")`; `routes.ts` holds the one prefix (`orgRoutes`, `/v1/orgs/{org}`), and every such call passes `org` (slug or id) as a path parameter, as `StreamScope` does.

In `web`, `src/api/types.ts` re-exports the generated types under the names the screens use, and `client.ts` holds every request (`listOrgs`, `createOrg`; then, each taking `org` first, `getOrg` (the caller's role and `can`), `listMembers`, `listInvitations`, `setFactoryRole`, `transferOwnership`, `getAttention`, `getFactoryOverview`, `getInbox`, `getOrgSettings`, `patchOrgSettings`, `deleteOrg`, `listFactories` (`{ archived }`: the archived factories only when true; `archivedFactoriesQuery` reads them), `pauseFactory`, `resumeFactory`, `archiveFactory`, `restoreFactory`, `purgeFactory` (the factory's Pause, Archive, Restore and Delete; spec 5 §10), `getBoard`, `getMetrics` (a `FlowRange` of from, to and type, with the range before), `getTouchMetrics` (human touches over a `FlowRange`, spec 4 §4.9; one week of trend), `getWeekly`, `getLeadTime`, `getConstraint`, `pullAndon`, `clearAndon`, `listItems`, `getItem`, `listItemEvents`, `listItemRuns`, `noteItem`, `submitItem`, `approveItem`, `rejectItem`, `mergeItem`, `retryItem`, `moveItem`, `pauseItem`, `setTouchReason`, `getAgentStats`, `subscribeToOrg`) over `createGoalClient`, `definitions.ts` the definitions routes over the same client (`getFactory`, `createFactory`, drafts, sections, files, validation, publish, versions, compare, restore, templates; B §10), `builder.ts` the builder routes (`getBuilderMeta`, `searchSkills`, `getSkillFiles`; D §8), and `keys.ts` the organization's API keys (`listApiKeys`, `createApiKey`, `revokeApiKey`; spec 3 §8.9) and personal tokens (`listPersonalTokens`, `createPersonalToken`, `revokePersonalToken`, `listMemberTokens`, `revokeMemberToken`; 3-12b), and `connections.ts` the organization's connections (`listConnectionKinds`, `putConnectionKind`, `listConnections`, `getConnection`, `createConnection`, `updateConnection`, `deleteConnection`, `testConnection`, and `listConnectionAudit`, the audit entries about connections; spec 4 §8.2), and `knowledge.ts` the organization's knowledge base (`listKnowledgeSources`, `getKnowledgeSource`, `createKnowledgeSource`, `updateKnowledgeSource`, `deleteKnowledgeSource`, `syncKnowledgeSource`, `listKnowledgeDocuments`, `uploadKnowledgeDocuments` (one file a request, so each file's refusal is its own), `getKnowledgeDocument`, `deleteKnowledgeDocument`, `searchKnowledge`, and `documentContentUrl`, where the browser downloads an uploaded original itself; spec 4 §8.2); factories are named by id. Nothing else in `apps/web/src` touches `fetch` or `EventSource`; `apps/web/mock` is typed with the same generated shapes. Query keys (`queries.ts`) are rooted at the organization's id, but for `['auth', 'config']` and `['me']`: `['orgs']`, `['org', org, 'detail']` (`getOrg`: role and `can`), `['org', org, 'members']`, `['org', org, 'invitations']`, `['org', org, 'attention']`, `['org', org, 'overview', window]`, `['org', org, 'inbox', factory ?? 'all']`, `['org', org, 'settings']`, `['org', org, 'api-keys']`, `['org', org, 'personal-tokens']`, `['org', org, 'member-tokens']`, `['org', org, 'factories']` and `['org', org, 'factories', 'archived']` (both under `keys.factories`, so `refreshFactory` refetches them together), `['org', org, 'connections', {factory?}]` (the list; `factory` an id), `['org', org, 'connections', 'detail', name]`, `['org', org, 'connections', 'activity', id ?? 'all']` (audit entries), all under `['org', org, 'connections']`, and `['org', org, 'connection-kinds']`, `['org', org, 'knowledge', 'sources', {factory?}]`, `['org', org, 'knowledge', 'source', name]`, `['org', org, 'knowledge', 'source', name, 'documents', limit]` (pages), `['org', org, 'knowledge', 'document', id]` and `['org', org, 'knowledge', 'search', {q, sources?}]`, all under `['org', org, 'knowledge']`, `['org', org, 'factory', f, 'board' | 'metrics' | 'items', …]` (f the factory's id; `metrics` takes the `FlowRange`), `['org', org, 'factory', f, 'weekly', to ?? 'today', type ?? 'all']`, `['org', org, 'factory', f, 'lead-time', range]`, `['org', org, 'factory', f, 'touches', range]` (the Floor's range and the one before; the board and the Item read the last 7 days, `{}`; a reason set on the Item refreshes the item and these), `['org', org, 'factory', f, 'constraint', from ?? 'week', to ?? 'today']`, `['org', org, 'item', ref]`, `['org', org, 'item', ref, 'events']` and `['org', org, 'item', ref, 'runs']` (refreshed with the item); the definitions keys are `defKeys`, beside the work keys under the factory's prefix (a comparison's key, `defKeys.compare(org, f, from, to, revision)`, ends with the draft revision it was read at, 0 when neither side is the draft; no screen builds a key by hand), and the builder keys `builderKeys`: `['org', org, 'builder', 'meta']`, `['org', org, 'builder', 'skills', q]`, `['org', org, 'builder', 'skill', key]`.

The URL names the organization (`/o/$org`, its slug or id) and the factory (`/f/$factory`, its name or id). The `/o/$org` and `/o/$org/f/$factory` layouts resolve both in `beforeLoad` (`ensureFound`: one the cached list lacks, made in another tab or with the CLI, reads the list once more before Not found), so every screen reads them from the route context (`useOrg()`, `useFactory()` in `shared/`): queries are keyed and requests made by their ids, and links use the slug and the name (`useFactoryParams()`). `useFactory()` then follows the factory list (selected by id), so a publish elsewhere changes the open page's stations and version without a navigation; an archived factory is not in that list, so `beforeLoad` falls back to the archived list and `useFactory()` reads that one while the factory list lacks it (see "Archived factories"); after Publish v1 the wizard waits for that list before it opens the Floor.

### Signing in (phase 3)

With `GOAL_AUTH=session` people sign in on `/sign-in` with an emailed code or Google (spec 3 §10). Only `api/auth.ts` calls the identity service (`/api/auth/*` on goald's origin, Better Auth's own contract, ADR 0007), with `better-auth/client` at the version `apps/auth/` pins (`better-auth: catalog:` in `pnpm-workspace.yaml`) and the email OTP, organization and device client plugins; it is imported only by the sign-in routes, the re-auth dialog and, through `import()`, the account's actions and the keep-alive, so the first bundle never carries it. goald reads the session from its httpOnly cookie; no script holds a token.

Every `/v1` request goes through `session.ts`: a 401 clears the query cache and opens `/sign-in?next=<the page>` (`next` is followed only when it is a path of this origin, `safeNext`), and a 403 `reauth-required` opens the re-auth dialog ("Confirm it's you", `shared/reauth.tsx`, mounted at the root) and sends the request once more after a fresh sign-in by code; with Google the page reloads and says "Signed in again. Try that once more." The account (`app/account-menu.tsx`, from `['me']` and the organization's `role`) names the person, their role and Platform admin, with Edit name…, Sign out and Sign out everywhere…, and asks the identity service for the session on load and hourly so Better Auth extends it; after two stream errors in a row the app reads `['me']` again. Under `GOAL_AUTH=none` (`getAuthConfig` says `mode: none`) there is no sign-in page and the account says "Local user · This install · Sign-in is off".

### What the caller may do (phase 3)

Screens offer a control from `can`, never from role names (spec 3 §10.1): the organization's (`Org.can`, `orgQuery`, loaded by the `/o/$org` layout; `useOrgHolds`) and each factory's (`Factory.can` in the factory list; `useFactory()`, `useFactoryById()`), asked through `holds()` in `shared/permissions.ts`, which also holds the reasons the screens give. Nothing known holds nothing. Under `GOAL_AUTH=none` `can` lists every permission, so the local user is offered everything. goald checks every request again: a 403 `forbidden` (a role changed under an open page) has `session.ts` call the app's `forbidden` handler, which refetches `['orgs']` and every organization's `detail` and `factories`, so the screen stops offering it.

- Hidden where it would only confuse: New factory (rail, Factories home; the wizard's Start says "Only owners and admins start factories") without `factory:create`; New item without `item:create`; Move (button, `M`, drag) on the board and Move and Pause on the Item page without `item:operate`; the Danger zone without `org:delete`; on a factory's Settings page, the Pause control without `factory:manage` on the factory's `can`, and the Danger zone without `factory:archive` on the organization's `can` (Delete also without `factory:delete`, and disabled until the factory is archived); and every factory write (`item:create`, `item:operate`, `item:decide`, `andon:manage`, `definition:edit`, `definition:publish`, `factory:manage`) on an archived factory, since `holds()` (`shared/permissions.ts`) answers false for them while `archived_at` is set: New item, Move, decisions, the andon cord, Pause, and the definition's edit controls go, and the banner says why.
- Disabled with a reason, focusable (`aria-disabled`, the reason visible and its `aria-describedby`): the andon cord without `andon:manage` ("Approvers and editors can pull the cord", canvas Board-Viewer); an item's decisions without `item:decide` ("Approvers of sre decide here."). A note composer without `item:operate` is replaced by who sends notes. Decisions wait "on you" only where `item:decide` is held, else "on an approver"; the board names a viewer's or approver's role ("You're a viewer of sre").
- View only: Organization settings without `org:manage` (the form inside `<ReadOnly>`, no Save, a `ViewOnlyBanner`); the Definition and the wizard without `definition:edit` (canvas Define-View): the draft session is `readOnly` (keyed by it, so a role change makes a new one) and shows the running version even while someone edits a draft ("M. Okafor is working on a draft of v9."; a factory never published shows its draft), refuses every edit, undo, revert and discard, and sends nothing; the section views render inside `<ReadOnly>` (`ui/read-only.tsx`: `Input` and `Textarea` readOnly, `NativeSelect` and `Switch` disabled), and each view leaves out its edits (`useViewOnly()`: Add station, row menus and order, chips' remove, Add agent, Restore…, the YAML file menu's New/Rename/Delete, Format; `SegmentedControl readOnly`); the frame drops Discard, Review and publish and the status line, the tiles say what each section holds, and Review and publish says who publishes.
- Basics shows Who approves (read-only: owners and admins, and the factory's approvers and editors); a human station's Who works here is "Approvers of {factory}".

### Archived factories

An archived factory (spec 2.2-b, `factory:archive`) is left out of the factory list, so it is kept apart in four places. The Factories home's "Active | Archived" filter (`SegmentedControl`, `?archived=true` through `factoriesSearch`) lists the organization's archived factories (`archived-factories.tsx`: the name links to its pages, "Archived Oct 25 by Dana", and Restore with `factory:archive` on the organization's `can`; an empty state when there are none). The rail and the phone's top row drop any factory with `archived_at` (`useFactorySquares`), and an archive in this tab takes its square off through the refetched list (`refreshFactory` invalidates `keys.factories`). Every page under `/o/$org/f/$factory` shows `ArchivedBanner` above its `Outlet`: "Archived on Oct 25 by Dana. It takes no work.", with Restore for `factory:archive` holders. Restore is one mutation, `useRestoreFactory` (`features/factory-settings/restore.ts`), shared with the Danger zone and the banner and the list; it sets the factory paused, and every list refetches. The write controls are off by `holds()`, not per screen (see "Hidden where it would only confuse").

### Live updates

`shared/stream.tsx` (`OrgStream`, mounted by the `/o/$org` layout) opens one `EventSource` per open organization, on its stream. Each `item` event invalidates what it can change (`invalidationsFor` in `shared/invalidation.ts`): the organization's attention counts and inboxes, the factory's work keys named one by one (board, metrics, touches, items; only the board for tool calls; never the factory's bare prefix) and that item by ref and by id, batched over 300 ms so a burst of tool calls costs one refetch; the factory overviews at most every 5 s. The item's history refetch asks only for events after the last one cached. `resync`, and every reconnect after the first open, invalidate everything of the organization (`['org', org]`). A `factory` event (an andon pulled or cleared; the client's typed `onFactory`) invalidates the factory's work keys, the attention counts, the inboxes and the overviews (`factoryInvalidations`). A `connection` event (spec 4 §8.3) invalidates everything under `['org', org, 'connections']` (`connectionInvalidations`), a `knowledge` event (a source's sync started or ended) everything under `['org', org, 'knowledge']` (`knowledgeInvalidations`), and a definition's `current` event both, since what uses a connection or may search a source is read from the current versions. Other event names are handled through the `streamHandlers` registry; names nobody registered are ignored. Switching organization closes the old stream and opens the new one. `shared/stream-presence.ts` (`followPresence`) decides what `OrgStream` exposes. The shell says "Reconnecting" and the board's line status adds "reconnecting…" only after the stream has been down for 5 s (the grace period) while the page is visible; a short drop is silent, and a hidden page never counts as reconnecting. A return to the page (`visibilitychange`, or `pageshow` from the back/forward cache) reopens the stream at once, without waiting for EventSource's retry, closing the old one first; the reopened stream resyncs (every organization query refetches) so the screens are current without a banner. The board's line status still adds "connecting…" while the first open is pending.

### Decisions

Approve and retry act at once (they only move work forward); send back asks for a reason in a dialog, and merge asks for confirmation. Merge replaces approve at a human station whose `action` is `merge`; nothing is keyed by station names. Decisions carry no actor: goald records the request's (the local user until phase 4 adds sign-in).

### The Inbox drawer

The Inbox button sits in the header of the Board, the Floor and Items, at every width, and shows the count of what waits on people in the factory it is on; it opens a right-side drawer (`ui/drawer.tsx`, Radix Dialog: about 460 px wide, a full-screen sheet below 768 px). Its data is the organization's inbox (`inboxQuery(org, factory?)`); the segmented control "All factories | This factory" (default This factory) switches between the two queries. `shared/inbox.ts` is the pure grouping: entries become Ready to merge (a `decision` whose `action` is `merge`: Merge), Needs a decision (any other `decision`: Approve; `rework_warning` and kinds this build does not know: Open), and Blocked (`blocked`: Retry; `andon`: Open to the board). Expedite entries are left out (they wait on no person and the board shows them). Groups with no rows are left out, and each group's rows are oldest first. Each row is a link to its item (or to the board, for rows about no item) with one action button named for the item's key (`Approve sre#5`); an action the entry's own factory does not allow (no `item:decide` there, by that factory's `can`) is disabled with who decides. Merge asks for confirmation; "Merge all" (Ready to merge with more than one row) asks once with the count, then merges each item in turn: a refusal names its item in the drawer and the rest still merge. Every action invalidates `keys.inboxes(org)`, so a handled row leaves the drawer. Escape closes; focus is trapped while open and returns to the Inbox button (`onCloseAutoFocus` in `ui/drawer.tsx`).

### Status icons

`ui/status-icon.tsx` (`StatusIcon`) is the one set of item status icons, the same everywhere an item's status shows: working is a half-filled circle (teal), waiting for another item a clock (amber), waiting for the same files two crossing arrows (muted, since overlapping work is normal flow: only blocked, expedite and andon are red), ready or merged a check (green). Each is decorative (`aria-hidden`); the words beside it say the same. `itemStatus` (`shared/item-state.ts`) maps an item's state to its icon, and the board's rows, the inbox drawer, the Items list (desktop column and phone sub-line) and the item page's state chip all use it: a clock for queued, waiting on you, and ready in the drum's buffer; a check for ready outside it and done; none for blocked, cancelled and assemblies. The Inbox drawer keeps its own row mapping (`shared/inbox.ts`). The two crossing arrows (same files) show for a queued or ready item whose `waiting` has kind `files` (the overlap wait the API reports on the card and on the item); the item page passes the item's `waiting` to `itemStatus` too, so it shows the same icon as the board.

The Items list (`features/items/items-page.tsx`, rows in `assembly-rows.tsx`) groups by default (ADR 0012): an assembly is one row, with a chevron that opens or closes its parts (open by default; `aria-expanded`), the "Assembly" label and ref, its title (two lines), its open parts' stations on one line, and a progress bar with "N of M parts merged" from its `parts`. Its parts sit beneath it, indented, in plan order, each with its number, title, station and a status pill (the words and the shared icon). A part is never a top-level row, and an item that is neither shows as a plain row with no type chip. `listRows` does the grouping: a part whose assembly is not on the page gets its row from the part's `assembly` ("N parts", no bar). `?show=flat` (the Show switch; the default drops the param, and a filter change keeps it) lists every item as a plain row. A filter applies to parts; while one is on, the list also reads `state=assembly` (the loader and the page) so a kept part's row has its assembly's progress, and an assembly with no kept part does not show. A footnote under the list says how it groups and filters.

### Colour theme (dark mode)

Type is Archivo (`@fontsource-variable/archivo`, the `wdth` axis file, self-hosted) for all text and JetBrains Mono (`@fontsource-variable/jetbrains-mono`) for code, paths and commands only; `html` sets tabular numerals. The scale and weights are in MASTER.md, Typography.

The colors are tokens: `apps/web/src/styles.css` defines the light values in `@theme` and the dark ones (MASTER.md, "Dark theme") under `:root[data-theme="dark"]` and under `prefers-color-scheme: dark` for `:root:not([data-theme="light"])`. Components use token names only, so a theme switches without edits; `apps/web/src/ui/raw-colors.test.ts` fails on a hex, `rgb()`, `rgba()` or `hsl()` outside the allow-list, which holds only the Monaco theme until its item lands. The text on a run, violet or red fill is `--color-white` (#FFFFFF in both themes), and the selection is `run-tint`. The shadow is one token, `--shadow-pop`, for popovers and dialogs; everything else is a border. Radii are `--radius-andon` (18), `--radius-card` (14), `--radius-cell` (10), `--radius-control` (8) and `--radius-chip` (6).

The type scale is `--text-meta` (12), `--text-secondary` (13), `--text-body` (14), `--text-heading` (16), `--text-detail-title` (22), `--text-page-title` (26) and `--text-kpi` (28), each a `text-<role>` utility with its line height and weight; the tracking tokens are `--tracking-display` and `--tracking-numeral`; numbers at 112% width use the `font-numeral` utility (styles.css), so no component writes `font-stretch-[112%]`. New code uses these and the 4px spacing scale. `apps/web/src/ui/arbitrary-values.test.ts` counts the existing arbitrary font-size, off-scale spacing and radius classes per file in `arbitrary-values.baseline.json`: a file may not gain one, and a screen that clears its uses lowers its counts in the same change.

The choice (System, Light or Dark) is one localStorage key, `goal-theme`, per browser (`apps/web/src/shared/theme.ts`; a missing or unknown value is System). `apps/web/public/theme.js` is a classic script in `index.html`'s `<head>`, before the module, so it sets `data-theme` and `style.colorScheme` on `<html>` before first paint; it reads the same key and must stay in step with `theme.ts`. CSP is `script-src 'self'`, so it cannot be inline, and it must not be a module or deferred. `useTheme()` (in `theme.ts`) gives the choice, the resolved theme and the setter; the account menu's Appearance choice sets it, and the toaster in `routes/__root.tsx` takes the resolved theme. `theme.test.ts` checks the persistence, fallbacks, live system changes and that the dark blocks define every token; `theme-boot.test.ts` runs `theme.js` against fake objects and checks its place in `index.html`.

### Lanes

Each agent or human station is one column split into lanes, one per `station.lanes` entry in API order (`features/board/board-lane.tsx`): a lane's header shows its role (`truncate`, with a `title`), its model label and `slots` boxes, `busy` of them filled; a person lane has none. A lane lists the items its role is doing as work cards (a doing line with the working icon, one truncated line: the run's `activity` ("Editing <path>") when it has one, else "Working, turn n"; under it the plan-step bar, one segment per todo of the run's `plan_steps`: done ones filled, the one in hand hatched, the rest outlined, absent when the run lists no todos; the touches count only above 0) above its waiting rows (the reason with its status icon: a clock while it waits, a check when it is ready; blocked has none). A waiting row whose item has `waiting` shows one muted line in place of the reason (`WaitLine`, `shared/waiting-for.tsx`): its status icon and `waitingShort` (`shared/waiting.ts`), "Waits for software#93", or "Waits for software#93 +2" when three items hold the wait. The line never shows paths or globs, and it is one `truncate` line, so a waiting card is no taller than a normal one. Its `title` is the full list from `waitingDetail`: each need's ref, title and the paths that hold the wait (the shared files for a files wait). The item page shows a "Waiting" section under its header (`WaitingFor`, same file): one entry per open need, its ref a link to that item's page (`/o/$org/f/$factory/items/$ref`), its title and its `paths`, plus "Shared files" for a files wait. The text wraps; nothing is truncated. A card joins the lane whose key equals its `board_lane`; one no lane claims sits in an untitled lane at the foot. A lane with nothing says "Nothing to do". Queue and done stations have no lanes and list their items as cards. On the desktop grid a queue column has no header row (no station name, no count): it shows only its cards, while agent, human and done columns keep theirs. The kind is read from `station.kind`, never from the station's name. Numbered agent copies (`investigator-2`) never show on the board: a lane is the role, and the card names what the run does, not its slot. The phone's station panel shows the same lanes, compact (see Phone Board). The item screens name a run the same way: its role (`runRole` in `features/items/timeline.ts`) and its model's label (`useModelLabel` in `shared/models.ts`, the install's models without a leading "Claude", the id until they load or when unlisted); the run's slot is a machine field, never shown. A run's block under its meta line says "Ran with rules: secrets, delivery" from its `rules_applied` event (the rule names in mono; no line when none applied, and sha256 is not shown). A `rule_violated` event is a rework note by the reviewing agent's role ("Reviewer found the rule secrets broken") with the reason as its detail.

The board's filters row sits above the columns and, on a phone, above the station tabs: Assembly (Any, or an assembly on the board by its title: the open ones of `Board.assemblies` and those of the parts on it; it matches the assembly's parts and, once done, its own card), Agent role (Any, or a lane's role, never a numbered agent) and Status (Any, Working, Waiting for another item, Waiting for same files, Ready, with the status icons of `itemStatus`, which read the card's `waiting`). Group by None or Assembly orders each station's cards so one assembly's parts sit together in part order, the assemblies in their first part's place and the cards of no assembly last; each lane keeps that order. An open assembly is no card: it is a header over each run of its parts in a station, a lane's working cards or its waiting rows, reading '<title> · <merged> of <total> merged' from `Board.assemblies` and linking to its item page (`AssemblyHeader` in `board-card.tsx`); a part whose assembly is not open keeps its assembly line. Open assemblies count toward no station's load, WIP, waiting count or statistics. Filters only hide cards: the line's and the stations' numbers stay the board's. The logic is pure, in `features/board/board-filters.ts` (`arrangeBoard` over the board response, `knownView` for the choices the board offers). The choices are search params of the board route, `assembly`, `role`, `status` and `group=assembly`, beside `station`: an absent param is Any or None, and a value the board does not offer is dropped, so a link reproduces the view.

### Phone Board

Below 768 px (`features/board/board-page.tsx`, `StationTabs`) the Board is a header (the title, Inbox with its count, New request), one status line and a station picker. The status line is the line's status, its WIP and, once an item finished this week, "X touches per item"; it is one truncated line with the full text in a `title`, and "By station" beside it is a 44 px link to the Floor's touches. The picker is a horizontal scroller of one-line tabs, each showing the station's name and load ("Build", "2 of 6"), truncated with a `title`; a tab is at least 56 px tall. The constraint's tab has an amber bottom edge, and the picked tab is ink-filled (`data-state=active`). When the picked station is the constraint, one sentence sits under the tabs (`constraintSentence` in `stats.ts`: "Build is the constraint: 2 of 6 busy, 3 waiting", or "N busy, K waiting" without a limit). The picked station's section shows its touches per item, then its lanes in compact form: cards with 44 px Move buttons and list-row waiting rows, and "Nothing to do" for a lane with nothing. The phone's status line leaves out the expedite count; the desktop line and the expedite cards carry it. A part's card and waiting row carry its assembly line on the desktop and the phone alike: "Migrate billing › 4 of 6", the title on one line with an ellipsis and the part count never cut, a link to the assembly's item page (`AssemblyLine` in `board-card.tsx`), except under an open assembly's header, grouped by assembly: that 44 px header replaces the line. The cards' doing line and plan-step bar read the same on the phone (see Lanes). Its waiting rows carry the need lines of the Lanes section, under the reason, as on the desktop.

### Card size

A board card stays compact in every column, so the board scans as flow: a done item's card shows its ref, age, title, PR link(s) and touches, and no state line or summary (the item page's Outputs card shows the summary in full). The state line is clamped to two lines and the title to two (`line-clamp-2`, with the full title in a `title` attribute). Any new card text that can grow goes through a clamp or an excerpt the same way.

### Moving work

Every board card has a Move button (32 px in the columns, the 44 px arrow on phones and in the compact lanes) and, when focused, the `M` shortcut (`aria-keyshortcuts`); both open the Move menu (`shared/move-menu.tsx`), a dialog listing the line's stations but the item's own and done stations (`shared/move-choices.ts`): upstream ones say they count as rework and require a reason, and an item in progress says its run stops. In the columns, dnd-kit drags a card by pointer (6 px before it starts, so clicks still open the item) or by keyboard (Space picks the focused card up, the left and right arrows carry it a column at a time, Space drops, Escape cancels; screen-reader announcements name the station); a drop on another column opens the same menu with that station chosen, so every move is confirmed. The board moves the card optimistically and puts it back with the problem's detail if goald refuses; while a move is in flight (mutation key `keys.move(org, f)`) the stream leaves that factory's board alone (`heldByMove`), and the move refetches it when it settles, so a refetch cannot put the card back meanwhile. The Item page adds Pause (a dialog with an optional reason), Resume (the retry of a paused item: its latest block is a `paused` event), `Send back to <rework_target>` at any state goald allows, and Move.

### The Assembly page

An item in state `assembly` (ADR 0012) opens the Assembly page instead of the item page: the route's component reads the item and switches on its state (`ItemRoute` in `routes/o.$org.f.$factory.items.$ref.tsx`); every other state keeps `ItemPage`. The loader prefetches `partsQuery` (`listItemParts`) without awaiting it. The page (`features/items/assembly-page.tsx`) shows the header (the Assembly label, the key, the title clamped to two lines, the brief), then:

- **Progress**: one segment per part in plan order, toned done, working or waiting (`barSegments`); "N of M parts merged"; lead time from `lead_time_seconds`; spend as the assembly's cost plus its parts' `cost_usd`, with "of $X" only when `flow.budget.per_item_usd` is set (`spend`: the budget is per item times the parts in all); human touches from `touches.count`; time to finish (`timeToFinish`): the mean lead time of the merged parts times the number of waves that still hold an open part, "—" with no merged part, and no cell once none is open.
- **Plan**: the parts in waves (`planWaves`, cancelled parts left out). A part being worked is a work card (title clamped to two lines, its station and its agent's role and model from the board lane, `agentLabel`; the board is read only when already loaded, else the agent's name as words). Every other part is a one-line row ending in an ellipsis, named by its status icon (`partStatus`, the item's `itemStatus` with one rule added): an open part with `waiting_on` is waiting for those refs, and a done part is ready.
- **Parts**: a table in plan order (index "N of M", ref, title, station, state, cost, pull request), each title linking to the part's own item page.

The header's actions (`AssemblyActions`, `features/items/assembly-actions.tsx`) act on the open parts (neither done nor cancelled), with no confirm for Pause. **Add a part** (`white`, Plus, first among them) needs `item:create` and opens `AddPartDialog` (`add-part-dialog.tsx`): a required title and brief, a Lane select (the parts' agents and the station's agent lanes by role, "Any agent" by default; hidden when there are none), and "What it needs", a checkbox list of the parts by title and ref, sent as their `key`s. It calls `addItemPart` (`POST /items/{item}/parts`); goald queues the part at the assembly's station, so the dialog picks none. Empty fields say so under themselves (`aria-describedby`); a 400 part-invalid or a 409 refusal (not-an-assembly, item-closed, edge-cycle) is a `role="alert"` in the dialog, which stays open. Success closes it, toasts "Added <title>." and invalidates the item's keys (`itemChangeInvalidations`). Esc or Cancel calls nothing. **Pause** (`white`) needs `item:operate` and shows while some open part is not blocked (`pauseAction`); it calls `pauseItem`. **Resume** (`primary`, Play) replaces it once every open part is blocked and needs `item:decide`; it calls `retryItem`. **Cancel assembly** (`rework`) needs `item:decide`, opens a dialog that names the open-part count (`openPartCount`, "Its 3 open parts are cancelled too.") and calls `cancelItem` on confirm; Keep it or Esc calls nothing, and a 409 shows inline as a `role="alert"`. Each action is hidden on a done or cancelled assembly. The decisions invalidate the item, its parts and the work keys (`itemChangeInvalidations`).

Board cards do not show assembly lines yet.

### Factory settings

`/o/$org/f/$factory/settings` (`features/factory-settings/`), reached from the factory sidebar's Build group and, on phones, from the More sheet. The Work section is Pause or Resume (`pauseFactory`, `resumeFactory`) and shows only with `factory:manage` in the factory's `can`; it is left out for an archived factory, which Restore starts paused. The Danger zone (`danger-zone.tsx`, modelled on the organization's) reads the organization's `can`, because `factory:archive` and `factory:delete` are organization-level: it is hidden without `factory:archive`. Archive asks first; a running factory offers "Pause first", which pauses, then archives, and a `factory-busy` answer shows goald's detail ("N items are being worked"). Restore replaces Archive once the factory is archived and says it stays paused. Delete is disabled with "Archive the factory first" until the factory is archived, and its dialog takes the factory's name typed exactly before it enables; after a delete the page opens the Factories home and the factory's queries leave the cache. Every action refreshes the factory list, the factory's detail and the rail's attention and overview queries (`refreshFactory`); the archived and paused state on the page comes from the factory's detail, since the list leaves archived factories out.

### Organization settings

One form (`/o/$org/settings`): "Save changes" is enabled while it differs from what goald last answered and sends `PATCH …/settings` with only the changed fields (`changedFields`; slots always carry the organization's cap, which the contract requires). Fields are checked against goald's own rules first; a `slug-taken` answer is the Address field's "Taken", other refusals show the Problem's detail. After a save the organization list is refetched (rail, sidebar, route context); a new address replaces the URL with the new slug, and links with the old one stop working (review decision 2). Leaving with unsaved changes asks first (`useBlocker`). Delete takes the typed name, then `/` opens the next organization or says there is none. Model access names the organization's model connections and what uses each (New model connection for `connection:manage`); Secrets counts the connections and opens Connections.

### Running it

```bash
task web:dev                     # Vite on :5173, /v1 and /api/auth proxied to goald on 127.0.0.1:8080 (GOAL_API_URL to change), Host kept for goald's cross-origin check
GOAL_API=mock task web:dev       # the same against the in-memory mock API (apps/web/mock), no goald needed
GOAL_API=mock GOAL_MOCK_LIVE=1 task web:dev   # the mock's agents keep calling tools, to watch the stream
task web:check                   # tsc, Biome, Vitest
task web:e2e                     # Playwright (Chromium) at 375, 768, 1440 px with axe, on vite preview + the mock (GOAL_E2E_PORT, default 4174; artifacts in test-results/<run>/)
task web:e2e-auth                # the sign-in flow against a running goald under GOAL_AUTH=session, the identity service and Mailpit (not the mock)
task build                       # web:build, then go build -tags webui: goald serves the UI at /
```

`apps/web/mock` is a stand-in for goald's API with the contract's shapes and realistic fixtures: two organizations, `acme` (a goal factory building itself, a small SRE factory, an empty support factory whose line is stopped by an andon pull, with an earlier cleared one) and `example-labs` (one empty factory), with weekly budgets for the Factories home. It serves the shell's attention, overview and inbox from the same state, keeps state per server, broadcasts stream events like goald, and `POST /__mock/reset` restores the seed (the e2e tests reset before each test; a body `{now}` also fixes the mock's clock until the next reset, which `floor.spec.ts` pairs with `page.clock` so its days hold across UTC midnight). Two queued goal items wait on open needs (`goal#15` for the same files as in-progress `goal#13`, `goal#14` after `goal#16`), so the board's waiting rows and the item page show both kinds of `Item.waiting`. The definitions and builder routes are `mock/definitions.ts`: every factory has versions and goal an open draft (seeded in `mock/definition-seed.ts` with a template and skills catalog), Visual section saves are applied to `factory.yaml` by `mock/model.ts` on the `yaml` package's Document (comments, key order and flow style kept, items matched by identity, as `internal/factorymodel` does), writes are conditional (412 `draft-conflict` with the current value, 428 without a precondition), and every draft change is a `definition` event. The andon routes (`GET`/`POST …/andon`, `POST …/andon/clear`) keep each factory's pulls, refuse as goald does (`andon-pulled`, `andon-not-pulled`, reason and note limits), write the audit entries and send `event: factory`; goal's review gate has a seeded tally. Human touches (`mock/touches.ts`, spec 4 §4.9) follow goald's rule over the mock's item events (decisions carry `data.actor` and `data.planned` as goald's do) and the seeded history's own touches, with the version each item finished under: `Item.touches`, `touch_count` on cards, the board's and the overview's headline, `…/metrics/touches`, `/metrics/touches` and `PUT …/touches/{event}/reason` (204, audited, 404 `touch-not-found`). The connections routes are `mock/connections.ts` (spec 4 §8.2): goald's own platform kinds (`internal/connkind/kinds`), organization kinds by PUT, acme's connections (pagerduty's test fails until its key is replaced, status-api misses its secret, gitlab-infra was never tested) with seeded audit entries of their recent use, `used_by` read from each factory's current version (its organization connections, model and repository connections), every refusal goald answers, and `event: connection` on each change and test. The knowledge routes are `mock/knowledge.ts` (spec 4 §8.2): acme's upload sources handbook (sre's current version lists it) and postmortems, and goal-docs and runbooks synced through github (runbooks' last sync failed, with a failed document), each source's `used_by` read from the factories' current versions (`knowledge.sources`), uploads as multipart parts named `files` checked as goald checks them, a sync or an upload indexing at once with `event: knowledge` as it starts and ends, and a search ranking documents by the query's words; `mock/connections-check.ts` is authoring's `connections` check and config's `tools` and `removed` checks (spec 4 §4.4) over the draft, with goald's fixes. `POST /__mock/fail {path, method?, status?, code?, detail?, times?, delay_ms?, problems?}` makes the next matching requests (path contains `path`; `times` 1 by default, 0 for all until a reset) wait and answer that status the way goald would (a 412 on a section save carries the section's value and etag, a 409 `definition-invalid` its problems); `DELETE /__mock/fail` clears them. Sign-in is off in the seed (the local user, as `GOAL_AUTH=none`); a reset with `{auth: {mode: 'session', signed_in?: 'fresh' | 'stale', email?, google?: 'ok' | 'not-invited' | 'unverified' | 'failed' | 'off'}}` turns it on (`mock/auth.ts`): `/v1/auth/config`, `/v1/me`, a 401 without the session cookie, 403 `reauth-required` on deleting an organization with a session older than 10 minutes, and the identity service's `/api/auth/*` the SPA calls with the identity service's answers (apps/auth/): sends answer the same for every address but codes, always `000000`, go only to f.cruz@example.com (owns every organization, platform admin) and j.doe@example.com (in none); `INVALID_OTP`, `OTP_EXPIRED`, `TOO_MANY_ATTEMPTS` on the 4th try, Google as a redirect round trip that may come back with `SIGN_UP_NOT_ALLOWED` or `unable_to_get_user_info`, `NAME_ONLY`. A reset with `{role: 'owner' | 'admin' | 'member' | 'editor' | 'approver' | 'viewer'}` (`mock/roles.ts`) makes the caller that member of every organization (editor, approver and viewer: members with that role on every factory; a member sees no factory), with `role` and `can` derived from `contracts/api/permissions.yaml`, decisions left out of the inbox and attention without `item:decide`, and every operation the role lacks refused 403 `forbidden` by its `x-goal-scope` in `contracts/api/openapi.yaml`, as goald does; `e2e/roles.spec.ts` checks each role. The people of each organization (`mock/people.ts`: acme's owner F. Cruz, admin M. Okafor, four members with factory roles, d.ross@example.com's pending invitation) answer `/members`, `/invitations`, factory roles and `/transfer` as goald does, give each organization the caller's `role` and `can`, and back the identity service's organization endpoints (invite, cancel, change a role, remove, leave, get, accept and reject an invitation) with Better Auth's and Goal's rules; any of them can sign in by a reset's `email`. An `X-Mock-Latency-Ms` request header delays every answer but the stream; the 768 px e2e project sends 200, so pending screens show and loads overlap navigations as they do against goald.

### Embedding in goald

`apps/web/webui.go` (Go package `web`) serves the build: files as they are (hashed `assets/` with a one-year immutable cache, the rest `no-cache`), `index.html` for every other path so client routes work on reload, and 404 for unknown `/v1`, `/api`, `/mcp` and asset paths. Every answer carries the Content-Security-Policy in `apps/web/csp.txt` (scripts, workers, fonts and requests from goald only; inline styles for Radix and Monaco; images from goald or `data:` only, so no remote profile pictures; `object-src 'none'`, `form-action 'self'`, `base-uri 'none'`, `frame-ancestors 'none'`), `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Referrer-Policy: same-origin`, `X-Content-Type-Options: nosniff` and, when `GOAL_PUBLIC_URL` is https, `Strict-Transport-Security: max-age=31536000`; `vite preview` sends the same but HSTS, so the e2e tests run the build under it (`e2e/csp.spec.ts`), and the build keeps fonts as files rather than `data:` URIs. `go build -tags webui` embeds `apps/web/dist`; without the tag goald embeds `apps/web/placeholder`, a page that says how to build the UI, so `go build ./...` works on a fresh clone without Node. `cmd/goald` mounts it with `mux.Handle("/", ui)`, `ui` being `web.Handler(web.Options{StrictTransport: …})`.

## Responsive and mobile

One app, not two, filling the viewport with no outer frame. From 768 px: a 72 px rail (organization mark, the organization's factory squares (its archived factories are left out), New factory, then Connections (a rust dot and "1 failing" while a connection's test fails), Knowledge and the account) and a 232 px sidebar (a factory's Work / Build / Learn, or the organization's own), both pinned to the left edge at full height, then `main`. Below 768 px: a top row with the organization mark and the factory squares, the crumb "Acme / sre", `main`, and on factory pages a bottom tab bar (Floor, Board, Items, Build, More). The Inbox drawer is a full-screen sheet below 768 px and a 460 px panel from there up. Since the sidebar takes room at every width from 768 px (`main` is 464 px wide at 768), screens lay out by the width of `main` with container queries (`@container` on `main`: `@[640px]:`, `@[720px]:`, `@[960px]:`), not viewport breakpoints; viewport breakpoints only switch the phone and desktop chrome. Cards stack, the board becomes a station picker plus a list, and every action has a 44 px target. Factory screens of later phases (Jonah) are listed in `app/screens.ts` with `available: false` and show as "Later" until the PR that adds their route flips them.

## PWA

Goal installs to a phone's home screen as its own app (standalone, no browser chrome). It starts from the cached app shell, and it updates itself after a Reload prompt (below). Push notifications come later, so approvals can arrive that way.

**Manifest.** `apps/web/public/manifest.webmanifest` is served from the root and linked from `index.html`. Name and short name "Goal", `id`, `start_url` and `scope` "/", `display` "standalone", and `background_color` and `theme_color` `#FFFFFF` (the light `--color-shell`; the dark `theme-color` meta in `index.html` is `#0E1120`). A manifest takes one colour, so dark mode keeps the media-specific `theme-color` metas in `index.html`. goald serves it as `application/manifest+json` (`apps/web/webui.go` names the type, since Go's mime table lacks `.webmanifest` and the image has no `mime.types`) and revalidates it like the favicon.

**Icons.** `icons/icon-192.png` and `icons/icon-512.png` (purpose `any`) are the favicon itself. `icons/icon-maskable-512.png` (purpose `maskable`) is the mark on a full-bleed `#1d2733` square, the bars at 60% of its height so they sit inside the central 80% safe zone. `apple-touch-icon.png` (180x180) is the same full-bleed design, written as an opaque RGB PNG: iOS rounds the corners itself and shows transparent pixels as black. All of them are rendered from `apps/web/public/favicon.svg`, and only the PNGs are committed. To make them again, from the repository root:

```sh
mkdir -p /tmp/pwa/run && cd /tmp/pwa/run && npm init -y >/dev/null && npm i @resvg/resvg-js@2
# save the script below as /tmp/pwa/run/render.mjs, then from the repository root:
cd <repository root> && node /tmp/pwa/run/render.mjs
```

```js
// render.mjs: renders the home-screen icons from apps/web/public/favicon.svg. Run from the repository root.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { deflateSync } from 'node:zlib';
import { Resvg } from '@resvg/resvg-js';

const out = 'apps/web/public';
const favicon = readFileSync(`${out}/favicon.svg`, 'utf8');
// The three bars of the mark, without the rounded square.
const bars = favicon.match(/<rect x="8"[\s\S]*<\/svg>/)[0].replace('</svg>', '');
// A full-bleed #1d2733 square with the bars scaled to 60% of its height (the bars are 14x17 units),
// inside the central 80% that a maskable icon keeps clear.
const full = (size) => {
  const scale = (size * 0.6) / 17;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}"><rect width="${size}" height="${size}" fill="#1d2733"/><g transform="translate(${size / 2} ${size / 2}) scale(${scale}) translate(-15 -14.5)">${bars}</g></svg>`;
};
const render = (svg, size) => new Resvg(svg, { fitTo: { mode: 'width', value: size } }).render();

mkdirSync(`${out}/icons`, { recursive: true });
writeFileSync(`${out}/icons/icon-192.png`, render(favicon, 192).asPng());
writeFileSync(`${out}/icons/icon-512.png`, render(favicon, 512).asPng());
writeFileSync(`${out}/icons/icon-maskable-512.png`, render(full(512), 512).asPng());

// apple-touch-icon.png is opaque: iOS shows transparent pixels as black, so it is written as RGB.
const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crc = (buf) => {
  let c = 0xffffffff;
  for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
};
const chunk = (type, data) => {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type), data]);
  const sum = Buffer.alloc(4);
  sum.writeUInt32BE(crc(body));
  return Buffer.concat([len, body, sum]);
};
const { width, height, pixels } = render(full(180), 180);
const raw = Buffer.alloc((width * 3 + 1) * height);
for (let y = 0; y < height; y++) {
  for (let x = 0; x < width; x++) {
    const s = (y * width + x) * 4;
    const d = y * (width * 3 + 1) + 1 + x * 3;
    raw[d] = pixels[s];
    raw[d + 1] = pixels[s + 1];
    raw[d + 2] = pixels[s + 2];
  }
}
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(width, 0);
ihdr.writeUInt32BE(height, 4);
ihdr[8] = 8; // bit depth
ihdr[9] = 2; // colour type RGB
const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
writeFileSync(
  `${out}/apple-touch-icon.png`,
  Buffer.concat([signature, chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]),
);
```

**iOS.** `index.html` carries `apple-mobile-web-app-capable` and `mobile-web-app-capable` (yes), `apple-mobile-web-app-status-bar-style` (black-translucent) and `apple-mobile-web-app-title` (Goal), and the `apple-touch-icon` link. `viewport-fit=cover` lets the page draw under the status bar and the home indicator.

**Service worker.** `apps/web/src/sw/sw.ts` is written by hand (no Workbox, no plugin: no new dependency). `vite build` emits it as `/sw.js` at the root of `dist/`, a self-contained classic script that shares no chunk with the app and imports nothing (a nested build in `apps/web/vite.config.ts`). goald serves it at scope `/` with `Cache-Control: no-cache`, like the other unhashed root files, and the CSP's `worker-src 'self'` allows it. `apps/web/src/pwa/register.ts` registers it from `main.tsx` in production builds only: never under `vite dev`, and never in a `GOAL_API=mock` dev server.

The worker's routing is one pure function, `routeRequest` in `apps/web/src/sw/routes.ts`, tested in `routes.test.ts`. A bypass returns before `respondWith`, so the browser goes to the network as if there were no worker:

| Request | Route | What the worker does |
| --- | --- | --- |
| any non-GET; any cross-origin request | `bypass` | nothing |
| `/v1/*`, `/api/*` (the identity service's `/api/auth/*`, OAuth callbacks included), `/mcp/*`, any navigation to them | `bypass` | nothing: never cached, never answered with the shell |
| `/sw.js`, `/manifest.webmanifest` | `bypass` | nothing |
| a same-origin navigation to any other path (`/`, `/o/acme/f/sre/board`, `/device`, `/sign-in`, `/invite/…`) | `network-first-shell` | fetches it; only when the fetch rejects (offline) answers the cached shell (`/`) |
| `/assets/*` (content-hashed, immutable) | `cache-first` | answers from the cache, fetches and caches on a miss |
| `/favicon.svg`, `/theme.js`, `/icons/*`, `/apple-touch-icon.png` | `stale-while-revalidate` | answers from the cache and refreshes it behind the answer |
| any other same-origin GET | `bypass` | nothing |

Only responses that are `ok` and of type `basic` (same origin, not opaque or redirected) are cached.

**Why the API and auth are never cached.** `/v1` answers are the signed-in user's private data, and `/api/auth/*` is the sign-in, the session and the OAuth callbacks (`/api/auth/callback/google` is a top-level navigation that sets a cookie). A cached answer would show one person's data to the next, and a shell answered for `/api/auth/*` would break the sign-in redirect and the device-code page. So the worker never reads or writes those paths, and offline use of API data does not exist: the app opens offline, and its data needs the network.

**Cache versioning.** The cache is `goal-shell-<build id>`. The build id is a 16-hex-digit hash of every file the build writes (the assets carry their own content hashes, so a change to any file changes it), injected into the worker as `__BUILD_ID__`. `install` precaches `/` (the shell, fetched with `cache: 'reload'`) and does not call `skipWaiting`, so a new build waits. `activate` deletes every other `goal-shell-*` cache, then calls `clients.claim()`. The old build's cache stays until the new worker activates, so an open page keeps running on the build it loaded.

**Update prompt.** On load and on every `visibilitychange` to visible, the page calls `registration.update()`, which is how a phone that keeps the app open finds a new build. When a new worker is waiting (or has just finished installing under a controlling worker), a sonner toast says "A new version of Goal is ready" with a **Reload** action, through the app's `Toaster` in `__root.tsx`. Reload posts `{type: 'SKIP_WAITING'}` to the waiting worker; the worker activates, claims the page, and the page reloads once on `controllerchange`. The first worker a page gets (a first visit) is not an update and reloads nothing.

**Checking it by hand.** Build A, serve it (goald or `pnpm --dir apps/web preview`), open the app in Chrome, and install or add it to the home screen. Build B, deploy it, then open the app again: it shows the toast; tap Reload; the page reloads and runs build B (its `goal-shell-` cache name changes in DevTools › Application › Cache storage).

**Safe areas.** With `black-translucent` the page is drawn under the status bar, so the phone top row pads its top by `env(safe-area-inset-top)` on top of its 8 px (`pt-[calc(8px_+_env(safe-area-inset-top))]`), and the shell pads its left and right by `env(safe-area-inset-left)` and `env(safe-area-inset-right)` for landscape. The bottom tab bar, the sheet and the wizard footer pad their bottoms by `env(safe-area-inset-bottom)`. In a browser tab every inset is 0, so nothing moves there. Use `calc()` with spaces around `+` (`calc(8px_+_env(...))`): without them the declaration is invalid CSS and is dropped.

The manifest and icons are checked by `apps/web/src/pwa/manifest.test.ts`, which reads the PNG sizes from their headers, and `apps/web/webui_test.go` checks the manifest's content type.

## Hosting

| Target | How |
| --- | --- |
| Laptop | `apps/web/dist` is embedded into `goald` with `go:embed`; one binary serves UI and API |
| AWS | `goald` serves it, or S3 + CloudFront with `/v1/*` routed to the ALB |
| Vercel | Static deploy of `apps/web/`, same-origin: rewrites send `/v1/*` and `/api/auth/*` to goald on AWS (phase 3, ADR 0007; no cross-site deployment); preview deployments per pull request run on the mock API and never sign in |

The UI holds no secrets. With sign-in on it signs in through goald's `/api/auth` (Better Auth, an httpOnly session cookie); on a laptop with `GOAL_AUTH=none` goald runs in single-user mode.

## Quality gates

- Biome for lint and format; `tsc --noEmit` for types; dependency-cruiser for boundaries (with `packages/`).
- The Biome config and the tsconfig bases come from `packages/config` (`@goal/config`). Each project's `biome.json` and `tsconfig.json` extends one of them and keeps only its own settings: `web`, `auth`, `worker`, `cloudflare` and `api-client` each extend the variant that matches their runtime. `api-client` has no Biome config, so it extends only the tsconfig base.
- Vitest for logic with real behavior (the trace built from events, which decisions an item allows); Testing Library for components when they carry such logic. No snapshot tests of markup.
- Playwright for the critical flows at 375, 768 and 1440 px, with axe accessibility checks.
- Budgets: initial JS under 250 KB gzipped; routes are code-split. Views loaded on first use go through `lazyRetrying` (`apps/web/src/shared/lazy.ts`), not bare `React.lazy`, which keeps a chunk that failed to arrive failed until the page reloads.

# Live UI audit: goal.byfrederick.work (2026-11-01, production d98930f7)

Walked signed in as the owner in Chrome at 1481×812. Every route below renders; what follows is how it looks and where it differs from the canvas (V2) and from good craft.

## What is live

| Route | What's there | Slop and craft problems | Gap vs canvas V2 |
|---|---|---|---|
| Home `/o/goal` | Greeting "Good evening, Freddy", factories row with a mini navy line, needs-you card, spend card | Greeting header is filler; navy mini-line panel inside a white page; cobalt everywhere | Matches V2 mostly |
| Floor `/f/software` | Navy andon panel "The line, now", six KPI tiles (one empty: "Spend today" blank), Needs you, Where work waits, Throughput by week, Activity, Pull the andon cord, time window | Navy hero panel; 6 tiles with a blank one; striped bars; activity icon tiles; violet "Open" chip | Matches V2 |
| Board `/board` | Filters bar (search, Board/List, Work type, Lane, Assembly, Agent role), expedite strip, six column headers with WIP meters, cards | **Layout bug**: column bodies start ~200px below the headers and only Done has cards, so the page is a tall empty grid with cards stacked far right; no Ready/In progress split visible; no assembly lines on cards | V2 has Ready/In progress under Build and assembly rows |
| Items `/items` | Saved views (Open, Waiting on you, Expedite, Done this week, + Save view), filters, grouped by assembly table, bulk select | Dense and fine; green progress bars and orange "Expedite" pills compete | Matches V2 |
| Item `/items/364` | Stepper (Inbox → Done), Details, Cost, Rules this run follows, Human touches, tabs (Timeline, Runs, Changes, Checks, Tests, Questions), tool-call rows, answered question, Watch live | Long raw tool JSON lines; red "failed" pills; question answer box is plain | V2 has the note composer and overlaps; Trace/Teardown not built |
| Needs you `/needs-you` | Only an empty state ("Nothing waits on you right now") in a small card | Empty state is a lone boxed card top-left with an icon tile | V2 list+detail exists only when items wait; not verified with data |
| Runs `/runs` | Four KPI tiles, status filter pills, search, empty state card, factory and date filters, Export CSV | Tiles with "—"; empty state card floating | Matches V2 |
| Connections `/connections` | Three KPI tiles, grouped tables (Code hosts, MCP servers, Model providers), "How runs get connections" explanatory paragraph block, Recent activity | Explanatory paragraphs on a working screen | V2 matches |
| Credentials `/credentials` | **Page title at ~32px** (other pages 20–26px), green "encrypted" banner, empty state | Inconsistent title size; green banner for a non-event | V2 has the "New" menu and import |
| Knowledge `/knowledge` | Sources table, Look something up, Where it was used (blue bars) | Fine; long source path overflows | Matches V2 |
| Agents `/agents` | Factory defaults strip, the line as nodes, agent cards per station with model chips and stats | **Blank white page for ~3s on first load** (no skeleton) | Matches V2 |
| Intake `/intake` | Where items came from bar, Triggers table (empty), Suggested triggers in tabs | Fine | Trigger editor (When/Only if/Create/Limits) not reachable with no triggers |
| Webhooks `/intake/webhooks` | Empty state | Floating boxed empty state | V2 shows endpoints + deliveries |
| Jonah `/jonah` | Tabs (Overview, Proposals 3, Judges, Evals, Bake-offs), "Jonah has nothing to review yet", How Jonah works here, Pass rate by judge, Look for improvements (disabled lilac) | Disabled lilac button; explanatory side panel | V2 overview has proposals list, judge chart |
| Definition `/definition` | Natural-language "Ask the factory to change" bar, Form/YAML, Versions, Review changes, Publish, a **five-step wizard header** (Basics, Line, Agents, Connections, Review and publish) with a navy selected step, Line editor rows | Navy selected tab; wizard look for an editor | **V2 designs a file tree + tabs + YAML editor + problems panel; live is the older wizard form** |
| Factory settings `/settings` | General, People with access, Danger zone | Short page | **V2 has budgets, targets, models, triggers, notifications, archive (2450px)**; most sections missing |
| Org settings `/o/goal/settings` | General, Slots, Sign-in, Data, Danger zone, lilac Save | Lilac disabled save | Matches V2 |
| Members `/members` | Members table, Invite link, What each role can do | Big 32px title again | Matches V2 |
| Spend `/spend` | Window segmented, 4 tiles with sparkline, Spend by day, By agent, By model, Budgets, Who pays for models | Many empty charts | Matches V2 |
| Models `/models` | Models agents may use (toggles), Tool policy, Harness, Runtime sizes, Commands, Where agents connect | Lilac toggles | Matches V2 |
| API keys `/api-keys` | Empty state, From your terminal (navy code block), Goal for other agents, Member API tokens | Navy code block | Matches V2 |
| Account `/account` | Profile, Appearance (three swatches), Notifications matrix, Personal tokens, Connected apps, Sessions; side nav | Fine | Matches V2 |
| Menus | Org menu (orgs, Create organization, Members, Spend, Models, API keys, Settings); account menu (theme Auto/Light/Dark, Profile, Notifications, Personal tokens, Connected apps, Keyboard shortcuts, Sign out, Sign out everywhere) | Fine | Match V2 |
| New item | Centered modal: Factory, Title, Details, Create item | Appears instantly, no backdrop fade | V2 composer matches |
| Search ⌘K | Sidebar top row becomes an input, sidebar body blanks out | Whole sidebar flashes empty | V2 shows grouped results |

## Motion today

None beyond the running-dot pulse and a first-load progress line. Menus, the New item modal, the search takeover, route changes, tab switches, the stepper, toasts and the sidebar collapse all change in a single frame. Slow routes (Agents) show a blank page instead of a skeleton.

## Gaps the redesign must close (no screen left behind)

1. **Board layout bug**: columns must fill from the header down; Ready/In progress under Build; assembly lines on cards.
2. **Definition editor**: build the V2 file tree + tabs + YAML/Form editor + problems panel, keeping the live "Ask the factory to change" bar, Versions and the guided sections as a Form view.
3. **Factory settings**: budgets, targets, models, notifications and archive sections from V2.
4. **Needs you**: list + detail with Approve / Edit then approve / Deny, verified with real waiting items.
5. **Credentials**: the New menu (single or import many); fix the title size.
6. **Webhooks and Trigger editor**: the endpoints/deliveries view and the trigger editor reachable from the empty state.
7. **Jonah overview**: proposals list and the judge pass-rate chart.
8. **Item**: note composer, overlaps and collapsed tool-call rows (raw JSON behind a disclosure).
9. **Loading**: every route shows a skeleton within 100ms; no blank pages.
10. **Consistency**: one page-title size, no explanatory paragraphs on working screens, no lilac disabled buttons, no navy panels or code blocks.
11. **Motion**: everything in MOTION.md.

## Code-level gaps (apps/web vs the V2 exports)

Built but still marked "Not available yet" although the page exists (wire them up):
- Org menu **Spend and billing** (desktop and phone) → `/o/$org/spend`.
- Account menu **Notifications** → `/account#notifications`.
- Rail **New item (+)** → the composer.

Missing UI (build in the redesign):
- **Factory settings**: Repository (base branch), Runtime (box size, Add a tool), Budgets and targets, Flow (finish what started / stop at once; pull when the constraint has room / as soon as it arrives; take turns / side by side; add a type).
- **Board**: "Show N more" overflow per column; the layout bug above.
- **Keyboard shortcuts**: a `?` overlay listing every shortcut (the account menu entry exists).
- **OAuth consent** `/oauth/consent` (phase 6; design it, build later).
- **Sign-up**: "Create your account" title for a new address.
- **Intake, new trigger**: "Or start from a suggestion" block.
- **Agent**: "Reset to default" on the prompt.
- **Credentials**: count chips on the kind filters.
- **Org settings**: field help text and "Type goal to confirm" on delete.
- **Connection add**: "Test again".

Implemented but not on the canvas (the redesign adds screens for them):
connection detail; knowledge source detail and new source; accept invitation (signed in and out); no-organization state; bake-off list; new factory start (template, clone, import); definition sections Basics, Line, Agents, Connections, Versions and their conflict, discard and unsaved-changes dialogs; assembly page; trace view; Pending, Error and Not found states; archived-factory and view-only banners; create organization dialog; the Docs link in the sidebar.

# Screens: design system v3 "Plain"

Each screen is here twice. `<screen>.jpg` is what it looks like. `<screen>.html` is its exact source: layout, copy, sizes and colours, which open in a browser. They are exported from the owner's Goal UI canvas, and [ADR 0016](../../adr/0016-ui-revamp.md) makes them binding. [MASTER.md](../MASTER.md) holds the tokens, components and vocabulary they share, and [MOTION.md](../MOTION.md) how each of them moves. Version 3 (2026-11-01) restyled every screen and added the ones below marked **v3**; [the live audit](../live-audit-2026-11-01.md) lists what the live app was missing.

`design-system/` shows the foundations, the components and the motion demos (`motion.html` is interactive: open it in a browser and press each demo's button).

How to use them:
- Build the screen to match the picture. Read the HTML for exact values, then map each one to the MASTER.md token or component it stands for. A hex value in the HTML is a token's value, so use the token name in code.
- The HTML uses invented sample data (the Software factory, items #135–#156). Real screens show real data in the same places.
- Desktop pictures are 1440px wide, and phone pictures 390px. Every desktop screen also works at 375 and 768px, following MASTER.md's phone rules and the `mobile/` screens.
- The canvas is changed first, then exported here. Only the owner changes these files.

"Status" says what exists today:
- **restyle**: the screen exists and changes to match.
- **new**: there is no screen yet, but its data exists.
- **new feature**: the backend it needs is not built yet.

"Not built yet" lists the controls whose feature comes later. They render disabled with the tooltip "Not available yet" (ADR 0016, decision 4).

## Sidebar and account (`account/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `shell`: sidebar expanded and collapsed, search, organization menu, account menu | the layout of every page (`/o/$org`) | restyle (replaces the rail and the factory sidebar) |  |
| `account`: profile, theme, notifications, tokens, connected apps, sessions | `/account` (new route; today in the account menu) | new | Notification routing for Slack and email, the daily digest, the email for notifications, connected apps (no endpoint yet), the session list with per-session sign-out (goald's /api/auth does not forward list-sessions or revoke-session) |
| `consent`: allow an app (OAuth), approve a CLI sign-in | `/device`, `/oauth/consent` | restyle (device), new feature (OAuth consent, phase 6) | |

## Get started (`get-started/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `sign-up`, `code` | `/sign-in`, `/sign-in/code` | restyle; sign-up is the same flow for a new address | GitHub sign-in (its button is disabled) |
| `org` | onboarding step 1 | new (create organization exists for platform admins) | |
| `repository`: a repository URL and a token, tested for clone and push, saved as a secret (no app to install) | step 2 | new ([ADR 0037](../../adr/0037-secrets-and-environment-files.md) decision 7; replaces the GitHub app flow) | |
| `template`, `line` | steps 3 and 4 | restyle of the new-factory wizard (`/o/$org/new/$step`) | quality-first or cost-first model preset |
| `budget` | step 5 | new (budgets exist in the definition) | |
| `triggers` | step 6 | new feature (needs Intake triggers) | |
| `invite` | step 7 | new (invitations exist) | |
| `first-item` | step 8 | new (a ready-made first item) | |
| `ready`: the new factory's Floor with a setup checklist | `/o/$org/f/$factory` | new | |

## Daily work (`daily-work/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `home`: every factory as a mini line, needs you, spend | `/o/$org` | restyle of the factories home | |
| `floor`: the line with the andon strip, four flow figures in a stat strip, needs you, where work waits, throughput, activity | `/o/$org/f/$factory` | restyle |  |
| `board`: columns with WIP meters, expedite lane, Ready and In progress, assembly lines | `…/board` | restyle | |
| `item`: stepper, the agent's question with quick answers, run timeline, note composer, details, cost, overlaps, rules | `…/items/$ref` | restyle | |
| `trace`: the item's Trace tab, every run's steps on a time axis with the selected step's detail (ADR 0019 decision 2; Watch live opens it on the running run) | `…/items/$ref?tab=trace` | new view of data the item page loads | |
| `teardown` (+ `teardown-768`, `teardown-dark`, `teardown-loading`, `teardown-empty`, `teardown-error`): the item's Teardown tab, lead time, cost, rework and human touches against the target and budget; time by station; runs; why it went back; human touches (ADR 0019 decision 3) | `…/items/$ref?tab=teardown` | built (`teardown-tab.tsx`, from `getItemTeardown`) | |
| `items`: saved views, filters, grouped by assembly, bulk actions | `…/items` | restyle |  |
| `needs-you`: list across factories by type, detail with Approve, Deny, "Edit, then approve" | `/o/$org/needs-you` (new page; replaces the inbox drawer) | new | Always allow |
| `runs`: every run across factories | `/o/$org/runs` | new (runs exist in the API) | Send to Jonah |

## Intake (`intake/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `intake`: where items came from, triggers table, suggested triggers | `…/intake` | new feature (triggers) | |
| `intake-new`: choose a source and event | `…/intake/new` | new feature | Slack, Linear, Jira, Sentry sources |
| `trigger`: When, Only if, Create, Limits, recent events, the YAML | `…/intake/$trigger` | new feature | |
| `webhooks`: endpoints, signing secret, deliveries | `…/intake/webhooks` | new feature | Send a sample delivery (no API) |

## Build a factory (`build/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `agents`: the line with each station's agents, factory defaults | `…/agents` | new (agents exist) | New agent |
| `agent`: overview, prompt, tools and access, runs | `…/agents/$agent` | restyle | Bake off this agent |
| `definition`: files, tabs, YAML editor, problems, outline, draft banner | `…/definition` | restyle (Monaco and the sections exist) | quick fixes |
| `definition-publish`: changes by section, checks, versions | `…/definition/publish` | restyle | evals before publish |
| `factory-settings`: repository and its token, model key, environments, notifications | `…/settings` | restyle | per-factory roles |
| `definition-environments` (+ `definition-environments-768`, the deploy environment): Form view, Environments section. A tab per environment and who uses it; the box (size, image, setup), command-line tools, variables (plain or a secret), files, AWS, MCP servers with tool modes, standing rules and tests; checks, including a key pasted in plain text | `…/definition/environments` | new feature ([ADR 0037](../../adr/0037-secrets-and-environment-files.md)) | Sign in and 1Password sources |
| `definition-environment-yaml`: the same environment as `environments/default.yaml` in the YAML view, with the plain-key problem | `…/definition` | new feature (ADR 0037 decision 5) | |
| `add-tool` (+ `add-tool-768`): the catalog of MCP servers, CLIs and APIs; pick or create the secret in place, the server's answer, what agents may do, and the lines added to the draft | `…/definition/add-tool` | new feature (ADR 0037 decision 15) | |
| `add-tool-custom`: Custom, a command-line tool with a variable and a file | `…/definition/add-tool?custom` | new feature (ADR 0037 decision 15) | |

## Jonah (`jonah/`)

Every Jonah screen is a **new feature** (phase 5, [ADR 0002](../../adr/0002-jonah-evals.md)): `jonah` (overview), `proposal`, `judges`, `judge`, `evals`, `bake-off`, under `…/jonah/…`.

## Access, secrets and knowledge (`connections/`)

[ADR 0037](../../adr/0037-secrets-and-environment-files.md) replaced the connection and credential screens with these.

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `access` (+ `access-768`, a list): what every factory's environments reach, by what they reach, by factory or by secret; the key each uses and its status; what needs attention | `/o/$org/access` (replaces `/connections`) | new feature (ADR 0037 decisions 14 and 16) | |
| `secrets` (+ `secrets-768`): every secret with where it is used, its factories, expiry and last use; New secret in a side sheet (name, where the value comes from, value, factories, expiry and reminder) | `/o/$org/secrets` (replaces `/credentials`) | new feature (credentials exist; ADR 0037 decisions 1 to 4) | Sign in and 1Password sources |
| `secret`: where it is used, activity, Replace value, settings, Delete (disabled while used) | `/o/$org/secrets/$secret` | new feature | |
| `secrets-import`: paste a .env or YAML, the dry run with Replace or Skip, factories to open them to | `/o/$org/secrets/import` | new feature (the import API exists for credentials) | |
| `knowledge` | `/o/$org/knowledge` | restyle |  |

## Organization (`organization/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `members` | `/o/$org/members` | restyle | restricting sign-up to a domain |
| `spend`: spend by factory, agent and model; budgets; who pays for models | `/o/$org/spend` | new (budgets and costs exist) | routers |
| `models`: models agents may use, harness, tool policy (Use secrets), box sizes, egress | `/o/$org/models` | new (model keys come from Secrets) | routers, other harnesses |
| `api-keys` | `/o/$org/api-keys` | restyle | the Goal MCP server address |
| `org-settings` | `/o/$org/settings` | restyle | single sign-on, your own bucket |

## Mobile (`mobile/`)

`sign-in`, `drawer`, `needs-you`, `approve`, `floor`, `board`, `item`, `intake`, `teardown` (+ its loading, empty and error states; the item tabs scroll sideways and Runs becomes a list), and for ADR 0037 `access`, `secrets`, `secret-new`, `environments` and `add-tool`: the phone layouts at 390px. Every other screen follows MASTER.md's phone rules: a top bar with the Goal mark, a drawer, a bottom tab bar, one column, and tables that scroll in their own box.

## Added in v3: live pages that had no design

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `daily-work/assembly` | the assembly page (`features/items/assembly-page.tsx`) | restyle | |
| `daily-work/trace` | an item's Trace tab | built (`trace-tab.tsx`) | |
| `build/new-factory` | `/o/$org/new/start`: template, clone, import | restyle | |
| `build/definition-form` | `…/definition/$section` in Form view, with the unsaved-changes dialog | restyle | |
| `jonah/bake-offs` | `…/jonah/bake-offs` empty state | restyle | |
| `connections/knowledge-source`, `connections/knowledge-new` | `/o/$org/knowledge/$source`, `…/knowledge/new` | restyle (a synced source reads with a secret, ADR 0037) | |
| `account/shortcuts` | the `?` keyboard-shortcuts dialog | new | |
| `account/states` | Pending (skeleton), Error, Not found; archived and view-only banners (`app/states.tsx`) | restyle | |
| `account/no-org` | `/` with no organization, and Create organization | restyle | |
| `account/invite-accept` | `/invite/$invitation`, signed out and in | restyle | |
| `account/oauth-consent` | `/oauth/consent` | new feature (phase 6) | Everything |

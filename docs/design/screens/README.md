# Screens: the UI revamp's reference

Each screen of the revamp is here twice. `<screen>.jpg` is what it looks like. `<screen>.html` is its exact source: layout, copy, sizes and colours, which open in a browser. They are exported from the Goal UI canvas (the owner's design artifact, pages "Revamp"), and [ADR 0016](../../adr/0016-ui-revamp.md) makes them binding. [MASTER.md](../MASTER.md) holds the tokens, components and vocabulary they share.

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
| `github` | step 2 | new (uses the github connection kind) | GitLab |
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
| `floor`: the andon panel, six KPI tiles, needs you, where work waits, throughput, activity | `/o/$org/f/$factory` | restyle |  |
| `board`: columns with WIP meters, expedite lane, Ready and In progress, assembly lines | `…/board` | restyle | |
| `item`: stepper, the agent's question with quick answers, run timeline, note composer, details, cost, overlaps, rules | `…/items/$ref` | restyle | Watch live, Trace, Teardown |
| `items`: saved views, filters, grouped by assembly, bulk actions | `…/items` | restyle | Save view, Set lane |
| `needs-you`: list across factories by type, detail with Approve, Deny, "Edit, then approve" | `/o/$org/needs-you` (new page; replaces the inbox drawer) | new | Always allow |
| `runs`: every run across factories | `/o/$org/runs` | new (runs exist in the API) | Export CSV, Send to Jonah |

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
| `agent`: overview, prompt, tools and access, runs | `…/agents/$agent` | restyle | Bake off this agent, Ask Jonah for a fix |
| `definition`: files, tabs, YAML editor, problems, outline, draft banner | `…/definition` | restyle (Monaco and the sections exist) | Ask the factory to change it, quick fixes |
| `definition-publish`: changes by section, checks, versions | `…/definition/publish` | restyle | evals before publish |
| `factory-settings` | `…/settings` | restyle | per-factory roles |

## Jonah (`jonah/`)

Every Jonah screen is a **new feature** (phase 5, [ADR 0002](../../adr/0002-jonah-evals.md)): `jonah` (overview), `proposal`, `judges`, `judge`, `evals`, `bake-off`, under `…/jonah/…`.

## Connections and knowledge (`connections/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `connections`: grouped by code hosts, apps, MCP servers and model providers | `/o/$org/connections` | restyle | writes waiting for approval |
| `connection-add`: catalog and details, tool permissions, factories, test | `/o/$org/connections/new` | restyle | the per-tool "Ask first" setting |
| `credentials`, `credential-new` | `/o/$org/credentials` | new feature: today secrets live in connection fields. The spec decides how credentials relate to connections. | encryption in the browser |
| `knowledge` | `/o/$org/knowledge` | restyle | verdicts on whether knowledge helped or misled, "where it was used" |

## Organization (`organization/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `members` | `/o/$org/members` | restyle | restricting sign-up to a domain |
| `spend`: spend by factory, agent and model; budgets; who pays for models | `/o/$org/spend` | new (budgets and costs exist) | Export CSV, routers |
| `models`: models agents may use, harness, tool policy, box sizes, egress | `/o/$org/models` | new (model connections exist) | routers, other harnesses |
| `api-keys` | `/o/$org/api-keys` | restyle | the Goal MCP server address |
| `org-settings` | `/o/$org/settings` | restyle | single sign-on, your own bucket |

## Mobile (`mobile/`)

`sign-in`, `drawer`, `needs-you`, `approve`, `floor`, `board`, `item`, `intake`: the phone layouts at 390px. Every other screen follows MASTER.md's phone rules: a top bar with the Goal mark, a drawer, a bottom tab bar, one column, and tables that scroll in their own box.

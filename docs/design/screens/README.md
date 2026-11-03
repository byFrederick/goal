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
| `template` | step 2, before Code ([ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | restyle of the new-factory wizard (`/o/$org/new/$step`) | |
| `repository`: Code as a choice of no code, read only, or read and write; then a whole organization or some repositories (one or more) and a token, tested for what the choice needs; one form for every case, saved as a secret open to the new factory (no app to install) ([ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | step 3, after the factory | new ([ADR 0037](../../adr/0037-secrets-and-environment-files.md) decision 7; [ADR 0041](../../adr/0041-code-and-github-without-an-app.md) decision 9) | |
| `line`: the stations scroll sideways, so a line of any length fits | step 4 | restyle of the new-factory wizard (`/o/$org/new/$step`) | quality-first or cost-first model preset |
| `budget` | step 5 | new (budgets exist in the definition) | |
| `triggers`: the template's disabled triggers to turn on, the GitHub webhook to set up, and `goal run` ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | step 6 | new feature (needs Intake triggers) | |
| `invite` | step 7 | new (invitations exist) | |
| `first-item` | step 8 | new (a ready-made first item) | |
| `ready`: the new factory's Floor with a setup checklist, and `goal run` as a way in | `/o/$org/f/$factory` | new | |

## Daily work (`daily-work/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `home`: every factory as a mini line, needs you, spend | `/o/$org` | restyle of the factories home | |
| `floor`: the line with the andon strip, four flow figures in a stat strip, needs you, where work waits, throughput, activity | `/o/$org/f/$factory` | restyle |  |
| `board`: columns with WIP meters, expedite lane, Ready and In progress, assembly lines; items that haven't started grouped under Later in the first column ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/board` | restyle | Later |
| `item`: stepper, the agent's question with quick answers, run timeline, note composer, details, cost, overlaps, rules | `…/items/$ref` | restyle | |
| `item-follow-ups`: a finished part's Follow-ups card (notes delivered, items filed when the work lands, one of them into another factory, those for the architect) with Drop, Notes from other items, and Came from ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/items/$ref` | new feature | Follow-ups, notes from other items |
| `item-later`: an item that hasn't started, with Start now and Change ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/items/$ref` | new feature | Start times |
| `item-incident`: an SRE item filed by a check from a PagerDuty incident; the investigator, which only reads, opened two repositories as it worked, read a third through tools, and Fix comes next, where the fixer writes ([ADR 0040](../../adr/0040-one-door-for-every-source.md), [ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | `…/items/$ref` | new feature | Repositories per item, write per agent |
| `new-item`: New item with Repositories (any number, or none) and Start: Now, Later or After items ([ADR 0040](../../adr/0040-one-door-for-every-source.md), [ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | dialog from New item | restyle | Repositories, Later, After items |
| `trace`: the item's Trace tab, every run's steps on a time axis with the selected step's detail (ADR 0019 decision 2; Watch live opens it on the running run) | `…/items/$ref?tab=trace` | new view of data the item page loads | |
| `teardown` (+ `teardown-768`, `teardown-dark`, `teardown-loading`, `teardown-empty`, `teardown-error`): the item's Teardown tab, lead time, cost, rework and human touches against the target and budget; time by station; runs; why it went back; human touches (ADR 0019 decision 3); Keep as a test, for a station it passed the first time ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 12) | `…/items/$ref?tab=teardown` | built (`teardown-tab.tsx`, from `getItemTeardown`) | Keep as a test |
| `items`: saved views, filters, grouped by assembly, bulk actions; "Starts …" for items that haven't started ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/items` | restyle |  |
| `needs-you`: list across factories by type, detail with Approve, Deny, "Edit, then approve"; a paused triage item from untrusted work ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `/o/$org/needs-you` (new page; replaces the inbox drawer) | new | Always allow |
| `runs`: every run across factories | `/o/$org/runs` | new (runs exist in the API) | |

## Intake (`intake/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `intake` (+ `intake-768`): Coming up, Where items came from by origin, Follow-ups to triage, Triggers (a check among them) with the definition's disabled ones as suggestions, and the organization's webhooks open to the factory ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/intake` | new feature (triggers) | Coming up, origins, follow-ups, checks |
| `intake-new`: choose a schedule, Check with an agent (any source the factory's tools reach, Slack, Linear and Jira among them), or one of the organization's webhooks open to the factory, then any of its sender's events; suggestions from the definition; Continue opens a draft ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/intake/new` | new feature | Checks, Sentry sources |
| `trigger`: When (the webhook by name and any event its sender supplies, with the filters it supports), Only if, Create (with Merge without a person), Limits (per day and per hour), recent events, the YAML; no model check ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/intake/$trigger` | new feature | Try with a delivery |
| `trigger-check`: a check, New incidents in SRE: every 5 minutes the watcher agent looks at PagerDuty with its tools and decides to file, note or skip; what it remembers, its limits, and the log of checks with reasons and cost; Try a check ([ADR 0040](../../adr/0040-one-door-for-every-source.md)) | `…/intake/$trigger` | new feature | Checks |

## Build a factory (`build/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `agents`: the line with each station's agents, factory defaults | `…/agents` | new (agents exist) | New agent |
| `agent`: overview, prompt, tools and access, runs | `…/agents/$agent` | restyle | Try another model ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 13) |
| `definition`: files, tabs, YAML editor, problems, outline, draft banner | `…/definition` | restyle (Monaco and the sections exist) | quick fixes |
| `definition-publish`: changes by section, checks, kept tests of the stations it reaches, versions with their watch | `…/definition/publish` | restyle | kept tests and the watch ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decisions 12 and 14) |
| `factory-settings` (+ `factory-settings-sre`): Code (each entry with its access, test and token, and the agents that write; SRE reads a whole organization with a read-only token and its fixer writes with another), model key, environments, notifications, Jonah (`learning.jonah`) | `…/settings` | restyle; the Code section ([ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | per-factory roles; the Jonah section ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 17) |
| `definition-environments` (+ `definition-environments-768`, the deploy environment): Form view, Environments section. A tab per environment and who uses it; the box (size, image, setup), command-line tools, variables (plain or a secret), files, MCP servers with tool modes, standing rules and tests; checks, including a key pasted in plain text | `…/definition/environments` | new feature ([ADR 0037](../../adr/0037-secrets-and-environment-files.md)) | Sign in and 1Password sources |
| `definition-environment-yaml`: the same environment as `environments/default.yaml` in the YAML view, with the plain-key problem | `…/definition` | new feature (ADR 0037 decision 5) | |
| `add-tool` (+ `add-tool-768`): the catalog of MCP servers, CLIs and APIs; pick or create the secret in place, the server's answer, what agents may do, and the lines added to the draft | `…/definition/add-tool` | new feature (ADR 0037 decision 15) | |
| `add-tool-custom`: Custom, a command-line tool with a variable and a file | `…/definition/add-tool?custom` | new feature (ADR 0037 decision 15) | |
| `definition-knowledge` (+ `definition-knowledge-768`): Form view, Knowledge section. The sources the factory reads, each with who reads it (every agent or its `appliesTo`), its own `readWhen` and its size, and the `knowledge-copies-checkout` warning; factory memory (`memory.yaml`: add, edit, remove into the draft); what agents see, the prompt section as one agent gets it | `…/definition/knowledge` | new feature ([ADR 0038](../../adr/0038-knowledge-is-files.md) decisions 5, 6 and 8) | |

## Jonah (`jonah/`)

[ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) redesigned Jonah: it learns from the line, tries each proposal on past items, and watches every change. Judges, eval cases, bake-offs and Send to Jonah went with it.

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `jonah` (+ `jonah-768`): the Overview tab: proposals waiting for review; After publish, each change's number before and after, with Undo this change on Worse; what Jonah noticed; how Jonah works here | `…/jonah` | new feature ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decisions 5, 14, 15, 18) | |
| `proposal` (+ `proposal-768`): a definition proposal: its number now and its estimate, the diffs, why (the items and their reasons), Tried on past items, details; Approve and publish, Edit, Reject with a reason | `…/jonah/proposals/$number` | new feature (ADR 0034, [ADR 0039](../../adr/0039-jonah-learns-from-the-line.md) decisions 6, 10 and 11) | |
| `proposal-memory`: a factory memory proposal: facts quoted from people, with their items; no try | `…/jonah/proposals/$number` | new feature ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 7) | |
| `proposal-model`: a model proposal from Try another model: three tries per item beside each item's line result | `…/jonah/proposals/$number` | new feature ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 13) | |
| `tests`: kept tests by station, with the last result and who kept each; a failing test warns and never blocks | `…/jonah/tests` | new feature ([ADR 0039](../../adr/0039-jonah-learns-from-the-line.md), decision 12) | |

## Access, secrets and knowledge (`connections/`)

[ADR 0037](../../adr/0037-secrets-and-environment-files.md) replaced the connection and credential screens with these, and [ADR 0038](../../adr/0038-knowledge-is-files.md) the knowledge screens.

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `access` (+ `access-768`, a list): what every factory's environments reach, by what they reach, by factory or by secret; the key each uses and its status; what needs attention | `/o/$org/access` (replaces `/connections`) | new feature (ADR 0037 decisions 14 and 16) | |
| `secrets` (+ `secrets-768`): every secret with where it is used, its factories, expiry and last use; New secret in a side sheet (name, where the value comes from, value, factories, expiry and reminder) | `/o/$org/secrets` (replaces `/credentials`) | new feature (credentials exist; ADR 0037 decisions 1 to 4) | Sign in and 1Password sources |
| `secret`: where it is used, activity, Replace value, settings, Delete (disabled while used) | `/o/$org/secrets/$secret` | new feature | |
| `secrets-import`: paste a .env or YAML, the dry run with Replace or Skip, factories to open them to | `/o/$org/secrets/import` | new feature (the import API exists for credentials) | |
| `knowledge` (+ `knowledge-768`, a list): the sources with their read-when line, kind, the factories that list them, sync, size and reads; Find a file; Most read; the alert for a failing source | `/o/$org/knowledge` | new feature (ADR 0038 decisions 1 and 12) | Website, Notion and Google Drive kinds |
| `knowledge-new` (+ `knowledge-new-768`): Add a source, a repository: the kind, its settings, the name and read-when line, the factories whose drafts get the line, sync; the Check panel, what agents will see, the first files | `/o/$org/knowledge/new` | new feature (ADR 0038 decisions 2, 5 and 12) | |
| `knowledge-new-confluence`: the same for Confluence: site, account email, API token, spaces, only the pages under one page | `/o/$org/knowledge/new` | new feature | Attachments |
| `knowledge-new-upload`: the same for an upload: files and folders, PDF and Office become Markdown, a file left out | `/o/$org/knowledge/new` | new feature | |
| `knowledge-source`: its files with their reads, the latest sync, settings, the line agents see, who reads it, Add to a factory | `/o/$org/knowledge/$source` | new feature | |

## Organization (`organization/`)

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `members` | `/o/$org/members` | restyle | restricting sign-up to a domain |
| `spend`: spend by factory, agent and model; budgets; who pays for models | `/o/$org/spend` | new (budgets and costs exist) | routers |
| `models`: models agents may use, harness, tool policy (Use secrets), box sizes, egress | `/o/$org/models` | new (model keys come from Secrets) | routers, other harnesses |
| `api-keys` | `/o/$org/api-keys` | restyle | the Goal MCP server address |
| `webhooks`, `webhook` and `webhook-new`: the organization's webhooks, each with its sender (GitHub, GitLab, PagerDuty, Sentry, Standard Webhooks, a token in a header), address, secret by name and the factories it's open to; a webhook's setup steps, GitHub's ping and the delivery log with what each delivery led to in each factory ([ADR 0040](../../adr/0040-one-door-for-every-source.md), [ADR 0041](../../adr/0041-code-and-github-without-an-app.md)) | `/o/$org/webhooks`, `/o/$org/webhooks/$webhook`, `/o/$org/webhooks/new` | new feature | `POST /hooks/{webhook id}`; the sender catalog |
| `org-settings`: general, sign-in, Code hosts (github.com and gitlab.com built in, Enterprise hosts added by owners; [ADR 0041](../../adr/0041-code-and-github-without-an-app.md)), data and danger zone | `/o/$org/settings` | restyle | single sign-on, your own bucket; Code hosts |

## Mobile (`mobile/`)

`sign-in`, `drawer`, `needs-you`, `approve`, `floor`, `board`, `item`, `intake`, `teardown` (+ its loading, empty and error states; the item tabs scroll sideways and Runs becomes a list), for ADR 0037 `access`, `secrets`, `secret-new`, `environments` and `add-tool`, and for ADR 0038 `knowledge`, `knowledge-source`, `knowledge-new` (an upload) and `memory` (the definition's Knowledge section), and for ADR 0039 `jonah` and `proposal` (with Approve), for ADR 0040 `new-item` and `item-follow-ups`, and for ADR 0041 `webhooks`: the phone layouts at 390px. Every other screen follows MASTER.md's phone rules: a top bar with the Goal mark, a drawer, a bottom tab bar, one column, and tables that scroll in their own box.

## Added in v3: live pages that had no design

| Screen | Route | Status | Not built yet |
|---|---|---|---|
| `daily-work/assembly` | the assembly page (`features/items/assembly-page.tsx`) | restyle | |
| `daily-work/trace` | an item's Trace tab | built (`trace-tab.tsx`) | |
| `build/new-factory` | `/o/$org/new/start`: template, clone, import | restyle | |
| `build/definition-form` | `…/definition/$section` in Form view, with the unsaved-changes dialog | restyle | |
| `account/shortcuts` | the `?` keyboard-shortcuts dialog | new | |
| `account/states` | Pending (skeleton), Error, Not found; archived and view-only banners (`app/states.tsx`) | restyle | |
| `account/no-org` | `/` with no organization, and Create organization | restyle | |
| `account/invite-accept` | `/invite/$invitation`, signed out and in | restyle | |
| `account/oauth-consent` | `/oauth/consent` | new feature (phase 6) | Everything |

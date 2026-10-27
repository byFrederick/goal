# Goal design system

The one source for how Goal looks, reads and behaves. It replaces the "control room" system of 2026-10-23: the user approved this revamp on 2026-10-26. Every screen is exported to [screens/](screens/README.md) as a picture and its HTML, from the owner's Goal UI canvas; [ADR 0016](../adr/0016-ui-revamp.md) makes them binding.

> **LOGIC:** when building a page, check `docs/design/pages/<page>.md` first. A rule there overrides this file. Otherwise, follow this file exactly.

## Principles

- **The line is the product.** A factory's stations, their load and their agents appear as a line wherever flow matters. The Floor's andon panel is the one bold element; everything around it stays quiet.
- **Each colour has one meaning everywhere** (see Colour). Colour is never the only signal: a word, an icon or a number says the same thing.
- **Show it, don't write it.** Use a number with its target, a bar, a chip, a list or a timeline before a sentence. Insight text is one line. There are no paragraphs of explanation on working screens.
- **People act where they read.** Approve, Deny, Reply and Add sit on the row or card that asks for them. One primary action per screen.
- **Plain words, Goal's words.** Sentence case, plain verbs, and the vocabulary below. Name an agent by its role, never by a copy number.

## Colour

Tokens keep their names across themes, and components use token names only. `apps/web/src/ui/raw-colors.test.ts` fails on a raw hex, `rgb()` or `hsl()`. No gradients, no decorative shadows.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-shell` | `#FFFFFF` | `#0E1120` | Page and content background |
| `--color-sidebar` | `#F3F4F9` | `#13172A` | Sidebar and rail |
| `--color-panel` | `#F7F8FC` | `#161A2F` | Quiet panels, board cells, table hover, segmented controls |
| `--color-hover` | `#E9EBF4` | `#1E2340` | Hover on nav and icon buttons |
| `--color-card` | `#FFFFFF` | `#0E1120` | Cards (white with a rule border, no fill change) |
| `--color-line` | `#E3E6EF` | `#252A45` | Card borders, dividers |
| `--color-rule` | `#CBD0DF` | `#3A4066` | Input and button borders, dashed "new" outlines |
| `--color-ink` | `#14182B` | `#ECEEF7` | Text and headings |
| `--color-ink-soft` | `#464D6A` | `#B7BCD4` | Secondary text, nav labels (8.3:1 on white) |
| `--color-muted` | `#626986` | `#9096B3` | Meta text, captions (5.3:1 on white, 4.8:1 on the sidebar) |
| `--color-run` | `#2742F5` | `#4A63F5` | Brand, primary buttons, links, running, the selected nav (white text on it 6.6:1) |
| `--color-run-ink` | `#1B33C9` | `#A3B2FF` | Link hover; text on the run tint |
| `--color-run-tint` | `#E8ECFF` | `#1D2556` | Selected nav item, "running" chips, the selection |
| `--color-run-soft` | `#9AA6FF` | `#5B6BC9` | Secondary data bars (past weeks, other stations) |
| `--color-you` | `#7B3FE4` | `#9061F5` | Waiting on a person: counts, approve buttons in Needs you (white text 5.7:1) |
| `--color-you-tint` | `#F1EBFE` | `#2A1E4F` | Questions, selected Needs you row |
| `--color-you-ink` | `#5A24B8` | `#CDB4FF` | Text on the violet tint |
| `--color-rework` | `#D63A1F` | `#E2512F` | Sent back, andon, loss, destructive actions (white text 4.7:1) |
| `--color-rework-tint` | `#FDECE8` | `#3B1C16` | Background of sent-back and alert chips |
| `--color-rework-ink` | `#AE2C14` | `#FFA08A` | Text in red (5.7:1 on its tint) |
| `--color-amber` | `#FFC21A` | `#FFC21A` | The constraint only: its tag, ring and busy bar (ink text on it) |
| `--color-amber-tint` | `#FFF4D1` | `#3A3013` | The expedite lane and constraint callouts |
| `--color-amber-ink` | `#6E4B00` | `#FFD666` | Text on the amber tint |
| `--color-ok` | `#0FA36B` | `#22C17F` | Done and healthy marks (bars, dots; never small text) |
| `--color-ok-tint` | `#E1F5EB` | `#10301F` | "Line running", "merged" chips |
| `--color-ok-ink` | `#0A7A4F` | `#62DDA5` | Healthy text, positive deltas |
| `--color-andon` | `#121633` | `#090C1A` | The andon panel (the line, now) |
| `--color-andon-raised` | `#1C2147` | `#151A36` | Station nodes on the andon panel |
| `--color-andon-line` | `#262C58` | `#272D58` | Dividers on the andon panel |
| `--color-andon-text` | `#C3C8EA` | `#C3C8EA` | Secondary text on the andon panel (9.5:1) |

**Factory colours** are a factory's identity on its letter badge only (white letter, at least 4.5:1). A factory picks one when it is created: cobalt `#2742F5`, teal `#0E8576`, orange `#C24E17`, green `#1F7A3A`, ocean `#0B6E99`, ochre `#8A5A00`, slate `#5B6170`, indigo `#3F3AB8`. Pink is never used.

Rules:
- Running is cobalt. Waiting on you is violet. Sent back, andon and destructive are red. The constraint is yellow, and nothing else is. Done is green.
- A delta's arrow and sign carry its direction. Green or red only says whether that direction is good.
- Charts: the current period in `run`, past periods in `run-soft`, the target as a dashed `muted` line with its label, and the constraint in `amber`. Categorical series (where work came from) use the factory colours in order.
- The Goal mark is a cobalt square holding a white G that ends in a yellow node: a line arriving at its station. It is the product's sign. It sits at the top of the sidebar and on sign-in and onboarding.

## Typography

- **Archivo** (variable, `wdth` 62–125, `wght` 100–900), self-hosted with `@fontsource-variable/archivo` (the `wdth` axis file), for all text. UI text is at width 100%. Page titles are at 108%, and numbers and factory letters at 112%, which gives the factory-signage feel.
- **JetBrains Mono** (`@fontsource-variable/jetbrains-mono`) only for code, file paths, commands, YAML and tool names. It is never used for labels.
- Tabular numerals everywhere (`font-variant-numeric: tabular-nums`).
- Scale:
  - 12px: meta and captions.
  - 13–13.5px: secondary lines and tables.
  - 14px: body and controls.
  - 16px at weight 620: section headings.
  - 22px: detail titles.
  - 26px at weight 650, letter-spacing −0.015em: page titles.
  - 28px at weight 600: KPI numbers.
- Weights: 520–580 for nav and buttons, 600–650 for titles and emphasis.
- Never ALL CAPS labels, never eyebrow labels above headings, never "A · B" strings.

## Shape, space and motion

- Radii:
  - 18px for the andon panel;
  - 14px for cards, popovers, dialogs and the KPI tiles;
  - 10px for item cards and board cells;
  - 8px for buttons, inputs and nav items;
  - 6px for chips and badges;
  - 50% only for dots, avatars and station nodes.
- Spacing: a 4px base, with 8, 12, 16, 20 and 28px between blocks. Content padding is 22px 28px on desktop and 16px on phones.
- Elevation: borders, not shadows. Popovers and dialogs get one shadow (`0 20px 44px -18px` of ink at 40%).
- Motion: the only ambient motion is the pulsing dot on running things (1.6s ease). Everything else moves because someone acted (opening, expanding, confirming), at 150–250ms. `prefers-reduced-motion` turns all of it off.

## The shell

- The app fills the viewport. The sidebar is pinned left at full height, and the content sits beside it. There is no framed shell and no outer margin.
- **Sidebar** (256px; collapses to a 60px rail):
  - **Top row:**
    - The Goal mark and the organization's name open the **organization menu**:
      - the organizations, with factory count and role (organizations have no image or letter square);
      - Create organization;
      - Members and roles, Spend and billing, Models and policies, API keys, Organization settings.
    - Search (⌘K) and Collapse sit at the right of the row.
  - **New item:** a cobalt button with the shortcut C. It opens the composer with a factory picker.
  - **Organization-wide:** Home, Needs you (violet count), Runs, Connections, Credentials, Knowledge.
  - **Factories:**
    - The section label has a quiet `+` (New factory). Each factory is a collapsible group: its letter badge, its name, and a violet count (needs you) or a red count or dot (andon).
    - Only the open factory is expanded. Its pages are Floor, Board, Items, Agents, Intake, Jonah, Definition and Settings, as plain text under a thin guide line. The current page is a white chip.
  - **Bottom:** "Spend this week" with a bar (it opens Spend and billing), then the person: avatar, name, role.
    - The **account menu** holds:
      - the name with a role pill and the GitHub identity;
      - Theme (Auto, Light, Dark) inline;
      - Profile, Notifications, Personal tokens, Connected apps, Keyboard shortcuts;
      - Sign out and Sign out everywhere (red).
- **Collapsed rail:** the Goal mark, expand, New item, the organization icons, the factory badges with their counts, `+`, a spend ring and the avatar.
- **Top bar** (56px):
  - On the left, a breadcrumb starting with the factory badge (Software / Intake / Flaky test on main).
  - On the right, the page's actions, with the primary one last.
- **Search** turns the sidebar's top row into an input. Results are grouped (Items, Pages, Actions) and use the arrow keys.
- **Phones** (below 768px):
  - a top bar with the Goal mark (which opens the same sidebar as a drawer), the page title and the page's actions;
  - a bottom tab bar: Needs you, Floor, Board, Items, More.
  - Primary buttons sit above the tab bar. Hover-only controls are always visible on touch. Targets are at least 44px.

## Vocabulary

| Say | Not | Meaning |
|---|---|---|
| New item | New chat, New request | The one way a person adds work; a factory picker chooses where |
| Needs you | Inbox | What waits on a person, typed **Approval** (an agent's write outside its item), **Decision** (a gate or rework limit), **Question** (an agent asks) and **Alert** (andon, budget, a refused trigger event) |
| Intake | Automations | Where a factory's work comes from |
| Trigger | Automation | An event, schedule or webhook that creates a work item of a type at a station, with "only if" checks and limits |
| Agents | Org chart | The line with each station's agents |
| Jonah | Self-improvement | The learning loop: **Proposals** (definition changes for a person to approve), **Judges** (LLM judges that score sampled runs), **Evals** (fixed cases), **Bake-offs** (configurations compared on cost and quality) |
| Credentials | Secrets | Stored secrets that connections and agents use |
| Connections | MCPs and apps, Integrations | Code hosts, apps, MCP servers and model providers |
| Hands-off | Autonomy | The share of items merged with no human touch |
| Sent back | Rejected | A gate returned the item for rework |
| frontend-engineer | frontend-engineer-1 | Agents are named by role; copies show as slots or counts |

Keep using Floor, Board, Items, line, station, lane, WIP, buffer, expedite lane, constraint, andon, assembly, part, touch and definition, as the [glossary](../reference/glossary.md) defines them.

## Components

- **Buttons:** 34px tall (30px small).
  - Primary: cobalt fill with white text.
  - Secondary: white with a rule border.
  - Danger: red text, or a red fill for the final confirmation.
  - Approve inside Needs you: violet fill.
  - Icon buttons: 32px, with an `aria-label`.
- **Chips:** 22px tall (28px large), 6px radius, a tint fill and ink text. Neutral chips are panel-coloured with a line border.
  - Status chips have a dot: running (cobalt, pulsing), needs you (violet), sent back (red), constraint and expedite (yellow), done (green).
- **Segmented control:** a panel track with a white raised selected segment. Used for time windows, views and filters with counts.
- **KPI tile:**
  - a label, the number at 28px with its unit at 14px;
  - a bar against the target, or a sparkline;
  - a meta line: the target on the left, the signed delta on the right.
  - Six tiles in a row on the Floor.
- **Andon panel:** a navy panel, "The line, now".
  - Each station is a 54px node on a cobalt track. Its number is the items at the station.
  - The constraint gets a yellow ring and a Constraint tag; Done gets a check.
  - Under each node: the name, what waits, the agent slots (14px squares: cobalt for working, violet for waiting on you, an outline for free), and its busy share.
  - A footer gives in progress, release, the expedite lane and the andon state.
- **Board column:** a header card with the name, "n of WIP", a segmented WIP meter, the agent count and busy share. The constraint's header has a yellow border.
  - The expedite lane is a yellow row above the standard lane.
  - Build splits into Ready (buffer) and In progress.
- **Item card:**
  - the ref, the work type and lane chips, the age;
  - the title, clamped to two lines;
  - the assembly line ("Usage and budgets", part pips, "2 of 4");
  - the agent (role) with the cost;
  - one state line or chip.
  - A card waiting on you has a violet ring.
- **Run timeline:**
  - Each run is a card: the station chip, the agent and the outcome chip, then duration, turns and cost.
  - Events are rows: time, a 22px icon tile, the text and a meta column on the right. Red and green tiles mark failed and passed checks; violet marks people.
- **Needs you list:**
  - Each row has the factory badge, the type in its colour, the title, the item and station, and the age. The selected row is violet-tinted with a 3px violet edge.
  - The detail shows exactly what will happen (the comment text, the tool, where, why it asks).
  - The action bar holds Approve (A), "Edit, then approve", Deny (D), and an optional standing approval.
- **Sentence builder** (triggers): rows of inline token buttons.
  - "When [GitHub] [a check run fails] on [main ×] in [repo ×]";
  - then "Only if", "Create" and "Limits" sections;
  - template variables as violet mono tokens.
- **Two-pane picker** (connections, triggers, credentials): a searchable catalog on the left, the chosen item's details and its factory scope on the right. On phones it is one pane with Back.
- **Definition editor:** a file tree, tabs (a preview tab in italics), a YAML editor with schema errors and a problems panel, a Form/YAML switch, and a draft banner with Review changes and Publish.
- **Stepper** (an item's progress, onboarding): circles joined by a track. Done ones are cobalt with a check; the current one has a violet ring when it waits on you.
- **Empty states:** a line icon, one sentence that says what to do, and one action.

## Screens

The screens, one folder per flow in [screens/](screens/README.md):

- **Get started:** sign up, the email code, then eight steps:
  1. name the organization;
  2. connect GitHub and choose repositories;
  3. the first factory from a template;
  4. shape the line;
  5. budget and targets;
  6. starter triggers;
  7. invite people;
  8. the first item, running end to end.

  The flow ends on the new factory's Floor with a setup checklist.
- **Daily work:** Home (every factory), Floor, Board, Work item, Needs you, Items, Runs.
- **Intake:** Intake, New trigger, Trigger, Webhooks.
- **Build a factory:** Agents, Agent, Definition, Review and publish, Factory settings.
- **Jonah:** Overview, Proposal, Judges, Judge, Evals, Bake-off.
- **Connections and knowledge:** Connections, Add a connection, Credentials, New credential, Knowledge.
- **Organization:** Members and roles, Spend and billing, Models and policies, API keys, Organization settings.
- **Sidebar and account:** the sidebar states (collapsed, search, organization menu, account menu), Account, and approving an app or a CLI sign-in.
- **Mobile:** Sign in, the drawer, Needs you, Approve, Floor, Board, Work item, Intake.

Buttons for features that are not built yet appear on these screens. They are described in the item that builds them, never as working features:
- Watch live;
- Trace and Teardown on an item;
- Always allow on an approval;
- trying a trigger on a past event;
- a model check in a trigger's "Only if";
- Add a build agent from the constraint;
- Bake off this agent.

## Accessibility and checks

- WCAG 2.2 AA in both themes. Text is at least 4.5:1 (3:1 at 24px and up), and controls are at least 3:1. Colours that must be told apart also differ in lightness.
- Use real elements: `<button>`, `<a href>`, `<input>` with `<label>`, and `role="switch"` on toggles. Focus rings are 2px cobalt with a 2px offset. Every drag has a Move menu, and every keyboard shortcut is shown where its action is.
- Lay out phone-first and check 375, 768 and 1440px. A wide table scrolls in its own box, and the page never scrolls sideways.
- Run the ui-ux-pro-max pre-delivery checklist on every visual change.

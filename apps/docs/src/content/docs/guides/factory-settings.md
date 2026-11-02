---
title: Factory settings
description: Set a factory's repository, box size, tools, budgets, flow targets and flow choices from its Settings page, and publish them as a new version.
---

You end with the factory's repository, runtime, budgets and flow set as you want them, published as a new version. The Settings page edits the same definition as the Definition editor, for the fields below.

## Before you start

- You need `definition:edit` on the factory to change anything. Without it, the groups show the running version read-only and there is no Save changes button.
- Changes are saved as you make them, into the factory's open draft. Nothing runs on them until you publish.

## How Save changes publishes

Save changes is in the top bar, with a line beside it that says which version it publishes, for example "Saving publishes v22". While it publishes the button reads "Saving…", and a check shows once the version is out. Save changes:

1. saves any change still waiting,
2. publishes the open draft as that version, and
3. closes the draft.

The draft is shared with the Definition editor. Saving publishes everything in it, including edits you made there, not only the fields on this page. If goald refuses the publish, the page shows its reason and the draft stays open.

## Sections

Settings lists its sections in this order. Every section is open; scroll the page or use the "On this page" list at the right, which marks the section in view.

### General

The factory's name, its purpose from the definition, and its Badge: the colour and letter the sidebar shows. All three are read-only here.

### Repository

The page shows the repository URL with a chip, "Connected with" and the connection's name, and a Change button. Change opens the two fields below in place; edits still save into the draft. With no repository set, the fields show directly.

| Field | Sets | Notes |
|---|---|---|
| Repository URL | `runtime.repository.url` | An https URL cloned into each run's box before the agent starts. Empty means no checkout and no pull requests. |
| Repository connection | `runtime.repository.connection` | The organization connection git signs in with. Choose "Public, no connection" for a public repository. |
| Base branch | none yet | Shows "Not available yet". The factory definition has no field for it. |

### Runtime

| Field | Sets | Notes |
|---|---|---|
| Size of each run's box | `runtime.size` | `small` (1 CPU, 2 GiB), `medium` (2 CPU, 4 GiB) or `large` (4 CPU, 8 GiB). Medium when unset. |
| Tools | `runtime.tools` | mise tool specs, such as `go`, `go@1.26.10` or `npm:@acme/cli@2`. Each run installs them after the repository's own `mise.toml`. Press Enter to add one. A value that is not a tool spec is refused. |

### Budgets and targets

| Field | Sets | Notes |
|---|---|---|
| Each day | `runtime.budget.dailyUsd` | US dollars. The schema accepts it, but the engine does not enforce a daily cap yet. See [WIP and budget](/docs/concepts/wip-and-budget/). |
| Each week | `runtime.budget.weeklyUsd` | US dollars. When the week's spend reaches it, agent work stops as set under "When a budget runs out". |
| Each item | `runtime.budget.perItemUsd` | US dollars. The cap for one run. |
| Turns per item | `runtime.budget.maxTurnsPerTask` | The most turns one task may take. |
| Items a day | `board.flow.targets.throughputPerDay` | A target. The Floor draws it on the throughput chart. |
| Hours lead time | `board.flow.targets.leadTimeHours` | A target: hours from arriving to done. The Floor compares each week with it. |
| Expedite lead time, minutes | `board.flow.targets.expediteLeadTimeMinutes` | A target for expedite items. The engine does not act on it. |

Leave a field empty to remove its value. A cap with no value is no cap.

### Flow

| Choice | Sets | Notes |
|---|---|---|
| When a budget runs out: Finish what started | `runtime.budget.onExhausted: drain` | The default. Runs in progress finish; nothing new starts. |
| When a budget runs out: Stop at once | `runtime.budget.onExhausted: stop` | Every agent stops at once. |
| New work enters: Pull when the constraint has room | `board.flow.release: drum-buffer-rope` | Saved now, not enforced yet: the engine releases work as it arrives. |
| New work enters: As soon as it arrives | `board.flow.release: push` | The default. |
| Items that touch the same files: Take turns | `board.flow.assemblies.overlap: serialize` | The default. An item whose files overlap an item in flight waits for it. |
| Items that touch the same files: Work side by side | `board.flow.assemblies.overlap: allow` | Items with overlapping files build in parallel. |
| Expedite lane | `board.flow.expedite.types` | Work types whose items are expedite items. Press Enter to add one. A value must be a work type the factory has. |

Each choice keeps the other settings of its object, such as `board.flow.assemblies.freshBase`.

## Related

- [Change a factory definition](/docs/guides/change-a-factory-definition/) edits the same definition in files, with the CLI.
- [WIP and budget](/docs/concepts/wip-and-budget/) explains which caps the engine enforces.
- [Expedite and priority](/docs/concepts/expedite-and-priority/) explains the expedite lane.
- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the release setting and what it does not yet do.

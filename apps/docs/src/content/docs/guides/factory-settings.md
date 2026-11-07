---
title: Factory settings
description: Set a factory's repository and its token, model key, environments, budgets, flow targets and flow choices from its Settings page, and publish them as a new version.
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

The factory's title, its name (the one used in URLs and the CLI), its purpose from the definition, and its Badge: the colour and letter the sidebar shows. All four are read-only here.

### Code

The section lists the factory's repositories. Each row shows the URL, its access ("Read only" or "Read and write"), the name of the secret that holds its token, and the last result of testing that token. A factory on the `runtime.repository` shorthand shows it as one "Read and write" row. A factory with neither shows "No code. Agents work through their tools."

Test asks the host's API whether the token can do what the access asks, without running git. A read entry says "Can read"; a write entry says "Can read and push". A read entry whose token can also push carries a warning to use a read-only token. A failed check is shown as the answer, in the "sent back" tone, and "couldn't test" means the host did not reply. Goal keeps the result for the factory, so it is still there when you return. Test shows only to people who may edit the definition; everyone with access to the factory sees the kept results.

| Field | Sets | Notes |
|---|---|---|
| Repositories | `runtime.repositories` | Add a repository opens the entries in place: name, URL, access, token and whether items without a repository use it. Remove takes a row out. Both edit the draft; Save changes publishes it. |
| Agents that write | each agent's `code` | "All agents" unless an agent is set to read; otherwise the agents that write. Change an agent's access on the Agents page. |
| Branch names | none | Read only: `goal/<number>-<slug>`, in every repository an item changes. |
| Default branch | none yet | Shows "Not available yet". The API does not return the host's default branch. |

Creating a secret or opening a webhook needs an owner or admin; the ⓘ beside Repositories says so. Ask one if the token you need is not in the secret list. See [Connect code to a factory](../connect-code/) and [Access and secrets](../../concepts/access-and-secrets/).

### Runtime

A line at the top says "Box size, CLIs and what agents reach live in environments." The word "environments" links to the Definition's Environments section, where each environment's size, image, tools and setup are set. Settings no longer edits them.

| Field | Sets | Notes |
|---|---|---|
| Model key | `runtime.modelKey` | The secret that gives every run its model. It shows here with a Change link to Secrets; set it in the YAML view. |
| Environments | `environments/<name>.yaml` | One chip per environment with the number of agents that use it, and an Edit link to the Definition's Environments section. |

### Budgets and targets

| Field | Sets | Notes |
|---|---|---|
| Each week | `runtime.budget.weeklyUsd` | US dollars. The budget Goal enforces: when the last 7 days' spend reaches it, no new agent work starts, as set under "When a budget runs out". A day's share is about this figure ÷ 7. |
| Each run | `runtime.budget.perItemUsd` | US dollars. The cap for one agent run, not for a whole item: an item that passes through several agent stations can spend it at each. |
| Turns per run | `runtime.budget.maxTurnsPerTask` | The most turns one task may take. |
| Items a day | `board.flow.targets.throughputPerDay` | A target. The Floor draws it on the throughput chart. |
| Hours lead time | `board.flow.targets.leadTimeHours` | A target: hours from arriving to done. The Floor compares each week with it. |
| Expedite lead time, minutes | `board.flow.targets.expediteLeadTimeMinutes` | A target for expedite items. The engine does not act on it. |

Leave a field empty to remove its value. A cap with no value is no cap.

### Flow

| Choice | Sets | Notes |
|---|---|---|
| When a budget runs out: Finish what started | `runtime.budget.onExhausted: drain` | The default. Runs in progress finish; nothing new starts until the last 7 days' spend is under the budget. |
| When a budget runs out: Stop at once | `runtime.budget.onExhausted: stop` | Every agent stops at once. |
| New work enters: Pull when the constraint has room | `board.flow.release: drum-buffer-rope` | Saved now, not enforced yet: the engine releases work as it arrives. |
| New work enters: As soon as it arrives | `board.flow.release: push` | The default. |
| Items that touch the same files: Take turns | `board.flow.assemblies.overlap: serialize` | The default. An item whose files overlap an item in flight waits for it. |
| Items that touch the same files: Work side by side | `board.flow.assemblies.overlap: allow` | Items with overlapping files build in parallel. |
| Expedite lane | `board.flow.expedite.types` | Work types whose items are expedite items. Press Enter to add one. A value must be a work type the factory has. |

Each choice keeps the other settings of its object, such as `board.flow.assemblies.freshBase`.

### Jonah

Jonah reviews the line once a day and proposes changes for you to approve. The section sets `learning.jonah`. Each field has an ⓘ tip beside it.

| Choice | Sets | Notes |
|---|---|---|
| Jonah reviews this line | `learning.jonah.enabled` | On by default. Off, Jonah reviews nothing. |
| Model | `learning.jonah.model` | The model of the daily review. The line's model when left at Same as the line. |
| Budget, % of the line's spend | `learning.jonah.share` | From 0 to 100, as a share of what the line spent in the last 7 days. 5 by default. It never stops the line. |
| Proposes after, items | `learning.jonah.proposeAfter` | From 1 to 50. How many items with the same cause Jonah needs before it makes a proposal. 3 by default. |

A value out of range shows its message and is not saved. Like every group, Jonah saves into the draft and Save changes publishes it. The fields are read-only without `definition:edit`.
### Notifications

A read-only Slack row. It shows "Sent with the secret `slack-bot`." when the organization has chosen a Slack secret, and "Slack is off" when it has not. Slack sends direct messages only, so a factory has no channel of its own. The secret is organization-wide: a person who holds `org:manage` sees a "Change in organization settings" link to pick or turn it off there. Without that permission the row has no link. If the setting cannot be read, the row says "Couldn’t load the Slack setting."

## Related

- [Change a factory definition](/docs/guides/change-a-factory-definition/) edits the same definition in files, with the CLI.
- [WIP and budget](/docs/concepts/wip-and-budget/) explains which caps the engine enforces.
- [Expedite and priority](/docs/concepts/expedite-and-priority/) explains the expedite lane.
- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the release setting and what it does not yet do.

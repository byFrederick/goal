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

Settings lists nine sections in this order: General, Code, Runtime, Budgets and targets, Flow, Jonah, Notifications, People with access and Danger zone. Every section is open; scroll the page or use the "On this page" list at the right, which marks the section in view. The Danger zone shows only to someone who may pause, archive or delete the factory.

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

Under each repository's test summary, a second line says whether the token can read CI. It shows "The token can read CI." or "Can’t read CI. <reason>", where the reason is the host's. The second is a warning only when a station of the line sets `waitFor`, because goald then waits for CI on that station; otherwise it is plain muted text. Its ⓘ gives the token step. A result kept from before goald checked CI shows no CI line. The scopes CI read needs are in [Connect code to a factory](/docs/guides/connect-code/).

Creating a secret or opening a webhook needs an owner or admin; the ⓘ beside Repositories says so. Ask one if the token you need is not in the secret list. See [Connect code to a factory](/docs/guides/connect-code/) and [Access and secrets](/docs/concepts/access-and-secrets/).

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

### People with access

The section lists who can see this factory: the organization's owners and admins, and the members who hold a factory role on it. Each row shows the person's name, a role chip and a line saying what the role allows:

| Chip | Line |
|---|---|
| Owner | Organization owner |
| Admin | Organization admin |
| Editor | Edits the definition and creates items |
| Approver | Approves and decides |
| Viewer | Sees the floor, creates items |

Owners and admins hold every factory role through their organization role, so their rows have no change control. The list is empty only when nobody has access: it then says "No people with access yet."

With `member:manage` (owners and admins), you can:

- Add a member: "Add people" opens a dialog with a Person list and a Role list (Viewer, Approver or Editor). Add gives them that role. When every member already has a role here, the dialog says "Everyone in the organization already has a role here."
- Change a role: "Change role" on a member's row opens a dialog with the Role list and Save. Changes apply at once.
- Remove a member: "Remove from factory" in the same dialog takes the role away, and the member no longer sees the factory.

Without `member:manage`, "Add people" is disabled, with the tip "Only owners and admins change who has access." and rows have no "Change role". See [Access and secrets](/docs/concepts/access-and-secrets/) for the roles.

### Danger zone

The Danger zone holds the actions that change whether the factory works at all. They apply when you press them, not with Save changes. Each row shows only to someone who holds its permission; with none, the whole section is hidden.

| Row | Button | Needs | What it does |
|---|---|---|---|
| Pause the factory | Pause, then Resume | `factory:manage` on the factory | Running items finish their run; nothing new starts until you resume. While paused, the row reads "It takes no work. Resume starts it again." Hidden on an archived factory. |
| Archive | Archive | `factory:archive` (organization) | Hides the factory from the sidebar and the Factories home. It stops taking work and stays readable, with its items, versions and history. |
| Restore | Restore | `factory:archive` (organization) | Replaces Archive on an archived factory. Lists it again in the sidebar and the Factories home. It stays paused until you resume it. |
| Delete | Delete | `factory:delete` (organization) | Removes the factory's items, runs, events and versions for good. The audit history stays. |

Archive opens "Archive <factory>?" with the button "Archive factory". A factory that is running cannot be archived, so the dialog reads "Pause first": it pauses the factory, then archives it. Pause it yourself first to get "Archive factory". If goald refuses, for example because items are being worked, the dialog shows its reason.

Delete is disabled, with "Archive the factory first", until the factory is archived. Then it opens "Delete <factory>?" and asks you to type the factory's name before "Delete factory" is enabled. After it, you land on the organization's home.

## Related

- [Change a factory definition](/docs/guides/change-a-factory-definition/) edits the same definition in files, with the CLI.
- [WIP and budget](/docs/concepts/wip-and-budget/) explains which caps the engine enforces.
- [Expedite and priority](/docs/concepts/expedite-and-priority/) explains the expedite lane.
- [Stations and the flow](/docs/concepts/stations-and-flow/) explains the release setting and what it does not yet do.

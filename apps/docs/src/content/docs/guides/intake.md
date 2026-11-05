---
title: Review what enters a factory
description: See where a factory's items came from, what is waiting to start, and which triggers create work, and try a trigger on a past event.
---

You end knowing where each of a factory's items came from and which triggers and webhooks create work, and you can add, switch off or try a trigger. Every item, from whatever source, enters the factory through the same door: it is checked against the factory's work types, lanes and stations, and it records where it came from.

## Where items come from

Each item records an origin and who filed it:

| Origin | What filed it |
| --- | --- |
| `person` | A person, on the web, from the CLI or with an API key. |
| `plan` | An assembly's plan, as a part. |
| `trigger` | A schedule, a webhook delivery or a check. |
| `follow-up` | An agent's follow-up that asked for an item of its own, or the factory's triage item. |

The factory's Intake page at `/o/<org>/f/<factory>/intake` shows the same picture in five sections, each with an ⓘ that says what it holds: Coming up, Where items came from, Follow-ups to triage, Triggers and Webhooks open to the factory. The API answers: `GET /v1/orgs/{org}/factories/{factory}/intake` counts the items created in a window of `7d`, `30d` (the default) or `90d` by origin, lists the items that have not started (`coming_up`, at most 50, with their total), the open triage item, and the organization's webhooks that are open to the factory. An item filed before origins were recorded counts as `person`.

The CLI prints the same picture:

```sh
goal intake acme-line --window 7d
```

It lists the items that have not started (when each may start, or the needs it waits for, and where it came from), the factory's items by origin over the window (default `30d`), the follow-ups waiting for triage and the items they join from, the triggers with checks marked, and the webhooks open to the factory, each receiving, with no trigger, or closed. `--json` prints it as JSON.

## Items that have not started

An item with a start time, or one waiting for other items, waits at the line's first station until it may start. On Intake, Coming up lists them soonest first, with where each came from. **Start now** clears the start time and the row leaves the list. A row that waits only for other items has no Start now, since those items still hold it. The API lists these as `coming_up`: items with a start first, soonest first, then items that wait only for open needs, oldest first. To change a start or start an item now, read [Start an item later](../submit-and-follow-items/#start-an-item-later).

## Triggers

A trigger is a rule in the factory definition's `board.triggers` that creates an item from a schedule, a webhook delivery or a check. The Intake page lists each trigger with whether it is on, when it fires, what it creates, the items it created on each of the last 7 days and why it may not be creating any. From the CLI:

```sh
goal triggers list acme-line
goal triggers show acme-line red-main
```

`show` prints the trigger, what its items did in the last 7 days and its recent events: created, merged, skipped, missed or checked. A schedule that missed instants by more than 5 minutes does not fire late; it records one `missed` event per gap, with how many instants the gap covers.

A trigger matches an event with `on`, narrows it with `filter` and `only`, and creates the item with `create`. Event text in a title is quoted and cut to 200 characters. Every key is in the [definition reference](/docs/reference/definition/).

An event trigger creates at most 10 items an hour unless `limits.perHour` says otherwise. A schedule has no hourly limit.

### Edit a trigger in the web app

A trigger's page opens from the Intake page. When picks the source (a schedule, GitHub or one organization webhook) and the event. For a GitHub event the page offers only the filters that event supplies: `action`, `conclusion`, `branch`, `repo` (one or a list), `label`, `paths` (for a push), `authors` and `skipBots`. A filter the event cannot supply is refused when you save, and the page shows the refusal under the field. More filters also takes payload matches: a dot path into the delivery's body and the values it may have, all of which must match (`filter.match`). Limits sets `perDay` and `perHour`.

A file that still has the old keys `github.check_run.failed`, `github.issues.labeled` or `webhook.<name>` opens in the current words (`github.check_run` with `conclusion: failure`, and so on) and keeps its lines until you change `on` or `filter`. The page no longer offers the model check. A file that still has `only.modelAgrees` keeps the key when you save another change, and the definition warns that it is ignored.

### Untrusted items

An item made from a webhook delivery is untrusted: its brief carries the event's text inside `<untrusted-event-data>`, which agents treat as data and never as instructions, and a person merges what the item builds. Follow-ups from an untrusted item wait instead of being delivered or filed. A trigger's `create.trusted: true` lifts this for deliveries you vouch for; publishing refuses it for the `gitlab` and `token` senders and for GitHub without `filter.authors`.

## Suggestions

The API and `goal intake` return a definition's disabled triggers as `suggestions`.

On the web, a trigger that is off in the definition appears in the Triggers table as a suggestion; turning it on publishes a new version. A trigger that runs a check shows **Check** and the agent that runs it. The Webhooks section shows each open webhook as Receiving, No trigger yet or Closed, with **Manage** opening the organization's webhooks page.

The New trigger dialog lists the same suggestions under "Or turn on a suggestion from the factory's definition", each with the station it creates items at. **Turn on** publishes a new version with the trigger on, and the row leaves the list. The block is absent when the definition has no disabled triggers. The onboarding Intake step offers the same suggestions as its starter triggers.

New trigger's **Built in** group lists Schedule and **Check with an agent**. A second group, also called Check with an agent, lists each tool the factory's agents have on. Choose one and **Continue** to open a blank check on a free name, such as `new-check`, with the factory's first agent that has the tool. Choose the agent, write the brief and save. Webhook and Cloudflare have no Continue; set up a webhook from the organization's Webhooks page.

## Try a trigger on a past event

Try shows what a saved trigger would have done with one of its 20 newest events, without creating an item, recording an event or changing a limit. It reports each step (the filter, the checks and the limits) and why it stopped, and the item it would create. It works for a schedule's firings and for a webhook's deliveries while the webhook still keeps their body (30 days).

On the trigger's page, choose Try with a delivery (Try with a past event for a schedule) and pick an event. From the CLI, take the event id from `goal triggers show`:

```sh
goal triggers try acme-line red-main --event <id>
```

The CLI prints the steps; `--json` prints them as JSON. A missed event, or a delivery whose body is no longer kept, cannot be tried. Try for a trigger with a check runs the check instead; see [Ask an agent what to file](../checks/).

## Next

- To receive events from outside Goal, read [Receive events with organization webhooks](../webhooks/).
- To look at a source that cannot send webhooks, read [Ask an agent what to file](../checks/).
- To see what agents leave for later, read [Follow-ups](../follow-ups/).
